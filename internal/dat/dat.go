// Package dat: database dei set (DAT) in formato ClrMamePro o XML (MAME listxml, datafile).
package dat

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Rom struct {
	Name  string
	Size  int64
	CRC   uint32
	Merge string // nome nel parent (set merged)
	Opt   bool   // optional / nodump
}

type Game struct {
	Name     string
	Desc     string
	CloneOf  string
	RomOf    string
	IsBios   bool
	IsDevice bool
	Roms     []Rom
	Devices  []string // device_ref
	Disks    []string // CHD richiesti (nome senza estensione; nodump esclusi)
	Year     string
	Manuf    string
}

type DB struct {
	ID       string
	Version  string
	Games    map[string]*Game
	byCRC    map[uint32][]crcRef
	byName   map[string]*Game // nome rom (senza estensione, minuscolo) → gioco (console)
	loadedAt time.Time
}

type crcRef struct {
	Game *Game
	Rom  int
}

// Parse legge un DAT (ClrMamePro o XML, anche .gz/.zip).
func Parse(id, path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	switch {
	case strings.HasSuffix(path, ".gz"):
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	case strings.HasSuffix(path, ".zip"):
		st, _ := f.Stat()
		zr, err := zip.NewReader(f, st.Size())
		if err != nil {
			return nil, err
		}
		var zf *zip.File
		for _, e := range zr.File {
			n := strings.ToLower(e.Name)
			if strings.HasSuffix(n, ".dat") || strings.HasSuffix(n, ".xml") {
				zf = e
				break
			}
		}
		if zf == nil {
			return nil, fmt.Errorf("nessun .dat/.xml nello zip")
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		r = rc
	}
	br := bufio.NewReaderSize(r, 1<<20)
	head, _ := br.Peek(512)
	db := &DB{ID: id, Games: map[string]*Game{}}
	if bytes.Contains(head, []byte("<")) && (bytes.Contains(head, []byte("<?xml")) || bytes.Contains(head, []byte("<mame")) || bytes.Contains(head, []byte("<datafile"))) {
		err = db.parseXML(br)
	} else {
		err = db.parseCMP(br)
	}
	if err != nil {
		return nil, err
	}
	db.index()
	return db, nil
}

// add inserisce un gioco; se il nome esiste già (es. dump con e senza header) unisce le ROM.
func (db *DB) add(g *Game) {
	if old := db.Games[g.Name]; old != nil {
		old.Roms = append(old.Roms, g.Roms...)
		if old.Desc == "" {
			old.Desc = g.Desc
		}
		return
	}
	db.Games[g.Name] = g
}

func (db *DB) index() {
	db.byCRC = map[uint32][]crcRef{}
	db.byName = map[string]*Game{}
	for _, g := range db.Games {
		for i, r := range g.Roms {
			if r.CRC != 0 {
				db.byCRC[r.CRC] = append(db.byCRC[r.CRC], crcRef{g, i})
			}
			db.byName[strings.ToLower(stripExt(r.Name))] = g
		}
		db.byName[strings.ToLower(g.Name)] = g
	}
	db.loadedAt = time.Now()
}

func stripExt(n string) string {
	if i := strings.LastIndex(n, "."); i > 0 {
		return n[:i]
	}
	return n
}

// ---- XML ----

type xmlRom struct {
	Name   string `xml:"name,attr"`
	Size   string `xml:"size,attr"`
	CRC    string `xml:"crc,attr"`
	Merge  string `xml:"merge,attr"`
	Status string `xml:"status,attr"`
	Opt    string `xml:"optional,attr"`
}
type xmlDev struct {
	Name string `xml:"name,attr"`
}
type xmlGame struct {
	Name     string   `xml:"name,attr"`
	CloneOf  string   `xml:"cloneof,attr"`
	RomOf    string   `xml:"romof,attr"`
	IsBios   string   `xml:"isbios,attr"`
	IsDevice string   `xml:"isdevice,attr"`
	Runnable string   `xml:"runnable,attr"`
	Desc     string   `xml:"description"`
	Year     string   `xml:"year"`
	Manuf    string   `xml:"manufacturer"`
	Roms     []xmlRom `xml:"rom"`
	Disks    []xmlRom `xml:"disk"`
	Devs     []xmlDev `xml:"device_ref"`
}

func (db *DB) parseXML(r io.Reader) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "mame", "datafile":
			for _, a := range se.Attr {
				if a.Name.Local == "build" || a.Name.Local == "version" {
					db.Version = a.Value
				}
			}
		case "header":
			var h struct {
				Version string `xml:"version"`
				Name    string `xml:"name"`
			}
			_ = dec.DecodeElement(&h, &se)
			if h.Version != "" {
				db.Version = h.Version
			}
		case "game", "machine":
			var xg xmlGame
			if err := dec.DecodeElement(&xg, &se); err != nil {
				continue
			}
			g := &Game{Name: xg.Name, Desc: xg.Desc, CloneOf: xg.CloneOf, RomOf: xg.RomOf, IsBios: xg.IsBios == "yes", IsDevice: xg.IsDevice == "yes", Year: xg.Year, Manuf: xg.Manuf}
			for _, xr := range xg.Roms {
				if xr.Status == "nodump" {
					continue
				}
				rom := Rom{Name: xr.Name, Merge: xr.Merge, Opt: xr.Opt == "yes"}
				rom.Size, _ = strconv.ParseInt(xr.Size, 10, 64)
				c, _ := strconv.ParseUint(strings.TrimSpace(xr.CRC), 16, 32)
				rom.CRC = uint32(c)
				g.Roms = append(g.Roms, rom)
			}
			for _, d := range xg.Devs {
				g.Devices = append(g.Devices, d.Name)
			}
			for _, xd := range xg.Disks {
				if xd.Status == "nodump" || xd.Opt == "yes" {
					continue
				}
				n := xd.Name
				if xd.Merge != "" {
					n = xd.Merge // il CHD è quello del parent
				}
				g.Disks = append(g.Disks, n)
			}
			if g.Name != "" {
				db.add(g)
			}
		}
	}
	return nil
}

// ---- ClrMamePro ----

var reTok = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|\(|\)|[^\s()"]+`)

func (db *DB) parseCMP(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var cur *Game
	depth := 0
	inHeader := false
	for sc.Scan() {
		line := sc.Text()
		toks := reTok.FindAllStringSubmatch(line, -1)
		var words []string
		for _, t := range toks {
			if t[1] != "" || strings.HasPrefix(t[0], `"`) {
				words = append(words, strings.ReplaceAll(t[1], `\"`, `"`))
			} else {
				words = append(words, t[0])
			}
		}
		for i := 0; i < len(words); i++ {
			w := words[i]
			switch w {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 {
					if cur != nil && cur.Name != "" {
						db.add(cur)
					}
					cur = nil
					inHeader = false
				}
			case "clrmamepro":
				inHeader = true
			case "game", "machine", "resource", "set":
				if depth == 0 {
					cur = &Game{IsBios: w == "resource"}
				}
			case "name":
				if i+1 < len(words) {
					if inHeader && depth <= 1 {
						// nome del dat
					} else if cur != nil && depth == 1 {
						cur.Name = words[i+1]
					}
					i++
				}
			case "version":
				if i+1 < len(words) && inHeader {
					db.Version = words[i+1]
					i++
				}
			case "description":
				if i+1 < len(words) && cur != nil && depth == 1 {
					cur.Desc = words[i+1]
					i++
				}
			case "cloneof":
				if i+1 < len(words) && cur != nil {
					cur.CloneOf = words[i+1]
					i++
				}
			case "romof":
				if i+1 < len(words) && cur != nil {
					cur.RomOf = words[i+1]
					i++
				}
			case "year":
				if i+1 < len(words) && cur != nil {
					cur.Year = words[i+1]
					i++
				}
			case "manufacturer":
				if i+1 < len(words) && cur != nil {
					cur.Manuf = words[i+1]
					i++
				}
			case "rom", "disk":
				if cur == nil {
					continue
				}
				// rom ( name x size n crc c ... )
				j := i + 1
				if j < len(words) && words[j] == "(" {
					rom := Rom{}
					skip := false
					j++
					for j < len(words) && words[j] != ")" {
						k := words[j]
						if j+1 < len(words) {
							v := words[j+1]
							switch k {
							case "name":
								rom.Name = v
							case "size":
								rom.Size, _ = strconv.ParseInt(v, 10, 64)
							case "crc":
								c, _ := strconv.ParseUint(v, 16, 32)
								rom.CRC = uint32(c)
							case "merge":
								rom.Merge = v
							case "flags":
								if v == "nodump" {
									skip = true
								}
							}
							j += 2
						} else {
							j++
						}
					}
					if !skip && rom.Name != "" && w == "rom" {
						cur.Roms = append(cur.Roms, rom)
					}
					i = j
				}
			}
		}
	}
	return sc.Err()
}

// ---- ricerche ----

type Match struct {
	Game *Game
	Rom  *Rom
}

func (db *DB) ByCRC(crc uint32) []Match {
	var out []Match
	for _, ref := range db.byCRC[crc] {
		out = append(out, Match{ref.Game, &ref.Game.Roms[ref.Rom]})
	}
	return out
}

func (db *DB) ByName(name string) *Game {
	if g := db.Games[name]; g != nil {
		return g
	}
	return db.byName[strings.ToLower(stripExt(name))]
}

func (db *DB) Count() int { return len(db.Games) }

// NeededRoms: i file che devono stare nello zip di `name` per un set "split" (senza parent),
// con fallback al parent per il set "merged". Restituisce anche i file dei device/bios richiesti
// che normalmente stanno in zip separati.
type Need struct {
	Rom     Rom
	FromSet string // set in cui normalmente si trova (parent/bios) se diverso
}

func (db *DB) SplitSet(name string) ([]Need, bool) {
	g := db.Games[name]
	if g == nil {
		return nil, false
	}
	var out []Need
	for _, r := range g.Roms {
		if r.Merge != "" && g.CloneOf != "" {
			continue // nel parent (split set)
		}
		if g.CloneOf == "" && g.RomOf != "" && r.Merge != "" {
			continue // BIOS separato
		}
		out = append(out, Need{Rom: r})
	}
	return out, true
}

// FullSet: tutti i file del set + parent + bios (per un set "non-merged").
func (db *DB) FullSet(name string) []Need {
	seen := map[string]bool{}
	var out []Need
	cur := name
	for cur != "" && !seen[cur] {
		seen[cur] = true
		g := db.Games[cur]
		if g == nil {
			break
		}
		for _, r := range g.Roms {
			from := ""
			if cur != name {
				from = cur
			}
			out = append(out, Need{Rom: r, FromSet: from})
		}
		if g.CloneOf != "" {
			cur = g.CloneOf
		} else {
			cur = g.RomOf
		}
	}
	return out
}

// ---- cache binaria ----

func (db *DB) SaveCache(path string) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	enc := gob.NewEncoder(gz)
	if err := enc.Encode(struct {
		Version string
		Games   map[string]*Game
	}{db.Version, db.Games}); err != nil {
		f.Close()
		return err
	}
	gz.Close()
	f.Close()
	return os.Rename(path+".tmp", path)
}

func LoadCache(id, path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var c struct {
		Version string
		Games   map[string]*Game
	}
	if err := gob.NewDecoder(gz).Decode(&c); err != nil {
		return nil, err
	}
	db := &DB{ID: id, Version: c.Version, Games: c.Games}
	db.index()
	return db, nil
}

var _ = sync.Mutex{}
