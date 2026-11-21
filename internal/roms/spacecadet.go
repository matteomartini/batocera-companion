package roms

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 3D Pinball - Space Cadet (Windows XP) e Full Tilt! Pinball: i file dati originali
// (PINBALL.DAT / CADET.DAT con .MID/.WAV/.BMP) vengono messi in
// ports/SpaceCadetPinball/ e letti dal port SDL2 di k4zmu2a, che il Companion installa
// sul cabinato (binario Linux + Space Cadet Pinball.sh in Ports) dopo la copia.

const SpaceCadetDir = "SpaceCadetPinball"

var spaceCadetExt = map[string]bool{".dat": true, ".mid": true, ".wav": true, ".bmp": true, ".mds": true}

func isSpaceCadetDat(name string) bool {
	switch strings.ToLower(name) {
	case "pinball.dat", "cadet.dat", "demo.dat":
		return true
	}
	return false
}

func spaceCadetName(dat string) string {
	if strings.EqualFold(dat, "cadet.dat") {
		return "Full Tilt! Pinball - Space Cadet"
	}
	return "3D Pinball - Space Cadet"
}

// trySpaceCadet: archivio che contiene PINBALL.DAT/CADET.DAT → estrazione in ports/SpaceCadetPinball/.
func (a *Analyzer) trySpaceCadet(it *Item, entries []Entry) bool {
	dat := ""
	for _, e := range entries {
		if isSpaceCadetDat(e.Name) {
			dat = e.Name
			break
		}
	}
	if dat == "" {
		return false
	}
	it.Kind = "spacecadet"
	it.System = "ports"
	it.SystemOptions = []string{"ports"}
	it.GameName = spaceCadetName(dat)
	it.DisplayName = it.GameName
	it.Action = "extract"
	it.DestName = SpaceCadetDir + "/" + dat
	it.Files = nil
	n := 0
	for _, e := range entries {
		if spaceCadetExt[strings.ToLower(filepath.Ext(e.Name))] && !strings.HasPrefix(e.Inner, "__MACOSX") {
			it.Files = append(it.Files, e)
			n++
		}
	}
	it.Confidence = "green"
	it.Notes = appendOnce(it.Notes, fmt.Sprintf("file dati di %s (%d file): vanno in ports/%s/; il Companion installa il gioco (port SDL2) in Ports", it.GameName, n, SpaceCadetDir))
	if n < 3 {
		it.Confidence = "yellow"
		it.Notes = appendOnce(it.Notes, "pochi file: servono anche i .MID e i SOUND*.WAV della cartella Pinball di Windows XP")
	}
	return true
}

// spaceCadetLayout: PINBALL.DAT sciolto → un solo elemento con tutti i file dati della
// stessa cartella come Extra; gli altri file della cartella non viaggiano da soli.
func (a *Analyzer) spaceCadetLayout(items []*Item) {
	for _, it := range items {
		if it.Skip || len(it.Sources) == 0 || IsArchive(it.Sources[0]) || !isSpaceCadetDat(filepath.Base(it.Sources[0])) {
			continue
		}
		dat := filepath.Base(it.Sources[0])
		dir := filepath.Dir(it.Sources[0])
		it.Kind = "spacecadet"
		it.System = "ports"
		it.SystemOptions = []string{"ports"}
		it.Core = ""
		it.CoreMatches = nil
		it.GameName = spaceCadetName(dat)
		it.DisplayName = it.GameName
		it.Action = "copy"
		it.DestName = SpaceCadetDir + "/" + dat
		it.Extra = nil
		it.Notes = nil
		it.Missing = nil
		it.Confidence = "green"
		total := it.Size
		n := 1
		ents, _ := os.ReadDir(dir)
		for _, d := range ents {
			if d.IsDir() || d.Name() == dat || !spaceCadetExt[strings.ToLower(filepath.Ext(d.Name()))] {
				continue
			}
			p := filepath.Join(dir, d.Name())
			it.Extra = append(it.Extra, p)
			n++
			if st, err := d.Info(); err == nil {
				total += st.Size()
			}
			for _, o := range items {
				if o != it && len(o.Sources) > 0 && o.Sources[0] == p {
					o.Skip = true
					o.Notes = []string{"copiato insieme a " + dat + " (" + it.GameName + ")"}
					o.Confidence = "green"
					o.System = "ports"
				}
			}
		}
		it.Size = total
		it.Notes = appendOnce(it.Notes, fmt.Sprintf("file dati di %s (%d file): vanno in ports/%s/; il Companion installa il gioco (port SDL2) in Ports", it.GameName, n, SpaceCadetDir))
		if n < 3 {
			it.Confidence = "yellow"
			it.Notes = appendOnce(it.Notes, "pochi file: servono anche i .MID e i SOUND*.WAV della cartella Pinball di Windows XP")
		}
	}
}
