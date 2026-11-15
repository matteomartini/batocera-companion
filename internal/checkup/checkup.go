// Package checkup: controlli sul cabinato con correzioni proposte.
package checkup

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"batocompanion/internal/cab"
)

type Fix struct {
	Label          string   `json:"label"`
	Description    string   `json:"description"`
	Commands       []string `json:"commands"`
	NeedsRestartES bool     `json:"needs_restart_es"`
}

type Finding struct {
	ID     string `json:"id"`
	Area   string `json:"area"`
	Level  string `json:"level"` // error | warn | info | ok
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Manual string `json:"manual,omitempty"`
	Fix    *Fix   `json:"fix,omitempty"`
}

type Report struct {
	Score    int            `json:"score"`
	Counts   map[string]int `json:"counts"`
	Findings []Finding      `json:"findings"`
}

type Options struct {
	CompanionVersion string
	ArcadeDats       int // DAT arcade presenti sul PC
}

func set(k, v string) string { return fmt.Sprintf("batocera-settings-set %s %s", k, cab.ShellQuote(v)) }

func Run(in *cab.Info, opt *Options) *Report {
	if opt == nil {
		opt = &Options{}
	}
	var f []Finding
	add := func(x Finding) { f = append(f, x) }
	conf := in.BatoceraConf
	get := func(k string) string { return strings.TrimSpace(conf[k]) }

	// ---------- controller ----------
	if len(in.Joysticks) == 0 {
		add(Finding{ID: "ctl.none", Area: "controller", Level: "error", Title: "Nessun joystick rilevato", Detail: "Batocera non vede nessun encoder/joystick USB. Controlla il cavo dell'encoder della plancia e che sia alimentato.", Manual: "Scollega e ricollega l'encoder, poi riavvia il cabinato."})
	} else {
		names := []string{}
		for _, j := range in.Joysticks {
			names = append(names, j.Name)
		}
		add(Finding{ID: "ctl.found", Area: "controller", Level: "ok", Title: fmt.Sprintf("%d joystick rilevati", len(in.Joysticks)), Detail: strings.Join(names, ", ")})
		configured := map[string]cab.InputConfig{}
		for _, ic := range in.ESInput {
			if ic.Type == "joystick" {
				configured[strings.TrimSpace(ic.DeviceName)] = ic
			}
		}
		std := []string{"up", "down", "left", "right", "a", "b", "x", "y", "start", "select", "pageup", "pagedown", "hotkey"}
		for i, j := range in.Joysticks {
			ic, ok := configured[strings.TrimSpace(j.Name)]
			if !ok {
				add(Finding{ID: fmt.Sprintf("ctl.noconf.%d", i), Area: "controller", Level: "error", Title: fmt.Sprintf("Controller %d \"%s\" non configurato in EmulationStation", i+1, j.Name), Detail: "Senza configurazione i tasti non funzionano nei menu e nei giochi.", Manual: "Dal cabinato: Menu → Impostazioni controller → Configura un controller, e assegna tutti i tasti. Per la plancia: Hotkey = Coin."})
				if i == 0 {
					break
				}
				continue
			}
			var missing []string
			for _, k := range std {
				if _, ok := ic.Inputs[k]; !ok {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				lvl := "warn"
				if contains(missing, "hotkey") || len(missing) > 5 {
					lvl = "error"
				}
				det := fmt.Sprintf("%d input mappati su %d standard.", len(ic.Inputs), len(std))
				if contains(missing, "hotkey") {
					det += " Senza Hotkey non puoi uscire dai giochi né usare le scorciatoie (salva/carica/menu)."
				}
				if len(missing) > 5 {
					det += " Sembra una configurazione interrotta a metà: rifalla da capo."
				}
				add(Finding{ID: fmt.Sprintf("ctl.missing.%d", i), Area: "controller", Level: lvl, Title: fmt.Sprintf("Controller %d \"%s\": mancano %s", i+1, j.Name, strings.Join(missing, ", ")), Detail: det, Manual: "Riconfigura il controller da Batocera (Impostazioni controller → Configura un controller) e assegna il tasto mancante. Per la plancia: Hotkey = Coin."})
			} else {
				add(Finding{ID: fmt.Sprintf("ctl.ok.%d", i), Area: "controller", Level: "ok", Title: fmt.Sprintf("Controller %d \"%s\" configurato", i+1, j.Name)})
			}
			// stessa configurazione riusata per il secondo (stesso nome): va bene
			if i == 1 && in.Joysticks[0].Name == j.Name {
				add(Finding{ID: "ctl.sameguid", Area: "controller", Level: "info", Title: "I due encoder hanno lo stesso GUID", Detail: "Normale con due DragonRise identici: ES li distingue solo per \"device path\"/ordine. Se P1 e P2 si scambiano dopo un riavvio, collega gli encoder sempre alle stesse porte USB del mini PC."})
			}
		}
		if get("controllers.joystick.p1.sdl") == "" && get("controllers.p1.guid") == "" && len(in.Joysticks) > 1 {
			add(Finding{ID: "ctl.order", Area: "controller", Level: "info", Title: "Player 1 / Player 2 non assegnati esplicitamente", Detail: "Con due encoder identici (DragonRise) Batocera può invertire P1 e P2 a ogni avvio, perché decide in base all'ordine USB."})
		}
	}

	// ---------- hotkey / plancia ----------
	if strings.Contains(in.Hotkeys, "hotkey") || strings.Contains(in.Hotkeys, "button") {
		add(Finding{ID: "hk.ok", Area: "hotkey", Level: "ok", Title: "Scorciatoie joystick attive", Detail: firstLines(in.Hotkeys, 3)})
	}
	if strings.Contains(in.Hotkeys, "exit:") && !strings.Contains(in.Hotkeys, "double") {
		add(Finding{ID: "hk.single", Area: "hotkey", Level: "info", Title: "Uscita dal gioco con pressione singola", Detail: "Su una plancia è facile premere Coin+Start per sbaglio durante una partita. Puoi richiedere la doppia pressione per uscire.", Fix: &Fix{Label: "Doppia pressione", Description: "hotkey.exit.double=1", Commands: []string{set("global.hotkey.exit.double", "1")}}})
	}

	// ---------- BIOS ----------
	bySys := map[string][]cab.BiosItem{}
	for _, b := range in.BiosMissing {
		bySys[b.System] = append(bySys[b.System], b)
	}
	if len(in.BiosMissing) == 0 {
		add(Finding{ID: "bios.ok", Area: "bios", Level: "ok", Title: "Tutti i BIOS richiesti sono presenti"})
	} else {
		keys := make([]string, 0, len(bySys))
		for k := range bySys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, sys := range keys {
			items := bySys[sys]
			has := in.RomCounts[strings.ToLower(strings.Fields(sys)[0])] > 0
			lvl := "info"
			t := fmt.Sprintf("BIOS %s: %d file mancanti o errati", sys, len(items))
			if has {
				lvl = "error"
			} else {
				t += " (nessuna ROM per questo sistema: ignorabile)"
			}
			var det []string
			for i, b := range items {
				if i >= 6 {
					det = append(det, fmt.Sprintf("… e altri %d", len(items)-6))
					break
				}
				det = append(det, fmt.Sprintf("%s  [%s] %s", b.Path, b.Status, b.Detail))
			}
			add(Finding{ID: "bios." + slug(sys), Area: "bios", Level: lvl, Title: t, Detail: strings.Join(det, "\n"), Manual: "Copia i file in /userdata/bios dalla scheda BIOS del Companion (nome esatto, dentro le sottocartelle indicate)."})
		}
	}

	// ---------- ROM ----------
	if in.RomTotal == 0 {
		add(Finding{ID: "rom.none", Area: "rom", Level: "warn", Title: "Nessuna ROM trovata", Detail: "Le cartelle /userdata/roms sono vuote. Usa \"Aggiungi ROM\"."})
	} else {
		var parts []string
		keys := make([]string, 0, len(in.RomCounts))
		for k, v := range in.RomCounts {
			if v > 0 {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return in.RomCounts[keys[i]] > in.RomCounts[keys[j]] })
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, in.RomCounts[k]))
		}
		add(Finding{ID: "rom.total", Area: "rom", Level: "ok", Title: fmt.Sprintf("%d file ROM in %d sistemi", in.RomTotal, len(keys)), Detail: strings.Join(parts, " · ")})
		if (in.RomCounts["mame"] > 0 || in.RomCounts["fbneo"] > 0) && opt.ArcadeDats == 0 {
			add(Finding{ID: "rom.nodat", Area: "rom", Level: "info", Title: "Nessun DAT arcade sul PC", Detail: "Senza DAT non posso verificare che i set arcade siano della versione giusta: scarica/genera i DAT nella sezione Database. FBNeo vuole il romset FBNeo più recente (coincide quasi sempre con MAME 0.2xx attuale). Aggiungi ROM lo verifica."})
		}
	}
	// core per gioco non installati
	reOv := regexp.MustCompile(`^([a-z0-9_]+)\["[^"]+"\]\.core$`)
	var badCores []string
	for k, v := range conf {
		if m := reOv.FindStringSubmatch(k); m != nil && len(in.Cores) > 0 && !contains(in.Cores, v) && v != "mame" {
			badCores = append(badCores, k+"="+v)
		}
	}
	if len(badCores) > 0 {
		sort.Strings(badCores)
		add(Finding{ID: "rom.badcore", Area: "rom", Level: "error", Title: fmt.Sprintf("%d giochi impostati su core non installati", len(badCores)), Detail: strings.Join(firstN(badCores, 8), "\n") + "\nQuesti giochi non partono: all'avvio Batocera cerca un core che non c'è.", Manual: "Dalla scheda Check-up → Libreria ROM → \"Togli i core non installati\", oppure cambia core dal menu del gioco."})
	}

	// ---------- video ----------
	if in.RefreshHz > 0 && (in.RefreshHz < 59.5 || in.RefreshHz > 60.5) {
		fx := (*Fix)(nil)
		if m := regexp.MustCompile(`(\d+x\d+)\s+(?:[\d.]+[* +]*\s+)*(60\.0\d)`).FindStringSubmatch(in.Xrandr); m != nil {
			fx = &Fix{Label: "Forza 60 Hz", Description: "global.videomode=" + m[1] + "." + m[2], Commands: []string{set("global.videomode", m[1]+"."+m[2])}}
		}
		add(Finding{ID: "video.hz", Area: "video", Level: "error", Title: fmt.Sprintf("Lo schermo va a %.2f Hz, non a 60", in.RefreshHz), Detail: "Un gioco a 60 fps su uno schermo che non va a 60 Hz salta o ripete fotogrammi di continuo: è la causa più comune di stutter su tutti i giochi.", Fix: fx, Manual: "Imposta la modalità video a 60 Hz (Impostazioni di sistema → Modalità video, o global.videomode)."})
	} else if in.RefreshHz > 0 {
		add(Finding{ID: "video.hz", Area: "video", Level: "ok", Title: fmt.Sprintf("Schermo %s a %.2f Hz", in.ResolutionCur, in.RefreshHz)})
	}
	if v := get("global.videomode"); v == "" || v == "auto" {
		add(Finding{ID: "video.mode", Area: "video", Level: "info", Title: "Modalità video dei giochi: automatica", Detail: "Con il pannello " + nz(in.ResolutionCur, "del cabinato") + " collegato via HDMI alla scheda VSDISPLAY è la scelta corretta: lasciala così finché non vedi problemi di cambio risoluzione all'avvio dei giochi."})
	}
	if v := get("global.ratio"); v == "" || v == "auto" {
		add(Finding{ID: "video.ratio", Area: "video", Level: "info", Title: "Rapporto d'aspetto globale: automatico", Detail: "Sul monitor 4:3 (" + nz(in.ResolutionCur, "1280x960") + ") molti core allargano l'immagine. Per i giochi arcade e le console classiche il rapporto giusto è \"core provided\" (quello originale del gioco), con bande laterali minime.", Fix: &Fix{Label: "Usa 'core provided'", Description: "global.ratio=core", Commands: []string{set("global.ratio", "core")}}})
	}
	if get("global.smooth") == "1" {
		add(Finding{ID: "video.smooth", Area: "video", Level: "info", Title: "Filtro bilineare (smooth) attivo", Detail: "Sfuma i pixel: su un pannello LCD l'arcade rende meglio con i pixel netti più uno shader scanline, oppure con l'integer scale.", Fix: &Fix{Label: "Spegni smooth", Description: "global.smooth=0", Commands: []string{set("global.smooth", "0")}}})
	}
	if v := get("global.shaderset"); v == "" || v == "none" {
		add(Finding{ID: "video.shader", Area: "video", Level: "info", Title: "Nessuno shader impostato", Detail: "Per il look da CRT dentro il cabinato: shader \"crt-geom\" (leggero) o \"scanlines\". Puoi impostarlo globale e poi togliergli nei singoli sistemi dove non piace.", Fix: &Fix{Label: "Shader scanlines", Description: "global.shaderset=scanlines", Commands: []string{set("global.shaderset", "scanlines")}}})
	}
	if get("global.showFPS") == "1" {
		add(Finding{ID: "video.fps", Area: "video", Level: "warn", Title: "Contatore FPS a schermo attivo", Detail: "Scrive i fotogrammi al secondo in un angolo. Utile per capire se un core va a scatti (sotto 60 fissi): accendilo per fare le prove, poi spegnilo.", Fix: &Fix{Label: "Spegni FPS", Description: "global.showFPS=0", Commands: []string{set("global.showFPS", "0")}}})
	}

	// ---------- audio ----------
	if in.AudioOut == "" || in.AudioOut == "auto" {
		add(Finding{ID: "audio.auto", Area: "audio", Level: "info", Title: "Uscita audio: automatica", Detail: "Batocera sceglie l'uscita audio da solo. Con la scheda VSDISPLAY l'audio deve uscire dall'HDMI del mini PC: se non senti nulla, seleziona esplicitamente l'uscita HDMI (Impostazioni di sistema → Uscita audio).\nUscite disponibili:\n" + strings.TrimSpace(in.AudioCards)})
	}
	if get("audio.bgmusic") != "0" {
		add(Finding{ID: "audio.bgm", Area: "audio", Level: "info", Title: "Musica di sottofondo nel menu attiva", Detail: "Batocera suona la sua musica nei menu. Se vuoi il cabinato silenzioso quando non giochi (o per i video), spegnila.", Fix: &Fix{Label: "Spegni musica menu", Description: "audio.bgmusic=0", Commands: []string{set("audio.bgmusic", "0")}, NeedsRestartES: true}})
	}

	// ---------- prestazioni ----------
	if in.Governor != "" && in.Governor != "performance" && get("system.cpu.governor") != "performance" {
		add(Finding{ID: "perf.governor", Area: "prestazioni", Level: "warn", Title: "Governor CPU: " + in.Governor, Detail: "Con la CPU in risparmio energetico la frequenza sale in ritardo e i giochi scattano. Meglio 'performance'.", Fix: &Fix{Label: "Imposta performance", Description: "system.cpu.governor=performance", Commands: []string{set("system.cpu.governor", "performance"), "echo performance > /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null; for c in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do echo performance > $c 2>/dev/null; done; true"}}})
	} else if in.Governor != "" {
		add(Finding{ID: "perf.governor", Area: "prestazioni", Level: "ok", Title: "Governor CPU: " + in.Governor})
	}
	if in.CPUTempC >= 85 {
		add(Finding{ID: "perf.temp", Area: "prestazioni", Level: "error", Title: fmt.Sprintf("CPU a %.0f °C", in.CPUTempC), Detail: "Sopra gli 85 °C la CPU rallenta (throttling) e i giochi scattano.", Manual: "Verifica che le prese d'aria del mini PC non siano coperte e valuta una ventola da 80/120 mm nel retro del cabinato."})
	} else if in.CPUTempC >= 75 {
		add(Finding{ID: "perf.temp", Area: "prestazioni", Level: "warn", Title: fmt.Sprintf("CPU a %.0f °C", in.CPUTempC), Detail: "Temperatura alta per un mini PC chiuso in un mobile.", Manual: "Verifica che le prese d'aria del mini PC non siano coperte e valuta una ventola da 80/120 mm nel retro del cabinato."})
	}
	if in.DiskFree > 0 && in.DiskFree < 5e9 {
		add(Finding{ID: "perf.disk", Area: "prestazioni", Level: "warn", Title: fmt.Sprintf("Spazio libero: %.1f GB", float64(in.DiskFree)/1e9), Detail: "Sotto i 5 GB i salvataggi, gli screenshot e gli aggiornamenti possono fallire."})
	}
	if get("global.rewind") == "1" {
		add(Finding{ID: "perf.rewind", Area: "prestazioni", Level: "warn", Title: "Rewind attivo globalmente", Detail: "Il rewind salva lo stato decine di volte al secondo: costa CPU e introduce micro-scatti. Su una plancia non serve.", Fix: &Fix{Label: "Spegni rewind", Description: "global.rewind=0", Commands: []string{set("global.rewind", "0")}}})
	}
	if v := get("global.runahead"); v == "" || v == "0" {
		add(Finding{ID: "perf.runahead", Area: "prestazioni", Level: "info", Title: "Run-ahead non attivo", Detail: "Per i picchiaduro (Street Fighter, KOF) run-ahead = 1 toglie un frame di ritardo ai comandi. Sull'i5-10400T lo regge per arcade a 16 bit e Neo Geo. Meglio attivarlo solo su fbneo/neogeo, non globale.", Fix: &Fix{Label: "Run-ahead 1 su fbneo e neogeo", Description: "fbneo.runahead=1, neogeo.runahead=1", Commands: []string{set("fbneo.runahead", "1"), set("neogeo.runahead", "1"), set("fbneo.secondinstance", "1"), set("neogeo.secondinstance", "1")}}})
	}
	if get("global.autosave") == "1" {
		add(Finding{ID: "perf.autosave", Area: "prestazioni", Level: "info", Title: "Salvataggio automatico stato attivo", Detail: "Con autosave Batocera riprende il gioco dove l'avevi lasciato. Per un cabinato arcade di solito non si vuole: lascialo spento.", Fix: &Fix{Label: "Spegni autosave", Description: "global.autosave=0", Commands: []string{set("global.autosave", "0")}}})
	}
	if strings.Contains(in.Processes, "ha-batocera") || strings.Contains(in.Processes, "xrandr") {
		add(Finding{ID: "perf.haagent", Area: "prestazioni", Level: "warn", Title: "Il vecchio agente Home Assistant interroga lo schermo", Detail: "Quella lettura blocca lo schermo per un attimo: scatti periodici in tutti i giochi. Reinstalla l'agente da Impostazioni → Home Assistant → Installa."})
	}
	for _, l := range cab.Lines(in.Processes) {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[1] == "ffmpeg") {
			add(Finding{ID: "perf.capture", Area: "prestazioni", Level: "info", Title: "Una cattura dello schermo (ffmpeg) è in corso", Detail: "Lo schermo in diretta o una registrazione del Companion: chiudili quando non servono, costano CPU."})
			break
		}
	}

	// ---------- rete / sistema ----------
	if !in.Samba && get("system.samba.enabled") != "1" {
		add(Finding{ID: "net.samba", Area: "rete", Level: "warn", Title: "Condivisione di rete (Samba) non attiva", Detail: "Senza Samba non puoi aprire \\\\BATOCERA\\share da Windows. Questa app usa SSH e funziona lo stesso.", Fix: &Fix{Label: "Attiva Samba", Description: "system.samba.enabled=1 (serve riavvio)", Commands: []string{set("system.samba.enabled", "1")}}})
	}
	if !in.ESApiOpen {
		add(Finding{ID: "net.esapi", Area: "rete", Level: "warn", Title: "API web di EmulationStation chiusa alla rete", Detail: "Di serie EmulationStation risponde sulla porta 1234 solo in locale: questa app non può vedere il gioco in corso, avviare/chiudere giochi o ricaricare la lista (semaforo rosso su \"EmulationStation\").", Fix: &Fix{Label: "Apri l'API", Description: "system.es.api=1 (ES riavviato)", Commands: []string{set("system.es.api", "1")}, NeedsRestartES: true}})
	} else {
		add(Finding{ID: "net.esapi", Area: "rete", Level: "ok", Title: "API web di EmulationStation raggiungibile"})
	}
	if in.Timezone == "" {
		add(Finding{ID: "sys.tz", Area: "impostazioni", Level: "info", Title: "Fuso orario non impostato", Detail: "L'orologio di Batocera userà UTC: orari sbagliati nelle statistiche e negli screenshot.", Fix: &Fix{Label: "Europe/Rome", Description: "system.timezone=Europe/Rome", Commands: []string{set("system.timezone", "Europe/Rome")}}})
	}
	if strings.EqualFold(in.Hostname, "BATOCERA") {
		add(Finding{ID: "sys.hostname", Area: "impostazioni", Level: "info", Title: "Nome in rete: BATOCERA (predefinito)", Detail: "Comodo rinominarlo, es. ARCADE, così lo trovi come \\\\ARCADE\\share.", Fix: &Fix{Label: "Chiama ARCADE", Description: "system.hostname=ARCADE (dal prossimo riavvio)", Commands: []string{set("system.hostname", "ARCADE")}}})
	}
	if in.Wifi && len(in.IPs) > 1 {
		add(Finding{ID: "net.wifi", Area: "rete", Level: "info", Title: "Wi-Fi attivo insieme al cavo", Detail: "Il cabinato è su cavo? Se sì puoi spegnere il Wi-Fi: meno latenza per Chiaki e per il controllo remoto.", Fix: &Fix{Label: "Spegni Wi-Fi", Description: "wifi.enabled=0", Commands: []string{set("wifi.enabled", "0")}}})
	}
	if get("system.power.switch") == "" {
		add(Finding{ID: "sys.power", Area: "impostazioni", Level: "info", Title: "Nessun power switch configurato", Detail: "Quando aggiungerai l'ESP32 o il relè con GPIO/USB per lo spegnimento, qui si imposta il tipo di switch (system.power.switch)."})
	}
	if get("updates.enabled") != "0" {
		add(Finding{ID: "sys.updates", Area: "impostazioni", Level: "info", Title: "Controllo aggiornamenti automatico attivo", Detail: "Batocera avvisa quando c'è una nuova versione. Un aggiornamento maggiore può cambiare le versioni dei core (e quindi i romset richiesti): aggiorna solo quando hai tempo di verificare."})
	}
	if v := in.ESSettings["ScreenSaverBehavior"]; v == "" || v == "dim" {
		add(Finding{ID: "sys.saver", Area: "impostazioni", Level: "info", Title: "Salvaschermo: oscuramento (predefinito)", Detail: "Dentro il cabinato fa più scena il salvaschermo con i video dei giochi (\"random video\"), che serve anche come attract mode per le riprese."})
	}
	if in.ESSettings["ScreenScraperUser"] == "" && in.ESSettings["Scraper"] == "" {
		add(Finding{ID: "sys.scraper", Area: "impostazioni", Level: "info", Title: "Scraper mai configurato", Detail: "Copertine, video e descrizioni dei giochi si scaricano con lo scraper (ScreenScraper è il più completo; richiede un account gratuito). Dal Companion: Impostazioni → ScreenScraper."})
	}

	// ---------- hardware ----------
	tier := cpuTier(in)
	add(Finding{ID: "hw.cpu", Area: "sistema", Level: "info", Title: "PC del cabinato: " + nz(in.CPUModel, "sconosciuto"), Detail: tier})
	if strings.Contains(in.Dmesg, "GPU HANG") || strings.Contains(in.Dmesg, "gpu hang") {
		add(Finding{ID: "hw.gpuhang", Area: "sistema", Level: "warn", Title: "Il kernel segnala blocchi della GPU (GPU hang)", Detail: firstLines(in.Dmesg, 4)})
	}
	if strings.Contains(in.ESLog, "Segmentation fault") || strings.Contains(in.ESLog, "core dumped") {
		add(Finding{ID: "sys.eslog", Area: "sistema", Level: "warn", Title: "L'ultimo gioco avviato è andato in crash", Detail: "Se il gioco non parte, il motivo è qui: di solito BIOS mancante, core non adatto o ROM del set sbagliato.\n" + lastLines(in.ESLog, 6)})
	}

	// ---------- Companion ----------
	if in.Companion["version"] == "" && in.Companion["companion"] == "" {
		add(Finding{ID: "comp.server", Area: "companion", Level: "info", Title: "Server del Companion non installato sul cabinato", Detail: "Con il server installato la web app sul telefono funziona anche a PC spento (Impostazioni → Server sul cabinato)."})
	}

	// ---------- punteggio ----------
	rep := &Report{Counts: map[string]int{}, Findings: f}
	score := 100
	for _, x := range f {
		rep.Counts[x.Level]++
		switch x.Level {
		case "error":
			score -= 12
		case "warn":
			score -= 5
		}
	}
	if score < 0 {
		score = 0
	}
	rep.Score = score
	order := map[string]int{"error": 0, "warn": 1, "info": 2, "ok": 3}
	sort.SliceStable(rep.Findings, func(i, j int) bool { return order[rep.Findings[i].Level] < order[rep.Findings[j].Level] })
	return rep
}

func cpuTier(in *cab.Info) string {
	m := strings.ToLower(in.CPUModel)
	switch {
	case strings.Contains(m, "i9") || strings.Contains(m, "i7") || strings.Contains(m, "ryzen 7") || strings.Contains(m, "ryzen 9"):
		return "CPU di fascia alta: regge tutto, PS2 e Saturn compresi; shader CRT pesanti e run-ahead 2."
	case strings.Contains(m, "i5") || strings.Contains(m, "ryzen 5"):
		return "CPU di fascia media (6 core): ottima per tutto il 2D e per PS1/N64/Dreamcast; PS2 e Saturn solo con core leggeri; shader CRT leggeri (scanlines/retro), run-ahead 1."
	case strings.Contains(m, "i3") || strings.Contains(m, "ryzen 3") || strings.Contains(m, "pentium"):
		return "CPU di fascia bassa: perfetta per arcade 2D, 8/16 bit e PS1; evita PS2, Saturn e shader pesanti."
	case strings.Contains(m, "celeron") || strings.Contains(m, "atom") || strings.Contains(m, "n100") || strings.Contains(m, "j4"):
		return "CPU leggera: arcade classici e 8/16 bit; PS1 al limite; niente shader."
	}
	return fmt.Sprintf("%d core", in.CPUCount)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
func nz(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
func slug(s string) string {
	return strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(s, "-"))
}
func firstN(l []string, n int) []string {
	if len(l) > n {
		return l[:n]
	}
	return l
}
func firstLines(s string, n int) string { return strings.Join(firstN(cab.Lines(s), n), "\n") }
func lastLines(s string, n int) string {
	l := cab.Lines(s)
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}
