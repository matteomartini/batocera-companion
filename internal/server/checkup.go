package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/checkup"
	"batocompanion/internal/jobs"
)

func (s *Server) runCheckup(refresh bool) (*checkup.Report, error) {
	in, err := s.collectInfo(refresh)
	if err != nil {
		return nil, err
	}
	arc := 0
	for _, id := range s.dats.Present() {
		if id == "fbneo" || strings.HasPrefix(id, "mame") {
			arc++
		}
	}
	rep := checkup.Run(in, &checkup.Options{CompanionVersion: s.Version, ArcadeDats: arc})
	if extra := s.romLibFindings(); len(extra) > 0 {
		var kept []checkup.Finding
		for _, f := range rep.Findings {
			if f.ID != "rom.none" && f.ID != "rom.total" && f.ID != "rom.badcore" {
				kept = append(kept, f)
			}
		}
		rep.Findings = append(kept, extra...)
		rep.Counts = map[string]int{}
		score := 100
		for _, f := range rep.Findings {
			rep.Counts[f.Level]++
			if f.Level == "error" {
				score -= 12
			} else if f.Level == "warn" {
				score -= 5
			}
		}
		if score < 0 {
			score = 0
		}
		rep.Score = score
	}
	return rep, nil
}

func (s *Server) hCheckup(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Start("Check-up", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Raccolgo le informazioni dal cabinato (30-60 s)")
		rep, err := s.runCheckup(true)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.lastChk = rep
		s.mu.Unlock()
		return rep, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) hCheckupFix(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Commands       []string `json:"commands"`
		NeedsRestartES bool     `json:"needs_restart_es"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if len(req.Commands) == 0 {
		return nil, errors.New("nessun comando")
	}
	var log []string
	cmd := "mkdir -p " + backupDir + "; cp /userdata/system/batocera.conf " + backupDir + "/batocera.conf." + time.Now().Format("20060102-150405") + " 2>/dev/null\n"
	for _, c := range req.Commands {
		cmd += c + "\n"
	}
	res, err := s.ssh.RunTimeout(cmd, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	log = append(log, cab.Lines(res.Stdout+res.Stderr)...)
	ok := res.Exit == 0
	if ok && req.NeedsRestartES {
		if g, _ := s.es.RunningGame(); g != nil {
			log = append(log, "· c'è un gioco in corso: EmulationStation verrà riavviato al prossimo avvio, non adesso")
		} else {
			_, _ = s.ssh.RunTimeout("batocera-es-swissknife --restart >/dev/null 2>&1 &", 20*time.Second)
			log = append(log, "· EmulationStation riavviato")
		}
	}
	s.mu.Lock()
	s.info = nil
	s.mu.Unlock()
	if len(log) == 0 {
		log = append(log, "fatto")
	}
	return map[string]any{"ok": ok, "log": log, "exit": res.Exit}, nil
}

// ---------- "Perché scatta?" ----------

const stutterScript = `
echo "@@mode"; DISPLAY=:0 xrandr 2>/dev/null | grep -E '\*' | head -2
echo "@@videomode"; batocera-settings-get global.videomode 2>/dev/null
echo "@@gov"; cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null; batocera-settings-get system.cpu.governor 2>/dev/null
echo "@@temp"; for z in /sys/class/thermal/thermal_zone*; do echo "$(cat $z/type) $(cat $z/temp)"; done; cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq 2>/dev/null; cat /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq 2>/dev/null
echo "@@throttle"; dmesg 2>/dev/null | grep -iE 'throttl|thermal' | tail -3
echo "@@capture"; pgrep -af 'ffmpeg' | grep -v pgrep
echo "@@heavy"; ps -eo pcpu,pmem,comm --sort=-pcpu | head -6
echo "@@ha"; pgrep -af 'ha-batocera|homeassistant|ha_agent' | grep -v pgrep; grep -l xrandr /userdata/system/services/* 2>/dev/null
echo "@@ra"; for k in video_vsync video_threaded video_frame_delay_auto video_frame_delay audio_sync video_hard_sync; do echo "$k=$(grep -m1 "^$k" /userdata/system/configs/retroarch/retroarchcustom.cfg 2>/dev/null | cut -d= -f2 | tr -d ' "')"; done
echo "@@conf"; for k in global.runahead global.rewind global.shaderset global.smooth global.autosave global.bezel global.videomode global.showFPS; do echo "$k=$(batocera-settings-get $k 2>/dev/null)"; done
echo "@@hotkeygen"; pgrep -af hotkeygen | grep -v pgrep | wc -l
echo "@@gpu"; dmesg 2>/dev/null | grep -iE 'i915.*(hang|reset|error)' | tail -3
echo "@@mem"; free -m | head -2
echo "@@services"; ls /userdata/system/services 2>/dev/null
echo "@@eslog"; tail -n 20 /userdata/system/logs/es_launch_stderr.log 2>/dev/null | grep -iE 'drop|fps|vsync|slow|hang' | tail -5
`

func (s *Server) hStutter(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Start("Diagnosi stutter", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Leggo schermo, CPU, servizi e impostazioni dal cabinato")
		res, err := s.ssh.RunTimeout(stutterScript, 2*time.Minute)
		if err != nil {
			return nil, err
		}
		sec := cab.SplitSections(res.Stdout)
		type item struct {
			Level, Title, Detail, Fix string
		}
		var items []map[string]any
		add := func(level, title, detail, fix string) {
			items = append(items, map[string]any{"level": level, "title": title, "detail": detail, "fix": fix})
		}
		mode := strings.TrimSpace(sec["mode"])
		if mode != "" {
			f := strings.Fields(mode)
			hz := ""
			for _, x := range f {
				if strings.Contains(x, "*") {
					hz = strings.Trim(x, "*+")
				}
			}
			if hz != "" && !strings.HasPrefix(hz, "60") && !strings.HasPrefix(hz, "59.9") {
				add("error", "Schermo a "+hz+" Hz invece di 60", "I giochi a 60 fps saltano fotogrammi di continuo. Forza una modalità a 60 Hz.", "")
			} else {
				add("ok", "Schermo: "+mode, "", "")
			}
		}
		gov := cab.Lines(sec["gov"])
		if len(gov) > 0 && strings.TrimSpace(gov[0]) != "performance" {
			add("warn", "Governor CPU: "+strings.TrimSpace(gov[0]), "La frequenza sale in ritardo: micro-scatti nei picchi.", "batocera-settings-set system.cpu.governor performance; for c in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do echo performance > $c; done")
		} else if len(gov) > 0 {
			add("ok", "Governor CPU: performance", "", "")
		}
		for _, l := range cab.Lines(sec["temp"]) {
			f := strings.Fields(l)
			if len(f) == 2 && strings.Contains(f[0], "x86_pkg") {
				var t float64
				fmt.Sscanf(f[1], "%f", &t)
				if t/1000 >= 85 {
					add("error", fmt.Sprintf("CPU a %.0f °C: throttling", t/1000), "Sopra 85 °C la CPU rallenta. Aria al mini PC / ventola nel retro.", "")
				} else {
					add("ok", fmt.Sprintf("Temperatura CPU %.0f °C", t/1000), "", "")
				}
			}
		}
		if t := strings.TrimSpace(sec["throttle"]); t != "" {
			add("warn", "Il kernel ha registrato throttling termico", t, "")
		}
		if c := strings.TrimSpace(sec["capture"]); c != "" {
			add("warn", "Una cattura dello schermo è in corso (ffmpeg)", "Schermo in diretta o registrazione: costano CPU/GPU mentre giochi. Chiudili dal Companion.", "pkill -f ffmpeg")
		}
		if h := strings.TrimSpace(sec["ha"]); h != "" {
			add("error", "Vecchio agente Home Assistant con lettura dello schermo (xrandr)", "Interroga lo schermo ogni pochi secondi bloccandolo per un attimo: scatti periodici in tutti i giochi.\n"+h, "")
		}
		ra := map[string]string{}
		for _, l := range cab.Lines(sec["ra"]) {
			if k, v, ok := strings.Cut(l, "="); ok {
				ra[k] = strings.TrimSpace(v)
			}
		}
		if ra["video_vsync"] == "false" {
			add("warn", "RetroArch: vsync disattivato", "Senza vsync l'immagine si strappa (tearing).", "sed -i 's/^video_vsync.*/video_vsync = \"true\"/' /userdata/system/configs/retroarch/retroarchcustom.cfg")
		}
		if ra["video_threaded"] == "true" {
			add("warn", "RetroArch: video threaded attivo", "Introduce micro-stutter su GPU Intel: meglio spento.", "sed -i 's/^video_threaded.*/video_threaded = \"false\"/' /userdata/system/configs/retroarch/retroarchcustom.cfg")
		}
		if ra["video_hard_sync"] == "true" {
			add("info", "RetroArch: hard GPU sync attivo", "Riduce la latenza ma su una GPU integrata può far scattare: se scatta, spegnilo.", "")
		}
		conf := map[string]string{}
		for _, l := range cab.Lines(sec["conf"]) {
			if k, v, ok := strings.Cut(l, "="); ok {
				conf[k] = strings.TrimSpace(v)
			}
		}
		if conf["global.rewind"] == "1" {
			add("warn", "Rewind attivo", "Costa CPU a ogni frame.", "batocera-settings-set global.rewind 0")
		}
		if v := conf["global.runahead"]; v != "" && v != "0" {
			add("info", "Run-ahead globale = "+v, "Raddoppia il lavoro del core: ok sul 2D, troppo per PS1/N64/Dreamcast. Meglio solo sui sistemi arcade.", "batocera-settings-set global.runahead 0")
		}
		if v := conf["global.shaderset"]; v != "" && v != "none" && (strings.Contains(v, "royale") || strings.Contains(v, "mega") || strings.Contains(v, "crt-guest")) {
			add("warn", "Shader pesante: "+v, "Shader come crt-royale/guest chiedono una GPU dedicata: usa crt-geom, scanlines o retro.", "batocera-settings-set global.shaderset scanlines")
		}
		if strings.TrimSpace(sec["hotkeygen"]) == "0" {
			add("warn", "hotkeygen non in esecuzione", "Senza hotkeygen alcune combinazioni della plancia non funzionano.", "batocera-services start hotkeygen 2>/dev/null || /etc/init.d/S90hotkeygen start")
		}
		if g := strings.TrimSpace(sec["gpu"]); g != "" {
			add("warn", "Errori della GPU nel log del kernel", g, "")
		}
		if ml := cab.Lines(sec["mem"]); len(ml) >= 2 {
			f := strings.Fields(ml[1])
			if len(f) >= 7 {
				var tot, avail float64
				fmt.Sscanf(f[1], "%f", &tot)
				fmt.Sscanf(f[6], "%f", &avail)
				if tot > 0 && avail/tot < 0.1 {
					add("warn", fmt.Sprintf("Memoria quasi esaurita (%.0f MB liberi)", avail), "", "")
				}
			}
		}
		if h := strings.TrimSpace(sec["heavy"]); h != "" {
			add("info", "Processi più pesanti adesso", h, "")
		}
		if e := strings.TrimSpace(sec["eslog"]); e != "" {
			add("info", "Dal log dell'ultimo gioco", e, "")
		}
		probs := 0
		for _, it := range items {
			if it["level"] == "warn" || it["level"] == "error" {
				probs++
			}
		}
		summary := "Nessun problema evidente: se scatta ancora, prova a spegnere run-ahead e shader sul sistema interessato."
		if probs > 0 {
			summary = fmt.Sprintf("%d possibili cause trovate: sistemale dall'alto verso il basso e riprova il gioco.", probs)
		}
		return map[string]any{"summary": summary, "items": items}, nil
	})
	return map[string]any{"job": j.ID}, nil
}
