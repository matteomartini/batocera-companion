package scrape

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SS: client ScreenScraper API v2. Richiede credenziali sviluppatore (devid/devpassword)
// oltre all'account utente.
type SS struct {
	DevID, DevPassword, Soft, User, Pass string
	Regions, Langs                       []string
	HTTP                                 *http.Client
}

// SSSystemID: cartella Batocera → id sistema ScreenScraper.
var SSSystemID = map[string]int{
	"nes": 3, "fds": 106, "snes": 4, "n64": 14, "gb": 9, "gbc": 10, "gba": 12, "nds": 15, "3ds": 17, "virtualboy": 11, "gamecube": 13, "wii": 16, "pokemini": 211,
	"megadrive": 1, "mastersystem": 2, "gamegear": 21, "sega32x": 19, "segacd": 20, "saturn": 22, "dreamcast": 23, "sg1000": 109, "naomi": 56, "atomiswave": 53,
	"psx": 57, "ps2": 58, "psp": 61, "pcengine": 31, "pcenginecd": 114, "supergrafx": 105,
	"atari2600": 26, "atari5200": 40, "atari7800": 41, "lynx": 28, "jaguar": 27, "atarist": 42,
	"ngp": 25, "ngpc": 82, "neogeo": 142, "wswan": 45, "wswanc": 46, "colecovision": 48, "mame": 75, "fbneo": 75, "3do": 29,
	"amiga500": 64, "amiga1200": 111, "c64": 66, "msx1": 113, "msx2": 116, "amstradcpc": 65, "zxspectrum": 76, "scummvm": 123, "prboom": 135, "gzdoom": 135, "intellivision": 115, "vectrex": 102, "ports": 135, "cps1": 6, "cps2": 7, "cps3": 8, "neogeocd": 70,
}

// ssMediaKinds: tipo ES → tipi media ScreenScraper in ordine di preferenza.
var ssMediaKinds = map[string][]string{
	"thumbnail": {"box-2D", "box-3D"},
	"image":     {"ss", "sstitle"},
	"titleshot": {"sstitle", "ss"},
	"marquee":   {"wheel-hd", "wheel", "marquee"},
	"fanart":    {"fanart", "screenmarqueesmall"},
	"boxback":   {"box-2D-back"},
	"video":     {"video-normalized", "video"},
	"manual":    {"manuel"},
}

type ssText struct {
	Region     string `json:"region"`
	Langue     string `json:"langue"`
	Text       string `json:"text"`
	ID         string `json:"id"`
	Type       string `json:"type"`
	Format     string `json:"format"`
	URL        string `json:"url"`
	Parent     string `json:"parent"`
	Principale string `json:"principale"`
}

type SSGame struct {
	ID          string
	Name        string
	Desc        string
	Genre       string
	Developer   string
	Publisher   string
	Players     string
	Rating      string
	ReleaseDate string
	Media       map[string]ssText // tipo → media scelto
}

type ssResp struct {
	Header   map[string]any `json:"header"`
	Response struct {
		Ssuser map[string]any `json:"ssuser"`
		Jeu    struct {
			ID       string   `json:"id"`
			Noms     []ssText `json:"noms"`
			Synopsis []ssText `json:"synopsis"`
			Genres   []struct {
				Noms []ssText `json:"noms"`
			} `json:"genres"`
			Developpeur struct {
				Text string `json:"text"`
			} `json:"developpeur"`
			Editeur struct {
				Text string `json:"text"`
			} `json:"editeur"`
			Joueurs struct {
				Text string `json:"text"`
			} `json:"joueurs"`
			Note struct {
				Text string `json:"text"`
			} `json:"note"`
			Dates  []ssText `json:"dates"`
			Medias []ssText `json:"medias"`
		} `json:"jeu"`
	} `json:"response"`
}

func (c *SS) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *SS) base(endpoint string) url.Values {
	v := url.Values{}
	v.Set("devid", c.DevID)
	v.Set("devpassword", c.DevPassword)
	v.Set("softname", c.Soft)
	v.Set("output", "json")
	v.Set("ssid", c.User)
	v.Set("sspassword", c.Pass)
	return v
}

type SSError struct {
	Code int
	Msg  string
}

func (e *SSError) Error() string { return e.Msg }

func IsQuotaError(err error) bool {
	var e *SSError
	if errors.As(err, &e) {
		return e.Code == 429 || e.Code == 430 || e.Code == 431 || e.Code == 423
	}
	return false
}

func IsNotFound(err error) bool {
	var e *SSError
	if errors.As(err, &e) {
		return e.Code == 404
	}
	return false
}

func (c *SS) call(endpoint string, v url.Values) ([]byte, error) {
	if c.DevID == "" || c.DevPassword == "" {
		return nil, &SSError{400, "servono anche le credenziali sviluppatore (devid/devpassword) di ScreenScraper: vedi Impostazioni → ScreenScraper"}
	}
	resp, err := c.client().Get("https://api.screenscraper.fr/api2/" + endpoint + "?" + v.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch resp.StatusCode {
	case 200:
		return b, nil
	case 400:
		return nil, &SSError{400, "richiesta non valida (" + strings.TrimSpace(string(b)) + ")"}
	case 401:
		return nil, &SSError{401, "credenziali sviluppatore non valide"}
	case 403:
		return nil, &SSError{403, "utente o password ScreenScraper errati"}
	case 404:
		return nil, &SSError{404, "gioco non trovato"}
	case 423:
		return nil, &SSError{423, "ScreenScraper è in manutenzione: riprova più tardi"}
	case 426:
		return nil, &SSError{426, "il software non è più supportato da ScreenScraper"}
	case 429:
		return nil, &SSError{429, "troppe richieste al secondo: rallento"}
	case 430:
		return nil, &SSError{430, "quota giornaliera di ScreenScraper esaurita per questo account"}
	case 431:
		return nil, &SSError{431, "quota giornaliera di ScreenScraper esaurita"}
	}
	return nil, &SSError{resp.StatusCode, fmt.Sprintf("ScreenScraper: HTTP %d", resp.StatusCode)}
}

// UserInfo verifica l'account e restituisce i dati utente (livello, quote…).
func (c *SS) UserInfo() (map[string]any, error) {
	b, err := c.call("ssuserInfos.php", c.base("ssuserInfos.php"))
	if err != nil {
		return nil, err
	}
	var r ssResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("risposta non valida")
	}
	if r.Response.Ssuser == nil {
		return nil, errors.New("risposta senza dati utente")
	}
	return r.Response.Ssuser, nil
}

// GameInfo cerca un gioco per nome file/dimensione/CRC/MD5.
func (c *SS) GameInfo(systemID int, romName string, size int64, crc, md5 string) (*SSGame, error) {
	v := c.base("jeuInfos.php")
	v.Set("systemeid", strconv.Itoa(systemID))
	v.Set("romtype", "rom")
	v.Set("romnom", romName)
	if size > 0 {
		v.Set("romtaille", strconv.FormatInt(size, 10))
	}
	if crc != "" {
		v.Set("crc", strings.ToUpper(crc))
	}
	if md5 != "" {
		v.Set("md5", strings.ToUpper(md5))
	}
	b, err := c.call("jeuInfos.php", v)
	if err != nil {
		return nil, err
	}
	var r ssResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("risposta non valida")
	}
	if r.Response.Jeu.ID == "" {
		return nil, &SSError{404, "gioco non trovato"}
	}
	return c.parseGame(&r), nil
}

func (c *SS) pick(list []ssText, byLang bool) string {
	prefs := c.Regions
	if byLang {
		prefs = c.Langs
	}
	for _, p := range prefs {
		for _, t := range list {
			if (byLang && t.Langue == p) || (!byLang && t.Region == p) {
				return t.Text
			}
		}
	}
	if len(list) > 0 {
		return list[0].Text
	}
	return ""
}

func (c *SS) parseGame(r *ssResp) *SSGame {
	j := r.Response.Jeu
	g := &SSGame{ID: j.ID, Media: map[string]ssText{}}
	g.Name = c.pick(j.Noms, false)
	g.Desc = c.pick(j.Synopsis, true)
	if len(j.Genres) > 0 {
		var gs []string
		for _, ge := range j.Genres {
			if n := c.pick(ge.Noms, true); n != "" {
				gs = append(gs, n)
			}
		}
		g.Genre = strings.Join(gs, " / ")
	}
	g.Developer = j.Developpeur.Text
	g.Publisher = j.Editeur.Text
	g.Players = j.Joueurs.Text
	if j.Note.Text != "" {
		if n, err := strconv.ParseFloat(j.Note.Text, 64); err == nil {
			g.Rating = fmt.Sprintf("%.2f", n/20)
		}
	}
	if d := c.pick(j.Dates, false); d != "" {
		d = strings.ReplaceAll(d, "-", "")
		for len(d) < 8 {
			d += "01"
		}
		g.ReleaseDate = d[:8] + "T000000"
	}
	for kind, types := range ssMediaKinds {
		for _, typ := range types {
			var best *ssText
			for i := range j.Medias {
				m := &j.Medias[i]
				if m.Type != typ || m.URL == "" {
					continue
				}
				if best == nil {
					best = m
				}
				for ri, reg := range c.Regions {
					if m.Region == reg {
						if best.Region != reg || indexOf(c.Regions, best.Region) > ri {
							best = m
						}
					}
				}
			}
			if best != nil {
				g.Media[kind] = *best
				break
			}
		}
	}
	return g
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return 1 << 20
}

// DownloadMedia scarica un media di ScreenScraper.
func (c *SS) DownloadMedia(u string) ([]byte, string, error) {
	resp, err := c.client().Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", &SSError{resp.StatusCode, fmt.Sprintf("media: HTTP %d", resp.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return nil, "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(b)
	}
	return b, ct, nil
}

// SSHit: risultato della ricerca per nome.
type SSHit struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	System string `json:"system"`
	Year   string `json:"year"`
	Debug  string `json:"debug,omitempty"`
}

type ssSearchResp struct {
	Response struct {
		Jeux []struct {
			ID      json.Number `json:"id"`
			Noms    []ssText `json:"noms"`
			Dates   []ssText `json:"dates"`
			Systeme struct {
				Text string `json:"text"`
			} `json:"systeme"`
		} `json:"jeux"`
	} `json:"response"`
}

// Search cerca un gioco per nome (jeuRecherche); systemID 0 = tutti i sistemi.
func (c *SS) Search(name string, systemID int) ([]SSHit, error) {
	v := c.base("jeuRecherche.php")
	v.Set("recherche", name)
	if systemID > 0 {
		v.Set("systemeid", strconv.Itoa(systemID))
	}
	b, err := c.call("jeuRecherche.php", v)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var r ssSearchResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("risposta non valida")
	}
	// id grezzi (il campo puo' chiamarsi o essere formattato diversamente da come lo aspetto)
	var raw struct {
		Response struct {
			Jeux []map[string]any `json:"jeux"`
		} `json:"response"`
	}
	_ = json.Unmarshal(b, &raw)
	var out []SSHit
	for i, j := range r.Response.Jeux {
		h := SSHit{ID: j.ID.String(), Name: c.pick(j.Noms, false), System: j.Systeme.Text}
		if h.ID == "" && i < len(raw.Response.Jeux) {
			m := raw.Response.Jeux[i]
			for _, k := range []string{"id", "jeuid", "gameid", "idjeu"} {
				if v, ok := m[k]; ok && v != nil {
					h.ID = strings.TrimSpace(fmt.Sprint(v))
					break
				}
			}
			if h.ID == "" {
				var keys []string
				for k := range m {
					keys = append(keys, k)
				}
				h.Debug = "campi: " + strings.Join(keys, ", ")
			}
		}
		if d := c.pick(j.Dates, false); len(d) >= 4 {
			h.Year = d[:4]
		}
		out = append(out, h)
	}
	return out, nil
}

// GameByID legge la scheda completa di un gioco dato il suo id ScreenScraper.
func (c *SS) GameByID(id string) (*SSGame, error) {
	v := c.base("jeuInfos.php")
	v.Set("gameid", id)
	b, err := c.call("jeuInfos.php", v)
	if err != nil {
		return nil, err
	}
	var r ssResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("risposta non valida")
	}
	if r.Response.Jeu.ID == "" {
		return nil, &SSError{404, "gioco non trovato"}
	}
	return c.parseGame(&r), nil
}
