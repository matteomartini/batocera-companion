package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// ---- campionatore comune (temperature, MHz, carico, GPU, processo emulatore) ----
// Stampa una riga al secondo: t temp mhz cpu% emu% emucore% gpu_mhz
const samplerScript = `
dur=$1; pat="$2"
ncpu=$(nproc)
gmax=$(cat /sys/class/drm/card*/gt_max_freq_mhz 2>/dev/null | head -1)
echo "@@meta ncpu=$ncpu gmax=${gmax:-0} cpumax=$(cat /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq 2>/dev/null)"
read_temp(){ m=0; for z in /sys/class/thermal/thermal_zone*; do ty=$(cat $z/type 2>/dev/null); v=$(cat $z/temp 2>/dev/null); case "$ty" in x86_pkg_temp|coretemp*|cpu*|TCPU|acpitz) [ "${v:-0}" -gt "$m" ] && m=$v;; esac; done; [ "$m" = 0 ] && m=$(cat /sys/class/thermal/thermal_zone0/temp 2>/dev/null); echo $((m/1000)); }
read_mhz(){ awk '/cpu MHz/{s+=$4;n++}END{if(n)printf "%d",s/n}' /proc/cpuinfo; }
read_stat(){ awk '/^cpu /{print $2+$3+$4+$6+$7+$8, $5}' /proc/stat; }
read_cores(){ awk '/^cpu[0-9]/{print $1, $2+$3+$4+$6+$7+$8, $5}' /proc/stat; }
pid=""
prev=$(read_stat); prevc=$(read_cores); pprev=0
i=0
while [ $i -lt $dur ]; do
  sleep 1; i=$((i+1))
  cur=$(read_stat); curc=$(read_cores)
  pb=${prev% *}; pi=${prev#* }; cb=${cur% *}; ci=${cur#* }
  tot=$(( (cb+ci)-(pb+pi) )); busy=$(( cb-pb )); cpu=0; [ $tot -gt 0 ] && cpu=$((busy*100/tot))
  # core più carico
  mc=$(paste <(echo "$prevc") <(echo "$curc") | awk '{t=($5+$6)-($2+$3); b=$5-$2; if(t>0){p=b*100/t; if(p>m)m=p}}END{printf "%d",m}')
  emu=0
  if [ -n "$pat" ]; then
    [ -z "$pid" ] && pid=$(for q in $(pgrep -f "$pat" 2>/dev/null); do [ "$q" = "$$" ] && continue; tr '\0' ' ' < /proc/$q/cmdline 2>/dev/null | grep -q 'bc_sampler\|pgrep' && continue; ct=$(awk '{print $14+$15}' /proc/$q/stat 2>/dev/null); [ -n "$ct" ] && echo "$ct $q"; done 2>/dev/null | sort -n | tail -1 | cut -d' ' -f2)
    if [ -n "$pid" ] && [ -r /proc/$pid/stat ]; then
      pt=$(awk '{print $14+$15}' /proc/$pid/stat 2>/dev/null)
      [ -n "$pt" ] && [ $tot -gt 0 ] && emu=$(( (pt-pprev)*100*ncpu/tot )); pprev=${pt:-0}
    else pid=""; fi
  fi
  g=$(cat /sys/class/drm/card*/gt_cur_freq_mhz 2>/dev/null | head -1)
  echo "$i $(read_temp) $(read_mhz) $cpu $emu $mc ${g:-0}"
  prev=$cur; prevc=$curc
done
`

type sample struct {
	T, Temp, MHz, CPU, Emu, MaxCore, GPU int
}

func parseSamples(out string) (map[string]string, []sample) {
	meta := map[string]string{}
	var ss []sample
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "@@meta") {
			for _, f := range strings.Fields(l)[1:] {
				if k, v, ok := strings.Cut(f, "="); ok {
					meta[k] = v
				}
			}
			continue
		}
		f := strings.Fields(l)
		if len(f) < 7 {
			continue
		}
		var s sample
		n := []*int{&s.T, &s.Temp, &s.MHz, &s.CPU, &s.Emu, &s.MaxCore, &s.GPU}
		for i := range n {
			*n[i], _ = strconv.Atoi(f[i])
		}
		ss = append(ss, s)
	}
	return meta, ss
}

func (s *Server) runSampler(j *jobs.Job, dur int, pat string, onTick func(sample)) (map[string]string, []sample, error) {
	rc, err := s.ssh.StreamSession("bash -c " + cab.ShellQuote(samplerScript) + " bc_sampler " + strconv.Itoa(dur) + " " + cab.ShellQuote(pat))
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()
	var out strings.Builder
	sc := bufio.NewScanner(rc)
	for sc.Scan() {
		if j.Cancelled() {
			return nil, nil, errors.New("annullato")
		}
		l := sc.Text()
		out.WriteString(l + "\n")
		if _, ss := parseSamples(l); len(ss) == 1 && onTick != nil {
			onTick(ss[0])
		}
	}
	meta, ss := parseSamples(out.String())
	if len(ss) == 0 {
		return meta, ss, errors.New("nessun campione ricevuto dal cabinato")
	}
	return meta, ss, nil
}

func avgLast(ss []sample, n int, f func(sample) int) int {
	if len(ss) == 0 {
		return 0
	}
	if n > len(ss) {
		n = len(ss)
	}
	t := 0
	for _, s := range ss[len(ss)-n:] {
		t += f(s)
	}
	return t / n
}

func maxOf(ss []sample, f func(sample) int) int {
	m := 0
	for _, s := range ss {
		if f(s) > m {
			m = f(s)
		}
	}
	return m
}

// ---- archivio prove ----
func (s *Server) runsFile(kind string) string { return filepath.Join(s.cfg.Dir(), kind+"-runs.json") }

func (s *Server) loadRuns(kind string) []map[string]any {
	var runs []map[string]any
	b, err := os.ReadFile(s.runsFile(kind))
	if err == nil {
		_ = json.Unmarshal(b, &runs)
	}
	return runs
}

func (s *Server) saveRun(kind string, run map[string]any) {
	runs := append(s.loadRuns(kind), run)
	if len(runs) > 12 {
		runs = runs[len(runs)-12:]
	}
	b, _ := json.MarshalIndent(runs, "", " ")
	_ = os.WriteFile(s.runsFile(kind), b, 0o644)
}

func (s *Server) deleteRun(kind, id string) {
	var keep []map[string]any
	for _, r := range s.loadRuns(kind) {
		if r["id"] != id {
			keep = append(keep, r)
		}
	}
	b, _ := json.MarshalIndent(keep, "", " ")
	_ = os.WriteFile(s.runsFile(kind), b, 0o644)
}

// ---- stress test ----
// GET: prove salvate. POST {minutes, label} avvia; POST {delete: id} cancella.
func (s *Server) hStress(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return map[string]any{"runs": s.loadRuns("stress")}, nil
	}
	var req struct {
		Minutes int    `json:"minutes"`
		Label   string `json:"label"`
		Delete  string `json:"delete"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Delete != "" {
		s.deleteRun("stress", req.Delete)
		return map[string]any{"runs": s.loadRuns("stress")}, nil
	}
	if req.Minutes < 1 {
		req.Minutes = 3
	}
	if req.Minutes > 20 {
		req.Minutes = 20
	}
	if g, _ := s.es.RunningGame(); g != nil {
		return nil, errors.New("c'è un gioco in corso sul cabinato: chiudilo prima")
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = time.Now().Format("02/01 15:04")
	}
	j := s.jobs.Start("Stress test "+label, func(j *jobs.Job) (any, error) {
		dur := req.Minutes*60 + 15 // 15 s di riposo iniziale
		_ = s.es.MessageBox(fmt.Sprintf("Stress test CPU in corso (%d min) — avviato dal Companion", req.Minutes))
		// carico: un processo per core, parte dopo 15 s, muore da solo alla fine
		// carico: uno script per core, parte dopo 15 s e muore da solo alla fine
		script := "#!/bin/bash\nsleep 15\nfor i in $(seq 1 $(nproc)); do (timeout " + strconv.Itoa(req.Minutes*60) + " bash -c 'while :; do :; done' &); done\n"
		if err := s.ssh.UploadBytes("/tmp/bc_load.sh", []byte(script), 0o755); err != nil {
			return nil, err
		}
		// pkill in un comando separato: nello stesso comando ucciderebbe sé stesso
		_, _ = s.ssh.RunTimeout("pkill -f '[b]c_load.sh'; pkill -f '[w]hile :; do :; done'; true", 10*time.Second)
		if _, err := s.ssh.RunTimeout("nohup /tmp/bc_load.sh >/dev/null 2>&1 &", 10*time.Second); err != nil {
			return nil, err
		}
		stopLoad := func() {
			_, _ = s.ssh.RunTimeout("pkill -f '[b]c_load.sh'; pkill -f '[w]hile :; do :; done'; true", 10*time.Second)
		}
		defer stopLoad()
		meta, ss, err := s.runSampler(j, dur, "", func(sm sample) {
			phase := "sotto carico"
			if sm.T <= 15 {
				phase = "a riposo"
			}
			j.SetProgress(float64(sm.T)/float64(dur), fmt.Sprintf("%s · %d °C · %d MHz · CPU %d%%", phase, sm.Temp, sm.MHz, sm.CPU))
			if sm.T%10 == 0 {
				j.Logf("%3ds  %2d °C  %4d MHz  cpu %3d%%", sm.T, sm.Temp, sm.MHz, sm.CPU)
			}
		})
		if err != nil {
			return nil, err
		}
		var idle, loaded []sample
		for _, x := range ss {
			if x.T <= 15 {
				idle = append(idle, x)
			} else {
				loaded = append(loaded, x)
			}
		}
		cpuMax, _ := strconv.Atoi(meta["cpumax"])
		cpuMax /= 1000
		run := map[string]any{
			"id": strconv.FormatInt(time.Now().Unix(), 10), "at": time.Now().Format(time.RFC3339), "label": label,
			"minutes": req.Minutes, "idle_temp": avgLast(idle, 10, func(x sample) int { return x.Temp }),
			"max_temp":    maxOf(loaded, func(x sample) int { return x.Temp }),
			"end_temp":    avgLast(loaded, 30, func(x sample) int { return x.Temp }),
			"mhz_start":   avgLast(loaded[:min(20, len(loaded))], 20, func(x sample) int { return x.MHz }),
			"mhz_end":     avgLast(loaded, 30, func(x sample) int { return x.MHz }),
			"cpu_max_mhz": cpuMax,
		}
		var pts [][2]int
		for _, x := range ss {
			pts = append(pts, [2]int{x.T, x.Temp})
		}
		run["points"] = pts
		// tempo per arrivare a 1 °C dal massimo
		mt := run["max_temp"].(int)
		for _, x := range loaded {
			if x.Temp >= mt-1 {
				run["t_to_max"] = x.T - 15
				break
			}
		}
		var notes []string
		if mt >= 95 {
			notes = append(notes, fmt.Sprintf("❌ %d °C: la CPU è al limite termico e rallenta (throttling). Senza aria il mini PC non regge il carico.", mt))
		} else if mt >= 85 {
			notes = append(notes, fmt.Sprintf("⚠️ %d °C: vicino alla soglia di throttling (≈95-100 °C). In gioco pesante può rallentare.", mt))
		} else {
			notes = append(notes, fmt.Sprintf("✅ Massimo %d °C sotto carico pieno: margine buono.", mt))
		}
		ms, me := run["mhz_start"].(int), run["mhz_end"].(int)
		if ms > 0 && me < ms*85/100 {
			notes = append(notes, fmt.Sprintf("⚠️ Frequenza scesa da %d a %d MHz durante la prova: la CPU si sta auto-limitando (temperatura o limite di potenza del Lenovo).", ms, me))
		} else if ms > 0 {
			notes = append(notes, fmt.Sprintf("✅ Frequenza stabile (%d → %d MHz).", ms, me))
		}
		run["notes"] = notes
		s.saveRun("stress", run)
		s.invalidateInfo()
		_ = s.es.Notify("Stress test finito: max " + strconv.Itoa(mt) + " °C")
		return map[string]any{"run": run, "runs": s.loadRuns("stress")}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

// ---- test prestazioni emulatore ----
const emuProcPattern = `retroarch|dolphin-emu|pcsx2|duckstation|flycast|PPSSPP|/mame|supermodel|xemu|cemu|rpcs3|yuzu|ryujinx|scummvm|hatari|fs-uae|amiberry|mupen64|melonDS|citra|lime3ds|azahar|ruffle|wine|xash3d`

func (s *Server) hPerf(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		return map[string]any{"runs": s.loadRuns("perf")}, nil
	}
	var req struct {
		System  string `json:"system"`
		Path    string `json:"path"`
		Name    string `json:"name"`
		Seconds int    `json:"seconds"`
		Compare bool   `json:"compare"` // seconda prova con le impostazioni consigliate
		Delete  string `json:"delete"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Delete != "" {
		s.deleteRun("perf", req.Delete)
		return map[string]any{"runs": s.loadRuns("perf")}, nil
	}
	if req.System == "" || req.Path == "" {
		return nil, errors.New("scegli sistema e gioco")
	}
	if req.Seconds < 20 {
		req.Seconds = 45
	}
	if req.Seconds > 180 {
		req.Seconds = 180
	}
	if g, _ := s.es.RunningGame(); g != nil {
		return nil, errors.New("c'è già un gioco in corso sul cabinato: chiudilo prima")
	}
	sys := req.System
	j := s.jobs.Start("Test prestazioni "+req.Name, func(j *jobs.Job) (any, error) {
		one := func(label string) (map[string]any, error) {
			j.SetProgress(-1, label+": avvio "+req.Name)
			if err := s.es.Launch(req.Path); err != nil {
				return nil, fmt.Errorf("avvio: %v", err)
			}
			defer func() { _ = s.es.EmuKill() }()
			// attesa caricamento
			for i := 0; i < 12; i++ {
				time.Sleep(time.Second)
				if j.Cancelled() {
					return nil, errors.New("annullato")
				}
			}
			meta, ss, err := s.runSampler(j, req.Seconds, emuProcPattern, func(sm sample) {
				j.SetProgress(float64(sm.T)/float64(req.Seconds), fmt.Sprintf("%s · emulatore %d%% · core max %d%% · %d °C · GPU %d MHz", label, sm.Emu, sm.MaxCore, sm.Temp, sm.GPU))
			})
			if err != nil {
				return nil, err
			}
			// scarta i primi 5 s (ancora in caricamento)
			if len(ss) > 10 {
				ss = ss[5:]
			}
			ncpu, _ := strconv.Atoi(meta["ncpu"])
			gmax, _ := strconv.Atoi(meta["gmax"])
			res := map[string]any{
				"label":    label,
				"emu_avg":  avgLast(ss, len(ss), func(x sample) int { return x.Emu }),
				"emu_max":  maxOf(ss, func(x sample) int { return x.Emu }),
				"core_avg": avgLast(ss, len(ss), func(x sample) int { return x.MaxCore }),
				"core_max": maxOf(ss, func(x sample) int { return x.MaxCore }),
				"cpu_avg":  avgLast(ss, len(ss), func(x sample) int { return x.CPU }),
				"temp_max": maxOf(ss, func(x sample) int { return x.Temp }),
				"mhz_avg":  avgLast(ss, len(ss), func(x sample) int { return x.MHz }),
				"gpu_avg":  avgLast(ss, len(ss), func(x sample) int { return x.GPU }),
				"gpu_max":  gmax, "ncpu": ncpu,
				"emu_found": maxOf(ss, func(x sample) int { return x.Emu }) > 0,
			}
			return res, nil
		}
		// impostazioni attuali del sistema
		cur, _ := s.ssh.RunTimeout("grep -E '^"+sys+"\\.' /userdata/system/batocera.conf | grep -v '\\['", 20*time.Second)
		curConf := strings.TrimSpace(cur.Stdout)
		a, err := one("impostazioni attuali")
		if err != nil {
			return nil, err
		}
		a["conf"] = curConf
		results := []map[string]any{a}
		if req.Compare {
			reco := recoFor(sys)
			var cmds []string
			cmds = append(cmds, "cp /userdata/system/batocera.conf /tmp/bc_perf_backup.conf")
			for _, rc := range reco {
				if rc.Key == "emulator" {
					e, c, _ := strings.Cut(rc.Value, "/")
					cmds = append(cmds, "bc_set "+sys+".emulator "+e, "bc_set "+sys+".core "+c)
				} else {
					cmds = append(cmds, "bc_set "+sys+"."+rc.Key+" "+rc.Value)
				}
			}
			if _, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 30*time.Second); err != nil {
				return nil, err
			}
			restore := func() {
				_, _ = s.ssh.RunTimeout("cat /tmp/bc_perf_backup.conf > /userdata/system/batocera.conf && rm -f /tmp/bc_perf_backup.conf", 20*time.Second)
			}
			b, err := one("impostazioni consigliate")
			restore()
			if err != nil {
				return nil, err
			}
			var rs []string
			for _, rc := range reco {
				rs = append(rs, sys+"."+rc.Key+"="+rc.Value)
			}
			b["conf"] = strings.Join(rs, "\n")
			results = append(results, b)
		}
		// verdetto sulla prova attuale
		verdict, tips := perfVerdict(sys, a, s.es)
		run := map[string]any{
			"id": strconv.FormatInt(time.Now().Unix(), 10), "at": time.Now().Format(time.RFC3339),
			"system": sys, "game": req.Name, "path": req.Path, "seconds": req.Seconds,
			"results": results, "verdict": verdict, "tips": tips,
		}
		if len(results) == 2 {
			da := results[0]["core_avg"].(int) - results[1]["core_avg"].(int)
			switch {
			case da >= 8:
				run["compare_note"] = fmt.Sprintf("Con le impostazioni consigliate il core più carico scende del %d%%: conviene applicarle dalla scheda Emulatori.", da)
			case da <= -8:
				run["compare_note"] = fmt.Sprintf("Con le consigliate il core più carico sale del %d%% (run-ahead o seconda istanza costano): tieni le attuali o disattiva il run-ahead.", -da)
			default:
				run["compare_note"] = "Nessuna differenza rilevante tra attuali e consigliate su questo gioco."
			}
		}
		s.saveRun("perf", run)
		s.invalidateInfo()
		return map[string]any{"run": run, "runs": s.loadRuns("perf")}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func perfVerdict(sys string, a map[string]any, es *cab.ES) (string, []string) {
	core := a["core_avg"].(int)
	emu := a["emu_avg"].(int)
	temp := a["temp_max"].(int)
	gpu, gmax := a["gpu_avg"].(int), a["gpu_max"].(int)
	found := a["emu_found"].(bool)
	var tips []string
	verdict := "✅ Gira con margine: il core più carico è al " + strconv.Itoa(core) + "%."
	if !found {
		verdict = "⚠️ Non ho trovato il processo dell'emulatore: il gioco potrebbe non essere partito o usa un emulatore che non riconosco. I dati sotto sono del sistema intero."
	}
	cpuBound := core >= 90
	gpuBound := gmax > 0 && gpu >= gmax*92/100
	thermal := temp >= 90
	if cpuBound {
		verdict = fmt.Sprintf("❌ CPU al limite: un core al %d%% per tutta la prova (emulatore %d%% in totale). Qui nascono gli scatti.", core, emu)
		if is2D(sys) {
			tips = append(tips, "Run-ahead: metti 0 o togli la seconda istanza (raddoppia il lavoro del core).")
			if sys == "mame" {
				tips = append(tips, "Core: prova mame2003_plus o fbneo per questo gioco (scheda Giochi → core per gioco): molto più leggeri del MAME attuale.")
			}
		} else {
			tips = append(tips, "Run-ahead spento e rewind spento per questo sistema.")
			switch sys {
			case "psx":
				tips = append(tips, "Core: pcsx_rearmed è più leggero di beetle-psx/duckstation; oppure abbassa la risoluzione interna a 1x.")
			case "n64":
				tips = append(tips, "Risoluzione interna a 320x240 (1x) e core mupen64plus-next; parallel-n64 come alternativa leggera.")
			case "saturn":
				tips = append(tips, "Core yabasanshiro invece di beetle-saturn.")
			case "dreamcast", "naomi", "atomiswave":
				tips = append(tips, "Flycast: risoluzione interna 640x480 (1x), niente anti-alias.")
			case "ps2":
				tips = append(tips, "PCSX2: risoluzione nativa, EE cycle rate -1/-2 e cycle skipping 1 sui giochi pesanti.")
			case "gamecube", "wii", "triforce":
				tips = append(tips, "Dolphin: risoluzione interna 1x nativa, shader compilation 'skip drawing', niente anti-alias.")
			default:
				tips = append(tips, "Risoluzione interna al minimo (1x) e core più leggero se disponibile.")
			}
		}
		tips = append(tips, "Shader: nessuno o uno leggero (zfast, crt-pi); cornice (bezel) spenta.")
	} else if core >= 75 {
		verdict = fmt.Sprintf("⚠️ Al limite: core più carico al %d%% (emulatore %d%%). Le scene pesanti possono scattare.", core, emu)
		if is2D(sys) {
			tips = append(tips, "Tieni il run-ahead a 1 senza seconda istanza, o spegnilo nei giochi che scattano.")
		} else {
			tips = append(tips, "Run-ahead spento; risoluzione interna 1x.")
		}
	}
	if gpuBound {
		verdict += fmt.Sprintf(" GPU al massimo (%d/%d MHz).", gpu, gmax)
		tips = append(tips, "La GPU Intel è satura: togli shader CRT pesanti (royale, guest), cornice e filtro, e abbassa la risoluzione interna nei 3D.")
	}
	if thermal {
		verdict += fmt.Sprintf(" Temperatura %d °C: throttling.", temp)
		tips = append(tips, "Il mini PC scalda troppo: ventola/aria nel retro del cabinato (vedi stress test).")
	}
	if len(tips) == 0 {
		tips = append(tips, "Puoi permetterti qualcosa in più: uno shader CRT leggero (crt-pi, zfast) o, sui 2D, il run-ahead a 1.")
	}
	return verdict, tips
}

// elenco sistemi+giochi per il selettore del test prestazioni
func (s *Server) hPerfGames(w http.ResponseWriter, r *http.Request) (any, error) {
	sys := r.URL.Query().Get("system")
	if sys == "" {
		systems, err := s.es.Systems()
		if err != nil {
			return nil, err
		}
		sort.Slice(systems, func(i, k int) bool { return systems[i].Fullname < systems[k].Fullname })
		return map[string]any{"systems": systems}, nil
	}
	games, err := s.es.Games(sys)
	if err != nil {
		return nil, err
	}
	type g struct{ Name, Path string }
	var out []g
	for _, x := range games {
		if !x.Hidden {
			out = append(out, g{x.Name, x.Path})
		}
	}
	sort.Slice(out, func(i, k int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name) })
	return map[string]any{"games": out}, nil
}
