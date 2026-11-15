package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
)

// Modalità "stock" per i video: restano visibili solo i giochi scelti (come su un Arcade1Up originale),
// gli altri vengono nascosti nei gamelist e i sistemi senza giochi scelti spariscono dal menu.
// EmulationStation va in Kiosk, senza orologio e suggerimenti. Tutto reversibile.

var stockKeys = []string{"HiddenSystems", "UIMode", "DrawClock", "ShowHelpPrompts", "ShowBatteryIndicator"}

var reEsString = regexp.MustCompile(`<(?:string|bool|int)\s+name="([^"]+)"\s+value="([^"]*)"`)

const stockHiddenFile = compDir + "/stock-hidden.json"

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
		var list []map[string]any
		for _, sy := range systems {
			if sy.Count == 0 {
				continue
			}
			games, err := s.es.Games(sy.Name)
			if err != nil {
				continue
			}
			var gl []map[string]any
			for _, g := range games {
				gl = append(gl, map[string]any{"id": g.ID, "path": g.Path, "name": g.Name, "hidden": g.Hidden})
			}
			list = append(list, map[string]any{"name": sy.Name, "fullname": sy.Fullname, "games": gl})
		}
		if list == nil {
			list = []map[string]any{}
		}
		cur, _ := s.esSettingsGet(stockKeys)
		sel := s.cfg.StockGames
		if sel == nil {
			sel = []string{}
		}
		return map[string]any{"active": s.cfg.StockActive, "systems": list, "selected": sel, "current": cur}, nil
	}
	var req struct {
		Action string   `json:"action"`
		Games  []string `json:"games"` // "sistema|percorso"
		Kiosk  bool     `json:"kiosk"`
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
		if len(req.Games) == 0 {
			return nil, fmt.Errorf("scegli almeno un gioco da lasciare visibile")
		}
		keep := map[string]map[string]bool{}
		for _, g := range req.Games {
			sys, p, ok := strings.Cut(g, "|")
			if !ok {
				continue
			}
			if keep[sys] == nil {
				keep[sys] = map[string]bool{}
			}
			keep[sys][p] = true
		}
		if s.cfg.StockActive {
			// riattivazione con una selezione diversa: prima rimetto visibili quelli nascosti la volta scorsa
			_ = s.stockUnhide()
		}
		systems, err := s.es.Systems()
		if err != nil {
			return nil, err
		}
		systems = stockRealSystems(systems)
		hiddenBySys := map[string][]string{}
		var hiddenSystems []string
		for _, sy := range systems {
			if len(keep[sy.Name]) == 0 {
				hiddenSystems = append(hiddenSystems, sy.Name)
				continue
			}
			games, err := s.es.Games(sy.Name)
			if err != nil {
				continue
			}
			var toHide []string
			for _, g := range games {
				if !keep[sy.Name][g.Path] && !g.Hidden {
					toHide = append(toHide, g.Path)
				}
			}
			if len(toHide) > 0 {
				out, err := s.gamelistRun(sy.Name, "hide", "", map[string]any{"paths": toHide, "hidden": true})
				if err != nil {
					return nil, err
				}
				var changed []string
				_ = json.Unmarshal([]byte(out), &changed)
				hiddenBySys[sy.Name] = changed
			}
		}
		b, _ := json.Marshal(hiddenBySys)
		if err := s.ssh.UploadBytes(stockHiddenFile, b, 0o644); err != nil {
			return nil, err
		}
		if !s.cfg.StockActive {
			prev, err := s.esSettingsGet(stockKeys)
			if err != nil {
				return nil, err
			}
			s.cfg.StockPrev = prev
		}
		s.cfg.StockGames = req.Games
		hv := strings.Join(hiddenSystems, ";")
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
		nh := 0
		for _, l := range hiddenBySys {
			nh += len(l)
		}
		return map[string]any{"ok": true, "hidden_systems": hiddenSystems, "hidden_games": nh}, config.Save(s.cfg)
	case "off":
		if err := s.stockUnhide(); err != nil {
			return nil, err
		}
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

// stockUnhide rimette visibili i giochi nascosti dalla modalità stock.
func (s *Server) stockUnhide() error {
	b, err := s.ssh.ReadFile(stockHiddenFile)
	if err != nil {
		return nil
	}
	var hidden map[string][]string
	if json.Unmarshal(b, &hidden) != nil {
		return nil
	}
	for sys, paths := range hidden {
		if len(paths) == 0 {
			continue
		}
		if _, err := s.gamelistRun(sys, "hide", "", map[string]any{"paths": paths, "hidden": false}); err != nil {
			return err
		}
	}
	_, _ = s.ssh.Run("rm -f " + stockHiddenFile)
	return nil
}
