package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
)

// Plancia → tastiera per gli emulatori Wine (Model 2 / Supermodel).

const wkService = `#!/bin/bash
case "$1" in
  start) setsid nohup python3 /userdata/system/companion/wine-keys.py >/dev/null 2>&1 </dev/null & ;;
  stop) pkill -f "[w]ine-keys.py" ;;
  restart) "$0" stop; sleep 1; "$0" start ;;
  status) pgrep -f "[w]ine-keys.py" >/dev/null && echo running || echo stopped ;;
esac
exit 0
`

func (s *Server) hWinekeys(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		res, err := s.ssh.RunTimeout("[ -f "+compDir+"/wine-keys.py ] && echo installed; pgrep -f \"[w]ine-keys.py\" >/dev/null && echo running; cat "+compDir+"/wine-keys.json 2>/dev/null", 30*time.Second)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"installed": strings.Contains(res.Stdout, "installed"), "running": strings.Contains(res.Stdout, "running")}
		if i := strings.Index(res.Stdout, "{"); i >= 0 {
			var m map[string]any
			if json.Unmarshal([]byte(res.Stdout[i:]), &m) == nil {
				out["swap"] = m["swap"]
				out["p1_phys"] = m["p1_phys"]
			}
		}
		return out, nil
	}
	var req struct {
		Action string `json:"action"`
		Swap   bool   `json:"swap"`
		P1Phys string `json:"p1_phys"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "detect":
		b, err := s.asset("wkdetect.py")
		if err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes("/tmp/companion_wkdetect.py", b, 0o755); err != nil {
			return nil, err
		}
		res, err := s.ssh.RunTimeout("python3 /tmp/companion_wkdetect.py 60", 90*time.Second)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if json.Unmarshal([]byte(lastLine(res.Stdout)), &out) != nil || out["phys"] == nil {
			return nil, errors.New("nessun movimento rilevato entro 60 secondi")
		}
		return out, nil
	case "install":
		script, err := s.asset("winekeys.py")
		if err != nil {
			return nil, err
		}
		conf, _ := json.Marshal(map[string]any{"swap": req.Swap, "p1_phys": req.P1Phys})
		if err := s.ssh.UploadBytes(compDir+"/wine-keys.json", conf, 0o644); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes(compDir+"/wine-keys.py", script, 0o755); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes("/userdata/system/services/wine_keys", []byte(wkService), 0o755); err != nil {
			return nil, err
		}
		res, err := s.ssh.RunTimeout(`/userdata/system/services/wine_keys stop; batocera-services enable wine_keys >/dev/null 2>&1; batocera-services start wine_keys >/dev/null 2>&1 </dev/null ; sleep 1; HKP=$(cat /var/run/hotkeygen.pid 2>/dev/null); if [ -z "$HKP" ] || ! kill -0 "$HKP" 2>/dev/null; then HKS=$(grep -l hotkeygen /etc/init.d/* 2>/dev/null | head -1); [ -n "$HKS" ] && "$HKS" restart >/dev/null 2>&1 </dev/null; fi; sleep 1; /userdata/system/services/wine_keys status`, 2*time.Minute)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": strings.TrimSpace(lastLine(res.Stdout))}, nil
	case "remove":
		_, err := s.ssh.RunTimeout("batocera-services stop wine_keys 2>/dev/null; batocera-services disable wine_keys 2>/dev/null; pkill -f '[w]ine-keys.py'; rm -f /userdata/system/services/wine_keys "+compDir+"/wine-keys.py "+compDir+"/wine-keys.json; echo ok", time.Minute)
		return nil, err
	}
	return nil, errors.New("azione sconosciuta")
}

// ---------- Tailscale ----------

const tsFind = `TS=""; for b in /userdata/system/add-ons/tailscale/tailscale /userdata/tailscale/tailscale /usr/bin/tailscale; do [ -x "$b" ] && TS="$b" && break; done`

func (s *Server) hTailscale(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout(tsFind+`; [ -z "$TS" ] && { echo "@@none"; exit 0; }
echo "@@ver"; "$TS" version 2>/dev/null | head -1
echo "@@run"; pgrep -x tailscaled >/dev/null && echo yes
echo "@@en"; batocera-settings-get system.services 2>/dev/null
echo "@@status"; "$TS" status --json 2>/dev/null | head -c 20000`, time.Minute)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	if _, none := sec["none"]; none || strings.TrimSpace(sec["ver"]) == "" && strings.TrimSpace(sec["status"]) == "" {
		return map[string]any{"installed": false}, nil
	}
	out := map[string]any{"installed": true, "version": strings.TrimSpace(sec["ver"]), "running": strings.TrimSpace(sec["run"]) == "yes", "enabled": strings.Contains(sec["en"], "tailscale"), "via_remote": s.ssh.ViaRemote(), "remote_host": s.cfg.RemoteHost}
	var st struct {
		BackendState string `json:"BackendState"`
		AuthURL      string `json:"AuthURL"`
		Self         struct {
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			KeyExpiry    string   `json:"KeyExpiry"`
		} `json:"Self"`
	}
	if json.Unmarshal([]byte(sec["status"]), &st) == nil {
		out["state"] = st.BackendState
		out["auth_url"] = st.AuthURL
		out["dns_name"] = strings.TrimSuffix(st.Self.DNSName, ".")
		out["key_expiry"] = st.Self.KeyExpiry
		if len(st.Self.TailscaleIPs) > 0 {
			out["ip"] = st.Self.TailscaleIPs[0]
			port := "8765"
			if p := s.remoteLanPort(); p != "" {
				port = p
			}
			out["app_url"] = "http://" + st.Self.TailscaleIPs[0] + ":" + port
		}
	}
	return out, nil
}

func (s *Server) remoteLanPort() string {
	res, err := s.ssh.RunTimeout(`grep -o '"lan_port": *[0-9]*' /userdata/system/companion/config.json 2>/dev/null | grep -o '[0-9]*$'`, 10*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

func (s *Server) hTailscaleAction(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	var cmd string
	switch req.Action {
	case "enable":
		cmd = "batocera-services enable tailscale 2>&1; batocera-services start tailscale >/dev/null 2>&1 </dev/null; sleep 2; pgrep -x tailscaled >/dev/null && echo running"
	case "disable":
		cmd = "batocera-services disable tailscale 2>&1; echo ok"
	case "restart":
		cmd = "batocera-services stop tailscale >/dev/null 2>&1; sleep 1; batocera-services start tailscale >/dev/null 2>&1 </dev/null; sleep 3; pgrep -x tailscaled >/dev/null && echo running"
	case "login":
		cmd = tsFind + `; [ -z "$TS" ] && echo "tailscale non installato" && exit 1; pgrep -x tailscaled >/dev/null || (batocera-services start tailscale >/dev/null 2>&1 </dev/null; sleep 3); timeout 12 "$TS" up --timeout=10s --accept-routes 2>&1 | tr -d '\r'; "$TS" status --json 2>/dev/null | grep -o '"AuthURL": *"[^"]*"'`
	default:
		return nil, errors.New("azione sconosciuta")
	}
	res, err := s.ssh.RunTimeout(cmd, time.Minute)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"output": strings.TrimSpace(res.Stdout + res.Stderr)}
	if m := regexpMust(`https://login\.tailscale\.com/\S+`).FindString(res.Stdout + res.Stderr); m != "" {
		out["auth_url"] = strings.Trim(m, `"`)
	}
	return out, nil
}
