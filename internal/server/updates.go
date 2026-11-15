package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
	"batocompanion/internal/jobs"
)

// Promemoria aggiornamenti: Batocera (stabile e beta), app Flatpak sul cabinato.
const batoceraUpdates = "https://updates.batocera.org/x86_64/%s/last/batocera.version"

type updatesCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[string]any
}

var upCache updatesCache

func fetchBatoceraVersion(branch string) (string, string) {
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Get(fmt.Sprintf(batoceraUpdates, branch))
	if err != nil {
		return "", err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return "", "risposta vuota"
	}
	date := ""
	if len(f) > 1 {
		date = f[1]
	}
	return f[0], date
}

func (s *Server) hUpdates(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Value  string `json:"value"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		switch req.Action {
		case "dismiss":
			if s.cfg.UpdatesSeen == nil {
				s.cfg.UpdatesSeen = map[string]string{}
			}
			s.cfg.UpdatesSeen[req.Key] = req.Value
			return nil, config.Save(s.cfg)
		case "refresh":
			upCache.mu.Lock()
			upCache.at = time.Time{}
			upCache.mu.Unlock()
			return s.updatesStatus()
		case "flatpak-update":
			j := s.jobs.Start("Aggiornamento app Flatpak", func(j *jobs.Job) (any, error) {
				j.SetProgress(-1, "flatpak update in corso (può richiedere diversi minuti)")
				res, err := s.ssh.RunTimeout("flatpak update -y --noninteractive 2>&1 | tail -20; echo \"@@rc=${PIPESTATUS[0]}\"; batocera-flatpak-update >/dev/null 2>&1; echo done", 40*time.Minute)
				if err != nil {
					return nil, err
				}
				j.Logf("%s", res.Stdout)
				if strings.Contains(res.Stdout, "@@rc=") && !strings.Contains(res.Stdout, "@@rc=0") {
					return nil, fmt.Errorf("flatpak update ha restituito un errore (vedi log)")
				}
				upCache.mu.Lock()
				upCache.at = time.Time{}
				upCache.mu.Unlock()
				_ = s.es.ReloadGames()
				return nil, nil
			})
			return map[string]any{"job": j.ID}, nil
		}
		return nil, fmt.Errorf("azione sconosciuta")
	}
	return s.updatesStatus()
}

func (s *Server) updatesStatus() (any, error) {
	upCache.mu.Lock()
	if upCache.data != nil && time.Since(upCache.at) < 6*time.Hour {
		d := upCache.data
		upCache.mu.Unlock()
		return s.decorateUpdates(d), nil
	}
	upCache.mu.Unlock()
	out := map[string]any{"checked": time.Now().Format("2006-01-02 15:04")}
	res, err := s.ssh.RunTimeout(`echo "@@ver"; cat /usr/share/batocera/batocera.version 2>/dev/null | head -1
echo "@@arch"; cat /usr/share/batocera/batocera.arch 2>/dev/null || uname -m
echo "@@flatpak"; which flatpak >/dev/null 2>&1 && timeout 120 flatpak remote-ls --updates --columns=application,name,version 2>/dev/null
echo "@@installed"; which flatpak >/dev/null 2>&1 && flatpak list --app --columns=application 2>/dev/null | wc -l`, 3*time.Minute)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	verf := strings.Fields(strings.TrimSpace(sec["ver"]))
	cur := ""
	if len(verf) > 0 {
		cur = verf[0]
	}
	out["batocera_current"] = cur
	out["batocera_arch"] = strings.TrimSpace(sec["arch"])
	stable, sdate := fetchBatoceraVersion("stable")
	beta, bdate := fetchBatoceraVersion("butterfly")
	out["batocera_stable"], out["batocera_stable_date"] = stable, sdate
	out["batocera_beta"], out["batocera_beta_date"] = beta, bdate
	if stable == "" {
		out["batocera_error"] = sdate
	}
	var fp []map[string]string
	for _, l := range cab.Lines(strings.TrimSpace(sec["flatpak"])) {
		f := strings.Split(l, "\t")
		if strings.TrimSpace(f[0]) == "" {
			continue
		}
		m := map[string]string{"id": strings.TrimSpace(f[0])}
		if len(f) > 1 {
			m["name"] = strings.TrimSpace(f[1])
		}
		if len(f) > 2 {
			m["version"] = strings.TrimSpace(f[2])
		}
		fp = append(fp, m)
	}
	out["flatpak"] = fp
	out["flatpak_installed"] = strings.TrimSpace(sec["installed"])
	upCache.mu.Lock()
	upCache.data, upCache.at = out, time.Now()
	upCache.mu.Unlock()
	s.cfg.UpdatesLast = out["checked"].(string)
	_ = config.Save(s.cfg)
	return s.decorateUpdates(out), nil
}

func (s *Server) decorateUpdates(d map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range d {
		out[k] = v
	}
	seen := s.cfg.UpdatesSeen
	items := []map[string]any{}
	cur, _ := d["batocera_current"].(string)
	stable, _ := d["batocera_stable"].(string)
	beta, _ := d["batocera_beta"].(string)
	if stable != "" && cur != "" && versionLess(cur, stable) {
		items = append(items, map[string]any{"key": "batocera", "value": stable, "kind": "batocera", "title": "Batocera " + stable + " disponibile (hai la " + cur + ")", "dismissed": seen["batocera"] == stable,
			"note": "Dal menu del cabinato: Impostazioni di sistema → Aggiornamenti e download → Avvia aggiornamento. Prima fai un backup dalla scheda Backup. Se vuoi passare alla versione x86-64-v3 serve invece una nuova installazione su chiavetta: il backup ti rimette tutto a posto."})
	}
	if beta != "" && cur != "" && versionLess(cur, beta) && (stable == "" || versionLess(stable, beta)) {
		items = append(items, map[string]any{"key": "batocera-beta", "value": beta, "kind": "info", "title": "Batocera " + beta + " è in beta (butterfly)", "dismissed": seen["batocera-beta"] == beta,
			"note": "Non ancora stabile: meglio aspettare la versione definitiva. Ti avviso qui quando esce."})
	}
	if fp, ok := d["flatpak"].([]map[string]string); ok && len(fp) > 0 {
		var names []string
		for _, f := range fp {
			n := f["name"]
			if n == "" {
				n = f["id"]
			}
			if f["version"] != "" {
				n += " " + f["version"]
			}
			names = append(names, n)
		}
		v := strings.Join(names, "|")
		items = append(items, map[string]any{"key": "flatpak", "value": v, "kind": "flatpak", "title": fmt.Sprintf("%d app Flatpak da aggiornare", len(fp)), "list": names, "dismissed": seen["flatpak"] == v,
			"note": "Steam, Chiaki e le altre app installate da Flathub: l'aggiornamento parte da qui e non tocca giochi o salvataggi."})
	}
	pending := 0
	for _, it := range items {
		if !it["dismissed"].(bool) {
			pending++
		}
	}
	out["items"] = items
	out["pending"] = pending
	return out
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			return x < y
		}
	}
	return false
}
