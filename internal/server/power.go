package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"batocompanion/internal/config"
)

// Alimentazione del cabinato tramite l'interruttore Zigbee (Sonoff su Zigbee2MQTT) via il broker
// MQTT di Home Assistant: stato, accensione, taglio corrente e "spegni e togli corrente"
// (poweroff di Batocera, attesa che sia giù, poi relè OFF).

type powerCtl struct {
	mu     sync.Mutex
	client mqtt.Client
	state  string // "ON" / "OFF" / ""
	seen   time.Time
	topic  string
	busy   string // fase di "spegni e togli corrente", "" se nessuna
}

func (s *Server) powerTopic() string {
	t := strings.TrimSpace(s.cfg.PowerTopic)
	if t == "" {
		t = "zigbee2mqtt/Cabinato"
	}
	return strings.TrimRight(t, "/")
}

// powerClient connette (una volta) al broker MQTT salvato con l'agente Home Assistant.
func (s *Server) powerClient() (mqtt.Client, error) {
	p := &s.power
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.cfg.MQTTHost == "" {
		return nil, errors.New("broker MQTT non configurato: installa l'agente Home Assistant (scheda Add-on) oppure inserisci i dati MQTT")
	}
	topic := s.powerTopic()
	if p.client != nil && p.client.IsConnectionOpen() && p.topic == topic {
		return p.client, nil
	}
	if p.client != nil {
		p.client.Disconnect(100)
		p.client = nil
	}
	port := s.cfg.MQTTPort
	if port == 0 {
		port = 1883
	}
	opts := mqtt.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:%d", s.cfg.MQTTHost, port)).
		SetClientID(fmt.Sprintf("bc-companion-%d", time.Now().UnixNano()%100000)).
		SetUsername(s.cfg.MQTTUser).SetPassword(s.cfg.MQTTPass).
		SetConnectTimeout(5 * time.Second).SetAutoReconnect(true).SetConnectRetry(false)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		c.Subscribe(topic, 0, func(_ mqtt.Client, m mqtt.Message) {
			var st struct {
				State string `json:"state"`
			}
			if json.Unmarshal(m.Payload(), &st) == nil && st.State != "" {
				p.mu.Lock()
				p.state, p.seen = strings.ToUpper(st.State), time.Now()
				p.mu.Unlock()
			}
		})
		// Zigbee2MQTT non ritiene lo stato: lo chiedo
		c.Publish(topic+"/get", 0, false, `{"state":""}`)
	})
	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(6*time.Second) || tok.Error() != nil {
		err := tok.Error()
		if err == nil {
			err = errors.New("timeout")
		}
		return nil, fmt.Errorf("broker MQTT %s: %v", s.cfg.MQTTHost, err)
	}
	p.client, p.topic = c, topic
	return c, nil
}

func (s *Server) powerState(wait time.Duration) (string, error) {
	c, err := s.powerClient()
	if err != nil {
		return "", err
	}
	p := &s.power
	p.mu.Lock()
	fresh := time.Since(p.seen) < 20*time.Second
	st := p.state
	p.mu.Unlock()
	if fresh {
		return st, nil
	}
	c.Publish(s.powerTopic()+"/get", 0, false, `{"state":""}`)
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		time.Sleep(150 * time.Millisecond)
		p.mu.Lock()
		if time.Since(p.seen) < 20*time.Second {
			st = p.state
			p.mu.Unlock()
			return st, nil
		}
		p.mu.Unlock()
	}
	return "", nil
}

func (s *Server) powerSet(on bool) error {
	c, err := s.powerClient()
	if err != nil {
		return err
	}
	v := "OFF"
	if on {
		v = "ON"
	}
	tok := c.Publish(s.powerTopic()+"/set", 0, false, `{"state":"`+v+`"}`)
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		return errors.New("invio al broker fallito")
	}
	s.power.mu.Lock()
	s.power.state, s.power.seen = v, time.Now()
	s.power.mu.Unlock()
	return nil
}

func (s *Server) hPower(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodGet {
		out := map[string]any{"configured": s.cfg.MQTTHost != "", "topic": s.powerTopic(), "host": s.cfg.MQTTHost, "user": s.cfg.MQTTUser, "port": s.cfg.MQTTPort}
		s.power.mu.Lock()
		out["busy"] = s.power.busy
		s.power.mu.Unlock()
		if s.cfg.MQTTHost == "" {
			return out, nil
		}
		st, err := s.powerState(3 * time.Second)
		if err != nil {
			out["error"] = err.Error()
			return out, nil
		}
		out["state"] = st
		out["online"] = s.ssh.Online()
		return out, nil
	}
	var req struct {
		Action string `json:"action"`
		Topic  string `json:"topic"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
		User   string `json:"user"`
		Pass   string `json:"pass"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "config":
		if req.Topic != "" {
			s.cfg.PowerTopic = strings.TrimSpace(req.Topic)
		}
		if req.Host != "" {
			s.cfg.MQTTHost, s.cfg.MQTTPort, s.cfg.MQTTUser = strings.TrimSpace(req.Host), req.Port, strings.TrimSpace(req.User)
			if req.Pass != "" {
				s.cfg.MQTTPass = req.Pass
			}
		}
		if err := config.Save(s.cfg); err != nil {
			return nil, err
		}
		s.power.mu.Lock()
		if s.power.client != nil {
			s.power.client.Disconnect(100)
			s.power.client = nil
		}
		s.power.mu.Unlock()
		return map[string]any{"ok": true}, nil
	case "on":
		return map[string]any{"ok": true}, s.powerSet(true)
	case "off":
		return map[string]any{"ok": true}, s.powerSet(false)
	case "shutdown":
		if _, err := s.powerClient(); err != nil {
			return nil, err
		}
		s.power.mu.Lock()
		if s.power.busy != "" {
			s.power.mu.Unlock()
			return nil, errors.New("spegnimento già in corso")
		}
		s.power.busy = "spegnimento di Batocera"
		s.power.mu.Unlock()
		if s.ssh.Online() {
			_, _ = s.ssh.RunTimeout("(sleep 1; poweroff) >/dev/null 2>&1 &", 10*time.Second)
		}
		go func() {
			defer func() { s.power.mu.Lock(); s.power.busy = ""; s.power.mu.Unlock() }()
			// aspetto che il cabinato non risponda piu' (max 90 s), poi altri 15 s per sicurezza
			end := time.Now().Add(90 * time.Second)
			for time.Now().Before(end) {
				time.Sleep(3 * time.Second)
				if !s.ssh.Online() {
					break
				}
			}
			s.power.mu.Lock()
			s.power.busy = "attendo 15 s e tolgo corrente"
			s.power.mu.Unlock()
			time.Sleep(15 * time.Second)
			_ = s.powerSet(false)
			s.invalidateInfo()
		}()
		return map[string]any{"ok": true}, nil
	}
	return nil, errors.New("azione sconosciuta")
}
