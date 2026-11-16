package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// Pulsanti volume del cabinato (bilanciere sullo zero delay).

const vbDetectPy = `
import os, struct, select, fcntl, glob, sys, time
EVIOCGNAME = 0x81004506
EVIOCGPHYS = 0x81004507
fmt = 'llHHi'; sz = struct.calcsize(fmt)
fds = {}
for p in glob.glob('/dev/input/event*'):
    n = os.path.basename(p)
    try: fd = os.open(p, os.O_RDONLY | os.O_NONBLOCK)
    except Exception: continue
    nm = n; ph = ''
    try:
        name = bytearray(256); fcntl.ioctl(fd, EVIOCGNAME, name); nm = name.split(b'\0',1)[0].decode(errors='ignore') or n
    except Exception: pass
    try:
        phys = bytearray(256); fcntl.ioctl(fd, EVIOCGPHYS, phys); ph = phys.split(b'\0',1)[0].decode(errors='ignore')
    except Exception: pass
    fds[fd] = (n, nm, ph)
if not fds: print('{"error":"nessun dispositivo in /dev/input"}'); sys.exit(0)
end = time.time() + float(sys.argv[1])
while time.time() < end:
    r, _, _ = select.select(list(fds), [], [], 0.2)
    for fd in r:
        try: data = os.read(fd, sz * 64)
        except Exception: continue
        for i in range(0, len(data) - sz + 1, sz):
            _, _, t, code, val = struct.unpack(fmt, data[i:i+sz])
            if t == 1 and val == 1 and code >= 0x100:
                print('{"device":"%s","name":"%s","phys":"%s","code":%d}' % (fds[fd][0], fds[fd][1].replace('"',''), fds[fd][2].replace('"',''), code)); sys.exit(0)
print('{}')
`

const vbService = `#!/bin/bash
case "$1" in
  start) [ -f /userdata/system/companion/volume-buttons.json ] && setsid nohup python3 /userdata/system/companion/volume-buttons.py >/dev/null 2>&1 </dev/null & ;;
  stop) pkill -f "[v]olume-buttons.py" ;;
  restart) "$0" stop; sleep 1; "$0" start ;;
  status) pgrep -f "[v]olume-buttons.py" >/dev/null && echo running || echo stopped ;;
esac
exit 0
`

func (s *Server) hVolbtn(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout(`echo "@@conf"; cat /userdata/system/companion/volume-buttons.json 2>/dev/null; echo; echo "@@run"; pgrep -f "[v]olume-buttons.py" >/dev/null && echo yes; echo "@@en"; batocera-settings-get system.services 2>/dev/null; echo "@@vol"; batocera-audio getSystemVolume 2>/dev/null; echo; echo "@@popup"; grep -o 'name="VolumePopup" value="[^"]*"' `+esSettings+` 2>/dev/null | sed 's/.*value="//;s/"$//'`, 30*time.Second)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	out := map[string]any{"installed": strings.TrimSpace(sec["conf"]) != "", "running": strings.TrimSpace(sec["run"]) == "yes", "enabled": strings.Contains(sec["en"], "volume_buttons"), "volume": strings.TrimSpace(sec["vol"]), "popup": strings.TrimSpace(sec["popup"]) != "false"}
	if c := strings.TrimSpace(sec["conf"]); c != "" {
		var m map[string]any
		if json.Unmarshal([]byte(c), &m) == nil {
			out["config"] = m
		}
	}
	return out, nil
}

func (s *Server) hVolbtnDetect(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(vbDetectPy)+" 15", 40*time.Second)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &out); err != nil || out == nil {
		return nil, errors.New("nessuna risposta dal cabinato")
	}
	if e, ok := out["error"]; ok {
		return nil, fmt.Errorf("%v", e)
	}
	if out["code"] == nil {
		return nil, errors.New("nessun pulsante premuto entro 15 secondi")
	}
	return out, nil
}

func (s *Server) hVolbtnInstall(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Up, Down, Step, Max, Boot int
		Interval                  float64
		Popup                     bool
		Name, Phys                string
	}
	var raw struct {
		Up       int     `json:"up"`
		Down     int     `json:"down"`
		Step     int     `json:"step"`
		Max      int     `json:"max"`
		Boot     int     `json:"boot"`
		Interval float64 `json:"interval"`
		Popup    bool    `json:"popup"`
		Name     string  `json:"name"`
		Phys     string  `json:"phys"` // porta USB (EVIOCGPHYS): se impostata, ascolta solo quel controller
	}
	if err := readJSON(r, &raw); err != nil {
		return nil, err
	}
	req.Up, req.Down, req.Step, req.Max, req.Boot, req.Interval, req.Popup, req.Name, req.Phys = raw.Up, raw.Down, raw.Step, raw.Max, raw.Boot, raw.Interval, raw.Popup, raw.Name, raw.Phys
	if req.Up == 0 || req.Down == 0 {
		return nil, errors.New("rileva prima i due pulsanti")
	}
	script, err := s.asset("volume-buttons.py")
	if err != nil {
		return nil, err
	}
	j := s.jobs.Start("Pulsanti volume", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Installo il servizio")
		conf, _ := json.Marshal(map[string]any{"up": req.Up, "down": req.Down, "step": req.Step, "max": req.Max, "boot": req.Boot, "interval": req.Interval, "name": req.Name, "phys": req.Phys, "notify": false})
		if err := s.ssh.UploadBytes(compDir+"/volume-buttons.json", conf, 0o644); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes(compDir+"/volume-buttons.py", script, 0o755); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes("/userdata/system/services/volume_buttons", []byte(vbService), 0o755); err != nil {
			return nil, err
		}
		pop := "true"
		if !req.Popup {
			pop = "false"
		}
		cmd := "/userdata/system/services/volume_buttons stop; batocera-services enable volume_buttons >/dev/null 2>&1; batocera-services start volume_buttons >/dev/null 2>&1 </dev/null; sleep 1; /userdata/system/services/volume_buttons status"
		cur, _ := s.esSettingsGet([]string{"VolumePopup"})
		if (cur["VolumePopup"] != "false") != req.Popup {
			cmd += "; " + esSettingsSetCmd("VolumePopup", &pop, true)
			if g, _ := s.es.RunningGame(); g != nil {
				j.Logf("· overlay volume di ES: c'è un gioco in corso, lo cambio al prossimo avvio (rilancia \"Installa\" a gioco chiuso)")
			} else {
				cmd += "; batocera-es-swissknife --restart >/dev/null 2>&1 &"
				j.Logf("· barra del volume di EmulationStation: %s (ES riavviato)", map[bool]string{true: "mostrata", false: "nascosta"}[req.Popup])
			}
		}
		res, err := s.ssh.RunTimeout(cmd, time.Minute)
		if err != nil {
			return nil, err
		}
		j.Logf("servizio: %s", strings.TrimSpace(lastLine(res.Stdout)))
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) hVolbtnRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	_, err := s.ssh.RunTimeout("batocera-services stop volume_buttons 2>/dev/null; batocera-services disable volume_buttons 2>/dev/null; pkill -f '[v]olume-buttons.py'; rm -f /userdata/system/services/volume_buttons "+compDir+"/volume-buttons.py "+compDir+"/volume-buttons.json; echo ok", time.Minute)
	return nil, err
}
