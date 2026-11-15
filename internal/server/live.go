package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// ---------- schermo in diretta (MJPEG via ffmpeg sul cabinato) ----------

type liveState struct {
	mu      sync.Mutex
	viewers int
}

const liveScript = `export DISPLAY=$(getLocalXDisplay 2>/dev/null || echo :0); GEOM=$(batocera-resolution currentResolution 2>/dev/null); [ -z "$GEOM" ] && GEOM=1280x960
GW=${GEOM%%x*}; GH=${GEOM#*x}; OW=%d; [ "$OW" -gt "$GW" ] && OW=$GW; OH=$(( (OW*GH/GW)/2*2 ))
CARD=$(ls /dev/dri/card* 2>/dev/null | head -1)
ffmpeg -loglevel error -device "$CARD" -f kmsgrab -framerate %d -i - -vf "hwmap=derive_device=vaapi,scale_vaapi=w=$OW:h=$OH:format=nv12" -c:v mjpeg_vaapi -global_quality %d -f mpjpeg - || exec ffmpeg -loglevel error -probesize 10M -f x11grab -framerate %d -video_size "$GEOM" -i "$DISPLAY+0,0" -vf scale=%d:-2 -q:v %d -f mpjpeg -`

func (s *Server) hLive(w http.ResponseWriter, r *http.Request) {
	fps, _ := strconv.Atoi(r.URL.Query().Get("fps"))
	if fps < 1 || fps > 30 {
		fps = 12
	}
	width, _ := strconv.Atoi(r.URL.Query().Get("w"))
	if width < 320 || width > 1920 {
		width = 960
	}
	q := 6
	if width <= 640 {
		q = 5
	}
	cmd := fmt.Sprintf(liveScript, width, fps, 60, fps, width, q)
	rc, err := s.ssh.StreamSession(cmd)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer rc.Close()
	s.live.mu.Lock()
	s.live.viewers++
	s.live.mu.Unlock()
	defer func() { s.live.mu.Lock(); s.live.viewers--; s.live.mu.Unlock() }()
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=ffmpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	// ffmpeg -f mpjpeg produce già un multipart con boundary "ffmpeg": lo passo così com'è
	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

// cursore del mouse (kmsgrab non lo cattura)
func (s *Server) hLiveCursor(w http.ResponseWriter, r *http.Request) {
	rc, err := s.ssh.StreamSession(`export DISPLAY=$(getLocalXDisplay 2>/dev/null || echo :0); xdotool getdisplaygeometry; while :; do eval "$(xdotool getmouselocation --shell 2>/dev/null)"; echo "x:$X y:$Y"; sleep 0.08; done`)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	sc := bufio.NewScanner(rc)
	for sc.Scan() {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(sc.Text())); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

// ---------- tastiera virtuale (uinput sul cabinato) ----------

type inputProc struct {
	stdin io.WriteCloser
	rc    io.ReadCloser
	last  time.Time
}

func (s *Server) inputProcess() (*inputProc, error) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if s.input != nil && time.Since(s.input.last) < 10*time.Minute {
		s.input.last = time.Now()
		return s.input, nil
	}
	if s.input != nil {
		s.input.stdin.Close()
		s.input.rc.Close()
		s.input = nil
	}
	script, err := s.asset("uinput-keyboard.py")
	if err != nil {
		return nil, err
	}
	if err := s.ssh.UploadBytes("/tmp/companion_uinput.py", script, 0o755); err != nil {
		return nil, err
	}
	c, err := s.ssh.Client()
	if err != nil {
		return nil, err
	}
	sess, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if err := sess.Start("python3 -u /tmp/companion_uinput.py"); err != nil {
		sess.Close()
		return nil, err
	}
	go func() { _ = sess.Wait(); pw.Close() }()
	// aspetto READY
	br := bufio.NewReader(pr)
	ready := make(chan string, 1)
	go func() { l, _ := br.ReadString('\n'); ready <- l }()
	select {
	case l := <-ready:
		if !strings.Contains(l, "READY") {
			sess.Close()
			return nil, fmt.Errorf("tastiera virtuale non disponibile: %s", strings.TrimSpace(l))
		}
	case <-time.After(8 * time.Second):
		sess.Close()
		return nil, errors.New("tastiera virtuale: nessuna risposta dal cabinato")
	}
	go func() { _, _ = io.Copy(io.Discard, br) }()
	ip := &inputProc{stdin: stdin, rc: &streamCloser{sess.Close}, last: time.Now()}
	s.input = ip
	return ip, nil
}

type streamCloser struct{ f func() error }

func (c *streamCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (c *streamCloser) Close() error             { return c.f() }

func (s *Server) sendKey(code, value int) error {
	ip, err := s.inputProcess()
	if err != nil {
		return err
	}
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if _, err := fmt.Fprintf(ip.stdin, "%d %d\n", code, value); err != nil {
		s.input = nil
		return err
	}
	return nil
}

func (s *Server) hInput(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Code   int    `json:"code"`
		Value  int    `json:"value"`
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Action == "close" {
		return nil, nil
	}
	if req.Code <= 0 || req.Code > 767 {
		return nil, errors.New("codice tasto non valido")
	}
	return nil, s.sendKey(req.Code, req.Value)
}

func (s *Server) hInputType(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Text  string `json:"text"`
		Enter bool   `json:"enter"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Text == "" {
		return nil, errors.New("testo vuoto")
	}
	if err := s.ssh.UploadBytes("/tmp/companion_type.txt", []byte(req.Text), 0o644); err != nil {
		return nil, err
	}
	res, err := s.ssh.RunTimeout("export DISPLAY=:0; command -v xdotool >/dev/null || exit 127; xdotool type --clearmodifiers --delay 12 --file /tmp/companion_type.txt", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	if res.Exit == 127 {
		return nil, errors.New("xdotool non presente sul cabinato")
	}
	if req.Enter {
		_ = s.sendKey(28, 1)
		_ = s.sendKey(28, 0)
	}
	return nil, nil
}

// ---------- appunti PC <-> cabinato ----------

func (s *Server) hClip(w http.ResponseWriter, r *http.Request) (any, error) {
	bin := compDir + "/companion"
	if r.Method != http.MethodPost {
		res, err := s.ssh.RunTimeout("[ -x "+bin+" ] || exit 127; export DISPLAY=:0; "+bin+" -clip-get 2>/dev/null", 20*time.Second)
		if err != nil {
			return nil, err
		}
		if res.Exit == 127 {
			return nil, errors.New("serve il server del Companion sul cabinato (Impostazioni → Server sul cabinato)")
		}
		return map[string]any{"text": res.Stdout}, nil
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if err := s.ssh.UploadBytes("/tmp/companion_clip.txt", []byte(req.Text), 0o644); err != nil {
		return nil, err
	}
	res, err := s.ssh.RunTimeout("[ -x "+bin+" ] || exit 127; export DISPLAY=:0; pkill -f 'companion -clip-set' 2>/dev/null; setsid nohup "+bin+" -clip-set /tmp/companion_clip.txt >/dev/null 2>&1 </dev/null & sleep 1; echo ok", 20*time.Second)
	if err != nil {
		return nil, err
	}
	if res.Exit == 127 {
		return nil, errors.New("serve il server del Companion sul cabinato (Impostazioni → Server sul cabinato)")
	}
	return nil, nil
}

// ---------- registrazione schermo ----------

func (s *Server) recScript() error {
	b, err := s.asset("companion-record.sh")
	if err != nil {
		return err
	}
	return s.ssh.UploadBytes("/userdata/system/companion-record.sh", b, 0o755)
}

func (s *Server) hRecord(w http.ResponseWriter, r *http.Request) (any, error) {
	rs := "/userdata/system/companion-record.sh"
	status := func() (map[string]any, error) {
		res, err := s.ssh.RunTimeout("[ -x "+rs+" ] && bash "+rs+" is_running; echo; L=$(ls -t /userdata/recordings/*.mkv 2>/dev/null | head -1); [ -n \"$L\" ] && { echo \"$L\"; stat -c %s \"$L\"; }; echo; cat /tmp/companion-rec.start 2>/dev/null", 20*time.Second)
		if err != nil {
			return nil, err
		}
		l := strings.Split(res.Stdout, "\n")
		out := map[string]any{"recording": len(l) > 0 && strings.TrimSpace(l[0]) == "true"}
		if len(l) >= 3 && strings.TrimSpace(l[1]) != "" {
			out["last"] = filepath.Base(strings.TrimSpace(l[1]))
			var sz int64
			fmt.Sscanf(strings.TrimSpace(l[2]), "%d", &sz)
			out["last_size"] = sz
		}
		if len(l) >= 5 && strings.TrimSpace(l[4]) != "" {
			out["since"] = strings.TrimSpace(l[4])
		}
		return out, nil
	}
	if r.Method != http.MethodPost {
		return status()
	}
	var req struct {
		Action  string `json:"action"`
		Quality string `json:"quality"`
		File    string `json:"file"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "start":
		if err := s.recScript(); err != nil {
			return nil, err
		}
		res, err := s.ssh.RunTimeout("bash "+rs+" start "+cab.ShellQuote(req.Quality)+"; echo \"rc=$?\"; date -u +%Y-%m-%dT%H:%M:%SZ > /tmp/companion-rec.start", time.Minute)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(res.Stdout, "rc=0") {
			return nil, fmt.Errorf("registrazione non avviata: %s", strings.TrimSpace(res.Stdout))
		}
		return status()
	case "stop":
		_, err := s.ssh.RunTimeout("bash "+rs+" stop; rm -f /tmp/companion-rec.start", time.Minute)
		if err != nil {
			return nil, err
		}
		return status()
	case "list":
		res, err := s.ssh.RunTimeout(`ls -t /userdata/recordings/*.mkv 2>/dev/null | while read f; do echo "$(basename "$f")|$(stat -c %s "$f")|$(date -r "$f" '+%d/%m/%Y %H:%M')"; done`, 30*time.Second)
		if err != nil {
			return nil, err
		}
		list := []map[string]any{}
		for _, l := range cab.Lines(res.Stdout) {
			f := strings.Split(l, "|")
			if len(f) == 3 {
				var sz int64
				fmt.Sscanf(f[1], "%d", &sz)
				list = append(list, map[string]any{"name": f[0], "size": sz, "time": f[2]})
			}
		}
		return list, nil
	case "delete":
		if strings.Contains(req.File, "/") || req.File == "" {
			return nil, errors.New("nome non valido")
		}
		_, err := s.ssh.Run("rm -f /userdata/recordings/" + cab.ShellQuote(req.File))
		return nil, err
	case "download":
		if strings.Contains(req.File, "/") || req.File == "" {
			return nil, errors.New("nome non valido")
		}
		dir := filepath.Join(s.cfg.ScreenshotDir, "..", "recordings")
		if s.cfg.ServerMode {
			return nil, errors.New("disponibile dall'app sul PC")
		}
		_ = os.MkdirAll(dir, 0o755)
		local := filepath.Join(dir, req.File)
		j := s.jobs.Start("Download registrazione", func(j *jobs.Job) (any, error) {
			err := s.ssh.Download(j.Context(), "/userdata/recordings/"+req.File, local, func(d, t int64) {
				if t > 0 {
					j.SetProgress(float64(d)/float64(t), fmt.Sprintf("%s / %s", humanB(d), humanB(t)))
				}
			})
			return map[string]any{"local": local}, err
		})
		return map[string]any{"job": j.ID, "local": local}, nil
	}
	return nil, errors.New("azione sconosciuta")
}
