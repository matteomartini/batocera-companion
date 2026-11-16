package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
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
		"echo '@@dir'; ls " + q("/userdata/roms/"+sys+"/") + " 2>/dev/null | tr '\\n' ' '"
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
			m := map[string]any{"core": id, "have": h, "total": t, "missing": missing, "bios": bios, "installed": installed, "complete": len(missing) == 0 && t > 0, "bios_missing": bios != "" && !dirFiles[bios+".zip"]}
			matches = append(matches, m)
			if m["complete"].(bool) && installed && !m["bios_missing"].(bool) && bestCore == "" {
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
				lines = append(lines, fmt.Sprintf("%s%s: %s", m["core"], inst, st))
				if m["core"] == coreUsed && m["complete"].(bool) && !m["bios_missing"].(bool) {
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
				if m["bios_missing"].(bool) && (m["core"] == coreUsed || m["core"] == bestCore) {
					add("error", fmt.Sprintf("Manca il BIOS %s.zip nella cartella %s", m["bios"], sys), "I giochi di questa scheda caricano le ROM di sistema da quello zip, nella stessa cartella del gioco. Libreria ROM → \"Copia i BIOS nelle cartelle ROM\" lo sistema se il BIOS è in /userdata/bios.", "")
					break
				}
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
