package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
	"batocompanion/internal/jobs"
)

// Server sul cabinato: questa stessa app installata in Batocera come servizio.

const companionService = `#!/bin/bash
# Batocera Companion - servizio (gestito da batocera-services)
DIR=/userdata/system/companion
PIDF=/var/run/companion.pid
case "$1" in
  start)
    mkdir -p "$DIR"
    BATOCERA_COMPANION_DIR="$DIR" start-stop-daemon -S -b -q -m -p "$PIDF" --exec "$DIR/companion" -- -server
    ;;
  stop)
    start-stop-daemon -K -q -p "$PIDF"; rm -f "$PIDF"
    ;;
  status)
    start-stop-daemon --status -q -p "$PIDF" && echo started || echo stopped
    ;;
esac
exit 0
`

func (s *Server) hCabinet(w http.ResponseWriter, r *http.Request) (any, error) {
	out := map[string]any{"payload": len(s.payload) > 0, "payload_version": s.Version}
	res, err := s.ssh.RunTimeout(`echo "@@bin"; [ -x /userdata/system/companion/companion ] && /userdata/system/companion/companion -version 2>/dev/null
echo "@@svc"; [ -f /userdata/system/services/companion ] && echo present; batocera-services status companion 2>/dev/null
echo "@@enabled"; batocera-settings-get system.services 2>/dev/null
echo "@@port"; grep -o '"lan_port": *[0-9]*' /userdata/system/companion/config.json 2>/dev/null | grep -o '[0-9]*$'
echo "@@ip"; ip -4 -o addr 2>/dev/null | awk '!/ lo /{print $4}' | cut -d/ -f1 | head -1`, 30*time.Second)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	ver := strings.TrimSpace(sec["bin"])
	out["installed"] = ver != ""
	out["version"] = strings.TrimPrefix(ver, "v")
	out["running"] = strings.Contains(sec["svc"], "started") || strings.Contains(sec["svc"], "running")
	out["enabled"] = strings.Contains(sec["enabled"], "companion")
	port := strings.TrimSpace(sec["port"])
	if port == "" {
		port = "8765"
	}
	ip := strings.TrimSpace(sec["ip"])
	if ip == "" {
		ip = s.cfg.Host
	}
	out["url"] = fmt.Sprintf("http://%s:%s", ip, port)
	if ver != "" {
		key := s.remoteLinkKey()
		if key != "" {
			out["ha_camera"] = fmt.Sprintf("http://%s:%s/live?fps=8&w=640&key=%s", ip, port, key)
			out["ha_page"] = fmt.Sprintf("http://%s:%s/live.html?fps=10&w=800&key=%s", ip, port, key)
		}
	}
	return out, nil
}

func (s *Server) remoteLinkKey() string {
	res, err := s.ssh.RunTimeout(`grep -o '"link_key": *"[^"]*"' /userdata/system/companion/config.json 2>/dev/null | sed 's/.*: *"//;s/"$//'`, 15*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

func (s *Server) hCabinetInstall(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Pin  string `json:"pin"`
		Port int    `json:"port"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if len(req.Pin) < 4 {
		return nil, errors.New("PIN di almeno 4 cifre")
	}
	if req.Port == 0 {
		req.Port = 8765
	}
	if len(s.payload) == 0 {
		return nil, errors.New("questa build non contiene il binario per il cabinato")
	}
	j := s.jobs.Start("Server sul cabinato", func(j *jobs.Job) (any, error) {
		j.SetProgress(0.1, "Fermo il servizio")
		_, _ = s.ssh.RunTimeout("mkdir -p "+compDir+" /userdata/system/services; batocera-services stop companion 2>/dev/null; true", time.Minute)
		j.SetProgress(0.2, "Carico il binario")
		if err := s.ssh.UploadBytes(compDir+"/companion.new", s.payload, 0o755); err != nil {
			return nil, err
		}
		// configurazione per la modalità server: parla con Batocera in locale
		rc := *s.cfg
		rc.Host, rc.RemoteHost = "127.0.0.1", ""
		rc.LanEnabled, rc.LanPin, rc.LanPort = true, req.Pin, req.Port
		rc.Sessions = map[string]string{}
		rc.ScreenshotDir = compDir + "/screenshots"
		rc.BackupDir = ""
		if k := s.remoteLinkKey(); k != "" {
			rc.LinkKey = k
		}
		rc.Path = compDir + "/config.json"
		b, _ := configJSON(&rc)
		if err := s.ssh.UploadBytes(compDir+"/config.json", b, 0o600); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes("/userdata/system/services/companion", []byte(companionService), 0o755); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes(compDir+"/version", []byte(s.Version+"\n"), 0o644); err != nil {
			return nil, err
		}
		j.SetProgress(0.8, "Avvio il servizio")
		res, err := s.ssh.RunTimeout("mv -f "+compDir+"/companion.new "+compDir+"/companion && chmod +x "+compDir+"/companion && batocera-services enable companion >/dev/null 2>&1; batocera-services start companion >/dev/null 2>&1 </dev/null; sleep 2; batocera-services status companion 2>/dev/null; "+compDir+"/companion -version", 2*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stdout) {
			j.Logf("%s", l)
		}
		j.Logf("✓ installato in %s", compDir)
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func configJSON(c *config.Config) ([]byte, error) {
	return jsonIndent(c)
}

func (s *Server) hCabinetRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	_, err := s.ssh.RunTimeout("batocera-services stop companion 2>/dev/null; batocera-services disable companion 2>/dev/null; rm -f /userdata/system/services/companion "+compDir+"/companion; echo ok", time.Minute)
	return nil, err
}
