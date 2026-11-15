package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"batocompanion/internal/cab"
)

// Pagina Emulatori: impostazioni per sistema in batocera.conf con spiegazioni e consigli.

type emuOpt struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type emuSetting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Options []emuOpt `json:"options,omitempty"`
	Value   string   `json:"value"`
	Global  string   `json:"global,omitempty"`
	Desc    string   `json:"desc"`
	Perf    string   `json:"perf,omitempty"`
}
type emuReco struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

func is2D(sys string) bool {
	switch sys {
	case "psx", "n64", "dreamcast", "saturn", "ps2", "psp", "gamecube", "wii", "naomi", "atomiswave", "3do", "model2", "model3", "nds", "3ds":
		return false
	}
	return true
}

func (s *Server) hEmu(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		return s.emuSave(r)
	}
	in, err := s.collectInfo(false)
	if err != nil {
		return nil, err
	}
	sys := r.URL.Query().Get("system")
	tier := "fascia media"
	cpu := strings.ToLower(in.CPUModel)
	switch {
	case strings.Contains(cpu, "i7") || strings.Contains(cpu, "i9") || strings.Contains(cpu, "ryzen 7"):
		tier = "fascia alta: regge PS2/Saturn e shader pesanti"
	case strings.Contains(cpu, "i5") || strings.Contains(cpu, "ryzen 5"):
		tier = "fascia media (6 core): 2D, PS1, N64 e Dreamcast senza problemi; PS2 e Saturn con core leggeri; shader CRT leggeri"
	case strings.Contains(cpu, "i3") || strings.Contains(cpu, "celeron") || strings.Contains(cpu, "pentium"):
		tier = "fascia bassa: arcade, 8/16 bit e PS1; niente shader pesanti"
	}
	conf := in.BatoceraConf
	if sys != "" {
		// Rilettura diretta: la cache info può essere vecchia di minuti.
		if res, err := s.ssh.RunTimeout(`grep -v '^[[:space:]]*#' /userdata/system/batocera.conf 2>/dev/null | grep '='`, 30*time.Second); err == nil {
			conf = map[string]string{}
			for _, l := range strings.Split(res.Stdout, "\n") {
				if k, v, ok := strings.Cut(l, "="); ok {
					conf[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
	}
	if sys == "" {
		var out []map[string]any
		names := make([]string, 0, len(in.Systems))
		for n := range in.Systems {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			es := in.Systems[n]
			custom := 0
			for k := range conf {
				if strings.HasPrefix(k, n+".") && !strings.Contains(k, "[") {
					custom++
				}
			}
			out = append(out, map[string]any{"name": n, "fullname": es.Fullname, "games": in.RomCounts[n], "custom": custom})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i]["games"].(int) > out[j]["games"].(int) })
		return map[string]any{"cpu": in.CPUModel, "tier_desc": tier, "systems": out}, nil
	}
	es, ok := in.Systems[sys]
	if !ok {
		return nil, errors.New("sistema sconosciuto al cabinato")
	}
	g := func(k string) string { return strings.TrimSpace(conf[sys+"."+k]) }
	gl := func(k string) string { return strings.TrimSpace(conf["global."+k]) }
	var emuOpts []emuOpt
	emuOpts = append(emuOpts, emuOpt{"", "Automatico (predefinito: " + es.Default + ")"})
	emus := make([]string, 0, len(es.Emulators))
	for e := range es.Emulators {
		emus = append(emus, e)
	}
	sort.Strings(emus)
	for _, e := range emus {
		for _, c := range es.Emulators[e] {
			lab := e
			if c != e {
				lab = e + " / " + c
			}
			emuOpts = append(emuOpts, emuOpt{e + "/" + c, lab})
		}
	}
	curEmu := ""
	if g("emulator") != "" || g("core") != "" {
		curEmu = g("emulator") + "/" + g("core")
		if g("emulator") == "" {
			for e, cs := range es.Emulators {
				for _, c := range cs {
					if c == g("core") {
						curEmu = e + "/" + c
					}
				}
			}
		}
	}
	settings := []emuSetting{
		{Key: "emulator", Label: "Emulatore / core", Type: "select", Options: emuOpts, Value: curEmu, Desc: "Il programma che fa girare i giochi di questo sistema. Il predefinito di Batocera è quasi sempre la scelta giusta; cambialo solo se un gioco non parte o scatta.", Perf: "i core 'leggeri' (es. mame2003_plus, YabaSanshiro, pcsx_rearmed) chiedono meno CPU di quelli più fedeli"},
		{Key: "ratio", Label: "Rapporto d'aspetto", Type: "select", Options: []emuOpt{{"", "Automatico"}, {"core", "Originale del gioco (core provided)"}, {"4/3", "4:3"}, {"16/9", "16:9"}, {"full", "Schermo intero"}, {"squarepixel", "Pixel quadrati"}, {"custom", "Personalizzato"}}, Value: g("ratio"), Global: gl("ratio"), Desc: "Come viene adattata l'immagine allo schermo 4:3 del cabinato. 'Originale' mostra il gioco com'era in sala, con bande minime."},
		{Key: "smooth", Label: "Filtro bilineare (smooth)", Type: "bool", Value: g("smooth"), Global: gl("smooth"), Desc: "Ammorbidisce i pixel. Sui giochi 2D (pixel art) rende tutto sfocato: meglio spento. Sui giochi 3D (PS1, N64, Dreamcast) riduce le seghettature: meglio acceso."},
		{Key: "shaderset", Label: "Shader", Type: "select", Options: []emuOpt{{"", "Globale"}, {"none", "Nessuno"}, {"scanlines", "Scanlines"}, {"retro", "Retro"}, {"crt-geom", "CRT geom (curvo, leggero)"}, {"crt-pi", "CRT pi (leggero)"}, {"crt-easymode", "CRT easymode"}, {"crt-guest-advanced", "CRT guest advanced (pesante)"}, {"crt-royale", "CRT royale (molto pesante)"}, {"enhanced", "Enhanced (2D levigato)"}, {"zfast", "zfast (CRT leggerissimo)"}}, Value: g("shaderset"), Global: gl("shaderset"), Desc: "Effetto applicato all'immagine. Gli shader CRT simulano il monitor a tubo di un cabinato vero.", Perf: "royale e guest chiedono una GPU dedicata; sull'Intel UHD usa geom, pi, zfast o scanlines"},
		{Key: "integerscale", Label: "Scala intera (pixel perfect)", Type: "bool", Value: g("integerscale"), Global: gl("integerscale"), Desc: "Ingrandisce l'immagine solo di multipli interi: pixel tutti uguali e nessuna sfocatura, ma possono comparire bordi neri."},
		{Key: "rewind", Label: "Rewind (riavvolgi)", Type: "bool", Value: g("rewind"), Global: gl("rewind"), Desc: "Permette di tornare indietro nel gioco con una combinazione hotkey.", Perf: "salva lo stato di continuo: micro-scatti e CPU in più. Spento su un cabinato"},
		{Key: "runahead", Label: "Run-ahead (frame anticipati)", Type: "select", Options: []emuOpt{{"", "Globale / spento"}, {"0", "Spento"}, {"1", "1 frame"}, {"2", "2 frame"}, {"3", "3 frame"}}, Value: g("runahead"), Global: gl("runahead"), Desc: "Toglie 1-3 frame di ritardo ai comandi emulando in anticipo. Fondamentale per i picchiaduro.", Perf: "ogni frame raddoppia circa il lavoro del core: 1 è sicuro sul 2D e 16 bit; evita su PS1/N64/Dreamcast"},
		{Key: "secondinstance", Label: "Run-ahead: seconda istanza", Type: "bool", Value: g("secondinstance"), Global: gl("secondinstance"), Desc: "Usa un secondo core in parallelo per il run-ahead: più fluido, un po' più di CPU."},
		{Key: "bezel", Label: "Cornice (bezel)", Type: "select", Options: []emuOpt{{"", "Globale"}, {"none", "Nessuna"}, {"default", "Predefinita"}, {"thebezelproject", "The Bezel Project"}, {"consoles", "Console"}}, Value: g("bezel"), Global: gl("bezel"), Desc: "Grafica decorativa intorno all'immagine. Sullo schermo 4:3 del cabinato le bande sono strette: la cornice copre pochi pixel e costa GPU per niente."},
		{Key: "videomode", Label: "Modalità video", Type: "select", Options: []emuOpt{{"", "Globale / automatica"}, {"1280x960.60.00", "1280x960 60 Hz"}, {"1024x768.60.00", "1024x768 60 Hz"}, {"640x480.60.00", "640x480 60 Hz"}}, Value: g("videomode"), Global: gl("videomode"), Desc: "Risoluzione dello schermo durante il gioco. Lascia automatico: il pannello va a 1280x960 e un cambio modo all'avvio può fallire."},
		{Key: "autosave", Label: "Salvataggio automatico stato", Type: "bool", Value: g("autosave"), Global: gl("autosave"), Desc: "Riprende il gioco dove l'avevi lasciato. Per un cabinato arcade di solito non si vuole: lascialo spento."},
		{Key: "showFPS", Label: "Mostra FPS", Type: "bool", Value: g("showFPS"), Global: gl("showFPS"), Desc: "Contatore dei fotogrammi in un angolo: utile per diagnosticare gli scatti, poi spegnilo."},
	}
	var reco []emuReco
	if is2D(sys) {
		reco = append(reco, emuReco{"smooth", "0", "pixel art: pixel netti"})
		if sys == "fbneo" || sys == "neogeo" || sys == "mame" || strings.HasPrefix(sys, "cps") {
			reco = append(reco, emuReco{"runahead", "1", "picchiaduro e arcade: un frame di ritardo in meno, la CPU lo regge"})
			reco = append(reco, emuReco{"secondinstance", "1", "run-ahead più fluido"})
			reco = append(reco, emuReco{"ratio", "core", "rapporto originale del gioco"})
		}
	} else {
		reco = append(reco, emuReco{"smooth", "1", "3D: meno seghettature"})
		reco = append(reco, emuReco{"runahead", "0", "3D: troppo pesante"})
		if sys == "saturn" {
			reco = append(reco, emuReco{"emulator", "libretro/yabasanshiro", "YabaSanshiro: il core Saturn più leggero; beetle-saturn è più fedele ma chiede una CPU molto più veloce."})
		}
		if sys == "n64" {
			reco = append(reco, emuReco{"emulator", "libretro/mupen64plus-next", "Mupen64Plus-Next: buon compromesso su GPU Intel"})
		}
	}
	reco = append(reco, emuReco{"rewind", "0", "niente micro-scatti"})
	reco = append(reco, emuReco{"bezel", "none", "sul 4:3 non serve"})
	overrides := 0
	for k := range conf {
		if strings.HasPrefix(k, sys+"[") {
			overrides++
		}
	}
	return map[string]any{"fullname": es.Fullname, "games": in.RomCounts[sys], "settings": settings, "recommended": reco, "per_game_overrides": overrides}, nil
}

func (s *Server) emuSave(r *http.Request) (any, error) {
	var req struct {
		System string            `json:"system"`
		Values map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.System == "" {
		return nil, errors.New("sistema mancante")
	}
	cmds := []string{"mkdir -p " + backupDir + "; cp /userdata/system/batocera.conf " + backupDir + "/batocera.conf." + time.Now().Format("20060102-150405")}
	var log []string
	for k, v := range req.Values {
		if !regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString(k) {
			continue
		}
		if k == "emulator" {
			e, c, _ := strings.Cut(v, "/")
			if v == "" {
				cmds = append(cmds, "bc_del "+req.System+".emulator", "bc_del "+req.System+".core")
				log = append(log, req.System+".emulator/core → automatico")
			} else {
				cmds = append(cmds, "batocera-settings-set "+req.System+".emulator "+cab.ShellQuote(e), "batocera-settings-set "+req.System+".core "+cab.ShellQuote(c))
				log = append(log, req.System+".emulator="+e, req.System+".core="+c)
			}
			continue
		}
		full := req.System + "." + k
		if v == "" {
			cmds = append(cmds, "bc_del "+full)
			log = append(log, full+" → automatico (riga tolta)")
		} else {
			cmds = append(cmds, "batocera-settings-set "+full+" "+cab.ShellQuote(v))
			log = append(log, full+"="+v)
		}
	}
	cmds = append(cmds, "echo @@verify; grep -E '^"+regexp.QuoteMeta(req.System)+"\\.' /userdata/system/batocera.conf | grep -v '\\['")
	res, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 2*time.Minute)
	if err != nil {
		return nil, err
	}
	if res.Exit != 0 {
		return nil, fmt.Errorf("comando fallito (exit %d): %s", res.Exit, strings.TrimSpace(res.Stderr))
	}
	_, after, _ := strings.Cut(res.Stdout, "@@verify")
	now := map[string]string{}
	for _, l := range strings.Split(after, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			now[strings.TrimPrefix(k, req.System+".")] = v
		}
	}
	var missing []string
	for k, v := range req.Values {
		kk := k
		if k == "emulator" {
			e, c, _ := strings.Cut(v, "/")
			if v != "" && (now["emulator"] != e || now["core"] != c) {
				missing = append(missing, k)
			}
			continue
		}
		if v != "" && now[kk] != v {
			missing = append(missing, k)
		}
	}
	s.invalidateInfo()
	if len(missing) > 0 {
		return nil, fmt.Errorf("scrittura non riuscita per: %s (batocera.conf attuale: %v)", strings.Join(missing, ", "), now)
	}
	log = append(log, "", "Verifica su batocera.conf: ok ("+strings.TrimSpace(after)+")")
	return map[string]any{"log": log}, nil

}
