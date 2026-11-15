package roms

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/dat"
)

type CoreMatch struct {
	Core     string `json:"core"`
	Complete bool   `json:"complete"`
	Have     int    `json:"have"`
	Total    int    `json:"total"`
	Folder   string `json:"folder"`
}

type Item struct {
	ID            string      `json:"id"`
	Sources       []string    `json:"sources"`
	DisplayName   string      `json:"display_name"`
	Size          int64       `json:"size"`
	GameName      string      `json:"game_name"`
	System        string      `json:"system"`
	SystemOptions []string    `json:"system_options"`
	Core          string      `json:"core"`
	CoreMatches   []CoreMatch `json:"core_matches"`
	PerGameCore   bool        `json:"per_game_core"`
	Action        string      `json:"action"` // copy | rebuild | extract | chd | skip
	Exists        bool        `json:"exists"`
	DestName      string      `json:"dest_name"`
	Notes         []string    `json:"notes"`
	Missing       []string    `json:"missing"`
	Confidence    string      `json:"confidence"` // green | yellow | red
	Skip          bool        `json:"skip"`

	// interni
	Kind    string            `json:"kind"`            // arcade | console | doom
	Files   []Entry           `json:"-"`               // file da scrivere nello zip ricostruito
	Extra   []string          `json:"extra,omitempty"` // file aggiuntivi da copiare (bin di un cue)
	SetName string            `json:"-"`               // nome set arcade
	ByCore  map[string]*setCk `json:"-"`
	entries []Entry
	crc     uint32
	crcNoHd uint32
	ext     string
}

type setCk struct {
	core    string
	folder  string
	needed  []dat.Rom
	found   []Entry
	missing []string
	asIs    bool // lo zip originale è già esatto
}

type Session struct {
	ID      string
	Items   []*Item
	Created time.Time
	byID    map[string]*Item
	mu      sync.Mutex
}

type Analyzer struct {
	Dats   *dat.Manager
	Info   *cab.Info
	SSH    *cab.SSH
	Pref   []string
	UseCHD bool
	Log    func(string)
	Step   func(float64, string)
}

var reDisc = regexp.MustCompile(`(?i)[\s_(\[-]*(disc|disk|cd)\s*0*(\d+)`)

// Analyze esamina percorsi (file o cartelle) e produce il piano di copia.
func (a *Analyzer) Analyze(ctx context.Context, paths []string) (*Session, error) {
	var files []string
	for _, p := range paths {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		st, err := os.Stat(p)
		if err != nil {
			a.logf("non trovato: %s", p)
			continue
		}
		if st.IsDir() {
			_ = filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if strings.HasPrefix(d.Name(), ".") {
					return nil
				}
				files = append(files, q)
				return nil
			})
		} else {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("nessun file da analizzare")
	}
	sort.Strings(files)
	sess := &Session{ID: fmt.Sprintf("s%d", time.Now().UnixNano()), Created: time.Now(), byID: map[string]*Item{}}

	// 1. inventario: entries e CRC
	type src struct {
		path    string
		entries []Entry
		size    int64
	}
	var srcs []src
	used := map[string]bool{}
	cueBins := map[string]bool{}
	for i, f := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a.step(float64(i)/float64(len(files))*0.6, "Leggo "+filepath.Base(f))
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".cue" || ext == ".gdi" {
			for _, b := range cueRefs(f) {
				cueBins[strings.ToLower(b)] = true
			}
		}
		if IsArchive(f) {
			ent, err := ListArchive(f)
			if err != nil {
				a.logf("%s: archivio non leggibile (%v)", filepath.Base(f), err)
				continue
			}
			srcs = append(srcs, src{f, ent, st.Size()})
			continue
		}
		srcs = append(srcs, src{f, nil, st.Size()})
	}

	// pool CRC di tutti i file (per ricostruire set da parent/merged)
	pool := map[uint32][]Entry{}
	for _, s := range srcs {
		for _, e := range s.entries {
			pool[e.CRC] = append(pool[e.CRC], e)
		}
	}

	arcadeDBs := a.arcadeDBs()
	consoleDBs := a.consoleDBs()
	hasArcadeDat := len(arcadeDBs) > 0

	n := 0
	for _, s := range srcs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		n++
		a.step(0.6+float64(n)/float64(len(srcs))*0.35, "Analizzo "+filepath.Base(s.path))
		lower := strings.ToLower(filepath.Base(s.path))
		if used[s.path] || cueBins[lower] || cueBins[strings.ToLower(s.path)] {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(s.path), filepath.Ext(s.path))
		it := &Item{ID: fmt.Sprintf("i%d", len(s.path)+n*7919), Sources: []string{s.path}, DisplayName: filepath.Base(s.path), Size: s.size, ext: strings.ToLower(filepath.Ext(s.path)), entries: s.entries}
		it.ID = fmt.Sprintf("i%d", n)
		if IsArchive(s.path) {
			// arcade?
			if a.tryArcade(it, strings.ToLower(base), arcadeDBs, pool, hasArcadeDat) {
				sess.Items = append(sess.Items, it)
				continue
			}
			// archivio console: singola ROM dentro
			roms := romEntries(s.entries)
			if len(roms) == 1 {
				e := roms[0]
				it.crc = e.CRC
				it.ext = strings.ToLower(filepath.Ext(e.Name))
				it.GameName = strings.TrimSuffix(e.Name, filepath.Ext(e.Name))
				a.classifyConsole(it, consoleDBs, e.Name, nil)
				if it.System != "" && a.systemAccepts(it.System, ".zip") && it.ext != ".cue" && strings.ToLower(filepath.Ext(s.path)) == ".zip" {
					it.Action = "copy"
					it.DestName = it.GameName + ".zip"
				} else {
					it.Action = "extract"
					it.Files = []Entry{e}
					it.DestName = it.GameName + it.ext
				}
				if it.System == "" {
					it.Action = "skip"
					it.Confidence = "red"
					it.Notes = append(it.Notes, "sistema non riconosciuto: scegli il sistema a mano")
					it.Action = "extract"
				}
				sess.Items = append(sess.Items, it)
				continue
			}
			if len(roms) > 1 {
				// zip con più file: cue/bin dentro o set non riconosciuto
				it.Action = "extract"
				it.Files = roms
				cue := ""
				for _, e := range roms {
					if strings.HasSuffix(strings.ToLower(e.Name), ".cue") {
						cue = e.Name
					}
				}
				if cue != "" {
					it.GameName = strings.TrimSuffix(cue, filepath.Ext(cue))
					it.DestName = it.GameName + "/" + cue
					it.ext = ".cue"
					a.classifyConsole(it, consoleDBs, cue, nil)
					it.Notes = append(it.Notes, fmt.Sprintf("archivio con %d file (cue+bin): viene estratto in una cartella", len(roms)))
				} else {
					it.GameName = base
					it.DestName = base + "/"
					it.Notes = append(it.Notes, fmt.Sprintf("archivio con %d file non riconosciuto come set arcade", len(roms)))
					if hasArcadeDat {
						it.Notes = append(it.Notes, "nome non presente nei DAT arcade: se è un set arcade rinominalo con il nome corto MAME")
					}
					it.Confidence = "red"
					it.Action = "skip"
				}
				sess.Items = append(sess.Items, it)
				continue
			}
			it.Action = "skip"
			it.Confidence = "red"
			it.Notes = append(it.Notes, "archivio vuoto o senza file ROM")
			sess.Items = append(sess.Items, it)
			continue
		}
		// file sciolto
		head := readHead(s.path, 128*1024)
		if it.ext == ".wad" || it.ext == ".pk3" || it.ext == ".ipk3" {
			a.classifyDoom(it, head)
			sess.Items = append(sess.Items, it)
			continue
		}
		if it.ext == ".cue" || it.ext == ".gdi" {
			refs := cueRefs(s.path)
			var extra []string
			var total int64 = s.size
			var first string
			for _, r := range refs {
				p := filepath.Join(filepath.Dir(s.path), r)
				if st, err := os.Stat(p); err == nil {
					extra = append(extra, p)
					total += st.Size()
					if first == "" {
						first = p
					}
				} else {
					it.Notes = append(it.Notes, "manca il file "+r)
					it.Confidence = "red"
				}
			}
			it.Extra = extra
			it.Size = total
			it.GameName = base
			if first != "" {
				head = readHead(first, 256*1024)
			}
			a.classifyConsole(it, consoleDBs, filepath.Base(s.path), head)
			it.DestName = base + it.ext
			if len(extra) > 0 && it.ext == ".cue" && it.System != "" {
				it.Notes = append(it.Notes, fmt.Sprintf("%d file collegati", len(extra)))
			}
			if a.UseCHD && it.Confidence != "red" && len(extra) > 0 {
				it.Action = "chd"
				it.DestName = base + ".chd"
			} else if it.Confidence != "red" {
				it.Action = "copy"
			} else {
				it.Action = "skip"
			}
			sess.Items = append(sess.Items, it)
			continue
		}
		if it.ext == ".m3u" {
			it.Action = "copy"
			it.GameName = base
			it.DestName = filepath.Base(s.path)
			a.classifyConsole(it, consoleDBs, filepath.Base(s.path), nil)
			it.Notes = append(it.Notes, "playlist multi-disco: copio anche i dischi se sono nella selezione")
			sess.Items = append(sess.Items, it)
			continue
		}
		it.GameName = base
		if s.size > 0 && s.size <= 256*1024*1024 {
			crc, _, err := FileCRC(s.path, 0)
			if err == nil {
				it.crc = crc
			}
		}
		a.classifyConsole(it, consoleDBs, filepath.Base(s.path), head)
		if it.System != "" {
			if skip := HeaderSkip(it.System, s.size, head); skip > 0 && it.GameName == base && it.crcNoHd == 0 {
				if c, _, err := FileCRC(s.path, skip); err == nil {
					it.crcNoHd = c
					a.matchConsoleCRC(it, consoleDBs)
				}
			}
		}
		if it.DestName == "" {
			it.DestName = it.GameName + it.ext
		}
		if it.Action == "" {
			it.Action = "copy"
		}
		if (it.ext == ".iso" || it.ext == ".img" || it.ext == ".bin") && a.UseCHD && it.Confidence != "red" && it.System != "" && isDiscSystem(it.System) {
			it.Action = "chd"
			it.DestName = it.GameName + ".chd"
		}
		sess.Items = append(sess.Items, it)
	}

	// 3. doppioni: stesso set arcade da più sorgenti → resta il migliore
	best := map[string]*Item{}
	for _, it := range sess.Items {
		if it.Kind != "arcade" || it.Action == "skip" {
			continue
		}
		key := it.System + "/" + it.DestName
		if b := best[key]; b == nil || (it.Action == "copy" && b.Action != "copy") {
			if b != nil {
				b.Skip = true
				b.Notes = append(b.Notes, "doppione: viene copiata l'altra copia dello stesso set")
			}
			best[key] = it
		} else {
			it.Skip = true
			it.Notes = append(it.Notes, "doppione: stesso set già presente nella selezione")
		}
	}
	// 4. esistenza sul cabinato
	a.step(0.97, "Controllo cosa c'è già sul cabinato")
	a.checkExists(sess.Items)
	for _, it := range sess.Items {
		it.finalize()
		sess.byID[it.ID] = it
	}
	sess.RefreshPerGameCore(a.Info)
	return sess, nil
}

func (a *Analyzer) logf(f string, args ...any) {
	if a.Log != nil {
		a.Log(fmt.Sprintf(f, args...))
	}
}
func (a *Analyzer) step(p float64, s string) {
	if a.Step != nil {
		a.Step(p, s)
	}
}

func readHead(p string, n int) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	k, _ := f.Read(b)
	return b[:k]
}

var reCueFile = regexp.MustCompile(`(?im)^\s*FILE\s+"?([^"\n]+?)"?\s+(BINARY|MOTOROLA|WAVE|MP3|AIFF)\s*$`)

func cueRefs(p string) []string {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []string
	if strings.HasSuffix(strings.ToLower(p), ".gdi") {
		for _, l := range strings.Split(string(b), "\n")[1:] {
			f := strings.Fields(l)
			if len(f) >= 5 {
				out = append(out, strings.Trim(f[4], `"`))
			}
		}
		return out
	}
	for _, m := range reCueFile.FindAllStringSubmatch(string(b), -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

var junkExt = map[string]bool{".txt": true, ".nfo": true, ".dat": true, ".xml": true, ".jpg": true, ".png": true, ".url": true, ".diz": true, ".ini": true, ".sav": true, ".srm": true, ".cfg": true}

func romEntries(es []Entry) []Entry {
	var out []Entry
	for _, e := range es {
		if junkExt[strings.ToLower(filepath.Ext(e.Name))] || strings.HasPrefix(e.Name, "__MACOSX") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ---------- arcade ----------

type arcadeDB struct {
	core   string
	db     *dat.DB
	folder string
}

func (a *Analyzer) arcadeDBs() []arcadeDB {
	var out []arcadeDB
	order := a.Pref
	if len(order) == 0 {
		order = []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"}
	}
	for _, core := range order {
		e := dat.Lookup(core)
		if e == nil || e.Kind != "arcade" {
			continue
		}
		db, err := a.Dats.Load(core)
		if err != nil {
			a.logf("DAT %s: %v", core, err)
			continue
		}
		if db == nil {
			continue
		}
		if a.Info != nil && len(a.Info.Cores) > 0 && !contains(a.Info.Cores, core) && core != "mame" {
			continue
		}
		folder := "mame"
		if core == "fbneo" {
			folder = "fbneo"
		}
		out = append(out, arcadeDB{core, db, folder})
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// neededRoms: ROM del set (non-merged: clone + parent) escludendo quelle del BIOS.
func neededRoms(db *dat.DB, name string) ([]dat.Rom, string) {
	g := db.Games[name]
	if g == nil {
		return nil, ""
	}
	seen := map[string]bool{}
	var out []dat.Rom
	bios := ""
	cur := g
	for cur != nil {
		if cur.IsBios {
			bios = cur.Name
			break
		}
		for _, r := range cur.Roms {
			key := strings.ToLower(r.Name)
			if seen[key] || seen[fmt.Sprintf("crc%08x", r.CRC)] {
				continue
			}
			// rom che appartiene al bios (merge nel romof bios)
			if r.Merge != "" && cur.CloneOf == "" && cur.RomOf != "" {
				if b := db.Games[cur.RomOf]; b != nil && b.IsBios {
					bios = b.Name
					continue
				}
			}
			seen[key] = true
			if r.CRC != 0 {
				seen[fmt.Sprintf("crc%08x", r.CRC)] = true
			}
			out = append(out, r)
		}
		next := cur.CloneOf
		if next == "" {
			next = cur.RomOf
		}
		if next == "" {
			break
		}
		cur = db.Games[next]
	}
	return out, bios
}

func (a *Analyzer) tryArcade(it *Item, name string, dbs []arcadeDB, pool map[uint32][]Entry, hasDat bool) bool {
	if !hasDat {
		// senza DAT: euristica sul nome
		if len(name) <= 12 && !strings.ContainsAny(name, " ()[]") && len(it.entries) >= 2 {
			it.Kind = "arcade"
			it.SetName = name
			it.GameName = name
			it.System = "fbneo"
			it.SystemOptions = []string{"fbneo", "mame", "neogeo"}
			it.Action = "copy"
			it.DestName = name + ".zip"
			it.Confidence = "yellow"
			it.Notes = append(it.Notes, "nessun DAT arcade: copiato senza verifica del set")
			return true
		}
		return false
	}
	it.ByCore = map[string]*setCk{}
	var matches []CoreMatch
	found := false
	bestName := ""
	var desc string
	for _, adb := range dbs {
		g := adb.db.Games[name]
		if g == nil {
			continue
		}
		if g.IsBios || g.IsDevice {
			// BIOS: copia così com'è
			found = true
		}
		found = true
		if desc == "" {
			desc = g.Desc
		}
		need, bios := neededRoms(adb.db, name)
		ck := &setCk{core: adb.core, folder: adb.folder}
		if g.RomOf == "neogeo" || bios == "neogeo" {
			if adb.core == "fbneo" {
				ck.folder = "neogeo"
			}
		}
		own := map[uint32]Entry{}
		for _, e := range it.entries {
			own[e.CRC] = e
		}
		asIs := len(own) == len(need) && len(need) > 0
		var have int
		for _, r := range need {
			if r.CRC == 0 {
				continue
			}
			if e, ok := own[r.CRC]; ok {
				have++
				ck.found = append(ck.found, Entry{Name: r.Name, Size: r.Size, CRC: r.CRC, Archive: e.Archive, Inner: e.Inner, Path: e.Path})
				if !strings.EqualFold(e.Name, r.Name) {
					asIs = false
				}
				continue
			}
			if es := pool[r.CRC]; len(es) > 0 {
				e := es[0]
				have++
				asIs = false
				ck.found = append(ck.found, Entry{Name: r.Name, Size: r.Size, CRC: r.CRC, Archive: e.Archive, Inner: e.Inner, Path: e.Path})
				continue
			}
			if r.Opt {
				continue
			}
			ck.missing = append(ck.missing, r.Name)
		}
		ck.needed = need
		ck.asIs = asIs
		total := 0
		for _, r := range need {
			if r.CRC != 0 && !r.Opt {
				total++
			}
		}
		complete := len(ck.missing) == 0 && total > 0
		if g.IsBios {
			complete = len(ck.missing) == 0
		}
		it.ByCore[adb.core] = ck
		matches = append(matches, CoreMatch{Core: adb.core, Complete: complete, Have: have, Total: total, Folder: ck.folder})
		if bios != "" && complete {
			it.Notes = appendOnce(it.Notes, "richiede il BIOS "+bios+".zip nella stessa cartella")
		}
	}
	if !found {
		return false
	}
	it.Kind = "arcade"
	it.SetName = name
	it.GameName = desc
	if it.GameName == "" {
		it.GameName = name
	}
	it.CoreMatches = matches
	bestName = ""
	for _, m := range matches {
		if m.Complete {
			bestName = m.Core
			break
		}
	}
	if bestName == "" {
		// nessun core completo: prendo il migliore per segnalare cosa manca
		best := -1
		for _, m := range matches {
			if m.Have > best {
				best = m.Have
				bestName = m.Core
			}
		}
		it.Core = bestName
		ck := it.ByCore[bestName]
		it.Missing = ck.missing
		it.System = ck.folder
		it.SystemOptions = []string{"fbneo", "mame", "neogeo"}
		it.Action = "skip"
		it.Confidence = "red"
		it.DestName = name + ".zip"
		it.Notes = append(it.Notes, fmt.Sprintf("set incompleto per tutti i core: il migliore è %s (%d file su %d)", bestName, ck.foundCount(), len(ck.needed)))
		return true
	}
	it.applyCore(bestName)
	return true
}

func (ck *setCk) foundCount() int { return len(ck.found) }

func appendOnce(l []string, s string) []string {
	for _, x := range l {
		if x == s {
			return l
		}
	}
	return append(l, s)
}

// applyCore imposta core, cartella, azione e file per un set arcade.
func (it *Item) applyCore(core string) {
	ck := it.ByCore[core]
	if ck == nil {
		return
	}
	it.Core = core
	it.System = ck.folder
	it.SystemOptions = []string{ck.folder}
	for _, m := range it.CoreMatches {
		if m.Complete && !contains(it.SystemOptions, m.Folder) {
			it.SystemOptions = append(it.SystemOptions, m.Folder)
		}
	}
	it.Missing = nil
	it.DestName = it.SetName + ".zip"
	if ck.asIs {
		it.Action = "copy"
		it.Files = nil
	} else {
		it.Action = "rebuild"
		it.Files = ck.found
	}
	it.Confidence = "green"
	if len(ck.missing) > 0 {
		it.Missing = ck.missing
		it.Action = "skip"
		it.Confidence = "red"
	}
}

// ---------- console ----------

type consoleDB struct {
	entry dat.Entry
	db    *dat.DB
}

func (a *Analyzer) consoleDBs() []consoleDB {
	var out []consoleDB
	for _, e := range dat.Catalog {
		if e.Kind != "console" {
			continue
		}
		db, err := a.Dats.Load(e.ID)
		if err != nil || db == nil {
			continue
		}
		out = append(out, consoleDB{e, db})
	}
	return out
}

// estensione → sistemi candidati (in ordine di preferenza)
var extSystems = map[string][]string{
	".nes": {"nes"}, ".unh": {"nes"}, ".fds": {"fds"}, ".sfc": {"snes"}, ".smc": {"snes"}, ".fig": {"snes"},
	".n64": {"n64"}, ".z64": {"n64"}, ".v64": {"n64"}, ".gb": {"gb"}, ".gbc": {"gbc"}, ".gba": {"gba"}, ".vb": {"virtualboy"}, ".nds": {"nds"},
	".md": {"megadrive"}, ".gen": {"megadrive"}, ".smd": {"megadrive"}, ".sms": {"mastersystem"}, ".gg": {"gamegear"}, ".32x": {"sega32x"}, ".sg": {"sg1000"},
	".pce": {"pcengine"}, ".sgx": {"supergrafx"}, ".a26": {"atari2600"}, ".a52": {"atari5200"}, ".a78": {"atari7800"}, ".lnx": {"lynx"}, ".lyx": {"lynx"},
	".ngp": {"ngp"}, ".ngc": {"ngpc"}, ".ws": {"wswan"}, ".wsc": {"wswanc"}, ".col": {"colecovision"}, ".min": {"pokemini"}, ".pbp": {"psp"}, ".cso": {"psp"},
	".gdi": {"dreamcast"}, ".cdi": {"dreamcast"}, ".rom": {"msx1"}, ".mx1": {"msx1"}, ".mx2": {"msx2"}, ".dsk": {"msx1"}, ".3ds": {"3ds"}, ".cia": {"3ds"},
	".rvz": {"gamecube", "wii"}, ".gcz": {"gamecube"}, ".wbfs": {"wii"}, ".wad": {"prboom"},
	".bin": {"megadrive", "atari2600", "psx", "sega32x", "colecovision"}, ".iso": {"psx", "ps2", "psp", "dreamcast", "saturn", "gamecube"},
	".img": {"psx"}, ".cue": {"psx", "segacd", "pcenginecd", "saturn", "dreamcast"}, ".chd": {"psx", "ps2", "dreamcast", "saturn", "segacd", "pcenginecd", "naomi"},
}

func (a *Analyzer) systemAccepts(system, ext string) bool {
	if a.Info == nil {
		return true
	}
	s, ok := a.Info.Systems[system]
	if !ok {
		return true
	}
	return contains(s.Extensions, strings.TrimPrefix(strings.ToLower(ext), "."))
}

func isDiscSystem(s string) bool {
	switch s {
	case "psx", "ps2", "dreamcast", "saturn", "segacd", "pcenginecd", "naomi", "atomiswave", "3do":
		return true
	}
	return false
}

func headerSystem(head []byte) string {
	if len(head) < 0x200 {
		return ""
	}
	h := head
	switch {
	case bytes.Contains(h[0x100:0x120], []byte("SEGA 32X")):
		return "sega32x"
	case bytes.Contains(h[0x100:0x120], []byte("SEGA MEGA DRIVE")), bytes.Contains(h[0x100:0x120], []byte("SEGA GENESIS")):
		return "megadrive"
	case bytes.HasPrefix(h, []byte("SEGADISCSYSTEM")), bytes.Contains(h[:0x20], []byte("SEGADISCSYSTEM")):
		return "segacd"
	case bytes.Contains(h, []byte("SEGA SEGAKATANA")):
		return "dreamcast"
	case bytes.Contains(h, []byte("SEGA SEGASATURN")):
		return "saturn"
	case bytes.Contains(h, []byte("PC Engine CD-ROM SYSTEM")):
		return "pcenginecd"
	case bytes.Contains(h, []byte("BOOT2")):
		return "ps2"
	case bytes.Contains(h, []byte("PLAYSTATION")) || bytes.Contains(h, []byte("BOOT =")):
		return "psx"
	case bytes.Contains(h, []byte("PSP GAME")):
		return "psp"
	case bytes.HasPrefix(h, []byte("NES\x1a")):
		return "nes"
	}
	return ""
}

func (a *Analyzer) classifyConsole(it *Item, dbs []consoleDB, fname string, head []byte) {
	ext := strings.ToLower(filepath.Ext(fname))
	if it.ext == "" {
		it.ext = ext
	}
	// 1. CRC nei DAT
	if it.crc != 0 && a.matchConsoleCRC(it, dbs) {
		return
	}
	// 2. header
	if hs := headerSystem(head); hs != "" {
		it.System = hs
		it.SystemOptions = uniq(append([]string{hs}, extSystems[ext]...))
		it.Confidence = "yellow"
		it.Notes = append(it.Notes, "sistema riconosciuto dall'intestazione del file")
		a.consoleNameMatch(it, dbs)
		return
	}
	// 3. estensione
	cands := extSystems[ext]
	if a.Info != nil {
		for name, s := range a.Info.Systems {
			if contains(s.Extensions, strings.TrimPrefix(ext, ".")) && !contains(cands, name) {
				cands = append(cands, name)
			}
		}
	}
	// indizio dalla cartella di origine
	for _, src := range it.Sources {
		dir := strings.ToLower(filepath.Base(filepath.Dir(src)))
		for _, c := range cands {
			if dir == c {
				cands = append([]string{c}, cands...)
			}
		}
	}
	cands = uniq(cands)
	it.SystemOptions = cands
	if len(cands) == 0 {
		it.Confidence = "red"
		it.Notes = append(it.Notes, "estensione "+ext+" non associata a nessun sistema del cabinato")
		return
	}
	it.System = cands[0]
	if len(cands) == 1 {
		it.Confidence = "yellow"
		it.Notes = append(it.Notes, "riconosciuto solo dall'estensione (non trovato nei DAT)")
	} else {
		it.Confidence = "yellow"
		it.Notes = append(it.Notes, "estensione ambigua: controlla il sistema")
	}
	a.consoleNameMatch(it, dbs)
}

func uniq(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range l {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (a *Analyzer) matchConsoleCRC(it *Item, dbs []consoleDB) bool {
	for _, c := range []uint32{it.crc, it.crcNoHd} {
		if c == 0 {
			continue
		}
		for _, d := range dbs {
			ms := d.db.ByCRC(c)
			if len(ms) == 0 {
				continue
			}
			m := ms[0]
			it.System = d.entry.Systems[0]
			it.SystemOptions = uniq(append([]string{it.System}, d.entry.Systems...))
			it.GameName = m.Game.Name
			if m.Game.Desc != "" {
				it.GameName = m.Game.Desc
			}
			it.Confidence = "green"
			it.DestName = m.Rom.Name
			if filepath.Ext(m.Rom.Name) == "" {
				it.DestName = m.Rom.Name + it.ext
			}
			it.Notes = append(it.Notes, "riconosciuto nel DAT "+d.entry.Name)
			return true
		}
	}
	return false
}

// consoleNameMatch: per immagini CD (niente CRC) prova il nome nel DAT del sistema scelto.
func (a *Analyzer) consoleNameMatch(it *Item, dbs []consoleDB) {
	base := strings.TrimSuffix(filepath.Base(it.Sources[0]), filepath.Ext(it.Sources[0]))
	for _, d := range dbs {
		if !contains(d.entry.Systems, it.System) {
			continue
		}
		if g := d.db.ByName(base); g != nil {
			it.GameName = g.Name
			if g.Desc != "" {
				it.GameName = g.Desc
			}
			it.Confidence = "green"
			it.Notes = append(it.Notes, "nome trovato nel DAT "+d.entry.Name)
			return
		}
	}
}

func (a *Analyzer) classifyDoom(it *Item, head []byte) {
	it.Kind = "doom"
	it.GameName = strings.TrimSuffix(it.DisplayName, filepath.Ext(it.DisplayName))
	it.DestName = it.DisplayName
	it.Action = "copy"
	iwad := bytes.HasPrefix(head, []byte("IWAD"))
	pwad := bytes.HasPrefix(head, []byte("PWAD"))
	known := map[string]string{"doom": "Doom", "doom1": "Doom (shareware)", "doom2": "Doom II", "tnt": "Final Doom: TNT", "plutonia": "Final Doom: Plutonia", "heretic": "Heretic", "hexen": "Hexen", "strife1": "Strife", "chex": "Chex Quest", "chex3": "Chex Quest 3", "freedoom1": "Freedoom Phase 1", "freedoom2": "Freedoom Phase 2"}
	if n, ok := known[strings.ToLower(it.GameName)]; ok {
		it.GameName = n
	}
	opts := []string{"prboom", "gzdoom"}
	if a.Info != nil {
		var have []string
		for _, s := range opts {
			if _, ok := a.Info.Systems[s]; ok {
				have = append(have, s)
			}
		}
		if len(have) > 0 {
			opts = have
		}
	}
	it.SystemOptions = opts
	it.System = opts[0]
	switch {
	case iwad:
		it.Confidence = "green"
		it.Notes = append(it.Notes, "IWAD (gioco completo)")
	case pwad:
		it.Confidence = "yellow"
		it.Notes = append(it.Notes, "PWAD (mod/livelli): serve anche l'IWAD del gioco base nella stessa cartella; con GZDoom funziona meglio")
		if contains(opts, "gzdoom") {
			it.System = "gzdoom"
		}
	default:
		it.Confidence = "yellow"
		it.Notes = append(it.Notes, "file WAD senza intestazione riconoscibile")
	}
}

// ---------- esistenza sul cabinato ----------

func (a *Analyzer) checkExists(items []*Item) {
	if a.SSH == nil {
		return
	}
	var cmds []string
	idx := map[string]*Item{}
	for _, it := range items {
		if it.System == "" || it.DestName == "" {
			continue
		}
		p := "/userdata/roms/" + it.System + "/" + strings.TrimSuffix(it.DestName, "/")
		idx[p] = it
		cmds = append(cmds, "[ -e "+cab.ShellQuote(p)+" ] && echo "+cab.ShellQuote(p))
	}
	if len(cmds) == 0 {
		return
	}
	res, err := a.SSH.RunTimeout(strings.Join(cmds, "\n")+"\ntrue", 2*time.Minute)
	if err != nil {
		return
	}
	for _, l := range cab.Lines(res.Stdout) {
		if it := idx[strings.TrimSpace(l)]; it != nil {
			it.Exists = true
		}
	}
}

func (it *Item) finalize() {
	if it.Confidence == "" {
		if it.System == "" {
			it.Confidence = "red"
		} else {
			it.Confidence = "yellow"
		}
	}
	if it.System == "" && it.Action != "skip" {
		it.Confidence = "red"
	}
	if it.Action == "" {
		it.Action = "copy"
	}
	if it.Confidence == "red" && it.Action != "skip" && it.System == "" {
		// resta modificabile scegliendo il sistema
	}
	if it.Notes == nil {
		it.Notes = []string{}
	}
	if it.Sources == nil {
		it.Sources = []string{}
	}
}

// ---------- sessione ----------

func (s *Session) Get(id string) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

// Set applica una modifica dall'interfaccia (sistema, core, skip).
func (s *Session) Set(a *Analyzer, id string, system, core *string, skip *bool) (*Item, error) {
	it := s.Get(id)
	if it == nil {
		return nil, fmt.Errorf("elemento non trovato")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if skip != nil {
		it.Skip = *skip
	}
	if core != nil && it.Kind == "arcade" {
		if ck := it.ByCore[*core]; ck != nil {
			it.applyCore(*core)
		}
	}
	if system != nil && *system != "" && *system != it.System {
		it.System = *system
		if it.Kind == "arcade" {
			// cartella cambiata: core per gioco se diverso dal default della cartella
		} else {
			if it.Action == "skip" {
				it.Action = "copy"
				if len(it.Files) > 0 {
					it.Action = "extract"
				}
			}
			if it.Confidence == "red" {
				it.Confidence = "yellow"
			}
			if it.DestName == "" {
				it.DestName = it.GameName + it.ext
			}
		}
		if a != nil {
			a.checkExists([]*Item{it})
		}
	}
	it.PerGameCore = false
	if it.Kind == "arcade" && a != nil && a.Info != nil {
		if sys, ok := a.Info.Systems[it.System]; ok && it.Core != "" {
			def := sys.Default
			if i := strings.Index(def, "/"); i >= 0 {
				def = def[i+1:]
			}
			it.PerGameCore = def != "" && def != it.Core
		}
	}
	return it, nil
}

func (s *Session) Summary() map[string]int {
	m := map[string]int{"total": len(s.Items)}
	for _, it := range s.Items {
		m[it.Confidence]++
	}
	return m
}

// RefreshPerGameCore calcola per tutti gli elementi se serve il core per gioco.
func (s *Session) RefreshPerGameCore(info *cab.Info) {
	if info == nil {
		return
	}
	for _, it := range s.Items {
		if it.Kind != "arcade" || it.Core == "" {
			continue
		}
		if sys, ok := info.Systems[it.System]; ok {
			def := sys.Default
			if i := strings.Index(def, "/"); i >= 0 {
				def = def[i+1:]
			}
			it.PerGameCore = def != "" && def != it.Core
		}
	}
}
