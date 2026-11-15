package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// Add-on: store ufficiale Batocera + Batocera Unofficial Add-Ons (BUA).

const buaRepo = "batocera-unofficial-addons/batocera-unofficial-addons"
const buaBase = "export TERM=dumb; curl -Ls https://github.com/batocera-unofficial-addons/batocera-unofficial-addons/raw/refs/heads/main/app/install_x86.sh | bash 2>&1"

var buaRecommended = map[string]string{"chiaki": "Giochi", "chiaki-ng": "Giochi", "moonlight": "Giochi", "steam": "Giochi", "heroic": "Giochi", "fightcade": "Giochi", "tailscale": "Rete", "sunshine": "Rete", "sdlpop": "Giochi", "dolphin": "Emulatori", "ryujinx": "Emulatori", "xemu": "Emulatori"}

type addonsCache struct {
	mu  sync.Mutex
	at  time.Time
	bua []map[string]any
	err string
}

var adCache addonsCache

var reBuaApp = regexp.MustCompile(`\["([A-Za-z0-9_\-]+)"\]="(curl[^"]+)"`)
var reBuaDesc = regexp.MustCompile(`\["([A-Za-z0-9_\-]+)"\]="([^"]*)"`)
var reBuaCat = regexp.MustCompile(`\["([^"]+)"\]="([A-Z0-9_\- ]+)"`)

// buaList legge la tabella delle app dallo script ufficiale di BUA (niente API GitHub, niente limiti).
func (s *Server) buaList() ([]map[string]any, error) {
	adCache.mu.Lock()
	if adCache.bua != nil && time.Since(adCache.at) < 6*time.Hour {
		l := adCache.bua
		adCache.mu.Unlock()
		return l, nil
	}
	adCache.mu.Unlock()
	req, _ := http.NewRequest("GET", "https://raw.githubusercontent.com/"+buaRepo+"/main/app/batocera-unofficial-addons.sh", nil)
	req.Header.Set("User-Agent", "BatoceraCompanion")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub: HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	txt := string(b)
	sect := func(name string) string {
		i := strings.Index(txt, name+"=(")
		if i < 0 {
			return ""
		}
		j := strings.Index(txt[i:], "\n)")
		if j < 0 {
			return txt[i:]
		}
		return txt[i : i+j]
	}
	cmds := map[string]string{}
	for _, m := range reBuaApp.FindAllStringSubmatch(sect("apps"), -1) {
		cmds[m[1]] = m[2]
	}
	descs := map[string]string{}
	for _, m := range reBuaDesc.FindAllStringSubmatch(sect("descriptions"), -1) {
		descs[m[1]] = m[2]
	}
	cats := map[string]string{}
	for _, m := range reBuaCat.FindAllStringSubmatch(sect("categories"), -1) {
		for _, a := range strings.Fields(m[2]) {
			cats[a] = m[1]
		}
	}
	if len(cmds) == 0 {
		return nil, errors.New("elenco BUA non leggibile")
	}
	var out []map[string]any
	for name, cmd := range cmds {
		cat := cats[name]
		if cat == "" {
			cat = "Altro"
		}
		url := ""
		if m := regexp.MustCompile(`https://\S+`).FindString(cmd); m != "" {
			url = m
		}
		low := strings.ToLower(name)
		out = append(out, map[string]any{"name": low, "label": name, "source": "bua", "category": cat, "description": descs[name], "recommended": buaRecommended[low] != "", "url": url, "cmd": cmd, "version": ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	adCache.mu.Lock()
	adCache.bua, adCache.at = out, time.Now()
	adCache.mu.Unlock()
	return out, nil
}

var reStoreLine = regexp.MustCompile(`^\s*(\S+)\s*(.*)$`)

func (s *Server) hAddons(w http.ResponseWriter, r *http.Request) (any, error) {
	refresh := r.URL.Query().Get("refresh") == "1"
	if refresh {
		adCache.mu.Lock()
		adCache.bua = nil
		adCache.mu.Unlock()
	}
	out := map[string]any{"warning": "Gli add-on non ufficiali vengono eseguiti come root sul cabinato: installa solo quello che conosci. Steam/Chiaki vanno poi configurati da \"Configurazione con un clic\"."}
	bua, err := s.buaList()
	if err != nil {
		out["bua_error"] = err.Error()
	}
	cmd := `echo "@@inst"; ls /userdata/system/add-ons 2>/dev/null; ls /userdata/roms/ports/*.sh 2>/dev/null | xargs -n1 basename 2>/dev/null; echo "@@base"; ls /userdata/system/services/*bua* /userdata/system/.local/share/applications 2>/dev/null | head -3; echo "@@store"; ` + ternary(refresh, "batocera-store refresh >/dev/null 2>&1; ", "") + `batocera-store list 2>/dev/null; echo "@@storeinst"; batocera-store list-installed 2>/dev/null`
	res, err := s.ssh.RunTimeout(cmd, 3*time.Minute)
	if err != nil {
		out["error"] = err.Error()
		out["bua"], out["official"] = bua, []any{}
		return out, nil
	}
	sec := cab.SplitSections(res.Stdout)
	inst := map[string]bool{}
	for _, l := range cab.Lines(sec["inst"]) {
		n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(l), ".sh"))
		inst[n] = true
	}
	for _, a := range bua {
		n := strings.ToLower(a["name"].(string))
		a["installed"] = inst[n] || inst[strings.ReplaceAll(n, "-", "")] || inst[n+"-ng"]
	}
	out["bua_base"] = strings.TrimSpace(sec["base"]) != ""
	storeInst := map[string]bool{}
	for _, l := range cab.Lines(sec["storeinst"]) {
		f := strings.Fields(l)
		if len(f) > 0 {
			storeInst[f[0]] = true
		}
	}
	var official []map[string]any
	for _, l := range cab.Lines(sec["store"]) {
		m := reStoreLine.FindStringSubmatch(l)
		if m == nil || strings.HasPrefix(m[1], "#") || strings.HasPrefix(strings.ToLower(l), "name") {
			continue
		}
		name := m[1]
		rest := m[2]
		cat := "Store"
		if strings.Contains(rest, "[") {
			if mm := regexp.MustCompile(`\[([^\]]+)\]`).FindStringSubmatch(rest); mm != nil {
				cat = mm[1]
			}
		}
		official = append(official, map[string]any{"name": name, "source": "official", "category": cat, "description": strings.TrimSpace(regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(rest, "")), "installed": storeInst[name] || strings.Contains(rest, "installed"), "recommended": false, "version": ""})
	}
	if official == nil {
		official = []map[string]any{}
	}
	if bua == nil {
		bua = []map[string]any{}
	}
	out["bua"], out["official"] = bua, official
	return out, nil
}

func ternary(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

func (s *Server) hAddonsAction(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action, Source, Name string
	}
	var raw struct {
		Action string `json:"action"`
		Source string `json:"source"`
		Name   string `json:"name"`
	}
	if err := readJSON(r, &raw); err != nil {
		return nil, err
	}
	req.Action, req.Source, req.Name = raw.Action, raw.Source, raw.Name
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(req.Name) {
		return nil, errors.New("nome non valido")
	}
	j := s.jobs.Start("Add-on "+req.Name, func(j *jobs.Job) (any, error) {
		var cmd string
		switch req.Source + "/" + req.Action {
		case "bua/install":
			j.SetProgress(-1, "Scarico ed eseguo lo script (serve internet sul cabinato)")
			list, _ := s.buaList()
			inst := ""
			for _, a := range list {
				if a["name"] == strings.ToLower(req.Name) {
					inst, _ = a["cmd"].(string)
				}
			}
			if inst == "" {
				return nil, fmt.Errorf("add-on %s non trovato nell'elenco BUA", req.Name)
			}
			cmd = fmt.Sprintf("export TERM=dumb; [ -d /userdata/system/add-ons ] || (%s); %s 2>&1 | tail -80", buaBase, inst)
		case "bua/remove":
			cmd = fmt.Sprintf("rm -rf /userdata/system/add-ons/%[1]s /userdata/roms/ports/%[1]s.sh /userdata/roms/ports/%[1]s /userdata/roms/ports/images/%[1]s* /userdata/system/services/%[1]s 2>/dev/null; batocera-services disable %[1]s 2>/dev/null; echo rimosso", req.Name)
		case "official/install":
			j.SetProgress(-1, "batocera-store install")
			cmd = "batocera-store install " + cab.ShellQuote(req.Name) + " 2>&1 | tail -40"
		case "official/remove":
			cmd = "batocera-store remove " + cab.ShellQuote(req.Name) + " 2>&1 | tail -20"
		default:
			return nil, errors.New("azione sconosciuta")
		}
		res, err := s.ssh.RunCtx(j.Context(), cmd, 40*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stdout) {
			j.Logf("%s", l)
		}
		_ = s.es.ReloadGames()
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

// ---------- Flathub ----------

var fhRecommended = []map[string]any{
	{"id": "com.valvesoftware.Steam", "name": "Steam", "summary": "Giochi PC. Dopo l'installazione usa \"Configurazione con un clic\" → Steam.", "recommended": "Consigliato: con la configurazione automatica si avvia dalla sezione Steam con la plancia."},
	{"id": "io.github.streetpea.Chiaki4deck", "name": "chiaki-ng", "summary": "PS5/PS4 Remote Play (versione moderna di Chiaki).", "recommended": "Alternativa a Chiaki di BUA, più aggiornata."},
	{"id": "com.moonlight_stream.Moonlight", "name": "Moonlight", "summary": "Streaming da un PC con GeForce Experience / Sunshine.", "recommended": "Per giocare i titoli del PC principale sul cabinato."},
	{"id": "com.heroicgameslauncher.hgl", "name": "Heroic Games Launcher", "summary": "Epic Games Store e GOG.", "recommended": ""},
	{"id": "net.lutris.Lutris", "name": "Lutris", "summary": "Giochi Windows via Wine.", "recommended": ""},
}

func (s *Server) hFlathub(w http.ResponseWriter, r *http.Request) (any, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := map[string]any{}
	installed := map[string]string{}
	res, err := s.ssh.RunTimeout("which flatpak >/dev/null 2>&1 && flatpak list --app --columns=application,version 2>/dev/null", 2*time.Minute)
	if err != nil {
		out["cabinet_error"] = err.Error()
	} else {
		for _, l := range cab.Lines(res.Stdout) {
			f := strings.Split(l, "\t")
			v := ""
			if len(f) > 1 {
				v = strings.TrimSpace(f[1])
			}
			installed[strings.TrimSpace(f[0])] = v
		}
	}
	var apps []map[string]any
	if q == "" {
		for _, a := range fhRecommended {
			m := map[string]any{}
			for k, v := range a {
				m[k] = v
			}
			if v, ok := installed[a["id"].(string)]; ok {
				m["installed"], m["version"] = true, v
			}
			apps = append(apps, m)
		}
		for id, v := range installed {
			found := false
			for _, a := range apps {
				if a["id"] == id {
					found = true
				}
			}
			if !found {
				apps = append(apps, map[string]any{"id": id, "name": id[strings.LastIndex(id, ".")+1:], "installed": true, "version": v, "summary": ""})
			}
		}
	} else {
		body := strings.NewReader(fmt.Sprintf(`{"query":%q,"locale":"it"}`, q))
		req, _ := http.NewRequest("POST", "https://flathub.org/api/v2/search", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "BatoceraCompanion")
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			out["error"] = err.Error()
		} else {
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			var sr struct {
				Hits []struct {
					AppID     string `json:"app_id"`
					Name      string `json:"name"`
					Summary   string `json:"summary"`
					Icon      string `json:"icon"`
					Developer string `json:"developer_name"`
					Installs  int    `json:"installs_last_month"`
				} `json:"hits"`
			}
			if err := json.Unmarshal(b, &sr); err != nil {
				out["error"] = "risposta Flathub non valida"
			}
			for _, h := range sr.Hits {
				m := map[string]any{"id": h.AppID, "name": h.Name, "summary": h.Summary, "icon": h.Icon, "developer": h.Developer, "installs": h.Installs}
				if v, ok := installed[h.AppID]; ok {
					m["installed"], m["version"] = true, v
				}
				apps = append(apps, m)
			}
		}
	}
	if apps == nil {
		apps = []map[string]any{}
	}
	out["apps"] = apps
	return out, nil
}

func (s *Server) hFlathubAction(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action string `json:"Action"`
		ID     string `json:"ID"`
		Name   string `json:"Name"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(req.ID) {
		return nil, errors.New("id non valido")
	}
	j := s.jobs.Start("Flathub "+req.Name, func(j *jobs.Job) (any, error) {
		var cmd string
		switch req.Action {
		case "install":
			j.SetProgress(-1, "flatpak install (il download può essere di centinaia di MB)")
			cmd = "flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo 2>/dev/null; flatpak install -y --noninteractive flathub " + req.ID + " 2>&1 | tail -30; echo \"@@rc=${PIPESTATUS[0]}\"; batocera-flatpak-update >/dev/null 2>&1; true"
		case "remove":
			cmd = "flatpak uninstall -y --noninteractive " + req.ID + " 2>&1 | tail -20; echo \"@@rc=${PIPESTATUS[0]}\"; batocera-flatpak-update >/dev/null 2>&1; true"
		default:
			return nil, errors.New("azione sconosciuta")
		}
		res, err := s.ssh.RunCtx(j.Context(), cmd, 60*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stdout) {
			if !strings.HasPrefix(l, "@@rc=") {
				j.Logf("%s", l)
			}
		}
		if !strings.Contains(res.Stdout, "@@rc=0") {
			return nil, errors.New("flatpak ha restituito un errore (vedi log)")
		}
		_ = s.es.ReloadGames()
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}
