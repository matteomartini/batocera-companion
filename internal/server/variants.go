package server

import (
	"regexp"
	"sort"
	"strings"

	"batocompanion/internal/dat"
)

// Varianti dello stesso gioco (revisioni, regioni, beta, bootleg…): il Companion
// propone quale copia tenere e quali togliere.

type RLVariantFile struct {
	Path  string `json:"path"`
	Name  string `json:"name,omitempty"`
	Score int    `json:"score"`
	Keep  bool   `json:"keep"`
	Why   string `json:"why"`
}

type RLVariant struct {
	System string          `json:"system"`
	Title  string          `json:"title"`
	Files  []RLVariantFile `json:"files"`
}

var (
	reVTags     = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)
	reRev       = regexp.MustCompile(`(?i)^(?:rev|revision|r)\s*([0-9]+|[a-z])$`)
	reVer       = regexp.MustCompile(`(?i)^v\s*([0-9]+)(?:\.([0-9]+))?`)
	reSpaces    = regexp.MustCompile(`\s+`)
	reArcadeSet = regexp.MustCompile(`(?i)\(set (\d+)\)`)
)

func variantTitle(stem string) string {
	t := reVTags.ReplaceAllString(stem, " ")
	t = strings.ToLower(t)
	t = strings.NewReplacer("_", " ", "-", " ", ":", " ", "'", "", ".", " ", ",", " ", "!", "", "&", " and ").Replace(t)
	t = strings.TrimSpace(reSpaces.ReplaceAllString(t, " "))
	// "the" finale (No-Intro: "Legend of Zelda, The")
	t = strings.TrimSuffix(t, " the")
	return t
}

// scoreConsole: punteggio di una variante console dal nome del file.
// Sistemi in cui la versione europea gira a 50 Hz (più lenta): meglio USA/World.
var palSlow = map[string]bool{"nes": true, "snes": true, "megadrive": true, "mastersystem": true, "sg1000": true, "n64": true, "atari2600": true, "atari7800": true, "segacd": true, "sega32x": true, "pcengine": true, "colecovision": true, "msx": true, "msx2": true}

func scoreConsole(sys, stem, status string) (int, string) {
	score := 0
	var why []string
	if status == "ok" {
		score += 50
		why = append(why, "dump riconosciuto")
	}
	tags := reVTags.FindAllString(stem, -1)
	regionBest := 0
	for _, raw := range tags {
		t := strings.ToLower(strings.Trim(raw, "()[]"))
		for _, part := range strings.Split(t, ",") {
			p := strings.TrimSpace(part)
			switch p {
			case "italy":
				if regionBest < 40 {
					regionBest = 40
				}
			case "it":
				score += 15 // testi in italiano
				why = appendOnce(why, "italiano")
			case "europe", "eu":
				eu := 30
				if palSlow[sys] {
					eu = 20 // PAL a 50 Hz: più lento, meglio USA/World su uno schermo a 60 Hz
				}
				if regionBest < eu {
					regionBest = eu
				}
			case "world":
				if regionBest < 28 {
					regionBest = 28
				}
			case "usa", "us":
				if regionBest < 25 {
					regionBest = 25
				}
			case "japan", "ja", "jp":
				if regionBest < 10 {
					regionBest = 10
				}
			case "!":
				score += 5
				why = append(why, "[!]")
			case "b", "b1", "b2", "bad":
				score -= 100
				why = append(why, "dump danneggiato [b]")
			case "h", "h1", "hack", "hacked", "t", "t1", "a", "o", "f", "p":
				score -= 60
				why = append(why, "hack/alt/overdump")
			case "beta", "proto", "prototype", "demo", "sample", "kiosk", "program", "aftermarket", "unl", "unlicensed", "pirate", "homebrew", "test program":
				score -= 100
				why = append(why, p)
			case "virtual console", "gamecube", "switch online", "nes classic", "collection":
				score -= 20
				why = append(why, "re-release")
			}
			if strings.HasPrefix(p, "beta") || strings.HasPrefix(p, "proto") || strings.HasPrefix(p, "demo") || strings.HasPrefix(p, "sample") {
				score -= 100
				why = appendOnce(why, p)
			}
			if m := reRev.FindStringSubmatch(p); m != nil {
				n := 0
				if m[1][0] >= 'a' && m[1][0] <= 'z' {
					n = int(m[1][0]-'a') + 1
				} else {
					for _, c := range m[1] {
						n = n*10 + int(c-'0')
					}
				}
				score += 3 * n
				why = append(why, "rev "+m[1])
			}
			if m := reVer.FindStringSubmatch(p); m != nil {
				n := 0
				for _, c := range m[1] {
					n = n*10 + int(c-'0')
				}
				d := 0
				for _, c := range m[2] {
					d = d*10 + int(c-'0')
				}
				score += 3*n + d
				why = append(why, p)
			}
		}
	}
	score += regionBest
	switch regionBest {
	case 40:
		why = append(why, "Italia")
	case 30:
		why = append(why, "Europa")
	case 20:
		why = append(why, "Europa (PAL 50 Hz)")
	case 28:
		why = append(why, "World")
	case 25:
		why = append(why, "USA")
	case 10:
		why = append(why, "Giappone")
	}
	return score, strings.Join(why, ", ")
}

// scoreArcade: punteggio di un set arcade dalla descrizione del DAT.
func scoreArcade(g *dat.Game, status string, isParent bool) (int, string) {
	score := 0
	var why []string
	if status == "ok" {
		score += 50
		why = append(why, "set completo")
	}
	if isParent {
		score += 10
		why = append(why, "set principale")
	}
	d := strings.ToLower(g.Desc)
	switch {
	case strings.Contains(d, "world"):
		score += 20
		why = append(why, "World")
	case strings.Contains(d, "euro"):
		score += 15
		why = append(why, "Europa")
	case strings.Contains(d, "usa") || strings.Contains(d, "us "):
		score += 12
		why = append(why, "USA")
	case strings.Contains(d, "japan"):
		score += 5
		why = append(why, "Giappone")
	}
	for _, bad := range []string{"bootleg", "hack", "prototype", "homebrew", "pirate"} {
		if strings.Contains(d, bad) {
			score -= 60
			why = append(why, bad)
		}
	}
	if m := reArcadeSet.FindStringSubmatch(g.Desc); m != nil {
		n := int(m[1][0] - '0')
		score -= n // set 1 preferito a set 2
	}
	return score, strings.Join(why, ", ")
}

func appendOnce(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// findVariants raggruppa i file dello stesso gioco per sistema.
func (s *Server) findVariants(rep *RomlibReport, dbs map[string]*dat.DB) []RLVariant {
	var out []RLVariant
	for _, sy := range rep.Systems {
		groups := map[string][]RLVariantFile{}
		titles := map[string]string{}
		order := []string{}
		for _, f := range sy.Files {
			if f.Status == "aux" || f.Status == "hidden_ext" || f.Status == "bad" {
				continue
			}
			stem := strings.TrimSuffix(f.Path, pathExt(f.Path))
			if strings.Contains(stem, "/") {
				continue // sottocartelle: lasciale stare
			}
			if reDiscTitle.MatchString(stem) {
				continue // multi-disco: gestiti a parte
			}
			var key, title, why string
			var score int
			if f.Set != "" && dbs[f.DB] != nil {
				db := dbs[f.DB]
				g := db.Games[f.Set]
				if g == nil || g.IsBios || g.IsDevice {
					continue
				}
				parent := g.CloneOf
				if parent == "" {
					parent = g.Name
				}
				key = "set:" + parent
				title = g.Desc
				if p := db.Games[parent]; p != nil {
					title = p.Desc
				}
				title = strings.TrimSpace(reVTags.ReplaceAllString(title, ""))
				score, why = scoreArcade(g, f.Status, g.CloneOf == "")
			} else {
				key = "t:" + variantTitle(stem)
				title = strings.TrimSpace(reVTags.ReplaceAllString(stem, ""))
				score, why = scoreConsole(sy.Name, stem, f.Status)
			}
			if strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(key, "t:"), "set:")) == "" {
				continue
			}
			if _, ok := groups[key]; !ok {
				order = append(order, key)
				titles[key] = title
			}
			groups[key] = append(groups[key], RLVariantFile{Path: f.Path, Name: f.Name, Score: score, Why: why})
		}
		for _, k := range order {
			g := groups[k]
			if len(g) < 2 {
				continue
			}
			sort.SliceStable(g, func(i, j int) bool { return g[i].Score > g[j].Score })
			g[0].Keep = true
			out = append(out, RLVariant{System: sy.Name, Title: titles[k], Files: g})
		}
	}
	return out
}

func pathExt(p string) string {
	if i := strings.LastIndex(p, "."); i > strings.LastIndex(p, "/") {
		return p[i:]
	}
	return ""
}
