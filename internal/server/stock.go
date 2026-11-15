package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
)

// Modalità "stock" per i video: il cabinato si presenta come un Arcade1Up originale.
// Nasconde i sistemi non arcade, mette EmulationStation in Kiosk, toglie orologio e suggerimenti.
// I valori precedenti vengono salvati nella configurazione del Companion.

var stockKeys = []string{"HiddenSystems", "UIMode", "DrawClock", "ShowHelpPrompts", "ShowBatteryIndicator"}

var reEsString = regexp.MustCompile(`<(?:string|bool|int)\s+name="([^"]+)"\s+value="([^"]*)"`)

func (s *Server) esSettingsGet(keys []string) (map[string]string, error) {
	res, err := s.ssh.RunTimeout("cat "+esSettings+" 2>/dev/null", 20*time.Second)
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, m := range reEsString.FindAllStringSubmatch(res.Stdout, -1) {
		all[m[1]] = m[2]
	}
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// esSettingsSetCmd: comando shell che imposta (o rimuove, se v == nil) una chiave di es_settings.cfg.
func esSettingsSetCmd(k string, v *string, isBool bool) string {
	if v == nil {
		return fmt.Sprintf(`sed -i '/name="%s"/d' %s`, k, esSettings)
	}
	val := strings.NewReplacer(`&`, `&amp;`, `"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`, `|`, `\|`).Replace(*v)
	typ := "string"
	if isBool {
		typ = "bool"
	}
	return fmt.Sprintf(`if grep -q 'name="%s"' %s; then sed -i 's|name="%s" value="[^"]*"|name="%s" value="%s"|' %s; else sed -i 's|</config>|  <%s name="%s" value="%s" />\n</config>|' %s; fi`,
		k, esSettings, k, k, val, esSettings, typ, k, val, esSettings)
}

func stockRealSystems(in []cab.System) []cab.System {
	var out []cab.System
	for _, sy := range in {
		n := strings.ToLower(sy.Name)
		if n == "all" || n == "favorites" || n == "recent" || n == "lastplayed" || strings.HasPrefix(n, "auto-") || strings.HasPrefix(n, "custom-") {
			continue
		}
		out = append(out, sy)
	}
	return out
}

func (s *Server) hStock(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		systems, _ := s.es.Systems()
		systems = stockRealSystems(systems)
		var names []map[string]any
		for _, sy := range systems {
			names = append(names, map[string]any{"name": sy.Name, "fullname": sy.Fullname, "games": sy.Count})
		}
		cur, _ := s.esSettingsGet(stockKeys)
		keep := s.cfg.StockKeep
		if keep == "" {
			var def []string
			for _, sy := range systems {
				n := strings.ToLower(sy.Name)
				if n == "fbneo" || n == "mame" || n == "neogeo" || strings.HasPrefix(n, "cps") || strings.Contains(n, "arcade") || n == "naomi" || n == "atomiswave" {
					def = append(def, sy.Name)
				}
			}
			keep = strings.Join(def, ",")
		}
		return map[string]any{"active": s.cfg.StockActive, "systems": names, "keep": keep, "current": cur}, nil
	}
	var req struct {
		Action string `json:"action"`
		Keep   string `json:"keep"`
		Kiosk  bool   `json:"kiosk"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if g, err := s.es.RunningGame(); err == nil && g != nil {
		return nil, fmt.Errorf("c'è un gioco in corso: chiudilo e riprova (EmulationStation va riavviato)")
	}
	backup := "mkdir -p " + backupDir + "; cp " + esSettings + " " + backupDir + "/es_settings.cfg." + time.Now().Format("20060102-150405") + " 2>/dev/null; "
	switch req.Action {
	case "on":
		systems, err := s.es.Systems()
		if err != nil {
			return nil, err
		}
		systems = stockRealSystems(systems)
		keep := map[string]bool{}
		for _, k := range strings.Split(req.Keep, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keep[k] = true
			}
		}
		var hidden []string
		for _, sy := range systems {
			if !keep[sy.Name] {
				hidden = append(hidden, sy.Name)
			}
		}
		if len(hidden) == len(systems) {
			return nil, fmt.Errorf("devi lasciare visibile almeno un sistema")
		}
		if !s.cfg.StockActive {
			prev, err := s.esSettingsGet(stockKeys)
			if err != nil {
				return nil, err
			}
			s.cfg.StockPrev = prev
		}
		s.cfg.StockKeep = req.Keep
		hv := strings.Join(hidden, ";")
		ui := "Full"
		if req.Kiosk {
			ui = "Kiosk"
		}
		f := "false"
		cmds := []string{esSettingsSetCmd("HiddenSystems", &hv, false), esSettingsSetCmd("UIMode", &ui, false), esSettingsSetCmd("DrawClock", &f, true), esSettingsSetCmd("ShowHelpPrompts", &f, true), esSettingsSetCmd("ShowBatteryIndicator", &f, true)}
		if _, err := s.ssh.RunTimeout(backup+strings.Join(cmds, "; ")+"; batocera-es-swissknife --restart >/dev/null 2>&1 &", time.Minute); err != nil {
			return nil, err
		}
		s.cfg.StockActive = true
		return map[string]any{"ok": true, "hidden": hidden}, config.Save(s.cfg)
	case "off":
		var cmds []string
		for _, k := range stockKeys {
			if v, ok := s.cfg.StockPrev[k]; ok {
				vv := v
				cmds = append(cmds, esSettingsSetCmd(k, &vv, k != "HiddenSystems" && k != "UIMode"))
			} else {
				cmds = append(cmds, esSettingsSetCmd(k, nil, false))
			}
		}
		if _, err := s.ssh.RunTimeout(backup+strings.Join(cmds, "; ")+"; batocera-es-swissknife --restart >/dev/null 2>&1 &", time.Minute); err != nil {
			return nil, err
		}
		s.cfg.StockActive = false
		s.cfg.StockPrev = nil
		return nil, config.Save(s.cfg)
	}
	return nil, fmt.Errorf("azione sconosciuta")
}
