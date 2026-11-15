package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// Agente Home Assistant (MQTT) sul cabinato.

const haDir = "/userdata/system/homeassistant"

func (s *Server) hHA(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout(`echo "@@conf"; cat `+haDir+`/config.conf 2>/dev/null
echo "@@inst"; [ -x `+haDir+`/homeassistant.sh ] && echo yes
echo "@@run"; pgrep -f "[h]omeassistant.sh start" >/dev/null && echo yes
echo "@@en"; batocera-settings-get system.services 2>/dev/null
echo "@@mosq"; which mosquitto_pub
echo "@@mac"; for i in /sys/class/net/e*; do cat $i/address 2>/dev/null; break; done
echo "@@log"; tail -n 5 `+haDir+`/logs/homeassistant.log 2>/dev/null`, 30*time.Second)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	conf := map[string]string{}
	for _, l := range cab.Lines(sec["conf"]) {
		if k, v, ok := strings.Cut(l, "="); ok {
			conf[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	delete(conf, "MQTT_PASSWORD")
	return map[string]any{"installed": strings.TrimSpace(sec["inst"]) == "yes", "running": strings.TrimSpace(sec["run"]) == "yes", "enabled": strings.Contains(sec["en"], "ha_batocera"), "config": conf, "mosquitto": strings.TrimSpace(sec["mosq"]) != "", "mac": strings.TrimSpace(sec["mac"]), "log": strings.TrimSpace(sec["log"])}, nil
}

func (s *Server) hHAInstall(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		User       string `json:"user"`
		Password   string `json:"password"`
		DeviceName string `json:"device_name"`
		DeviceID   string `json:"device_id"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Host == "" {
		return nil, errors.New("inserisci l'IP del broker MQTT")
	}
	if req.Port == 0 {
		req.Port = 1883
	}
	if req.DeviceID == "" || !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(req.DeviceID) {
		req.DeviceID = "cabinato"
	}
	if req.DeviceName == "" {
		req.DeviceName = "Cabinato Arcade1Up"
	}
	agent, err := s.asset("ha_agent.sh")
	if err != nil {
		return nil, err
	}
	svc, err := s.asset("ha-service.sh")
	if err != nil {
		return nil, err
	}
	j := s.jobs.Start("Agente Home Assistant", func(j *jobs.Job) (any, error) {
		j.SetProgress(0.1, "Verifico il broker MQTT dal cabinato")
		res, err := s.ssh.RunTimeout(fmt.Sprintf("which mosquitto_pub >/dev/null || { echo NOMOSQ; exit 0; }; timeout 10 mosquitto_pub -h %s -p %d -u %s -P %s -t %s/companion_test -m ok 2>&1 && echo MQTTOK", cab.ShellQuote(req.Host), req.Port, cab.ShellQuote(req.User), cab.ShellQuote(req.Password), req.DeviceID), 30*time.Second)
		if err != nil {
			return nil, err
		}
		if strings.Contains(res.Stdout, "NOMOSQ") {
			return nil, errors.New("mosquitto_pub assente su questa Batocera: l'agente non può pubblicare")
		}
		if !strings.Contains(res.Stdout, "MQTTOK") {
			return nil, fmt.Errorf("il cabinato non riesce a pubblicare sul broker %s:%d con utente %q — controlla IP, porta e credenziali MQTT (%s)", req.Host, req.Port, req.User, strings.TrimSpace(res.Stdout))
		}
		j.Logf("✓ broker MQTT raggiungibile (%s:%d)", req.Host, req.Port)
		j.SetProgress(0.4, "Fermo il vecchio agente e rimuovo i sensori vecchi")
		_, _ = s.ssh.RunTimeout("for s in ha_batocera homeassistant ha-batocera; do batocera-services stop $s 2>/dev/null; batocera-services disable $s 2>/dev/null; done; pkill -f '[h]omeassistant.sh' 2>/dev/null; pkill -f '[h]a_batocera' 2>/dev/null; [ -f /userdata/system/custom.sh ] && sed -i '/homeassistant\\.sh\\|ha_batocera_agent\\.sh/d' /userdata/system/custom.sh; mkdir -p "+haDir+"/logs; true", time.Minute)
		conf := fmt.Sprintf("MQTT_HOST=%q\nMQTT_PORT=%d\nMQTT_USER=%q\nMQTT_PASSWORD=%q\nDEVICE_NAME=%q\nDEVICE_ID=%q\nBASE_TOPIC=%q\nDISCOVERY_PREFIX=\"homeassistant\"\n", req.Host, req.Port, req.User, req.Password, req.DeviceName, req.DeviceID, "batocera/"+req.DeviceID)
		if err := s.ssh.UploadBytes(haDir+"/config.conf", []byte(conf), 0o600); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes(haDir+"/homeassistant.sh", agent, 0o755); err != nil {
			return nil, err
		}
		if err := s.ssh.UploadBytes("/userdata/system/services/ha_batocera", svc, 0o755); err != nil {
			return nil, err
		}
		j.SetProgress(0.7, "Registro il dispositivo in Home Assistant e avvio il servizio")
		res, err = s.ssh.RunTimeout(haDir+"/homeassistant.sh install 2>&1 | tail -5; batocera-services enable ha_batocera >/dev/null 2>&1; batocera-services start ha_batocera >/dev/null 2>&1 </dev/null; sleep 3; pgrep -f '[h]omeassistant.sh start' >/dev/null && echo RUNNING", 2*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stdout) {
			j.Logf("%s", l)
		}
		if !strings.Contains(res.Stdout, "RUNNING") {
			return nil, errors.New("il servizio non risulta in esecuzione: vedi il log in Impostazioni → Home Assistant")
		}
		j.Logf("✓ agente attivo: in Home Assistant compare il dispositivo \"%s\"", req.DeviceName)
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) hHARemove(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Start("Rimozione agente HA", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "")
		_, err := s.ssh.RunTimeout(haDir+"/homeassistant.sh stop >/dev/null 2>&1 || true\nbatocera-services disable ha_batocera >/dev/null 2>&1 || true\npkill -f '[h]omeassistant.sh' 2>/dev/null\nrm -f /userdata/system/services/ha_batocera\nfor e in game-start game-end system-selected controls-changed; do rm -f /userdata/system/configs/emulationstation/scripts/$e/homeassistant.sh; done\n[ -f /userdata/system/custom.sh ] && sed -i '/homeassistant\\.sh\\|ha_batocera_agent\\.sh/d' /userdata/system/custom.sh || true\nrm -rf "+haDir+"\necho done", time.Minute)
		if err != nil {
			return nil, err
		}
		j.Logf("✓ agente rimosso. In Home Assistant puoi eliminare il dispositivo MQTT da Impostazioni → Dispositivi.")
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}
