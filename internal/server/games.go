package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

func r0() context.Context { return context.Background() }

// gamelistPy: lettura/scrittura di gamelist.xml sul cabinato.
const gamelistPy = `
import sys, json, os, xml.etree.ElementTree as ET
sysname, op = sys.argv[1], sys.argv[2]
root_dir = "/userdata/roms/" + sysname
gl = os.path.join(root_dir, "gamelist.xml")
if os.path.exists(gl):
    tree = ET.parse(gl); root = tree.getroot()
else:
    root = ET.Element("gameList"); tree = ET.ElementTree(root)
def norm(p):
    p = p.strip()
    if p.startswith(root_dir + "/"): p = "./" + p[len(root_dir)+1:]
    if not p.startswith("./") and not p.startswith("/"): p = "./" + p
    return p
def find(p):
    p = norm(p)
    for g in root.findall("game"):
        if norm(g.findtext("path", "")) == p: return g
    return None
if op == "get":
    g = find(sys.argv[3])
    print(json.dumps({c.tag: (c.text or "") for c in g} if g is not None else {}))
elif op == "set":
    p = sys.argv[3]; meta = json.loads(sys.stdin.read())
    g = find(p)
    if g is None:
        g = ET.SubElement(root, "game"); ET.SubElement(g, "path").text = norm(p)
    for k, v in meta.items():
        if k == "path": continue
        el = g.find(k)
        if v is None or v == "":
            if el is not None: g.remove(el)
            continue
        if el is None: el = ET.SubElement(g, k)
        el.text = str(v)
    tree.write(gl, encoding="utf-8", xml_declaration=True)
    print("ok")
elif op == "hide":
    req = json.loads(sys.stdin.read()); want = set(norm(x) for x in req["paths"]); hidden = bool(req["hidden"])
    changed = []
    for g in root.findall("game"):
        p = norm(g.findtext("path", ""))
        if p not in want: continue
        el = g.find("hidden"); cur = (el is not None and (el.text or "").strip() == "true")
        if cur == hidden: continue
        if hidden:
            if el is None: el = ET.SubElement(g, "hidden")
            el.text = "true"
        else:
            if el is not None: g.remove(el)
        changed.append(p)
    tree.write(gl, encoding="utf-8", xml_declaration=True)
    print(json.dumps(changed))
elif op == "remove":
    paths = set(norm(x) for x in json.loads(sys.stdin.read()))
    n = 0
    for g in list(root.findall("game")):
        if norm(g.findtext("path", "")) in paths: root.remove(g); n += 1
    tree.write(gl, encoding="utf-8", xml_declaration=True)
    print(n)
`

func (s *Server) gamelistRun(system, op, arg string, stdin any) (string, error) {
	cmd := "python3 -c " + cab.ShellQuote(gamelistPy) + " " + cab.ShellQuote(system) + " " + op + " " + cab.ShellQuote(arg)
	if stdin != nil {
		b, _ := json.Marshal(stdin)
		if err := s.ssh.UploadBytes("/tmp/companion_gl.json", b, 0); err != nil {
			return "", err
		}
		cmd += " < /tmp/companion_gl.json"
	}
	res, err := s.ssh.RunTimeout(cmd, 2*time.Minute)
	if err != nil {
		return "", err
	}
	if res.Exit != 0 {
		return "", fmt.Errorf("gamelist: %s", strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (s *Server) hGamesMeta(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		sys, id := r.URL.Query().Get("system"), r.URL.Query().Get("id")
		g, err := s.es.GameByID(sys, id)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for k, v := range g.Raw {
			out[k] = cab.AsStr(v)
		}
		if gl, err := s.gamelistRun(sys, "get", g.Path, nil); err == nil && gl != "" {
			var m map[string]string
			if json.Unmarshal([]byte(gl), &m) == nil {
				for k, v := range m {
					if _, ok := out[k]; !ok || out[k] == "" {
						out[k] = v
					}
				}
			}
		}
		out["path"] = g.Path
		out["name"] = g.Name
		out["suggested_name"] = s.suggestName(sys, g)
		return out, nil
	}
	var req struct {
		System string            `json:"system"`
		ID     string            `json:"id"`
		Meta   map[string]string `json:"meta"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	g, err := s.es.GameByID(req.System, req.ID)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{}
	for k, v := range req.Meta {
		switch k {
		case "name", "desc", "genre", "developer", "publisher", "players", "rating", "releasedate", "favorite", "hidden", "kidgame":
			if k == "releasedate" && v == "not-a-date-time" {
				v = ""
			}
			if k == "releasedate" && len(v) == 10 && v[4] == '-' {
				v = strings.ReplaceAll(v, "-", "") + "T000000"
			}
			if (k == "favorite" || k == "hidden" || k == "kidgame") && v == "false" {
				v = ""
			}
			meta[k] = v
		}
	}
	if _, err := s.gamelistRun(req.System, "set", g.Path, meta); err != nil {
		return nil, err
	}
	_ = s.es.ReloadGames()
	return nil, nil
}

var reTags = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

func cleanName(n string) string {
	n = reTags.ReplaceAllString(n, "")
	n = strings.TrimSpace(n)
	if i := strings.Index(n, ", The"); i > 0 && i == len(n)-5 {
		n = "The " + n[:i]
	}
	return n
}

func (s *Server) suggestName(system string, g *cab.Game) string {
	base := path.Base(g.Path)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if system == "mame" || system == "fbneo" || system == "neogeo" || strings.HasPrefix(system, "cps") {
		for _, id := range []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"} {
			db, _ := s.dats.Load(id)
			if db == nil {
				continue
			}
			if gm := db.Games[strings.ToLower(stem)]; gm != nil && gm.Desc != "" {
				return gm.Desc
			}
		}
		return ""
	}
	return cleanName(stem)
}

// ---------- rinomina ----------

func (s *Server) hGamesRename(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		System string   `json:"system"`
		IDs    []string `json:"ids"`
		Apply  bool     `json:"apply"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	games, err := s.es.Games(req.System)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range req.IDs {
		want[id] = true
	}
	type row struct {
		ID, Old, New, Source string
		Changed              bool
	}
	var rows []row
	for i := range games {
		g := &games[i]
		if len(want) > 0 && !want[g.ID] {
			continue
		}
		n := s.suggestName(req.System, g)
		src := "nome file ripulito"
		if req.System == "mame" || req.System == "fbneo" || req.System == "neogeo" {
			src = "DAT arcade"
		}
		if n == "" {
			continue
		}
		rows = append(rows, row{ID: g.ID, Old: g.Name, New: n, Source: src, Changed: n != g.Name})
	}
	if !req.Apply {
		return map[string]any{"rows": rows}, nil
	}
	changed := 0
	for _, rw := range rows {
		if !rw.Changed {
			continue
		}
		var g *cab.Game
		for i := range games {
			if games[i].ID == rw.ID {
				g = &games[i]
			}
		}
		if g == nil {
			continue
		}
		if _, err := s.gamelistRun(req.System, "set", g.Path, map[string]any{"name": rw.New}); err == nil {
			changed++
		}
	}
	_ = s.es.ReloadGames()
	return map[string]any{"changed": changed}, nil
}

// ---------- eliminazione / cestino ----------

const deletePy = `
import sys, os, json, shutil, re, time
system, permanent = sys.argv[1], sys.argv[2] == "1"
paths = json.loads(sys.stdin.read())
root = "/userdata/roms/" + system
trash = "/userdata/system/companion_trash/" + system + "/" + time.strftime("%Y%m%d-%H%M%S")
moved, errors = 0, []
def linked(p):
    out = [p]
    ext = os.path.splitext(p)[1].lower()
    d = os.path.dirname(p)
    try:
        if ext == ".cue":
            for l in open(p, errors="ignore"):
                m = re.match(r'\s*FILE\s+"?([^"\n]+?)"?\s+\w+\s*$', l, re.I)
                if m: out.append(os.path.join(d, m.group(1).strip()))
        elif ext == ".gdi":
            for l in list(open(p, errors="ignore"))[1:]:
                f = l.split()
                if len(f) >= 5: out.append(os.path.join(d, f[4].strip('"')))
        elif ext == ".m3u":
            for l in open(p, errors="ignore"):
                l = l.strip()
                if l and not l.startswith("#"):
                    q = os.path.join(d, l); out.append(q)
                    out.extend(linked(q)[1:])
    except Exception as e: pass
    return out
def media(p):
    stem = os.path.splitext(os.path.basename(p))[0]
    out = []
    for sub in ("images", "videos", "manuals", "media"):
        md = os.path.join(root, sub)
        if not os.path.isdir(md): continue
        for f in os.listdir(md):
            if f.startswith(stem + "-") or f.startswith(stem + ".") or f == stem:
                out.append(os.path.join(md, f))
    return out
for p in paths:
    if not p.startswith(root + "/"):
        errors.append(p + ": fuori dalla cartella del sistema"); continue
    for f in linked(p) + media(p):
        if not os.path.exists(f): continue
        try:
            if permanent:
                if os.path.isdir(f): shutil.rmtree(f)
                else: os.remove(f)
            else:
                rel = os.path.relpath(f, root)
                dst = os.path.join(trash, rel)
                os.makedirs(os.path.dirname(dst), exist_ok=True)
                shutil.move(f, dst)
            moved += 1
        except Exception as e:
            errors.append("%s: %s" % (f, e))
print(json.dumps({"moved": moved, "errors": errors}))
`

func (s *Server) hGamesDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Paths     []string `json:"paths"`
		Permanent bool     `json:"permanent"`
		System    string   `json:"system"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if len(req.Paths) == 0 || req.System == "" {
		return nil, errors.New("niente da eliminare")
	}
	j := s.jobs.Start("Eliminazione giochi", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, fmt.Sprintf("Elimino %d giochi", len(req.Paths)))
		b, _ := json.Marshal(req.Paths)
		if err := s.ssh.UploadBytes("/tmp/companion_del.json", b, 0); err != nil {
			return nil, err
		}
		perm := "0"
		if req.Permanent {
			perm = "1"
		}
		res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(deletePy)+" "+cab.ShellQuote(req.System)+" "+perm+" < /tmp/companion_del.json", 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if res.Exit != 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(res.Stderr))
		}
		var out struct {
			Moved  int      `json:"moved"`
			Errors []string `json:"errors"`
		}
		if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &out); err != nil {
			return nil, fmt.Errorf("risposta non valida: %s", res.Stdout)
		}
		for _, e := range out.Errors {
			j.Logf("⚠ %s", e)
		}
		j.SetProgress(0.9, "Aggiorno la lista di EmulationStation")
		if err := s.es.RemoveGames(req.System, req.Paths); err != nil {
			// gamelist a mano
			_, _ = s.gamelistRun(req.System, "remove", "", req.Paths)
		}
		_ = s.es.ReloadGames()
		j.Logf("%d file %s, lista giochi rigenerata", out.Moved, map[bool]string{true: "eliminati", false: "nel cestino"}[req.Permanent])
		return map[string]any{"moved": out.Moved, "errors": out.Errors}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func lastLine(s string) string {
	l := cab.Lines(s)
	if len(l) == 0 {
		return ""
	}
	return l[len(l)-1]
}

func (s *Server) hGamesTrash(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method == http.MethodPost {
		_, err := s.ssh.RunTimeout("rm -rf "+trashDir+"/*", 5*time.Minute)
		return nil, err
	}
	res, err := s.ssh.RunTimeout("[ -d "+trashDir+" ] && find "+trashDir+" -type f | wc -l && du -sb "+trashDir+" | cut -f1", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	f := strings.Fields(res.Stdout)
	out := map[string]any{"count": 0, "size": 0}
	if len(f) >= 2 {
		var c, sz int64
		fmt.Sscanf(f[0], "%d", &c)
		fmt.Sscanf(f[1], "%d", &sz)
		out["count"], out["size"] = c, sz
	}
	return out, nil
}

// ---------- grafiche caricate a mano ----------

func (s *Server) hGamesMediaSet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error()})
		return
	}
	sys, id, kind := r.FormValue("system"), r.FormValue("id"), r.FormValue("kind")
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 200, map[string]any{"error": "file mancante"})
		return
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	ct := hdr.Header.Get("Content-Type")
	if kind != "video" && kind != "manual" {
		// immagini in formati non capiti da ES → PNG
		ext := strings.ToLower(path.Ext(hdr.Filename))
		if ext == ".webp" || ext == ".bmp" || ext == ".tif" || ext == ".tiff" || ext == ".gif" {
			if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				var buf bytes.Buffer
				if png.Encode(&buf, img) == nil {
					data = buf.Bytes()
					ct = "image/png"
				}
			}
		}
	}
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	if err := s.setGameMedia(sys, id, kind, data, ct, hdr.Filename); err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// setGameMedia: prova l'API di ES, altrimenti scrive il file e aggiorna gamelist.xml.
func (s *Server) setGameMedia(sys, id, kind string, data []byte, ct, filename string) error {
	if err := s.es.UploadMedia(sys, id, kind, data, ct); err == nil {
		_ = s.es.ReloadGames()
		return nil
	}
	g, err := s.es.GameByID(sys, id)
	if err != nil {
		return err
	}
	ext := strings.ToLower(path.Ext(filename))
	if ext == "" || ct == "image/png" {
		ext = ".png"
	}
	sub := "images"
	suffix := map[string]string{"thumbnail": "-thumb", "image": "-image", "titleshot": "-titleshot", "marquee": "-marquee", "fanart": "-fanart", "boxback": "-boxback", "video": "-video", "manual": "-manual"}[kind]
	if kind == "video" {
		sub = "videos"
	} else if kind == "manual" {
		sub = "manuals"
	}
	stem := strings.TrimSuffix(path.Base(g.Path), path.Ext(g.Path))
	rel := "./" + sub + "/" + stem + suffix + ext
	remote := path.Join("/userdata/roms", sys, sub, stem+suffix+ext)
	if err := s.ssh.UploadBytes(remote, data, 0o644); err != nil {
		return err
	}
	if _, err := s.gamelistRun(sys, "set", g.Path, map[string]any{kind: rel}); err != nil {
		return err
	}
	return s.es.ReloadGames()
}

// ---------- multi-disco ----------

const multidiscPy = `
import os, re, sys, json
system = sys.argv[1]; root = "/userdata/roms/" + system
m3u_ok = system in ("psx","segacd","pcenginecd","saturn","dreamcast","3do","ps2","psp","amiga500","amiga1200","amigacd32","neogeocd","scummvm")
rx = re.compile(r'^(.*?)[\s_]*[\(\[](?:disc|disk|cd)\s*0*(\d+)(?:\s*(?:of|/)\s*\d+)?[\)\]](.*)$', re.I)
groups = {}
for f in sorted(os.listdir(root)):
    p = os.path.join(root, f)
    if not os.path.isfile(p): continue
    ext = os.path.splitext(f)[1].lower()
    if ext not in (".cue",".chd",".iso",".gdi",".cdi",".pbp",".mds"): continue
    m = rx.match(os.path.splitext(f)[0])
    if not m: continue
    key = (m.group(1).strip() + m.group(3).strip(), ext)
    groups.setdefault(key, []).append((int(m.group(2)), f))
hidden, created = [], 0
for (title, ext), discs in groups.items():
    if len(discs) < 2: continue
    discs.sort()
    if m3u_ok:
        m3u = os.path.join(root, title + ".m3u")
        if not os.path.exists(m3u):
            open(m3u, "w").write("\n".join(f for _, f in discs) + "\n"); created += 1
        hidden += [os.path.join(root, f) for _, f in discs]
    else:
        hidden += [os.path.join(root, f) for _, f in discs[1:]]
print(json.dumps({"groups": sum(1 for d in groups.values() if len(d) >= 2), "created": created, "hidden": hidden, "m3u": m3u_ok}))
`

func (s *Server) hGamesMultidisc(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		System string `json:"system"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	j := s.jobs.Start("Multi-disco", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Cerco i giochi multi-disco in "+req.System)
		res, err := s.ssh.RunTimeout("python3 -c "+cab.ShellQuote(multidiscPy)+" "+cab.ShellQuote(req.System), 5*time.Minute)
		if err != nil {
			return nil, err
		}
		var out struct {
			Groups  int      `json:"groups"`
			Created int      `json:"created"`
			Hidden  []string `json:"hidden"`
			M3U     bool     `json:"m3u"`
		}
		if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &out); err != nil {
			return nil, fmt.Errorf("%s", strings.TrimSpace(res.Stderr+res.Stdout))
		}
		j.Logf("%d giochi multi-disco, %d .m3u creati", out.Groups, out.Created)
		if !out.M3U {
			j.Logf("questo sistema non usa .m3u: resta visibile solo il disco 1")
		}
		n := 0
		for _, h := range out.Hidden {
			if _, err := s.gamelistRun(req.System, "set", h, map[string]any{"hidden": "true"}); err == nil {
				n++
			}
		}
		_ = s.es.ReloadGames()
		return map[string]any{"groups": out.Groups, "hidden": n, "created": out.Created}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

// ---------- set MAME alla versione del cabinato ----------

func (s *Server) hGamesMame(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Mode  string   `json:"mode"`
		Names []string `json:"names"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Mode != "convert" {
		req.Mode = "check"
	}
	script, err := s.asset("mame_rebuild.py")
	if err != nil {
		return nil, err
	}
	j := s.jobs.Start("Set MAME", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Preparo lo script sul cabinato")
		if err := s.ssh.UploadBytes(compDir+"/mame_rebuild.py", script, 0o755); err != nil {
			return nil, err
		}
		args := "/userdata/roms/mame " + req.Mode
		if len(req.Names) > 0 {
			args += " --only " + cab.ShellQuote(strings.Join(req.Names, ","))
		}
		j.SetProgress(-1, "Leggo la lista del MAME installato e confronto gli zip (può richiedere qualche minuto)")
		res, err := s.ssh.RunTimeout("python3 "+compDir+"/mame_rebuild.py "+args+" 2>/tmp/mame_rebuild.log; tail -40 /tmp/mame_rebuild.log >&2", 60*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stderr) {
			j.Logf("%s", l)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &out); err != nil {
			return nil, fmt.Errorf("risposta non valida: %s", strings.TrimSpace(res.Stderr))
		}
		if req.Mode == "convert" {
			_ = s.es.ReloadGames()
		}
		return out, nil
	})
	return map[string]any{"job": j.ID}, nil
}
