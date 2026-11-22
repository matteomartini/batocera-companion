package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
	"batocompanion/internal/roms"
)

// "Perché non parte?": avvia il gioco, osserva se resta in esecuzione, legge i log
// di Batocera e confronta il set con i DAT. Restituisce esito + cause + rimedi.

var reLogInteresting = regexp.MustCompile(`(?i)(not found|missing|fatal|error|failed|segmentation|crash|unsupported|no such|cannot|unable|exit(ed|ing)? with|status -?\d+|bios|romset|content)`)

const zipCrcPy = `import sys, zipfile, json
f=sys.argv[1]
try:
    z=zipfile.ZipFile(f); print(json.dumps([[i.filename,i.CRC,i.file_size] for i in z.infolist()]))
except Exception as e: print(json.dumps({"error":str(e)}))`

func (s *Server) hGamesDiag(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		System string `json:"system"`
		Path   string `json:"path"`
		NoRun  bool   `json:"no_run"` // solo analisi, senza avviare
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.System == "" || req.Path == "" {
		return nil, errors.New("sistema e gioco mancanti")
	}
	if !req.NoRun {
		if g, _ := s.es.RunningGame(); g != nil {
			return nil, errors.New("c'è già un gioco in corso sul cabinato: chiudilo prima")
		}
	}
	j := s.jobs.Start("Diagnosi "+path.Base(req.Path), func(j *jobs.Job) (any, error) {
		return s.diagGame(j, req.System, req.Path, !req.NoRun)
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) diagGame(j *jobs.Job, sys, p string, run bool) (any, error) {
	var out []map[string]any
	add := func(level, title, detail, fix string) {
		out = append(out, map[string]any{"level": level, "title": title, "detail": detail, "fix": fix})
	}
	base := path.Base(p)
	stem := strings.TrimSuffix(base, path.Ext(base))
	arcade := isArcadeSys(sys)
	q := cab.ShellQuote

	// 1) file, override di core, core installati, CRC dello zip, file nella cartella
	script := "f=" + q(p) + "; echo '@@file'; ls -la \"$f\" 2>&1; echo '@@conf'; grep -E " + q("^"+regexp.QuoteMeta(sys)+`(\[|\.)`) + " /userdata/system/batocera.conf 2>/dev/null\n" +
		"echo '@@cores'; ls /usr/lib/libretro/ 2>/dev/null | sed 's/_libretro.so//' | tr '\\n' ' '\n" +
		"echo '@@crc'; python3 -c " + q(zipCrcPy) + " \"$f\"\n" +
		"echo '@@dir'; ls " + q("/userdata/roms/"+sys+"/") + " 2>/dev/null | tr '\\n' ' '; echo; echo '@@chd'; find " + q("/userdata/roms/"+sys+"/") + " -maxdepth 2 -iname '*.chd' 2>/dev/null | sed 's|.*/roms/" + sys + "/||' | tr '\\n' ' '"
	res, err := s.ssh.RunTimeout(script, time.Minute)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	if strings.Contains(sec["file"], "No such file") {
		add("error", "Il file non esiste sul cabinato", strings.TrimSpace(sec["file"]), "")
		return map[string]any{"items": out, "summary": "Il file non c'è più: forse è stato spostato o cancellato. Ricarica la lista di EmulationStation."}, nil
	}
	cores := strings.Fields(sec["cores"])
	dirFiles := map[string]bool{}
	for _, f := range strings.Fields(sec["dir"]) {
		dirFiles[strings.ToLower(f)] = true
	}
	chds := map[string]bool{} // "set/disk.chd" e "disk.chd"
	for _, f := range strings.Fields(sec["chd"]) {
		chds[strings.ToLower(f)] = true
		chds[strings.ToLower(path.Base(f))] = true
	}
	// override per gioco / per sistema
	gameCore, gameEmu, sysCore := "", "", ""
	for _, l := range cab.Lines(sec["conf"]) {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if strings.Contains(k, `["`+base+`"]`) {
			if strings.HasSuffix(k, ".core") {
				gameCore = v
			}
			if strings.HasSuffix(k, ".emulator") {
				gameEmu = v
			}
		}
		if k == sys+".core" {
			sysCore = v
		}
	}
	coreUsed := gameCore
	if coreUsed == "" {
		coreUsed = sysCore
	}
	if coreUsed == "" {
		coreUsed = map[string]string{"fbneo": "fbneo", "mame": "mame", "neogeo": "fbneo", "cps1": "fbneo", "cps2": "fbneo", "cps3": "fbneo"}[sys]
	}
	delOverride := "bc_del " + q(sys+`["`+base+`"].core`) + "; bc_del " + q(sys+`["`+base+`"].emulator`)
	if gameCore != "" {
		if !contains(cores, gameCore) && gameCore != "mame" {
			add("error", "Core per gioco non installato: "+gameCore, "batocera.conf imposta per questo gioco un core che su questa Batocera non esiste: il gioco non può partire.", delOverride)
		} else {
			add("info", "Core per gioco: "+gameCore+" ("+gameEmu+")", "Impostazione specifica per questo gioco in batocera.conf.", "")
		}
	}
	// 2) confronto con i DAT (arcade)
	var matches []map[string]any
	bestCore := ""
	if arcade {
		var entries [][]any
		crcOut := strings.TrimSpace(sec["crc"])
		if strings.HasPrefix(crcOut, "[") {
			_ = json.Unmarshal([]byte(crcOut), &entries)
			if len(entries) == 0 {
				add("error", "Lo zip è vuoto", "0 file dentro: va ricaricato (Aggiungi ROM).", "")
			}
		} else if crcOut != "" {
			add("error", "Lo zip non si apre", crcOut, "")
		}
		have := map[uint32]bool{}
		for _, e := range entries {
			if len(e) >= 2 {
				if c, ok := e[1].(float64); ok {
					have[uint32(c)] = true
				}
			}
		}
		known := false
		for _, id := range []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"} {
			db, _ := s.dats.Load(id)
			if db == nil {
				continue
			}
			h, t, missing, bios, ok := roms.SetMissing(db, strings.ToLower(stem), have)
			if !ok {
				continue
			}
			known = true
			installed := contains(cores, id) || id == "mame"
			devs, disks := roms.SetExtras(db, strings.ToLower(stem))
			var missDev, missDisk []string
			for _, d := range devs {
				if !dirFiles[d+".zip"] {
					missDev = append(missDev, d+".zip")
				}
			}
			for _, d := range disks {
				if !chds[strings.ToLower(stem+"/"+d+".chd")] && !chds[strings.ToLower(d+".chd")] {
					missDisk = append(missDisk, d+".chd")
				}
			}
			m := map[string]any{"core": id, "have": h, "total": t, "missing": missing, "bios": bios, "installed": installed, "complete": len(missing) == 0 && t > 0, "bios_missing": bios != "" && !dirFiles[bios+".zip"], "missing_devices": missDev, "missing_disks": missDisk}
			matches = append(matches, m)
			if m["complete"].(bool) && installed && !m["bios_missing"].(bool) && len(missDev) == 0 && len(missDisk) == 0 && bestCore == "" {
				bestCore = id
			}
		}
		if !known {
			add("error", "Set sconosciuto a tutti i DAT: "+stem, "Nessun core di Batocera conosce un set con questo nome. Nome del file sbagliato, oppure è un gioco di un altro sistema.", "")
		} else {
			var lines []string
			curOK := false
			for _, m := range matches {
				st := "✅ completo"
				if !m["complete"].(bool) {
					ms := m["missing"].([]string)
					show := ms
					if len(show) > 6 {
						show = append(append([]string{}, show[:6]...), fmt.Sprintf("… altri %d", len(ms)-6))
					}
					st = fmt.Sprintf("❌ mancano %d/%d: %s", len(ms), m["total"], strings.Join(show, ", "))
				}
				inst := ""
				if !m["installed"].(bool) {
					inst = " (core non installato)"
				}
				if m["bios_missing"].(bool) {
					st += fmt.Sprintf(" · BIOS %s.zip NON presente nella cartella", m["bios"])
				}
				if md := m["missing_devices"].([]string); len(md) > 0 {
					st += " · mancano i device: " + strings.Join(md, ", ")
				}
				if md := m["missing_disks"].([]string); len(md) > 0 {
					st += " · mancano i CHD: " + strings.Join(md, ", ") + " (in roms/" + sys + "/" + stem + "/)"
				}
				lines = append(lines, fmt.Sprintf("%s%s: %s", m["core"], inst, st))
				if m["core"] == coreUsed && m["complete"].(bool) && !m["bios_missing"].(bool) && len(m["missing_devices"].([]string)) == 0 && len(m["missing_disks"].([]string)) == 0 {
					curOK = true
				}
			}
			switch {
			case curOK:
				add("ok", "Il set è completo per il core in uso ("+coreUsed+")", strings.Join(lines, "\n"), "")
			case bestCore != "":
				emu := "libretro"
				if bestCore == "mame" {
					emu = "mame"
				}
				add("error", "Il set non va con il core in uso ("+coreUsed+") ma è completo per "+bestCore, strings.Join(lines, "\n"), "bc_set "+q(sys+`["`+base+`"].emulator`)+" "+emu+"; bc_set "+q(sys+`["`+base+`"].core`)+" "+bestCore)
			default:
				add("error", "Il set non è completo per nessun core installato", strings.Join(lines, "\n")+"\n\nServe lo zip della versione giusta: Aggiungi ROM lo verifica prima di copiarlo e, se trova i file mancanti in altri zip, ricostruisce il set.", "")
			}
			for _, m := range matches {
				if m["core"] != coreUsed && m["core"] != bestCore {
					continue
				}
				if m["bios_missing"].(bool) {
					add("error", fmt.Sprintf("Manca il BIOS %s.zip nella cartella %s", m["bios"], sys), "I giochi di questa scheda caricano le ROM di sistema da quello zip, nella stessa cartella del gioco. Libreria ROM → \"Copia i BIOS nelle cartelle ROM\" lo sistema se il BIOS è in /userdata/bios.", "")
				}
				if md := m["missing_devices"].([]string); len(md) > 0 {
					add("error", "Mancano gli zip dei device richiesti da MAME: "+strings.Join(md, ", "), "MAME moderno carica alcune parti della scheda (tabellone, chip audio, I/O) da set separati, come i BIOS. Vanno nella stessa cartella del gioco; Libreria ROM → \"Copia i BIOS nelle cartelle ROM\" li copia anche da /userdata/bios o da un'altra cartella ROM se ce li hai.", "")
				}
				if md := m["missing_disks"].([]string); len(md) > 0 {
					add("error", "Mancano i CHD: "+strings.Join(md, ", "), "Questo gioco usa un disco (CD/HD) oltre alle ROM: il file .chd va in roms/"+sys+"/"+stem+"/ (sottocartella con il nome del set).", "")
				}
				break
			}
		}
	}
	// 3) avvio di prova + log di Batocera
	if run {
		j.SetProgress(-1, "Avvio il gioco sul cabinato e osservo 12 secondi")
		_, _ = s.ssh.RunTimeout(": > /userdata/system/logs/es_launch_stderr.log; : > /userdata/system/logs/es_launch_stdout.log; true", 10*time.Second)
		if err := s.es.Launch(p); err != nil {
			add("error", "EmulationStation non ha avviato il gioco", err.Error(), "")
		} else {
			alive, exitedAt := 0, -1
			for i := 1; i <= 12; i++ {
				time.Sleep(time.Second)
				if j.Cancelled() {
					_ = s.es.EmuKill()
					return nil, errors.New("annullato")
				}
				g, _ := s.es.RunningGame()
				if g != nil {
					alive++
				} else if alive > 0 || i >= 3 {
					exitedAt = i
					break
				}
			}
			_ = s.es.EmuKill()
			time.Sleep(1500 * time.Millisecond)
			lg, _ := s.ssh.RunTimeout("echo '@@err'; tail -c 12000 /userdata/system/logs/es_launch_stderr.log 2>/dev/null; echo; echo '@@out'; tail -c 12000 /userdata/system/logs/es_launch_stdout.log 2>/dev/null", 20*time.Second)
			var interesting []string
			cmdLine := ""
			if lg != nil {
				ls := cab.SplitSections(lg.Stdout)
				for _, l := range cab.Lines(ls["out"] + "\n" + ls["err"]) {
					t := strings.TrimSpace(l)
					if t == "" {
						continue
					}
					if i := strings.Index(t, "command: ["); i >= 0 && cmdLine == "" {
						cmdLine = t[i+9:]
					}
					if reLogInteresting.MatchString(t) && !strings.Contains(t, "command:") && len(interesting) < 25 {
						interesting = append(interesting, t)
					}
				}
			}
			switch {
			case exitedAt >= 0:
				add("error", fmt.Sprintf("Il gioco si è chiuso da solo dopo %d s", exitedAt), "EmulationStation è tornato al menu: l'emulatore è uscito subito.", "")
			case alive > 0:
				add("ok", "Il gioco è rimasto in esecuzione per 12 s", "L'emulatore è partito e non è uscito: se vedi schermo nero o un errore a video, il problema è dentro il gioco (BIOS/ROM), non nell'avvio.", "")
			default:
				add("warn", "Non ho visto il gioco in esecuzione", "EmulationStation non ha mai segnalato un gioco in corso.", "")
			}
			if cmdLine != "" {
				add("info", "Comando usato da Batocera", cmdLine, "")
			}
			if len(interesting) > 0 {
				detail := strings.Join(interesting, "\n")
				low := strings.ToLower(detail)
				title := "Messaggi dell'emulatore"
				switch {
				case strings.Contains(low, "not found") && strings.Contains(low, "rom"):
					title = "MAME: file ROM non trovati nel set"
				case strings.Contains(low, "missing"):
					title = "File mancanti nel set (vedi messaggi)"
				case strings.Contains(low, "failed to load"):
					title = "RetroArch: il core non ha caricato il contenuto"
				case strings.Contains(low, "segmentation") || strings.Contains(low, "core dumped"):
					title = "L'emulatore è andato in crash"
				}
				add("warn", title, detail, "")
			} else if exitedAt >= 0 {
				add("info", "Nessun messaggio utile nei log di Batocera", "Guarda il messaggio che compare sullo schermo del cabinato all'avvio (FBNeo e MAME lo mostrano lì).", "")
			}
		}
	}
	summary := "Nessun problema evidente trovato."
	for _, it := range out {
		if it["level"] == "error" {
			summary = it["title"].(string)
			break
		}
	}
	return map[string]any{"items": out, "summary": summary, "matches": matches}, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Diagnosi di massa (senza avvio): per tutti gli zip arcade di una cartella,
// per ogni core: set completo? BIOS presente? Propone il core per gioco.
const zipCrcAllPy = `import sys, zipfile, json, os
d=sys.argv[1]; out={}
for n in sorted(os.listdir(d)):
    if not n.lower().endswith('.zip'): continue
    p=os.path.join(d,n)
    try:
        z=zipfile.ZipFile(p); out[n]=[i.CRC for i in z.infolist()]
    except Exception as e: out[n]={"error":str(e)}
print(json.dumps(out))`

func (s *Server) hGamesDiagAll(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		System string   `json:"system"`
		Apply  []string `json:"apply"` // "file.zip=core" da applicare
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.System == "vpinball" {
		if len(req.Apply) > 0 {
			return s.vpinballApply(req.Apply)
		}
		return s.diagVpinball()
	}
	if req.System == "" || !isArcadeSys(req.System) {
		return nil, errors.New("funziona per le cartelle arcade (mame, fbneo, neogeo, cps…) e vpinball")
	}
	sys := req.System
	q := cab.ShellQuote
	if len(req.Apply) > 0 {
		var cmds []string
		cmds = append(cmds, "mkdir -p "+backupDir+"; cp /userdata/system/batocera.conf "+backupDir+"/batocera.conf."+time.Now().Format("20060102-150405"))
		n := 0
		moved := 0
		for _, a := range req.Apply {
			if file, target, ok := strings.Cut(a, ">"); ok && !strings.Contains(a, "=") {
				if strings.ContainsAny(file, "'\"\n/") || strings.ContainsAny(target, "'\"\n/ ") || target == "" {
					continue
				}
				stem := strings.TrimSuffix(file, ".zip")
				cmds = append(cmds, "mkdir -p "+q("/userdata/roms/"+target)+" && mv -f "+q("/userdata/roms/"+sys+"/"+file)+" "+q("/userdata/roms/"+target+"/")+"; [ -d "+q("/userdata/roms/"+sys+"/"+stem)+" ] && mv -f "+q("/userdata/roms/"+sys+"/"+stem)+" "+q("/userdata/roms/"+target+"/")+"; bc_del "+q(sys+`["`+file+`"].core`)+"; bc_del "+q(sys+`["`+file+`"].emulator`)+"; true")
				moved++
				continue
			}
			file, core, ok := strings.Cut(a, "=")
			if !ok || strings.ContainsAny(file, "'\"\n") {
				continue
			}
			emu := "libretro"
			if core == "mame" {
				emu = "mame"
			}
			if core == "" {
				cmds = append(cmds, "bc_del "+q(sys+`["`+file+`"].core`), "bc_del "+q(sys+`["`+file+`"].emulator`))
			} else {
				cmds = append(cmds, "bc_set "+q(sys+`["`+file+`"].emulator`)+" "+emu, "bc_set "+q(sys+`["`+file+`"].core`)+" "+core)
			}
			n++
		}
		if _, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 5*time.Minute); err != nil {
			return nil, err
		}
		s.invalidateInfo()
		if moved > 0 {
			_ = s.es.ReloadGames()
		}
		return map[string]any{"applied": n, "moved": moved}, nil
	}
	j := s.jobs.Start("Controllo core "+sys, func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Leggo i CRC di tutti gli zip in roms/"+sys)
		res, err := s.ssh.RunCtx(j.Context(), "python3 -c "+q(zipCrcAllPy)+" "+q("/userdata/roms/"+sys)+"; echo '@@conf'; grep -E "+q("^"+regexp.QuoteMeta(sys)+`\[`)+" /userdata/system/batocera.conf 2>/dev/null; echo '@@cores'; ls /usr/lib/libretro/ 2>/dev/null | sed 's/_libretro.so//' | tr '\\n' ' '; echo '@@sysconf'; grep -E "+q("^"+regexp.QuoteMeta(sys)+`\.(core|emulator)=`)+" /userdata/system/batocera.conf 2>/dev/null", 20*time.Minute)
		if err != nil {
			return nil, err
		}
		sec := cab.SplitSections(res.Stdout)
		// la prima sezione (prima di @@conf) è il JSON
		jsonPart := res.Stdout
		if i := strings.Index(jsonPart, "@@conf"); i >= 0 {
			jsonPart = jsonPart[:i]
		}
		var all map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &all); err != nil {
			return nil, fmt.Errorf("lettura zip: %v", err)
		}
		cores := strings.Fields(sec["cores"])
		overrides := map[string]string{}
		for _, l := range cab.Lines(sec["conf"]) {
			k, v, ok := strings.Cut(l, "=")
			if ok && strings.HasSuffix(k, "].core") {
				if a := strings.Index(k, `["`); a >= 0 {
					if b := strings.Index(k[a+2:], `"]`); b >= 0 {
						overrides[k[a+2:a+2+b]] = strings.TrimSpace(v)
					}
				}
			}
		}
		sysCore := map[string]string{"fbneo": "fbneo", "mame": "mame", "neogeo": "fbneo", "cps1": "fbneo", "cps2": "fbneo", "cps3": "fbneo"}[sys]
		for _, l := range cab.Lines(sec["sysconf"]) {
			if k, v, ok := strings.Cut(l, "="); ok && k == sys+".core" && strings.TrimSpace(v) != "" {
				sysCore = strings.TrimSpace(v)
			}
		}
		dbs := []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"}
		loaded := map[string]bool{}
		for _, id := range dbs {
			if db, _ := s.dats.Load(id); db != nil {
				loaded[id] = true
			}
		}
		names := make([]string, 0, len(all))
		for n := range all {
			names = append(names, n)
		}
		sort.Strings(names)
		present := map[string]bool{}
		for _, n := range names {
			present[strings.ToLower(n)] = true
		}
		chds := map[string]bool{}
		for _, f := range strings.Fields(sec["chd"]) {
			chds[strings.ToLower(f)] = true
			chds[strings.ToLower(path.Base(f))] = true
		}
		var needExtras []string // "device.zip" mancanti in tutta la cartella (per il riepilogo)
		type row struct {
			File     string            `json:"file"`
			Current  string            `json:"current"`  // core in uso (override o sistema)
			Override bool              `json:"override"` // c'è un core per gioco
			Status   string            `json:"status"`   // ok | wrong_core | incomplete | unknown | empty | error
			Best     string            `json:"best"`     // core consigliato
			Cores    map[string]string `json:"cores"`    // core → "ok" | "mancano N/M" | "no bios X" | "n/d"
			Note     string            `json:"note"`
		}
		var rows []row
		counts := map[string]int{}
		for i, n := range names {
			j.SetProgress(float64(i)/float64(len(names)), n)
			stem := strings.ToLower(strings.TrimSuffix(n, ".zip"))
			rw := row{File: n, Cores: map[string]string{}}
			rw.Current = sysCore
			if c, ok := overrides[n]; ok {
				rw.Current, rw.Override = c, true
			}
			if m, ok := all[n].(map[string]any); ok {
				rw.Status, rw.Note = "error", fmt.Sprint(m["error"])
				rows = append(rows, rw)
				counts["error"]++
				continue
			}
			crcs, _ := all[n].([]any)
			if len(crcs) == 0 {
				rw.Status, rw.Note = "empty", "zip vuoto"
				rows = append(rows, rw)
				counts["empty"]++
				continue
			}
			have := map[uint32]bool{}
			for _, c := range crcs {
				if f, ok := c.(float64); ok {
					have[uint32(f)] = true
				}
			}
			known := false
			isBios := false
			anyHave := 0
			for _, id := range dbs {
				if !loaded[id] {
					continue
				}
				db, _ := s.dats.Load(id)
				h, t, missing, bios, ok := roms.SetMissing(db, stem, have)
				if !ok {
					continue
				}
				known = true
				if h > anyHave {
					anyHave = h
				}
				installed := contains(cores, id) || id == "mame"
				if db.Games[stem].IsBios {
					rw.Cores[id] = "bios"
					isBios = true
					continue
				}
				devs, disks := roms.SetExtras(db, stem)
				var missDev, missDisk []string
				for _, d := range devs {
					if !present[d+".zip"] {
						missDev = append(missDev, d+".zip")
					}
				}
				for _, d := range disks {
					if !chds[strings.ToLower(stem+"/"+d+".chd")] && !chds[strings.ToLower(d+".chd")] {
						missDisk = append(missDisk, d+".chd")
					}
				}
				switch {
				case !installed:
					rw.Cores[id] = "non installato"
				case len(missing) > 0:
					rw.Cores[id] = fmt.Sprintf("mancano %d/%d", len(missing), t)
				case bios != "" && !present[bios+".zip"]:
					rw.Cores[id] = "manca " + bios + ".zip"
				case len(missDev) > 0:
					rw.Cores[id] = "manca device " + strings.Join(missDev, ", ")
					for _, d := range missDev {
						needExtras = appendOnceS(needExtras, d)
					}
				case len(missDisk) > 0:
					rw.Cores[id] = "manca CHD " + strings.Join(missDisk, ", ")
				default:
					_ = h
					rw.Cores[id] = "ok"
					if rw.Best == "" {
						rw.Best = id
					}
				}
			}
			wrongSys, wrongWhy := "", ""
			if loaded["mame"] {
				if db, _ := s.dats.Load("mame"); db != nil {
					if t, w := db.DedicatedSystem(stem); t != "" && t != sys {
						wrongSys, wrongWhy = t, w
					}
				}
			}
			switch {
			case !known:
				rw.Status, rw.Note = "unknown", "nome non presente in nessun DAT (non è un set arcade?)"
			case wrongSys != "":
				rw.Status, rw.Best, rw.Note = "wrong_system", wrongSys, wrongWhy
			case isBios:
				rw.Status, rw.Note = "ok", "BIOS"
			case anyHave == 0:
				rw.Status, rw.Note = "mismatch", "nessun file dentro lo zip corrisponde al set arcade con questo nome: quasi certamente non è un gioco arcade (ROM console o file rinominato)"
			case !loaded[rw.Current]:
				rw.Status, rw.Note = "nodat", "DAT del core "+rw.Current+" non disponibile sul PC (scheda Database)"
			case rw.Cores[rw.Current] == "ok":
				rw.Status = "ok"
			case rw.Best != "":
				rw.Status = "wrong_core"
				rw.Note = "incompleto per " + rw.Current + " (" + rw.Cores[rw.Current] + "), completo per " + rw.Best
			default:
				rw.Status = "incomplete"
				parts := []string{}
				for _, id := range dbs {
					if v, ok := rw.Cores[id]; ok {
						parts = append(parts, id+": "+v)
					}
				}
				rw.Note = strings.Join(parts, " · ")
			}
			counts[rw.Status]++
			rows = append(rows, rw)
		}
		return map[string]any{"system": sys, "rows": rows, "counts": counts, "sys_core": sysCore, "missing_devices": needExtras}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func appendOnceS(l []string, v string) []string {
	for _, x := range l {
		if x == v {
			return l
		}
	}
	return append(l, v)
}

// diagVpinball: per ogni tavolo .vpx, la ROM PinMAME che usa: c'è? è completa (DAT MAME)?
const vpxScanPy = `import os, re, zipfile, json, configparser
root='/userdata/roms/vpinball'; out=[]
rx=re.compile(rb'cGameName\s*=\s*"([^"]+)"', re.I)
def rom_of(p):
    try:
        with open(p,'rb') as f:
            data=f.read()
        m=rx.search(data)
        return m.group(1).decode(errors='ignore').lower() if m else ''
    except Exception: return ''
glob=''
try:
    c=configparser.RawConfigParser(allow_no_value=True, strict=False); c.optionxform=str
    c.read('/userdata/system/configs/vpinball/VPinballX.ini')
    glob=c.get('Plugin.PinMAME','PinMAMEPath',fallback='').strip()
except Exception: pass
home=os.environ.get('HOME') or '/userdata/system'
def ci(d):
    if os.path.isdir(d): return d
    par,name=os.path.split(d)
    if not os.path.isdir(par): return ''
    for n in os.listdir(par):
        if n.lower()==name.lower() and os.path.isdir(os.path.join(par,n)): return os.path.join(par,n)
    return ''
def find(d, rom):
    d=ci(d)
    if not d: return ''
    for n in os.listdir(d):
        if n.lower()==rom+'.zip' and os.path.isfile(os.path.join(d,n)): return os.path.join(d,n)
    return ''
for dp, dn, fn in os.walk(root):
    if dp.count('/')-root.count('/')>1: continue
    for n in fn:
        if not n.lower().endswith('.vpx'): continue
        p=os.path.join(dp,n); rom=rom_of(p); rel=os.path.relpath(p,root)
        found=''; crcs=[]; wrong=''
        if rom:
            good=[os.path.join(dp,'pinmame','roms')]
            if glob and glob not in ('./','.'): good.append(os.path.join(glob,'roms'))
            good.append(os.path.join(home,'.pinmame','roms'))
            for g in good:
                found=find(g, rom)
                if found: break
            if not found:
                for b in [os.path.join(dp,'roms'), os.path.join(root,'roms'), os.path.join(root,'pinmame','roms'), '/userdata/system/configs/vpinball/pinmame/roms']:
                    wrong=find(b, rom)
                    if wrong: break
            z=found or wrong
            if z:
                try: crcs=[i.CRC for i in zipfile.ZipFile(z).infolist()]
                except Exception: crcs=[]
        out.append({"table":rel,"rom":rom,"zip":found,"wrong":wrong,"dst":os.path.join(dp,'pinmame','roms'),"crcs":crcs})
print(json.dumps({"glob":glob,"tables":out}))`

func (s *Server) diagVpinball() (any, error) {
	j := s.jobs.Start("Controllo tavoli Visual Pinball", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Leggo i tavoli e le ROM PinMAME")
		res, err := s.ssh.RunCtx(j.Context(), "python3 -c "+cab.ShellQuote(vpxScanPy), 10*time.Minute)
		if err != nil {
			return nil, err
		}
		var scan struct {
			Glob   string
			Tables []struct {
				Table, Rom, Zip, Wrong, Dst string
				Crcs                        []float64
			}
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(res.Stdout))), &scan); err != nil {
			return nil, fmt.Errorf("lettura tavoli: %v %s", err, res.Stderr)
		}
		tables := scan.Tables
		db, _ := s.dats.Load("mame")
		type row struct {
			File     string            `json:"file"`
			Current  string            `json:"current"`
			Override bool              `json:"override"`
			Status   string            `json:"status"`
			Best     string            `json:"best"`
			Cores    map[string]string `json:"cores"`
			Note     string            `json:"note"`
			Src      string            `json:"src,omitempty"`
			Dst      string            `json:"dst,omitempty"`
		}
		var rows []row
		counts := map[string]int{}
		for _, t := range tables {
			rw := row{File: t.Table, Current: "vpinball", Cores: map[string]string{}}
			switch {
			case t.Rom == "":
				rw.Status, rw.Note = "ok", "tavolo senza ROM PinMAME (o script non leggibile)"
			case t.Zip == "" && t.Wrong != "":
				rw.Status = "wrong_place"
				rw.Src, rw.Dst = t.Wrong, t.Dst
				rw.Best = strings.TrimPrefix(t.Dst, "/userdata/roms/vpinball/")
				rw.Note = "ROM " + t.Rom + ".zip in " + strings.TrimPrefix(t.Wrong, "/userdata/roms/vpinball/") + ": Visual Pinball di Batocera 43 la cerca in <tavolo>/pinmame/roms/ (non più in roms/), quindi il flipper parte senza ROM"
			case t.Zip == "":
				rw.Status, rw.Note = "incomplete", "manca la ROM PinMAME "+t.Rom+".zip (cercata in pinmame/roms/ accanto al tavolo e nella cartella PinMAME comune): caricala con Aggiungi ROM"
			default:
				rw.Note = "ROM " + t.Rom + ".zip in " + strings.TrimPrefix(t.Zip, "/userdata/roms/vpinball/")
				if db != nil && db.Games[t.Rom] != nil {
					have := map[uint32]bool{}
					for _, c := range t.Crcs {
						have[uint32(c)] = true
					}
					h, tot, missing, _, _ := roms.SetMissing(db, t.Rom, have)
					if len(missing) > 0 {
						rw.Status = "incomplete"
						rw.Note += fmt.Sprintf(" — INCOMPLETA: mancano %d/%d (%s): il flipper non si avvia", len(missing), tot, strings.Join(missing, ", "))
					} else {
						rw.Status = "ok"
						rw.Note += fmt.Sprintf(" — completa (%d file)", h)
					}
				} else {
					rw.Status = "ok"
					rw.Note += " — presente (non verificabile: DAT di MAME non disponibile o set non in MAME)"
				}
			}
			counts[rw.Status]++
			rows = append(rows, rw)
		}
		note := ""
		if scan.Glob == "" || scan.Glob == "./" || scan.Glob == "." {
			note = "VPinballX.ini non ha una cartella PinMAME comune: \"Applica\" imposta PinMAMEPath = " + vpxPinmameRoot + " (le ROM in vpinball/pinmame/roms/ valgono per tutti i tavoli)"
			counts["ini"] = 1
		}
		return map[string]any{"system": "vpinball", "rows": rows, "counts": counts, "sys_core": "vpinball", "note": note}, nil
	})
	return map[string]any{"job": j.ID}, nil
}
