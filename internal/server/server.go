// Package server: API HTTP e interfaccia web del Companion.
package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
	"batocompanion/internal/dat"
	"batocompanion/internal/jobs"
	"batocompanion/internal/roms"
)

type Server struct {
	Version string
	cfg     *config.Config
	ssh     *cab.SSH
	es      *cab.ES
	jobs    *jobs.Manager
	dats    *dat.Manager
	web     fs.FS
	assets  fs.FS
	payload []byte

	mu       sync.Mutex
	info     *cab.Info
	infoAt   time.Time
	infoGen  uint64 // incrementato a ogni invalidazione: scarta raccolte iniziate prima
	sessions map[string]*roms.Session
	lastPing time.Time
	quit     chan struct{}
	lanSrv   *http.Server
	lanErr   string
	keepOpen bool
	tmpDir   string
	inputMu  sync.Mutex
	input    *inputProc
	live     *liveState
	romlib   *romlibState
	lastChk  any
}

type ctxKey int

const (
	esSettings = "/userdata/system/configs/emulationstation/es_settings.cfg"
	backupDir  = "/userdata/system/companion_backup"
	trashDir   = "/userdata/system/companion_trash"
	compDir    = "/userdata/system/companion"
)

func New(version string, cfg *config.Config, web, assets embed.FS, payload []byte, keepOpen bool) *Server {
	w, _ := fs.Sub(web, "web")
	a, _ := fs.Sub(assets, "assets")
	s := &Server{Version: version, cfg: cfg, web: w, assets: a, payload: payload, keepOpen: keepOpen, jobs: jobs.NewManager(), sessions: map[string]*roms.Session{}, quit: make(chan struct{}), lastPing: time.Now()}
	s.ssh = cab.NewSSH(cfg.Host, cfg.RemoteHost, cfg.SSHPort, cfg.SSHUser, cfg.SSHPass)
	s.es = cab.NewES(cfg.Host, cfg.ESPort)
	s.dats = dat.NewManager(filepath.Join(cfg.Dir(), "dats"))
	s.tmpDir = filepath.Join(os.TempDir(), "batocompanion")
	_ = os.MkdirAll(s.tmpDir, 0o755)
	if cfg.LinkKey == "" {
		cfg.LinkKey = randHex(16)
		_ = config.Save(cfg)
	}
	if cfg.Sessions == nil {
		cfg.Sessions = map[string]string{}
	}
	s.live = &liveState{}
	s.romlib = &romlibState{}
	return s
}

func randHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }

// Quit: chiuso quando l'app deve terminare.
func (s *Server) Quit() <-chan struct{} { return s.quit }

func (s *Server) requestQuit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
}

// WatchHeartbeat chiude l'app se la finestra non manda più ping (solo modalità finestra).
func (s *Server) WatchHeartbeat() {
	if s.keepOpen || s.cfg.ServerMode {
		return
	}
	for {
		time.Sleep(5 * time.Second)
		s.mu.Lock()
		idle := time.Since(s.lastPing)
		s.mu.Unlock()
		if idle > 45*time.Second {
			log.Println("nessun ping dalla finestra: chiudo")
			s.requestQuit()
			return
		}
	}
}

// ---------- routing ----------

type apiFunc func(w http.ResponseWriter, r *http.Request) (any, error)

func (s *Server) Handler(local bool) http.Handler {
	mux := http.NewServeMux()
	api := func(name string, fn apiFunc) {
		mux.HandleFunc("/api/"+name, func(w http.ResponseWriter, r *http.Request) {
			res, err := fn(w, r)
			if err != nil {
				writeJSON(w, 200, map[string]any{"error": err.Error()})
				return
			}
			if res == nil {
				res = map[string]any{"ok": true}
			}
			writeJSON(w, 200, res)
		})
	}
	api("ping", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.mu.Lock()
		s.lastPing = time.Now()
		s.mu.Unlock()
		return map[string]any{"ok": true, "version": s.Version, "server": s.cfg.ServerMode}, nil
	})
	api("quit", func(w http.ResponseWriter, r *http.Request) (any, error) {
		go func() { time.Sleep(300 * time.Millisecond); s.requestQuit() }()
		return nil, nil
	})
	api("status", s.hStatus)
	api("info", s.hInfo)
	api("cmd", s.hCmd)
	api("open", s.hOpen)
	api("config", s.hConfig)
	api("lan", s.hLan)
	api("lan/logout_all", s.hLanLogoutAll)
	api("login", s.hLogin)
	api("job", s.hJob)
	api("job/cancel", s.hJobCancel)
	api("pick", s.hPick)
	api("shell", s.hShell)
	api("systems", s.hSystems)
	api("games", s.hGames)
	api("games/delete", s.hGamesDelete)
	api("games/trash", s.hGamesTrash)
	api("games/meta", s.hGamesMeta)
	api("games/scrape", s.hGamesScrape)
	api("games/rename", s.hGamesRename)
	api("games/multidisc", s.hGamesMultidisc)
	api("games/mame-rebuild", s.hGamesMame)
	api("roms/analyze", s.hRomsAnalyze)
	api("roms/set", s.hRomsSet)
	api("roms/apply", s.hRomsApply)
	api("dats", s.hDats)
	api("dats/download", s.hDatsDownload)
	api("dats/generate", s.hDatsGenerate)
	api("dats/import", s.hDatsImport)
	api("bios/upload", s.hBiosUpload)
	api("checkup", s.hCheckup)
	api("checkup/fix", s.hCheckupFix)
	api("romlib", s.hRomlib)
	api("romlib/fix", s.hRomlibFix)
	api("stutter", s.hStutter)
	api("stress", s.hStress)
	api("reconnect", s.hReconnect)
	api("perf", s.hPerf)
	api("perf/games", s.hPerfGames)
	api("emu", s.hEmu)
	api("look", s.hLook)
	api("look/splash", s.hLookSplash)
	api("look/splash/reset", s.hLookSplashReset)
	api("look/theme", s.hLookTheme)
	api("stock", s.hStock)
	api("addons", s.hAddons)
	api("addons/action", s.hAddonsAction)
	api("appsetup", s.hAppSetup)
	api("flathub", s.hFlathub)
	api("flathub/action", s.hFlathubAction)
	api("screenscraper", s.hScreenScraper)
	api("sinden", s.hSinden)
	api("backup", s.hBackup)
	api("backup/usb", s.hBackupUSB)
	api("record", s.hRecord)
	api("input", s.hInput)
	api("input/type", s.hInputType)
	api("clip", s.hClip)
	api("cabinet", s.hCabinet)
	api("cabinet/install", s.hCabinetInstall)
	api("cabinet/remove", s.hCabinetRemove)
	api("ha", s.hHA)
	api("ha/install", s.hHAInstall)
	api("ha/remove", s.hHARemove)
	api("volbtn", s.hVolbtn)
	api("volbtn/detect", s.hVolbtnDetect)
	api("volbtn/install", s.hVolbtnInstall)
	api("volbtn/remove", s.hVolbtnRemove)
	api("winekeys", s.hWinekeys)
	api("tailscale", s.hTailscale)
	api("tailscale/action", s.hTailscaleAction)
	api("plancia", s.hPlancia)
	api("updates", s.hUpdates)
	mux.HandleFunc("/api/upload", s.hUpload)
	mux.HandleFunc("/api/games/media/set", s.hGamesMediaSet)
	mux.HandleFunc("/media", s.hMedia)
	mux.HandleFunc("/live", s.hLive)
	mux.HandleFunc("/live/cursor", s.hLiveCursor)
	mux.HandleFunc("/plancia/events", s.hPlanciaEvents)
	mux.HandleFunc("/report.pdf", s.hReport)
	mux.Handle("/shots/", http.StripPrefix("/shots/", http.FileServer(http.Dir(s.cfg.ScreenshotDir))))
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return s.auth(local, mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("richiesta non valida: %v", err)
	}
	return nil
}

// ---------- autenticazione (PIN per l'accesso dalla rete) ----------

func isLoopback(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) authorized(r *http.Request) bool {
	if k := r.URL.Query().Get("key"); k != "" && k == s.cfg.LinkKey {
		return true
	}
	c, err := r.Cookie("bc_session")
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.cfg.Sessions[c.Value]
	return ok
}

func (s *Server) auth(local bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if local && isLoopback(r) {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p == "/login.html" || p == "/api/login" || p == "/api/ping" || p == "/app.css" || p == "/icon.svg" || p == "/icon-192.png" || p == "/icon-512.png" || p == "/manifest.json" {
			next.ServeHTTP(w, r)
			return
		}
		if s.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			writeJSON(w, 401, map[string]any{"error": "accesso non autorizzato", "login": true})
			return
		}
		http.Redirect(w, r, "/login.html", http.StatusFound)
	})
}

func (s *Server) hLogin(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Pin string `json:"pin"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	pin := s.cfg.LanPin
	if pin == "" || strings.TrimSpace(req.Pin) != pin {
		time.Sleep(800 * time.Millisecond)
		return nil, errors.New("PIN errato")
	}
	tok := randHex(24)
	s.mu.Lock()
	s.cfg.Sessions[tok] = r.UserAgent() + " " + time.Now().Format("2006-01-02")
	s.mu.Unlock()
	_ = config.Save(s.cfg)
	http.SetCookie(w, &http.Cookie{Name: "bc_session", Value: tok, Path: "/", MaxAge: 10 * 365 * 24 * 3600, SameSite: http.SameSiteLaxMode, HttpOnly: true})
	return map[string]any{"ok": true}, nil
}

func (s *Server) hLanLogoutAll(w http.ResponseWriter, r *http.Request) (any, error) {
	s.mu.Lock()
	s.cfg.Sessions = map[string]string{}
	s.cfg.LinkKey = randHex(16)
	s.mu.Unlock()
	return nil, config.Save(s.cfg)
}

// ---------- LAN ----------

func localIPs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

func (s *Server) StartLAN() {
	s.mu.Lock()
	if s.lanSrv != nil {
		old := s.lanSrv
		s.lanSrv = nil
		s.mu.Unlock()
		_ = old.Close()
		s.mu.Lock()
	}
	s.lanErr = ""
	if !s.cfg.LanEnabled || s.cfg.ServerMode {
		s.mu.Unlock()
		return
	}
	if len(s.cfg.LanPin) < 4 {
		s.lanErr = "PIN di almeno 4 cifre"
		s.mu.Unlock()
		return
	}
	srv := &http.Server{Addr: fmt.Sprintf(":%d", s.cfg.LanPort), Handler: s.Handler(false)}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		s.lanErr = err.Error()
		s.mu.Unlock()
		return
	}
	s.lanSrv = srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
}

func (s *Server) hLan(w http.ResponseWriter, r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var urls []string
	if s.lanSrv != nil {
		for _, ip := range localIPs() {
			urls = append(urls, fmt.Sprintf("http://%s:%d", ip, s.cfg.LanPort))
		}
	}
	return map[string]any{"active": s.lanSrv != nil, "urls": urls, "error": s.lanErr}, nil
}

// ---------- config ----------

func (s *Server) hConfig(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		var c config.Config
		if err := readJSON(r, &c); err != nil {
			return nil, err
		}
		s.mu.Lock()
		cur := s.cfg
		cur.Host, cur.RemoteHost, cur.SSHUser, cur.SSHPass, cur.SSHPort, cur.ESPort = strings.TrimSpace(c.Host), strings.TrimSpace(c.RemoteHost), c.SSHUser, c.SSHPass, c.SSHPort, c.ESPort
		cur.RomsPath, cur.BiosPath, cur.ScreenshotDir = c.RomsPath, c.BiosPath, c.ScreenshotDir
		cur.ArcadePref, cur.ConvertToCHD = c.ArcadePref, c.ConvertToCHD
		cur.LanEnabled, cur.LanPin, cur.LanPort = c.LanEnabled, strings.TrimSpace(c.LanPin), c.LanPort
		if cur.SSHPort == 0 {
			cur.SSHPort = 22
		}
		if cur.ESPort == 0 {
			cur.ESPort = 1234
		}
		s.info = nil
		s.infoGen++
		s.mu.Unlock()
		s.ssh.Configure(cur.Host, cur.RemoteHost, cur.SSHPort, cur.SSHUser, cur.SSHPass)
		s.es.Configure(cur.Host, cur.ESPort)
		if err := config.Save(cur); err != nil {
			return nil, err
		}
		s.StartLAN()
		s.mu.Lock()
		le := s.lanErr
		s.mu.Unlock()
		return map[string]any{"ok": true, "lan_error": le}, nil
	}
	return map[string]any{"config": s.cfg, "data_dir": s.cfg.Dir()}, nil
}

// ---------- stato ----------

func (s *Server) hStatus(w http.ResponseWriter, r *http.Request) (any, error) {
	out := map[string]any{"host": s.cfg.Host, "ssh_online": false, "es_online": false, "via_remote": s.ssh.ViaRemote()}
	res, err := s.ssh.RunTimeout(`echo "@@h"; hostname; echo "@@v"; cat /usr/share/batocera/batocera.version 2>/dev/null | head -1; echo "@@u"; cat /proc/uptime; echo "@@d"; df -B1 /userdata | tail -1; echo "@@t"; for z in /sys/class/thermal/thermal_zone*; do echo "$(cat $z/type) $(cat $z/temp)"; done; echo "@@i"; ip -4 -o addr | grep -v ' lo ' | awk '{print $4}' | cut -d/ -f1`, 15*time.Second)
	if err != nil {
		out["ssh_error"] = err.Error()
		return out, nil
	}
	sec := cab.SplitSections(res.Stdout)
	out["ssh_online"] = true
	out["hostname"] = strings.TrimSpace(sec["h"])
	if f := strings.Fields(sec["v"]); len(f) > 0 {
		out["version"] = f[0]
	}
	if f := strings.Fields(sec["u"]); len(f) > 0 {
		var up float64
		fmt.Sscanf(f[0], "%f", &up)
		out["uptime"] = fmt.Sprintf("%dh %02dm", int(up)/3600, (int(up)%3600)/60)
	}
	if f := strings.Fields(sec["d"]); len(f) >= 5 {
		var tot, free float64
		fmt.Sscanf(f[1], "%f", &tot)
		fmt.Sscanf(f[3], "%f", &free)
		out["disk_free"] = fmt.Sprintf("%.1f GB", free/1e9)
		if tot > 0 {
			out["disk_pct"] = int((tot - free) / tot * 100)
		}
	}
	for _, l := range cab.Lines(sec["t"]) {
		f := strings.Fields(l)
		if len(f) == 2 && (strings.Contains(f[0], "x86_pkg") || out["temp_c"] == nil) {
			var t float64
			fmt.Sscanf(f[1], "%f", &t)
			out["temp_c"] = t / 1000
		}
	}
	out["ips"] = cab.Lines(sec["i"])
	s.mu.Lock()
	if s.info != nil {
		out["rom_total"] = s.info.RomTotal
	}
	s.mu.Unlock()
	if s.es.Online() {
		out["es_online"] = true
		if g, err := s.es.RunningGame(); err == nil && g != nil {
			out["running"] = map[string]any{"name": g.Name, "system": g.System, "path": g.Path}
		}
	}
	return out, nil
}

// collectInfo: raccolta completa con cache di 2 minuti.
func (s *Server) collectInfo(refresh bool) (*cab.Info, error) {
	s.mu.Lock()
	if !refresh && s.info != nil && time.Since(s.infoAt) < 2*time.Minute {
		in := s.info
		s.mu.Unlock()
		return in, nil
	}
	gen := s.infoGen
	s.mu.Unlock()
	in, err := s.ssh.CollectInfo()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.infoGen == gen {
		s.info, s.infoAt = in, time.Now()
	}
	s.mu.Unlock()
	return in, nil
}

// invalidateInfo: dopo una scrittura sul cabinato. Le raccolte già in corso,
// iniziate prima, non finiranno in cache.
func (s *Server) invalidateInfo() {
	s.mu.Lock()
	s.info = nil
	s.infoGen++
	s.mu.Unlock()
}

func (s *Server) hInfo(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.collectInfo(r.URL.Query().Get("refresh") == "1")
}

// ---------- job ----------

func (s *Server) hJob(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Get(r.URL.Query().Get("id"))
	if j == nil {
		return nil, errors.New("job non trovato")
	}
	return j.Snapshot(), nil
}

func (s *Server) hJobCancel(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if !s.jobs.Cancel(req.ID) {
		return nil, errors.New("job non trovato")
	}
	return nil, nil
}

// ---------- shell ----------

func (s *Server) hShell(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Cmd) == "" {
		return nil, errors.New("comando vuoto")
	}
	// script su file: comandi multi-riga, heredoc e caratteri speciali passano intatti
	script := "export TERM=dumb HOME=/userdata/system; cd /userdata/system\n" + req.Cmd + "\n"
	if err := s.ssh.UploadBytes("/tmp/companion_shell.sh", []byte(script), 0o755); err != nil {
		return nil, err
	}
	res, err := s.ssh.RunTimeout("bash /tmp/companion_shell.sh 2>&1 | tail -c 200000; exit ${PIPESTATUS[0]}", 10*time.Minute)
	if err != nil {
		if res == nil {
			return nil, err
		}
		return map[string]any{"stdout": res.Stdout, "stderr": err.Error(), "exit": res.Exit}, nil
	}
	return map[string]any{"stdout": res.Stdout, "stderr": res.Stderr, "exit": res.Exit}, nil
}

// ---------- comandi rapidi ----------

func (s *Server) hCmd(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action string `json:"action"`
		Arg    string `json:"arg"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "screenshot":
		return s.screenshot()
	case "emukill":
		if err := s.es.EmuKill(); err != nil {
			_, err2 := s.ssh.Run("batocera-es-swissknife --emukill")
			if err2 != nil {
				return nil, err
			}
		}
		return nil, nil
	case "reloadgames":
		return nil, s.es.ReloadGames()
	case "restart_es":
		_, err := s.ssh.RunTimeout("batocera-es-swissknife --restart >/dev/null 2>&1 &", 20*time.Second)
		return nil, err
	case "reboot":
		_, err := s.ssh.RunTimeout("(sleep 1; reboot) >/dev/null 2>&1 &", 10*time.Second)
		return nil, err
	case "shutdown":
		_, err := s.ssh.RunTimeout("(sleep 1; poweroff) >/dev/null 2>&1 &", 10*time.Second)
		return nil, err
	case "notify":
		return nil, s.es.Notify(req.Arg)
	case "launch":
		if req.Arg == "" {
			return nil, errors.New("percorso mancante")
		}
		return nil, s.es.Launch(req.Arg)
	}
	return nil, fmt.Errorf("azione %q sconosciuta", req.Action)
}

func (s *Server) screenshot() (any, error) {
	_ = os.MkdirAll(s.cfg.ScreenshotDir, 0o755)
	name := "cabinato-" + time.Now().Format("20060102-150405") + ".png"
	remote := "/tmp/" + name
	cmd := fmt.Sprintf(`rm -f %[1]s; ffmpeg -loglevel error -y -f kmsgrab -device /dev/dri/card0 -i - -vf 'hwdownload,format=bgr0' -frames:v 1 %[1]s 2>/dev/null || DISPLAY=:0 ffmpeg -loglevel error -y -f x11grab -i :0 -frames:v 1 %[1]s 2>/dev/null; [ -s %[1]s ] && echo ok`, remote)
	res, err := s.ssh.RunTimeout(cmd, 40*time.Second)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(res.Stdout, "ok") {
		return nil, errors.New("cattura non riuscita (ffmpeg assente o schermo non attivo)")
	}
	local := filepath.Join(s.cfg.ScreenshotDir, name)
	if err := s.ssh.Download(r0(), remote, local, nil); err != nil {
		return nil, err
	}
	_, _ = s.ssh.Run("rm -f " + remote)
	return map[string]any{"ok": true, "url": "/shots/" + name, "file": local}, nil
}

// ---------- sistemi e giochi ----------

func (s *Server) hSystems(w http.ResponseWriter, r *http.Request) (any, error) {
	sys, err := s.es.Systems()
	if err != nil {
		return nil, err
	}
	var out []cab.System
	for _, x := range sys {
		n := strings.ToLower(x.Name)
		if n == "all" || n == "favorites" || n == "recent" || n == "lastplayed" || strings.HasPrefix(n, "auto-") || strings.HasPrefix(n, "custom-") {
			continue
		}
		out = append(out, x)
	}
	return out, nil
}

func (s *Server) hGames(w http.ResponseWriter, r *http.Request) (any, error) {
	sys := r.URL.Query().Get("system")
	if sys == "" {
		return nil, errors.New("sistema mancante")
	}
	g, err := s.es.Games(sys)
	if err != nil {
		return nil, err
	}
	if g == nil {
		g = []cab.Game{}
	}
	return g, nil
}

func (s *Server) hMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	b, ct, err := s.es.Media(q.Get("system"), q.Get("id"), q.Get("kind"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if ct == "" {
		ct = http.DetectContentType(b)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// hReconnect: "Cerca il cabinato": chiude la connessione SSH e riprova subito.
func (s *Server) hReconnect(w http.ResponseWriter, r *http.Request) (any, error) {
	s.ssh.Reconnect()
	s.invalidateInfo()
	_, err := s.ssh.RunTimeout("true", 15*time.Second)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "via_remote": s.ssh.ViaRemote()}, nil
}
