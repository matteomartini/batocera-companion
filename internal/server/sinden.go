package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
)

// Lightgun Sinden: rilevamento e impostazioni controllers.guns.* di batocera.conf.
var gunKeys = map[string][]string{
	"borderssize":  {"thin", "medium", "big"},
	"bordersmode":  {"auto", "normal", "hidden", "force"},
	"borderscolor": {"white", "red", "green", "blue"},
	"recoil":       {"none", "gun", "gun-quiet", "machinegun", "machinegun-quiet"},
	"bordersratio": {"", "4:3"},
}

const gunStatusScript = `
echo "@@guns"; for e in /sys/class/input/event*; do n=$(cat $e/device/name 2>/dev/null); p=$(udevadm info -q property -p $e 2>/dev/null); if echo "$p" | grep -q 'ID_INPUT_GUN=1'; then echo "$(basename $e)|$n|$(echo "$p" | grep -c ID_INPUT_GUN_NEED_BORDERS=1)"; fi; done
echo "@@sindenraw"; grep -il 'sinden' /sys/class/input/event*/device/name 2>/dev/null | wc -l; ls /dev/video* 2>/dev/null | wc -l
echo "@@usb"; lsusb 2>/dev/null | grep -i 'sinden\|16c0:0f01\|16c0:0f02\|16c0:0f38\|16c0:0f39'
echo "@@daemon"; pgrep -af 'LightgunMono|virtual-sindenlightgun' 2>/dev/null | grep -v pgrep | wc -l
echo "@@conf"; for k in borderssize bordersmode borderscolor recoil bordersratio; do echo "$k=$(batocera-settings-get controllers.guns.$k 2>/dev/null)"; done
echo "@@res"; batocera-settings-get global.videomode 2>/dev/null
echo "@@support"; ls /usr/bin/virtual-sindenlightgun-add /usr/share/sinden 2>/dev/null | head -2; ls /usr/lib/mono 2>/dev/null | head -1
`

func (s *Server) hSinden(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		res, err := s.ssh.RunTimeout(gunStatusScript, 60*time.Second)
		if err != nil {
			return nil, err
		}
		sec := cab.SplitSections(res.Stdout)
		var guns []map[string]any
		for _, l := range cab.Lines(sec["guns"]) {
			f := strings.Split(l, "|")
			if len(f) < 3 {
				continue
			}
			guns = append(guns, map[string]any{"dev": f[0], "name": f[1], "borders": strings.TrimSpace(f[2]) == "1", "sinden": strings.Contains(strings.ToLower(f[1]), "sinden")})
		}
		conf := map[string]string{}
		for _, l := range cab.Lines(sec["conf"]) {
			if k, v, ok := strings.Cut(l, "="); ok {
				conf[k] = strings.TrimSpace(v)
			}
		}
		raw := strings.Fields(strings.TrimSpace(sec["sindenraw"]))
		rawSinden := "0"
		if len(raw) >= 1 {
			rawSinden = raw[0]
		}
		return map[string]any{
			"guns": guns, "usb": strings.TrimSpace(sec["usb"]), "raw_sinden": rawSinden != "0",
			"daemon": strings.TrimSpace(sec["daemon"]) != "0", "conf": conf, "videomode": strings.TrimSpace(sec["res"]),
			"support": strings.TrimSpace(sec["support"]) != "", "keys": gunKeys,
		}, nil
	}
	var req struct {
		Conf map[string]string `json:"conf"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	var cmds []string
	for k, v := range req.Conf {
		allowed, ok := gunKeys[k]
		if !ok {
			return nil, fmt.Errorf("chiave %q non prevista", k)
		}
		valid := false
		for _, a := range allowed {
			if a == v {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("valore %q non valido per %s", v, k)
		}
		if v == "" || (k == "bordersmode" && v == "auto") || (k == "recoil" && v == "none") {
			cmds = append(cmds, "sed -i '/^controllers\\.guns\\."+k+"=/d' /userdata/system/batocera.conf")
		} else {
			cmds = append(cmds, "batocera-settings-set controllers.guns."+k+" "+cab.ShellQuote(v))
		}
	}
	if len(cmds) == 0 {
		return nil, nil
	}
	cmd := "mkdir -p " + backupDir + "; cp /userdata/system/batocera.conf " + backupDir + "/batocera.conf." + time.Now().Format("20060102-150405") + " 2>/dev/null; " + strings.Join(cmds, "; ")
	if _, err := s.ssh.RunTimeout(cmd, time.Minute); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.info = nil
	s.mu.Unlock()
	return map[string]any{"ok": true, "note": "le impostazioni valgono dal prossimo gioco avviato; il rinculo richiede di ricollegare la pistola"}, nil
}
