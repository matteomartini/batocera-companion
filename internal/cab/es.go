package cab

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ES: API HTTP di EmulationStation (porta 1234 su Batocera).
type ES struct {
	mu   sync.Mutex
	host string
	port int
	cl   *http.Client
}

type System struct {
	Name     string `json:"name"`
	Fullname string `json:"fullname"`
	Count    int    `json:"count"`
	Visible  bool   `json:"visible"`
}

type Game struct {
	ID          string         `json:"id"`
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	System      string         `json:"system"`
	Desc        string         `json:"desc,omitempty"`
	Image       string         `json:"image,omitempty"`
	Thumbnail   string         `json:"thumbnail,omitempty"`
	Video       string         `json:"video,omitempty"`
	Marquee     string         `json:"marquee,omitempty"`
	Hidden      bool           `json:"hidden"`
	Favorite    bool           `json:"favorite"`
	Genre       string         `json:"genre,omitempty"`
	Developer   string         `json:"developer,omitempty"`
	Publisher   string         `json:"publisher,omitempty"`
	Players     string         `json:"players,omitempty"`
	Rating      string         `json:"rating,omitempty"`
	ReleaseDate string         `json:"releasedate,omitempty"`
	Raw         map[string]any `json:"-"`
}

func NewES(host string, port int) *ES {
	return &ES{host: host, port: port, cl: &http.Client{Timeout: 20 * time.Second}}
}

func (e *ES) Configure(host string, port int) {
	e.mu.Lock()
	e.host, e.port = host, port
	e.mu.Unlock()
}

func (e *ES) base() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return fmt.Sprintf("http://%s:%d", e.host, e.port)
}

func (e *ES) do(method, p string, body io.Reader, ctype string, timeout time.Duration) ([]byte, int, error) {
	req, err := http.NewRequest(method, e.base()+p, body)
	if err != nil {
		return nil, 0, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	cl := e.cl
	if timeout > 0 {
		cl = &http.Client{Timeout: timeout}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

func (e *ES) get(p string) ([]byte, error) {
	b, st, err := e.do("GET", p, nil, "", 0)
	if err != nil {
		return nil, fmt.Errorf("EmulationStation non raggiungibile: %w", err)
	}
	if st >= 400 {
		return nil, fmt.Errorf("EmulationStation: HTTP %d su %s", st, p)
	}
	return b, nil
}

func (e *ES) Online() bool {
	_, _, err := e.do("GET", "/caps", nil, "", 3*time.Second)
	return err == nil
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}

// AsStr converte un valore JSON generico in stringa.
func AsStr(v any) string { return asStr(v) }

func asStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%g", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

func (e *ES) Systems() ([]System, error) {
	b, err := e.get("/systems")
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("risposta /systems non valida")
	}
	var out []System
	for _, r := range raw {
		s := System{Name: asStr(r["name"]), Fullname: asStr(r["fullname"]), Visible: true}
		for _, k := range []string{"totalGames", "games", "count", "visibleGames"} {
			if v, ok := r[k]; ok {
				s.Count = asInt(v)
				break
			}
		}
		if v, ok := r["visible"]; ok {
			s.Visible = asStr(v) != "false"
		}
		out = append(out, s)
	}
	return out, nil
}

func parseGame(r map[string]any) Game {
	g := Game{Raw: r, ID: asStr(r["id"]), Path: asStr(r["path"]), Name: asStr(r["name"]), System: asStr(r["system"]), Desc: asStr(r["desc"]), Image: asStr(r["image"]), Thumbnail: asStr(r["thumbnail"]), Video: asStr(r["video"]), Marquee: asStr(r["marquee"]), Genre: asStr(r["genre"]), Developer: asStr(r["developer"]), Publisher: asStr(r["publisher"]), Players: asStr(r["players"]), Rating: asStr(r["rating"]), ReleaseDate: asStr(r["releasedate"])}
	g.Hidden = asStr(r["hidden"]) == "true"
	g.Favorite = asStr(r["favorite"]) == "true"
	if g.System == "" {
		g.System = asStr(r["systemName"])
	}
	return g
}

func (e *ES) Games(system string) ([]Game, error) {
	b, err := e.get("/systems/" + url.PathEscape(system) + "/games")
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("risposta giochi non valida")
	}
	out := make([]Game, 0, len(raw))
	for _, r := range raw {
		g := parseGame(r)
		if g.System == "" {
			g.System = system
		}
		out = append(out, g)
	}
	return out, nil
}

func (e *ES) GameByID(system, id string) (*Game, error) {
	b, err := e.get("/systems/" + url.PathEscape(system) + "/games/" + url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("risposta gioco non valida")
	}
	g := parseGame(r)
	if g.System == "" {
		g.System = system
	}
	return &g, nil
}

// RunningGame: nil se nessun gioco in corso.
func (e *ES) RunningGame() (*Game, error) {
	b, st, err := e.do("GET", "/runningGame", nil, "", 5*time.Second)
	if err != nil {
		return nil, err
	}
	// ES risponde 200 + JSON del gioco solo con un gioco avviato; altrimenti 201/204/404 o testo ("No game running")
	if st != 200 || len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, nil
	}
	g := parseGame(r)
	if g.Path == "" && g.Name == "" {
		return nil, nil
	}
	return &g, nil
}

func (e *ES) ReloadGames() error { _, err := e.get("/reloadgames"); return err }
func (e *ES) EmuKill() error     { _, err := e.get("/emukill"); return err }

func (e *ES) Launch(path string) error {
	_, st, err := e.do("POST", "/launch", strings.NewReader(path), "text/plain", 10*time.Second)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("avvio rifiutato (HTTP %d)", st)
	}
	return nil
}

func (e *ES) MessageBox(text string) error {
	_, st, err := e.do("POST", "/messagebox", strings.NewReader(text), "text/plain", 5*time.Second)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("HTTP %d", st)
	}
	return nil
}

func (e *ES) Notify(text string) error {
	_, st, err := e.do("POST", "/notify", strings.NewReader(text), "text/plain", 5*time.Second)
	if err != nil || st >= 400 {
		return e.MessageBox(text)
	}
	return nil
}

// AddGames registra giochi in gamelist.xml tramite ES (lista di {path, name, hidden…}).
func (e *ES) AddGames(system string, games []map[string]any) error {
	b, _ := json.Marshal(games)
	_, st, err := e.do("POST", "/addgames/"+url.PathEscape(system), bytes.NewReader(b), "application/json", 30*time.Second)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("addgames: HTTP %d", st)
	}
	return nil
}

func (e *ES) RemoveGames(system string, paths []string) error {
	var list []map[string]any
	for _, p := range paths {
		list = append(list, map[string]any{"path": p})
	}
	b, _ := json.Marshal(list)
	_, st, err := e.do("POST", "/removegames/"+url.PathEscape(system), bytes.NewReader(b), "application/json", 30*time.Second)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("removegames: HTTP %d", st)
	}
	return nil
}

// Media legge un'immagine/video di un gioco.
func (e *ES) Media(system, id, kind string) ([]byte, string, error) {
	req, _ := http.NewRequest("GET", e.base()+"/systems/"+url.PathEscape(system)+"/games/"+url.PathEscape(id)+"/media/"+kind, nil)
	resp, err := e.cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", errors.New("media non disponibile")
	}
	b, _ := io.ReadAll(resp.Body)
	return b, resp.Header.Get("Content-Type"), nil
}

// UploadMedia carica un'immagine/video per un gioco (ES aggiorna gamelist.xml).
func (e *ES) UploadMedia(system, id, kind string, data []byte, ctype string) error {
	_, st, err := e.do("POST", "/systems/"+url.PathEscape(system)+"/games/"+url.PathEscape(id)+"/media/"+kind, bytes.NewReader(data), ctype, 60*time.Second)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("upload media: HTTP %d", st)
	}
	return nil
}
