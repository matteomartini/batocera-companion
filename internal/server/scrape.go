package server

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
	"batocompanion/internal/jobs"
	"batocompanion/internal/scrape"
)

func (s *Server) hGamesScrape(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		System    string   `json:"system"`
		IDs       []string `json:"ids"`
		Kinds     []string `json:"kinds"`
		Overwrite bool     `json:"overwrite"`
		Source    string   `json:"source"`
		Meta      bool     `json:"meta"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.System == "" {
		return nil, errors.New("sistema mancante")
	}
	games, err := s.es.Games(req.System)
	if err != nil {
		return nil, err
	}
	if len(req.IDs) > 0 {
		want := map[string]bool{}
		for _, id := range req.IDs {
			want[id] = true
		}
		var sel []cab.Game
		for _, g := range games {
			if want[g.ID] {
				sel = append(sel, g)
			}
		}
		games = sel
	}
	if len(games) == 0 {
		return nil, errors.New("nessun gioco")
	}
	if req.Source == "screenscraper" {
		cl, err := s.ssClient()
		if err != nil {
			return nil, err
		}
		j := s.jobs.Start("ScreenScraper", func(j *jobs.Job) (any, error) {
			return s.ssScrapeGames(j, cl, req.System, games, req.Kinds, req.Meta, req.Overwrite)
		})
		return map[string]any{"job": j.ID}, nil
	}
	j := s.jobs.Start("Scraper", func(j *jobs.Job) (any, error) {
		found := 0
		for i, g := range games {
			if j.Cancelled() {
				break
			}
			j.SetProgress(float64(i)/float64(len(games)), g.Name)
			got := false
			for _, kind := range req.Kinds {
				if !req.Overwrite && hasMedia(g, kind) {
					continue
				}
				data := s.libretroLookup(req.System, g, kind)
				if data == nil {
					continue
				}
				if err := s.setGameMediaNoReload(req.System, g, kind, data, "image/png"); err != nil {
					j.Logf("✗ %s (%s): %v", g.Name, kind, err)
					continue
				}
				got = true
			}
			if got {
				found++
				j.Logf("✓ %s", g.Name)
			} else {
				j.Logf("– %s: nessuna immagine trovata", g.Name)
			}
		}
		_ = s.es.ReloadGames()
		return map[string]any{"found": found}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func hasMedia(g cab.Game, kind string) bool {
	switch kind {
	case "thumbnail":
		return g.Thumbnail != ""
	case "image":
		return g.Image != ""
	case "marquee":
		return g.Marquee != ""
	case "video":
		return g.Video != ""
	}
	return cab.AsStr(g.Raw[kind]) != ""
}

// libretroLookup prova vari nomi: descrizione DAT (arcade), nome file, nome ES.
func (s *Server) libretroLookup(system string, g cab.Game, kind string) []byte {
	stem := strings.TrimSuffix(path.Base(g.Path), path.Ext(g.Path))
	var names []string
	if system == "mame" || system == "fbneo" || system == "neogeo" || strings.HasPrefix(system, "cps") {
		for _, id := range []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"} {
			db, _ := s.dats.Load(id)
			if db == nil {
				continue
			}
			if gm := db.Games[strings.ToLower(stem)]; gm != nil && gm.Desc != "" {
				names = append(names, gm.Desc)
				break
			}
		}
	}
	names = append(names, stem, g.Name, cleanName(stem))
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		b, err := scrape.FetchLibretro(system, kind, n)
		if err == nil && b != nil {
			return b
		}
	}
	return nil
}

// setGameMediaNoReload: scrive il file nella cartella del sistema e aggiorna gamelist.xml.
func (s *Server) setGameMediaNoReload(sys string, g cab.Game, kind string, data []byte, ct string) error {
	if err := s.es.UploadMedia(sys, g.ID, kind, data, ct); err == nil {
		return nil
	}
	ext := ".png"
	switch {
	case strings.Contains(ct, "jpeg"):
		ext = ".jpg"
	case strings.Contains(ct, "mp4") || strings.Contains(ct, "video"):
		ext = ".mp4"
	case strings.Contains(ct, "pdf"):
		ext = ".pdf"
	}
	sub := "images"
	suffix := map[string]string{"thumbnail": "-thumb", "image": "-image", "titleshot": "-titleshot", "marquee": "-marquee", "fanart": "-fanart", "boxback": "-boxback", "video": "-video", "manual": "-manual"}[kind]
	if kind == "video" {
		sub = "videos"
	} else if kind == "manual" {
		sub = "manuals"
	}
	stem := strings.TrimSuffix(path.Base(g.Path), path.Ext(g.Path))
	remote := path.Join("/userdata/roms", sys, sub, stem+suffix+ext)
	if err := s.ssh.UploadBytes(remote, data, 0o644); err != nil {
		return err
	}
	_, err := s.gamelistRun(sys, "set", g.Path, map[string]any{kind: "./" + sub + "/" + stem + suffix + ext})
	return err
}

// ---------- ScreenScraper ----------

func (s *Server) ssClient() (*scrape.SS, error) {
	c := s.cfg
	if c.SSUser == "" || c.SSPass == "" {
		return nil, errors.New("inserisci utente e password ScreenScraper in Impostazioni")
	}
	if c.SSDevID == "" || c.SSDevPass == "" {
		return nil, errors.New("servono anche le credenziali sviluppatore (devid/devpassword) di ScreenScraper: vedi Impostazioni → ScreenScraper")
	}
	regions := splitList(c.SSRegion)
	if len(regions) == 0 {
		regions = []string{"it", "eu", "wor", "us", "jp"}
	}
	langs := splitList(c.SSLang)
	if len(langs) == 0 {
		langs = []string{"it", "en"}
	}
	return &scrape.SS{DevID: c.SSDevID, DevPassword: c.SSDevPass, Soft: "BatoceraCompanion-" + s.Version, User: c.SSUser, Pass: c.SSPass, Regions: regions, Langs: langs}, nil
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) hScreenScraper(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		c := s.cfg
		out := map[string]any{"user": c.SSUser, "has_pass": c.SSPass != "", "devid": c.SSDevID, "has_devpass": c.SSDevPass != "", "region": c.SSRegion, "lang": c.SSLang, "ready": c.SSUser != "" && c.SSPass != "" && c.SSDevID != "" && c.SSDevPass != ""}
		if es, err := s.esSettingsGet([]string{"Scraper", "ScreenScraperUser"}); err == nil {
			out["es_source"] = es["Scraper"]
			out["es_user"] = es["ScreenScraperUser"]
		}
		return out, nil
	}
	var req struct {
		Action                                   string `json:"action"`
		User, Pass, DevID, DevPass, Region, Lang string
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "save":
		s.cfg.SSUser = strings.TrimSpace(req.User)
		if req.Pass != "" {
			s.cfg.SSPass = req.Pass
		}
		s.cfg.SSDevID = strings.TrimSpace(req.DevID)
		if req.DevPass != "" {
			s.cfg.SSDevPass = req.DevPass
		}
		s.cfg.SSRegion, s.cfg.SSLang = req.Region, req.Lang
		return nil, config.Save(s.cfg)
	case "test":
		cl, err := s.ssClient()
		if err != nil {
			return nil, err
		}
		info, err := cl.UserInfo()
		if err != nil {
			return nil, err
		}
		return map[string]any{"info": info}, nil
	case "es":
		if s.cfg.SSUser == "" || s.cfg.SSPass == "" {
			return nil, errors.New("salva prima utente e password")
		}
		if g, _ := s.es.RunningGame(); g != nil {
			return nil, errors.New("c'è un gioco in corso: chiudilo prima (EmulationStation va riavviato)")
		}
		u, p, src := s.cfg.SSUser, s.cfg.SSPass, "ScreenScraper"
		cmd := "mkdir -p " + backupDir + "; cp " + esSettings + " " + backupDir + "/es_settings.cfg." + time.Now().Format("20060102-150405") + "; " +
			esSettingsSetCmd("ScreenScraperUser", &u, false) + "; " + esSettingsSetCmd("ScreenScraperPass", &p, false) + "; " + esSettingsSetCmd("Scraper", &src, false) + "; batocera-es-swissknife --restart >/dev/null 2>&1 &"
		_, err := s.ssh.RunTimeout(cmd, time.Minute)
		return nil, err
	}
	return nil, fmt.Errorf("azione sconosciuta")
}

// ssScrapeGames: scraping con ScreenScraper (metadati + media) per una lista di giochi.
func (s *Server) ssScrapeGames(j *jobs.Job, cl *scrape.SS, sys string, games []cab.Game, kinds []string, meta, overwrite bool) (any, error) {
	sid, ok := scrape.SSSystemID[sys]
	if !ok {
		return nil, fmt.Errorf("sistema %s non mappato su ScreenScraper", sys)
	}
	// dimensione e CRC calcolati sul cabinato (file ≤ 64 MB)
	j.SetProgress(-1, "Calcolo dimensioni e CRC sul cabinato")
	var cmds []string
	for _, g := range games {
		p := g.Path
		if strings.HasSuffix(strings.ToLower(p), ".m3u") {
			p = `$(d=$(dirname ` + cab.ShellQuote(g.Path) + `); f=$(head -1 ` + cab.ShellQuote(g.Path) + ` | tr -d '\r'); echo "$d/$f")`
		}
		cmds = append(cmds, fmt.Sprintf(`f=%s; sz=$(stat -c %%s "$f" 2>/dev/null || echo 0); if [ "$sz" -gt 0 ] && [ "$sz" -le 67108864 ]; then c=$(python3 -c 'import zlib,sys; print("%%08X" %% (zlib.crc32(open(sys.argv[1],"rb").read()) & 0xffffffff))' "$f" 2>/dev/null); else c=""; fi; echo "$sz|$c"`, p))
	}
	res, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 10*time.Minute)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	found := 0
	for i, g := range games {
		if j.Cancelled() {
			break
		}
		j.SetProgress(float64(i)/float64(len(games)), g.Name)
		var size int64
		crc := ""
		if i < len(lines) {
			f := strings.Split(lines[i], "|")
			fmt.Sscanf(f[0], "%d", &size)
			if len(f) > 1 {
				crc = f[1]
			}
		}
		info, err := cl.GameInfo(sid, path.Base(g.Path), size, crc, "")
		if err != nil {
			if scrape.IsQuotaError(err) {
				j.Logf("✗ %v — mi fermo", err)
				return map[string]any{"found": found}, err
			}
			if scrape.IsNotFound(err) {
				j.Logf("– %s: non trovato", g.Name)
			} else {
				j.Logf("✗ %s: %v", g.Name, err)
			}
			time.Sleep(400 * time.Millisecond)
			continue
		}
		got := false
		if meta {
			m := map[string]any{}
			set := func(k, v string, cur string) {
				if v != "" && (overwrite || cur == "") {
					m[k] = v
				}
			}
			set("desc", info.Desc, g.Desc)
			set("genre", info.Genre, g.Genre)
			set("developer", info.Developer, g.Developer)
			set("publisher", info.Publisher, g.Publisher)
			set("players", info.Players, g.Players)
			set("rating", info.Rating, g.Rating)
			set("releasedate", info.ReleaseDate, g.ReleaseDate)
			if len(m) > 0 {
				if _, err := s.gamelistRun(sys, "set", g.Path, m); err != nil {
					j.Logf("✗ %s: metadati: %v", g.Name, err)
				} else {
					got = true
				}
			}
		}
		for _, kind := range kinds {
			if !overwrite && hasMedia(g, kind) {
				continue
			}
			md, ok := info.Media[kind]
			if !ok {
				continue
			}
			data, ct, err := cl.DownloadMedia(md.URL)
			if err != nil {
				if scrape.IsQuotaError(err) {
					j.Logf("✗ %v — mi fermo", err)
					return map[string]any{"found": found}, err
				}
				j.Logf("✗ %s (%s): %v", g.Name, kind, err)
				continue
			}
			if err := s.setGameMediaNoReload(sys, g, kind, data, ct); err != nil {
				j.Logf("✗ %s (%s): %v", g.Name, kind, err)
				continue
			}
			got = true
			time.Sleep(250 * time.Millisecond)
		}
		if got {
			found++
			j.Logf("✓ %s → %s", g.Name, info.Name)
		} else {
			j.Logf("– %s: niente di nuovo", g.Name)
		}
		time.Sleep(400 * time.Millisecond)
	}
	_ = s.es.ReloadGames()
	return map[string]any{"found": found}, nil
}
