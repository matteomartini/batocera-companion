package server

import (
	"bufio"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
)

// Pagina "Plancia": comandi fisici con le scritte della plancia, illuminati in tempo reale.

// Le scritte sono per ruolo di EmulationStation (y x pageup / b a pagedown ...): così la plancia
// segue la configurazione del controller, qualunque porta dell'encoder usi ogni pulsante.
var planciaDefaultLabels = map[string]string{
	"y": "JAB", "x": "STRONG", "pageup": "FIERCE", "b": "SHORT", "a": "FORWARD", "pagedown": "ROUNDHOUSE",
	"select": "SELECT", "start": "START", "hotkey": "HOTKEY", "l2": "L2", "r2": "R2", "l3": "L3", "r3": "R3",
}

// Lettore eventi evdev in python puro: calcola l'indice SDL dei pulsanti enumerando i bit EV_KEY
// nell'ordine usato da SDL (da BTN_JOYSTICK in poi).
const planciaReader = `
import os, sys, struct, select, glob, fcntl, array, subprocess
EVIOCGBIT_KEY = 0x80000000 | ((96 & 0x3fff) << 16) | (ord('E') << 8) | (0x20 + 1)
def keybits(fd):
    buf = array.array('B', [0]*96)
    try: fcntl.ioctl(fd, EVIOCGBIT_KEY, buf, True)
    except OSError: return []
    codes = [i for i in range(len(buf)*8) if buf[i//8] & (1 << (i % 8))]
    hi = [c for c in codes if c >= 0x120]; lo = [c for c in codes if c < 0x120]
    return hi + lo
devs = []
for e in sorted(glob.glob('/sys/class/input/event*'), key=lambda p: int(p.split('event')[-1])):
    n = os.path.basename(e)
    try: p = subprocess.run(['udevadm', 'info', '-q', 'property', '-p', e], capture_output=True, text=True, timeout=5).stdout
    except Exception: p = ''
    if 'ID_INPUT_JOYSTICK=1' not in p: continue
    try: name = open(e + '/device/name').read().strip()
    except Exception: name = n
    try:
        fd = os.open('/dev/input/' + n, os.O_RDONLY | os.O_NONBLOCK)
    except OSError as ex:
        print('err|%s|%s' % (n, ex), flush=True); continue
    kb = keybits(fd)
    devs.append((fd, n, name, {c: i for i, c in enumerate(kb)}))
    print('dev|%d|%s|%s|%d' % (len(devs)-1, n, name, len(kb)), flush=True)
print('ready|%d' % len(devs), flush=True)
if not devs: sys.exit(0)
fds = {d[0]: d for d in devs}
SZ = struct.calcsize('llHHi')
while True:
    r, _, _ = select.select(list(fds), [], [], 30)
    if not r:
        print('ping', flush=True); continue
    for fd in r:
        try: data = os.read(fd, SZ * 64)
        except OSError:
            print('gone|%s' % fds[fd][1], flush=True); del fds[fd]; continue
        for i in range(0, len(data) - SZ + 1, SZ):
            _, _, t, c, v = struct.unpack('llHHi', data[i:i+SZ])
            if t == 1:
                print('b|%d|%d|%d' % (devs.index(fds[fd]), fds[fd][3].get(c, -1), v), flush=True)
            elif t == 3:
                print('a|%d|%d|%d' % (devs.index(fds[fd]), c, v), flush=True)
    if not fds: break
`

// planciaESMap: per nome dispositivo, "button:<id>" / "hat:<id>:<v>" / "axis:<id>:<v>" → nome ES.
func (s *Server) planciaESMap() map[string]map[string]string {
	out := map[string]map[string]string{}
	res, err := s.ssh.RunTimeout("cat /userdata/system/configs/emulationstation/es_input.cfg 2>/dev/null", 20*time.Second)
	if err != nil {
		return out
	}
	for _, ic := range cab.ParseESInput(res.Stdout) {
		if ic.Type != "joystick" {
			continue
		}
		mp := map[string]string{}
		for name, v := range ic.Inputs {
			if old, dup := mp[v]; dup {
				if name == "hotkey" {
					mp[v] = old + "+hotkey"
				} else if old == "hotkey" {
					mp[v] = name + "+hotkey"
				}
				continue
			}
			mp[v] = name
		}
		out[strings.TrimSpace(ic.DeviceName)] = mp
	}
	return out
}

func (s *Server) hPlancia(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var req struct {
			Labels map[string]string `json:"labels"`
			Swap   *bool             `json:"swap"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if req.Labels != nil {
			clean := map[string]string{}
			for k, v := range req.Labels {
				if planciaLabelKey(k) && strings.TrimSpace(v) != "" {
					clean[k] = strings.TrimSpace(v)
				}
			}
			s.cfg.PlanciaLabels = clean
		}
		if req.Swap != nil {
			s.cfg.PlanciaSwap = *req.Swap
		}
		return nil, config.Save(s.cfg)
	}
	res, err := s.ssh.RunTimeout(`for e in /sys/class/input/event*; do p=$(udevadm info -q property -p $e 2>/dev/null); if echo "$p" | grep -q 'ID_INPUT_JOYSTICK=1'; then echo "$(basename $e)|$(cat $e/device/name 2>/dev/null)|$(cat $e/device/phys 2>/dev/null)|$(cat $e/device/id/vendor 2>/dev/null):$(cat $e/device/id/product 2>/dev/null)"; fi; done`, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var devs []map[string]any
	for _, l := range cab.Lines(res.Stdout) {
		f := strings.Split(l, "|")
		if len(f) < 4 {
			continue
		}
		devs = append(devs, map[string]any{"event": f[0], "name": strings.TrimSpace(f[1]), "phys": f[2], "id": f[3]})
	}
	sort.Slice(devs, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(devs[i]["event"].(string), "event"))
		b, _ := strconv.Atoi(strings.TrimPrefix(devs[j]["event"].(string), "event"))
		return a < b
	})
	if devs == nil {
		devs = []map[string]any{}
	}
	labels := map[string]string{}
	for k, v := range planciaDefaultLabels {
		labels[k] = v
	}
	for k, v := range s.cfg.PlanciaLabels {
		labels[k] = v
	}
	return map[string]any{"devices": devs, "labels": labels, "defaults": planciaDefaultLabels, "es": s.planciaESMap(), "swap": s.cfg.PlanciaSwap}, nil
}

func (s *Server) hPlanciaEvents(w http.ResponseWriter, r *http.Request) {
	rc, err := s.ssh.StreamSession("python3 -u -c " + cab.ShellQuote(planciaReader) + " 2>&1")
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	sc := bufio.NewScanner(rc)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

// planciaLabelKey: ruolo ES (lettere) oppure, per i pulsanti non configurati in ES, l'indice.
func planciaLabelKey(k string) bool {
	if _, err := strconv.Atoi(k); err == nil {
		return true
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '+' {
			return false
		}
	}
	return k != ""
}
