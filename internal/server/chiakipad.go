package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
)

// Mappatura tasti dedicata a Chiaki: il Companion salva quale tasto/joystick degli encoder fa
// cosa sulla PS5; Chiaki.sh avvia chiaki-pad.py che presenta un unico pad virtuale.

const chiakiPadCfg = "/userdata/system/companion/chiaki-pad.json"

// elenca gli encoder (dispositivi con pulsanti joystick) e rileva la prossima pressione/movimento
const cpDetectPy = `
import os, struct, select, fcntl, glob, sys, time, json
EVIOCGNAME = 0x81004506; EVIOCGPHYS = 0x81004507; EVIOCGBIT_KEY = 0x80604521
fmt = 'llHHi'; sz = struct.calcsize(fmt)
mode = sys.argv[1]; wait = float(sys.argv[2]) if len(sys.argv) > 2 else 0
fds = {}
for p in sorted(glob.glob('/dev/input/event*')):
    try: fd = os.open(p, os.O_RDONLY | os.O_NONBLOCK)
    except Exception: continue
    nm = ''; ph = ''
    try:
        b = bytearray(256); fcntl.ioctl(fd, EVIOCGNAME, b); nm = b.split(b'\0',1)[0].decode(errors='ignore')
    except Exception: pass
    try:
        b = bytearray(256); fcntl.ioctl(fd, EVIOCGPHYS, b); ph = b.split(b'\0',1)[0].decode(errors='ignore')
    except Exception: pass
    # solo dispositivi con pulsanti joystick (BTN_JOYSTICK 0x120 .. 0x13f)
    try:
        kb = bytearray(96); fcntl.ioctl(fd, EVIOCGBIT_KEY, kb)
        joy = any(kb[c // 8] & (1 << (c % 8)) for c in range(0x120, 0x140))
    except Exception: joy = False
    if not joy or 'Companion' in nm: os.close(fd); continue
    fds[fd] = (os.path.basename(p), nm, ph)
if mode == 'list':
    print(json.dumps([{"device": v[0], "name": v[1], "phys": v[2]} for v in fds.values()])); sys.exit(0)
if not fds: print('{"error":"nessun encoder trovato"}'); sys.exit(0)
# svuota eventi vecchi
for fd in fds:
    try: os.read(fd, sz * 256)
    except Exception: pass
center = {}
end = time.time() + wait
while time.time() < end:
    r, _, _ = select.select(list(fds), [], [], 0.2)
    for fd in r:
        try: data = os.read(fd, sz * 64)
        except Exception: continue
        for i in range(0, len(data) - sz + 1, sz):
            _, _, t, code, val = struct.unpack(fmt, data[i:i+sz])
            d = fds[fd]
            if mode == 'button' and t == 1 and val == 1 and 0x100 <= code < 0x300:
                print(json.dumps({"device": d[0], "name": d[1], "phys": d[2], "kind": "b", "code": code})); sys.exit(0)
            if mode == 'axis' and t == 3 and code < 16:
                c = center.setdefault((fd, code), val)
                if abs(val - c) > 60:
                    print(json.dumps({"device": d[0], "name": d[1], "phys": d[2], "kind": "a", "code": code & ~1})); sys.exit(0)
print('{}')
`

func (s *Server) hChiakiPad(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(cpDetectPy)+" list; echo @@; cat "+chiakiPadCfg+" 2>/dev/null", 30*time.Second)
	if err != nil {
		return nil, err
	}
	devPart, cfgPart, _ := strings.Cut(res.Stdout, "@@")
	var devs []map[string]string
	_ = json.Unmarshal([]byte(strings.TrimSpace(lastLine(strings.TrimSpace(devPart)))), &devs)
	var cfg struct {
		Map map[string]string `json:"map"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(cfgPart)), &cfg)
	if cfg.Map == nil {
		cfg.Map = map[string]string{}
	}
	return map[string]any{"devices": devs, "map": cfg.Map, "configured": len(cfg.Map) > 0}, nil
}

func (s *Server) hChiakiPadDetect(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Kind string `json:"kind"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	mode := "button"
	if req.Kind == "axis" {
		mode = "axis"
	}
	res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(cpDetectPy)+" "+mode+" 15", 40*time.Second)
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
		if mode == "axis" {
			return nil, errors.New("nessun joystick mosso entro 15 secondi")
		}
		return nil, errors.New("nessun pulsante premuto entro 15 secondi")
	}
	return out, nil
}

func (s *Server) hChiakiPadSave(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Map map[string]string `json:"map"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	for k, v := range req.Map {
		if v == "" {
			delete(req.Map, k)
		}
	}
	if len(req.Map) == 0 {
		if _, err := s.ssh.RunTimeout("rm -f "+chiakiPadCfg+"; pkill -f '[c]hiaki-pad.py'; true", 20*time.Second); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "removed": true}, nil
	}
	b, _ := json.MarshalIndent(map[string]any{"map": req.Map}, "", " ")
	if _, err := s.ssh.RunTimeout("mkdir -p /userdata/system/companion", 20*time.Second); err != nil {
		return nil, err
	}
	if err := s.ssh.UploadBytes(chiakiPadCfg, b, 0o644); err != nil {
		return nil, err
	}
	if err := s.writeChiakiLauncher(); err != nil {
		return nil, err
	}
	_, _ = s.ssh.RunTimeout("pkill -f '[c]hiaki-pad.py'; true", 20*time.Second)
	return map[string]any{"ok": true, "count": len(req.Map)}, nil
}
