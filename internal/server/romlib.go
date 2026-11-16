package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/checkup"
	"batocompanion/internal/dat"
	"batocompanion/internal/jobs"
)

// Libreria ROM: inventario completo del cabinato confrontato con i DAT.

type romlibState struct {
	mu     sync.Mutex
	report *RomlibReport
	at     time.Time
}

type RLFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // ok | incomplete | unknown | bad | hidden_ext | aux
	Name    string `json:"name,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Missing int    `json:"missing,omitempty"`
	Set     string `json:"set,omitempty"` // nome del set arcade riconosciuto
	DB      string `json:"db,omitempty"`  // DAT che lo riconosce
}

type RLSystem struct {
	Name       string   `json:"name"`
	Total      int      `json:"total"`
	OK         int      `json:"ok"`
	Incomplete int      `json:"incomplete"`
	Unknown    int      `json:"unknown"`
	Bad        int      `json:"bad"`
	HiddenExt  int      `json:"hidden_ext"`
	Verified   bool     `json:"verified"`
	Files      []RLFile `json:"files"`
}

type RLDup struct {
	A string `json:"a"` // primo trovato (da tenere per default)
	B string `json:"b"` // copia identica (stesso CRC)
}

type RLDisc struct {
	System string   `json:"system"`
	Title  string   `json:"title"`
	Files  []string `json:"files"`
	HasM3U bool     `json:"has_m3u"`
}

type RLOverride struct {
	System    string `json:"System"`
	File      string `json:"File"`
	Value     string `json:"Value"`
	Installed bool   `json:"installed"`
}

type RomlibReport struct {
	Summary    string       `json:"summary"`
	Systems    []RLSystem   `json:"systems"`
	Discs      []RLDisc     `json:"discs"`
	Overrides  []RLOverride `json:"overrides"`
	Duplicates []string     `json:"duplicates"`
	DupPairs   []RLDup      `json:"dup_pairs"`
}

type invFile struct {
	Path    string  `json:"path"`
	Size    int64   `json:"size"`
	Ext     string  `json:"ext"`
	CRC     *uint32 `json:"crc"`
	Entries [][]any `json:"entries"`
	Bad     string  `json:"bad"`
}

type inventory struct {
	Systems   map[string][]invFile `json:"systems"`
	Overrides []struct {
		System, File, Key, Value string
	} `json:"overrides"`
	Cores []string            `json:"cores"`
	M3U   map[string][]string `json:"m3u"`
}

var reDiscTitle = regexp.MustCompile(`(?i)^(.*?)[\s_]*[\(\[](?:disc|disk|cd)\s*0*(\d+)(?:\s*(?:of|/)\s*\d+)?[\)\]](.*)$`)

func (s *Server) hRomlib(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		s.romlib.mu.Lock()
		defer s.romlib.mu.Unlock()
		if s.romlib.report == nil {
			return map[string]any{"available": false}, nil
		}
		return map[string]any{"available": true, "report": s.romlib.report, "at": s.romlib.at, "ok_flags": s.romlibFlags()}, nil
	}
	script, err := s.asset("rom_inventory.py")
	if err != nil {
		return nil, err
	}
	j := s.jobs.Start("Libreria ROM", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Leggo tutti i file in /userdata/roms (CRC compresi): può richiedere qualche minuto")
		if err := s.ssh.UploadBytes(compDir+"/rom_inventory.py", script, 0o755); err != nil {
			return nil, err
		}
		res, err := s.ssh.RunCtx(j.Context(), "python3 "+compDir+"/rom_inventory.py /userdata/roms 2>/tmp/rom_inventory.log; tail -60 /tmp/rom_inventory.log >&2", 60*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stderr) {
			j.Logf("%s", l)
		}
		var inv inventory
		if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &inv); err != nil {
			return nil, fmt.Errorf("inventario non leggibile: %s", strings.TrimSpace(res.Stderr))
		}
		j.SetProgress(-1, "Confronto con i DAT")
		in, _ := s.collectInfo(false)
		rep := s.buildRomlib(&inv, in, j)
		s.romlib.mu.Lock()
		s.romlib.report, s.romlib.at = rep, time.Now()
		s.romlib.mu.Unlock()
		return map[string]any{"report": rep, "at": time.Now()}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) buildRomlib(inv *inventory, in *cab.Info, j *jobs.Job) *RomlibReport {
	rep := &RomlibReport{}
	// DAT arcade
	var arcade []*dat.DB
	for _, id := range []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"} {
		if db, _ := s.dats.Load(id); db != nil {
			arcade = append(arcade, db)
		}
	}
	crcIndex := map[uint32]string{} // per duplicati: crc → "sys/path"
	names := make([]string, 0, len(inv.Systems))
	for n := range inv.Systems {
		names = append(names, n)
	}
	sort.Strings(names)
	var discs = map[string]*RLDisc{}
	for _, sys := range names {
		files := inv.Systems[sys]
		rs := RLSystem{Name: sys, Files: []RLFile{}}
		var exts []string
		if in != nil {
			if es, ok := in.Systems[sys]; ok {
				exts = es.Extensions
			}
		}
		isArcade := sys == "mame" || sys == "fbneo" || sys == "neogeo" || strings.HasPrefix(sys, "cps")
		var consoleDBs []*dat.DB
		for _, e := range dat.ForSystem(sys) {
			if e.Kind == "console" {
				if db, _ := s.dats.Load(e.ID); db != nil {
					consoleDBs = append(consoleDBs, db)
				}
			}
		}
		rs.Verified = (isArcade && len(arcade) > 0) || len(consoleDBs) > 0
		for _, f := range files {
			ext := strings.ToLower(f.Ext)
			if ext == "m3u" || ext == "cue" || ext == "gdi" || ext == "ccd" || ext == "sub" || ext == "txt" {
				rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "aux"})
				continue
			}
			rs.Total++
			if len(exts) > 0 && ext != "" && !containsStr(exts, ext) && ext != "bin" && ext != "img" {
				rs.HiddenExt++
				rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "hidden_ext", Detail: "." + ext + " non è tra le estensioni di " + sys})
				continue
			}
			if f.Bad != "" {
				rs.Bad++
				rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "bad", Detail: f.Bad})
				continue
			}
			// multi-disco
			stem := strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
			if m := reDiscTitle.FindStringSubmatch(stem); m != nil && (ext == "chd" || ext == "cue" || ext == "iso" || ext == "pbp" || ext == "cdi" || ext == "gdi") {
				key := sys + "|" + strings.TrimSpace(m[1]+m[3])
				d := discs[key]
				if d == nil {
					d = &RLDisc{System: sys, Title: strings.TrimSpace(m[1] + m[3])}
					discs[key] = d
				}
				d.Files = append(d.Files, f.Path)
			}
			if isArcade && len(arcade) > 0 && ext == "zip" {
				st := s.checkArcadeZip(arcade, stem, f)
				rs.Files = append(rs.Files, st)
				switch st.Status {
				case "ok":
					rs.OK++
				case "incomplete":
					rs.Incomplete++
				case "unknown":
					rs.Unknown++
				}
				continue
			}
			if len(consoleDBs) > 0 {
				found := false
				if f.CRC != nil {
					if p, dup := crcIndex[*f.CRC]; dup && *f.CRC != 0 {
						rep.Duplicates = append(rep.Duplicates, sys+"/"+f.Path+" = "+p)
						rep.DupPairs = append(rep.DupPairs, RLDup{A: p, B: sys + "/" + f.Path})
					} else if *f.CRC != 0 {
						crcIndex[*f.CRC] = sys + "/" + f.Path
					}
					for _, db := range consoleDBs {
						if ms := db.ByCRC(*f.CRC); len(ms) > 0 {
							rs.OK++
							rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "ok", Name: ms[0].Game.Name})
							found = true
							break
						}
					}
				}
				if !found && f.CRC == nil {
					// immagini CD: per nome
					for _, db := range consoleDBs {
						if g := db.ByName(stem); g != nil {
							rs.OK++
							rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "ok", Name: g.Name})
							found = true
							break
						}
					}
				}
				if !found {
					rs.Unknown++
					rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "unknown", Detail: "non nel DAT (dump diverso, hack o traduzione?)"})
				}
				continue
			}
			rs.Files = append(rs.Files, RLFile{Path: f.Path, Status: "ok"})
			rs.OK++
		}
		rep.Systems = append(rep.Systems, rs)
	}
	for _, d := range discs {
		if len(d.Files) < 2 {
			continue
		}
		sort.Strings(d.Files)
		for k, lines := range inv.M3U {
			if strings.HasPrefix(k, d.System+"/") {
				for _, l := range lines {
					if containsStr(d.Files, l) {
						d.HasM3U = true
					}
				}
			}
		}
		rep.Discs = append(rep.Discs, *d)
	}
	sort.Slice(rep.Discs, func(i, k int) bool {
		return rep.Discs[i].System+rep.Discs[i].Title < rep.Discs[k].System+rep.Discs[k].Title
	})
	for _, o := range inv.Overrides {
		if o.Key != "core" {
			continue
		}
		rep.Overrides = append(rep.Overrides, RLOverride{System: o.System, File: o.File, Value: o.Value, Installed: containsStr(inv.Cores, o.Value) || o.Value == "mame" || len(inv.Cores) == 0})
	}
	tot, ok, inc, unk := 0, 0, 0, 0
	for _, sy := range rep.Systems {
		tot += sy.Total
		ok += sy.OK
		inc += sy.Incomplete
		unk += sy.Unknown
	}
	rep.Summary = fmt.Sprintf("%d file: %d riconosciuti, %d set incompleti, %d sconosciuti, %d giochi multi-disco", tot, ok, inc, unk, len(rep.Discs))
	if rep.Discs == nil {
		rep.Discs = []RLDisc{}
	}
	if rep.Overrides == nil {
		rep.Overrides = []RLOverride{}
	}
	if rep.Duplicates == nil {
		rep.Duplicates = []string{}
	}
	return rep
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// checkArcadeZip verifica uno zip arcade contro i DAT (basta che un core lo accetti).
func (s *Server) checkArcadeZip(dbs []*dat.DB, name string, f invFile) RLFile {
	have := map[uint32]bool{}
	for _, e := range f.Entries {
		if len(e) >= 2 {
			if c, ok := e[1].(float64); ok {
				have[uint32(c)] = true
			}
		}
	}
	best := RLFile{Path: f.Path, Status: "unknown", Detail: "nome non presente nei DAT"}
	bestMissing := 1 << 30
	for _, db := range dbs {
		g := db.Games[strings.ToLower(name)]
		if g == nil {
			continue
		}
		if g.IsBios || g.IsDevice {
			return RLFile{Path: f.Path, Status: "ok", Name: g.Desc, Set: g.Name, DB: db.ID}
		}
		// set split: le ROM proprie del set (quelle con merge stanno nel parent)
		missing := 0
		total := 0
		for _, r := range g.Roms {
			if r.CRC == 0 || r.Opt {
				continue
			}
			if r.Merge != "" && g.CloneOf != "" {
				continue
			}
			total++
			if !have[r.CRC] {
				missing++
			}
		}
		if missing == 0 {
			return RLFile{Path: f.Path, Status: "ok", Name: g.Desc, Detail: db.ID, Set: g.Name, DB: db.ID}
		}
		if missing < bestMissing {
			bestMissing = missing
			best = RLFile{Path: f.Path, Status: "incomplete", Name: g.Desc, Missing: missing, Detail: fmt.Sprintf("%s: %d/%d file", db.ID, total-missing, total), Set: g.Name, DB: db.ID}
		}
	}
	return best
}

// correzioni
func (s *Server) hRomlibFix(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	s.romlib.mu.Lock()
	rep := s.romlib.report
	s.romlib.mu.Unlock()
	if rep == nil {
		return nil, fmt.Errorf("esegui prima l'analisi")
	}
	j := s.jobs.Start("Correzione libreria", func(j *jobs.Job) (any, error) {
		switch req.Action {
		case "multidisc":
			seen := map[string]bool{}
			for _, d := range rep.Discs {
				if seen[d.System] {
					continue
				}
				seen[d.System] = true
				j.SetProgress(-1, d.System)
				res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(multidiscPy)+" "+cab.ShellQuote(d.System), 5*time.Minute)
				if err != nil {
					return nil, err
				}
				var out struct {
					Groups  int      `json:"groups"`
					Created int      `json:"created"`
					Hidden  []string `json:"hidden"`
				}
				_ = json.Unmarshal([]byte(lastLine(res.Stdout)), &out)
				for _, h := range out.Hidden {
					_, _ = s.gamelistRun(d.System, "set", h, map[string]any{"hidden": "true"})
				}
				j.Logf("%s: %d giochi, %d .m3u creati, %d dischi nascosti", d.System, out.Groups, out.Created, len(out.Hidden))
			}
			_ = s.es.ReloadGames()
		case "cores":
			var cmds []string
			cmds = append(cmds, "mkdir -p "+backupDir+"; cp /userdata/system/batocera.conf "+backupDir+"/batocera.conf."+time.Now().Format("20060102-150405"))
			n := 0
			for _, o := range rep.Overrides {
				if o.Installed {
					continue
				}
				key := fmt.Sprintf(`%s["%s"].core`, o.System, o.File)
				esc := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `.`, `\.`, `"`, `\"`, `/`, `\/`).Replace(key)
				cmds = append(cmds, fmt.Sprintf("sed -i '/^%s=/d' /userdata/system/batocera.conf", esc))
				j.Logf("tolto %s=%s", key, o.Value)
				n++
			}
			if _, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 2*time.Minute); err != nil {
				return nil, err
			}
			j.Logf("%d impostazioni rimosse", n)
		case "bios":
			return s.romlibCopyBios(j, rep)
		default:
			return nil, fmt.Errorf("azione sconosciuta")
		}
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

// romlibCopyBios: per ogni cartella arcade, trova i BIOS (neogeo.zip, pgm.zip, …) richiesti
// dai set presenti e mancanti nella cartella, e li copia da /userdata/bios o da un'altra
// cartella ROM dove già ci sono. I BIOS copiati vengono nascosti nella lista di EmulationStation.
func (s *Server) romlibCopyBios(j *jobs.Job, rep *RomlibReport) (any, error) {
	dbs := map[string]*dat.DB{}
	for _, id := range []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"} {
		if db, _ := s.dats.Load(id); db != nil {
			dbs[id] = db
		}
	}
	type need struct{ sys, bios, desc string }
	var needs []need
	for _, sy := range rep.Systems {
		have := map[string]bool{}
		for _, f := range sy.Files {
			have[strings.ToLower(strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path)))] = true
		}
		req := map[string]string{}
		for _, f := range sy.Files {
			if f.Set == "" || dbs[f.DB] == nil {
				continue
			}
			db := dbs[f.DB]
			// risali la catena romof: clone → parent → bios. Un romof che non è il
			// parent (cloneof) è un BIOS, anche nei DAT vecchi senza attributo isbios.
			seen := map[string]bool{}
			cur := f.Set
			for cur != "" && !seen[cur] {
				seen[cur] = true
				g := db.Games[cur]
				if g == nil {
					break
				}
				next := g.RomOf
				if next != "" && next != g.CloneOf {
					if b := db.Games[next]; b != nil {
						req[next] = b.Desc
					}
				}
				cur = next
			}
		}
		for b, d := range req {
			if !have[b] {
				needs = append(needs, need{sy.Name, b, d})
			}
		}
	}
	if len(needs) == 0 {
		j.Logf("Tutti i BIOS richiesti dai set presenti sono già nelle cartelle ROM.")
		return map[string]any{"copied": 0}, nil
	}
	var script []string
	for _, n := range needs {
		script = append(script, fmt.Sprintf(`b=%s; dst=/userdata/roms/%s/$b.zip; src=""; for c in /userdata/bios/$b.zip /userdata/bios/*/$b.zip /userdata/roms/*/$b.zip; do [ -f "$c" ] && [ "$c" != "$dst" ] && { src="$c"; break; }; done; if [ -n "$src" ]; then cp -f "$src" "$dst" && echo "OK %s $b $src"; else echo "MISS %s $b"; fi`, cab.ShellQuote(n.bios), n.sys, n.sys, n.sys))
	}
	j.SetProgress(-1, fmt.Sprintf("Copio %d BIOS", len(needs)))
	res, err := s.ssh.RunCtx(j.Context(), strings.Join(script, "\n"), 10*time.Minute)
	if err != nil {
		return nil, err
	}
	copied, missing := 0, 0
	desc := map[string]string{}
	for _, n := range needs {
		desc[n.bios] = n.desc
	}
	touched := map[string]bool{}
	for _, l := range cab.Lines(res.Stdout) {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		switch f[0] {
		case "OK":
			copied++
			touched[f[1]] = true
			j.Logf("✓ %s/%s.zip (%s) ← %s", f[1], f[2], desc[f[2]], f[3])
			_, _ = s.gamelistRun(f[1], "set", "./"+f[2]+".zip", map[string]any{"hidden": "true"})
		case "MISS":
			missing++
			j.Logf("✗ %s: manca %s.zip (%s): caricalo da BIOS → Carica, poi ripeti", f[1], f[2], desc[f[2]])
		}
	}
	if len(touched) > 0 {
		_ = s.es.ReloadGames()
	}
	j.Logf("%d BIOS copiati, %d non trovati da nessuna parte", copied, missing)
	return map[string]any{"copied": copied, "missing": missing}, nil
}

// romLibFindings: esiti della libreria da inserire nel check-up.
func (s *Server) romLibFindings() []checkup.Finding {
	s.romlib.mu.Lock()
	rep := s.romlib.report
	s.romlib.mu.Unlock()
	if rep == nil {
		return nil
	}
	var out []checkup.Finding
	okFlags := map[string]bool{}
	for _, p := range s.romlibFlags() {
		okFlags[p] = true
	}
	for _, sy := range rep.Systems {
		if sy.Total == 0 {
			continue
		}
		// set incompleti, esclusi quelli provati a mano e segnati "funziona"
		var ex []string
		inc, flagged := 0, 0
		for _, f := range sy.Files {
			if f.Status != "incomplete" {
				continue
			}
			if okFlags["/userdata/roms/"+sy.Name+"/"+f.Path] {
				flagged++
				continue
			}
			inc++
			if len(ex) < 6 {
				ex = append(ex, f.Path+" ("+f.Detail+")")
			}
		}
		if inc > 0 {
			t := fmt.Sprintf("%s: %d set incompleti", sy.Name, inc)
			if flagged > 0 {
				t += fmt.Sprintf(" (altri %d provati e funzionanti)", flagged)
			}
			out = append(out, checkup.Finding{ID: "rom.inc." + sy.Name, Area: "rom", Level: "warn", Title: t, Detail: strings.Join(ex, "\n"), Manual: "Provali dalla Libreria ROM → dettagli (▶ Prova): se girano segnali ✓ Funziona, altrimenti rimpiazzali con set della versione giusta (Aggiungi ROM li verifica prima di copiarli; per MAME usa Giochi → Set MAME)."})
		}
		if sy.Unknown > 0 && sy.Verified {
			out = append(out, checkup.Finding{ID: "rom.unk." + sy.Name, Area: "rom", Level: "info", Title: fmt.Sprintf("%s: %d file non riconosciuti dai DAT", sy.Name, sy.Unknown), Detail: "Dump diversi, hack o traduzioni: non è un errore, ma lo scraper potrebbe non trovarli."})
		}
		if sy.HiddenExt > 0 {
			out = append(out, checkup.Finding{ID: "rom.ext." + sy.Name, Area: "rom", Level: "warn", Title: fmt.Sprintf("%s: %d file con estensione ignorata da EmulationStation", sy.Name, sy.HiddenExt), Detail: "Non compaiono in lista. Rinominali o convertili nel formato giusto."})
		}
	}
	noM3U := 0
	for _, d := range rep.Discs {
		if !d.HasM3U {
			noM3U++
		}
	}
	if noM3U > 0 {
		out = append(out, checkup.Finding{ID: "rom.discs", Area: "rom", Level: "info", Title: fmt.Sprintf("%d giochi multi-disco con i CD separati in lista", noM3U), Manual: "Check-up → Libreria ROM → \"Sistema i multi-disco\" crea gli .m3u e nasconde i singoli dischi."})
	}
	bad := 0
	for _, o := range rep.Overrides {
		if !o.Installed {
			bad++
		}
	}
	if bad > 0 {
		out = append(out, checkup.Finding{ID: "rom.badcore", Area: "rom", Level: "error", Title: fmt.Sprintf("%d giochi impostati su core non installati", bad), Detail: "All'avvio Batocera cerca un core che non c'è: il gioco non parte.", Manual: "Check-up → Libreria ROM → \"Togli i core non installati\"."})
	}
	out = append(out, checkup.Finding{ID: "rom.total", Area: "rom", Level: "ok", Title: rep.Summary})
	return out
}

// Flag "funziona": giochi provati a mano e tenuti anche se il set risulta incompleto.
func (s *Server) romlibFlagsFile() string { return filepath.Join(s.cfg.Dir(), "romlib-ok.json") }

func (s *Server) romlibFlags() []string {
	var out []string
	if b, err := os.ReadFile(s.romlibFlagsFile()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func (s *Server) hRomlibFlag(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return map[string]any{"ok_flags": s.romlibFlags()}, nil
	}
	var req struct {
		Path string `json:"path"`
		OK   bool   `json:"ok"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("percorso mancante")
	}
	cur := s.romlibFlags()
	var out []string
	for _, p := range cur {
		if p != req.Path {
			out = append(out, p)
		}
	}
	if req.OK {
		out = append(out, req.Path)
	}
	if out == nil {
		out = []string{}
	}
	b, _ := json.MarshalIndent(out, "", " ")
	if err := os.WriteFile(s.romlibFlagsFile(), b, 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"ok_flags": out}, nil
}
