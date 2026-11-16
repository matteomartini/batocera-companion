package dat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // arcade | console
	Note     string   `json:"note"`
	URL      string   `json:"-"`
	Generate bool     `json:"generate"`
	Systems  []string `json:"systems"` // cartelle Batocera coperte
	Core     string   `json:"core"`    // core libretro (arcade)
}

const lrdb = "https://raw.githubusercontent.com/libretro/libretro-database/master/metadat/no-intro/"
const lrred = "https://raw.githubusercontent.com/libretro/libretro-database/master/metadat/redump/"

var Catalog = []Entry{
	{ID: "fbneo", Name: "FinalBurn Neo (arcade)", Kind: "arcade", Note: "Core consigliato per CPS1/2/3, Neo-Geo e la maggior parte degli arcade fino al 2000. Romset FBNeo attuale.", URL: "https://raw.githubusercontent.com/libretro/FBNeo/master/dats/FinalBurn%20Neo%20(ClrMame%20Pro%20XML%2C%20Arcade%20only).dat", Systems: []string{"fbneo", "neogeo"}, Core: "fbneo"},
	{ID: "mame", Name: "MAME (versione del cabinato)", Kind: "arcade", Note: "Generato dal MAME installato su Batocera: è sempre la versione giusta per la cartella mame.", Generate: true, Systems: []string{"mame"}, Core: "mame"},
	{ID: "mame2015", Name: "MAME 2015 (0.160)", Kind: "arcade", Note: "Core leggero per giochi 3D anni '90 (Model 1, Namco System 22…).", URL: "https://github.com/libretro/mame2015-libretro/raw/master/metadata/mame2015-xml.zip", Systems: []string{"mame"}, Core: "mame2015"},
	{ID: "mame2010", Name: "MAME 2010 (0.139)", Kind: "arcade", Note: "Compromesso tra compatibilità e leggerezza.", URL: "https://raw.githubusercontent.com/libretro/mame2010-libretro/master/metadata/mame2010.xml", Systems: []string{"mame"}, Core: "mame2010"},
	{ID: "mame2003_plus", Name: "MAME 2003-Plus (0.78+)", Kind: "arcade", Note: "Il più leggero: ideale per i classici anni '80.", URL: "https://raw.githubusercontent.com/libretro/mame2003-plus-libretro/master/metadata/mame2003-plus.xml", Systems: []string{"mame"}, Core: "mame2003_plus"},

	{ID: "nes", Name: "Nintendo NES", Kind: "console", URL: lrdb + "Nintendo%20-%20Nintendo%20Entertainment%20System.dat", Systems: []string{"nes"}},
	{ID: "fds", Name: "Famicom Disk System", Kind: "console", URL: lrdb + "Nintendo%20-%20Family%20Computer%20Disk%20System.dat", Systems: []string{"fds"}},
	{ID: "snes", Name: "Super Nintendo", Kind: "console", URL: lrdb + "Nintendo%20-%20Super%20Nintendo%20Entertainment%20System.dat", Systems: []string{"snes"}},
	{ID: "n64", Name: "Nintendo 64", Kind: "console", URL: lrdb + "Nintendo%20-%20Nintendo%2064.dat", Systems: []string{"n64"}},
	{ID: "gb", Name: "Game Boy", Kind: "console", URL: lrdb + "Nintendo%20-%20Game%20Boy.dat", Systems: []string{"gb"}},
	{ID: "gbc", Name: "Game Boy Color", Kind: "console", URL: lrdb + "Nintendo%20-%20Game%20Boy%20Color.dat", Systems: []string{"gbc"}},
	{ID: "gba", Name: "Game Boy Advance", Kind: "console", URL: lrdb + "Nintendo%20-%20Game%20Boy%20Advance.dat", Systems: []string{"gba"}},
	{ID: "virtualboy", Name: "Virtual Boy", Kind: "console", URL: lrdb + "Nintendo%20-%20Virtual%20Boy.dat", Systems: []string{"virtualboy"}},
	{ID: "nds", Name: "Nintendo DS", Kind: "console", URL: lrdb + "Nintendo%20-%20Nintendo%20DS.dat", Systems: []string{"nds"}},
	{ID: "megadrive", Name: "Sega Mega Drive", Kind: "console", URL: lrdb + "Sega%20-%20Mega%20Drive%20-%20Genesis.dat", Systems: []string{"megadrive"}},
	{ID: "mastersystem", Name: "Sega Master System", Kind: "console", URL: lrdb + "Sega%20-%20Master%20System%20-%20Mark%20III.dat", Systems: []string{"mastersystem"}},
	{ID: "gamegear", Name: "Sega Game Gear", Kind: "console", URL: lrdb + "Sega%20-%20Game%20Gear.dat", Systems: []string{"gamegear"}},
	{ID: "sega32x", Name: "Sega 32X", Kind: "console", URL: lrdb + "Sega%20-%2032X.dat", Systems: []string{"sega32x"}},
	{ID: "sg1000", Name: "Sega SG-1000", Kind: "console", URL: lrdb + "Sega%20-%20SG-1000.dat", Systems: []string{"sg1000"}},
	{ID: "pcengine", Name: "PC Engine / TurboGrafx-16", Kind: "console", URL: lrdb + "NEC%20-%20PC%20Engine%20-%20TurboGrafx%2016.dat", Systems: []string{"pcengine"}},
	{ID: "supergrafx", Name: "SuperGrafx", Kind: "console", URL: lrdb + "NEC%20-%20PC%20Engine%20SuperGrafx.dat", Systems: []string{"supergrafx"}},
	{ID: "psx", Name: "PlayStation", Kind: "console", Note: "Redump: riconosce bin/cue per nome; le immagini grandi vanno per nome file.", URL: lrred + "Sony%20-%20PlayStation.dat", Systems: []string{"psx"}},
	{ID: "psp", Name: "PlayStation Portable", Kind: "console", URL: lrred + "Sony%20-%20PlayStation%20Portable.dat", Systems: []string{"psp"}},
	{ID: "atari2600", Name: "Atari 2600", Kind: "console", URL: lrdb + "Atari%20-%202600.dat", Systems: []string{"atari2600"}},
	{ID: "atari5200", Name: "Atari 5200", Kind: "console", URL: lrdb + "Atari%20-%205200.dat", Systems: []string{"atari5200"}},
	{ID: "atari7800", Name: "Atari 7800", Kind: "console", URL: lrdb + "Atari%20-%207800.dat", Systems: []string{"atari7800"}},
	{ID: "lynx", Name: "Atari Lynx", Kind: "console", URL: lrdb + "Atari%20-%20Lynx.dat", Systems: []string{"lynx"}},
	{ID: "ngpc", Name: "Neo Geo Pocket / Color", Kind: "console", URL: lrdb + "SNK%20-%20Neo%20Geo%20Pocket%20Color.dat", Systems: []string{"ngpc", "ngp"}},
	{ID: "wswan", Name: "WonderSwan / Color", Kind: "console", URL: lrdb + "Bandai%20-%20WonderSwan%20Color.dat", Systems: []string{"wswanc", "wswan"}},
	{ID: "colecovision", Name: "ColecoVision", Kind: "console", URL: lrdb + "Coleco%20-%20ColecoVision.dat", Systems: []string{"colecovision"}},
	{ID: "pokemini", Name: "Pokémon Mini", Kind: "console", URL: lrdb + "Nintendo%20-%20Pokemon%20Mini.dat", Systems: []string{"pokemini"}},
	{ID: "ps2", Name: "PlayStation 2", Kind: "console", Note: "Redump: riconoscimento per nome file (immagini grandi).", URL: lrred + "Sony%20-%20PlayStation%202.dat", Systems: []string{"ps2"}},
	{ID: "dreamcast", Name: "Dreamcast", Kind: "console", URL: lrred + "Sega%20-%20Dreamcast.dat", Systems: []string{"dreamcast"}},
	{ID: "saturn", Name: "Sega Saturn", Kind: "console", URL: lrred + "Sega%20-%20Saturn.dat", Systems: []string{"saturn"}},
	{ID: "segacd", Name: "Mega-CD", Kind: "console", URL: lrred + "Sega%20-%20Mega-CD%20-%20Sega%20CD.dat", Systems: []string{"segacd"}},
	{ID: "pcenginecd", Name: "PC Engine CD", Kind: "console", URL: lrred + "NEC%20-%20PC%20Engine%20CD%20-%20TurboGrafx-CD.dat", Systems: []string{"pcenginecd"}},
	{ID: "msx", Name: "MSX / MSX2", Kind: "console", URL: lrdb + "Microsoft%20-%20MSX.dat", Systems: []string{"msx1", "msx2"}},
}

func Lookup(id string) *Entry {
	for i := range Catalog {
		if Catalog[i].ID == id {
			return &Catalog[i]
		}
	}
	return nil
}

// Manager: cartella dei DAT + cache in memoria.
type Manager struct {
	Dir string
	mu  sync.Mutex
	dbs map[string]*DB
}

func NewManager(dir string) *Manager {
	_ = os.MkdirAll(dir, 0o755)
	return &Manager{Dir: dir, dbs: map[string]*DB{}}
}

func (m *Manager) Path(id string) string {
	for _, ext := range []string{".dat", ".xml", ".xml.gz", ".zip"} {
		p := filepath.Join(m.Dir, id+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// v2: aggiunti Disks/Devices → cache rigenerata
func (m *Manager) cachePath(id string) string { return filepath.Join(m.Dir, "cache", id+".v2.gob.gz") }

// Load carica (e mette in cache) un DAT; nil se assente.
func (m *Manager) Load(id string) (*DB, error) {
	m.mu.Lock()
	if db := m.dbs[id]; db != nil {
		m.mu.Unlock()
		return db, nil
	}
	m.mu.Unlock()
	p := m.Path(id)
	if p == "" {
		return nil, nil
	}
	st, _ := os.Stat(p)
	cp := m.cachePath(id)
	if cst, err := os.Stat(cp); err == nil && st != nil && cst.ModTime().After(st.ModTime()) {
		if db, err := LoadCache(id, cp); err == nil {
			m.mu.Lock()
			m.dbs[id] = db
			m.mu.Unlock()
			return db, nil
		}
	}
	db, err := Parse(id, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	go func() { _ = db.SaveCache(cp) }()
	m.mu.Lock()
	m.dbs[id] = db
	m.mu.Unlock()
	return db, nil
}

func (m *Manager) Invalidate(id string) {
	m.mu.Lock()
	delete(m.dbs, id)
	m.mu.Unlock()
	os.Remove(m.cachePath(id))
}

// Present: id dei DAT disponibili su disco.
func (m *Manager) Present() []string {
	var out []string
	for _, e := range Catalog {
		if m.Path(e.ID) != "" {
			out = append(out, e.ID)
		}
	}
	return out
}

// ForSystem: DAT (id) che coprono una cartella Batocera.
func ForSystem(system string) []Entry {
	var out []Entry
	for _, e := range Catalog {
		for _, s := range e.Systems {
			if s == system {
				out = append(out, e)
			}
		}
	}
	return out
}

type Status struct {
	Entry
	Present  bool      `json:"present"`
	Size     int64     `json:"size"`
	Updated  time.Time `json:"updated"`
	Version  string    `json:"version"`
	Machines int       `json:"machines"`
	Error    string    `json:"error,omitempty"`
}

// List: stato di tutti i DAT (caricando la cache per versione/conteggio se già in memoria).
func (m *Manager) List(loadCounts bool) []Status {
	var out []Status
	for _, e := range Catalog {
		s := Status{Entry: e}
		if p := m.Path(e.ID); p != "" {
			st, _ := os.Stat(p)
			s.Present = true
			s.Size = st.Size()
			s.Updated = st.ModTime()
			m.mu.Lock()
			db := m.dbs[e.ID]
			m.mu.Unlock()
			if db == nil && loadCounts {
				db, _ = m.Load(e.ID)
			}
			if db != nil {
				s.Version = db.Version
				s.Machines = db.Count()
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// Download scarica un DAT dal catalogo.
func (m *Manager) Download(ctx context.Context, id string, progress func(string)) error {
	e := Lookup(id)
	if e == nil || e.URL == "" {
		return fmt.Errorf("DAT %q non scaricabile", id)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", e.URL, nil)
	req.Header.Set("User-Agent", "BatoceraCompanion")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d da %s", resp.StatusCode, e.URL)
	}
	ext := ".dat"
	if strings.HasSuffix(strings.ToLower(e.URL), ".zip") {
		ext = ".zip"
	} else if strings.HasSuffix(strings.ToLower(e.URL), ".xml") {
		ext = ".xml"
	}
	m.removeAll(id)
	tmp := filepath.Join(m.Dir, id+ext+".part")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(m.Dir, id+ext)); err != nil {
		return err
	}
	m.Invalidate(id)
	if progress != nil {
		progress(fmt.Sprintf("%s: %.1f MB", e.Name, float64(n)/1e6))
	}
	// verifica che si legga
	if _, err := m.Load(id); err != nil {
		return err
	}
	return nil
}

func (m *Manager) removeAll(id string) {
	for _, ext := range []string{".dat", ".xml", ".xml.gz", ".zip"} {
		os.Remove(filepath.Join(m.Dir, id+ext))
	}
}

// Import copia un file DAT fornito dall'utente.
func (m *Manager) Import(id, src string) (*DB, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if ext != ".zip" && ext != ".xml" {
		ext = ".dat"
	}
	if strings.HasSuffix(strings.ToLower(src), ".xml.gz") {
		ext = ".xml.gz"
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	m.removeAll(id)
	if err := os.WriteFile(filepath.Join(m.Dir, id+ext), b, 0o644); err != nil {
		return nil, err
	}
	m.Invalidate(id)
	return m.Load(id)
}

// SaveGenerated salva il listxml (gz) generato dal cabinato come DAT "mame".
func (m *Manager) SaveGenerated(id string, data []byte) (*DB, error) {
	m.removeAll(id)
	if err := os.WriteFile(filepath.Join(m.Dir, id+".xml.gz"), data, 0o644); err != nil {
		return nil, err
	}
	m.Invalidate(id)
	return m.Load(id)
}
