package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/dat"
	"batocompanion/internal/jobs"
	"batocompanion/internal/roms"
)

func (s *Server) asset(name string) ([]byte, error) {
	b, err := fs.ReadFile(s.assets, name)
	if err != nil {
		return nil, fmt.Errorf("risorsa %s mancante nel programma", name)
	}
	return b, nil
}

func (s *Server) analyzer() *roms.Analyzer {
	s.mu.Lock()
	in := s.info
	s.mu.Unlock()
	if in == nil {
		in, _ = s.collectInfo(false)
	}
	return &roms.Analyzer{Dats: s.dats, Info: in, SSH: s.ssh, Pref: s.cfg.ArcadePref, UseCHD: s.cfg.ConvertToCHD}
}

// ---------- analisi ----------

func (s *Server) hRomsAnalyze(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if len(req.Paths) == 0 {
		return nil, errors.New("nessun percorso")
	}
	j := s.jobs.Start("Analisi ROM", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "Leggo le informazioni del cabinato")
		a := s.analyzer()
		a.Log = func(m string) { j.Logf("%s", m) }
		a.Step = func(p float64, st string) { j.SetProgress(p, st) }
		sess, err := a.Analyze(j.Context(), req.Paths)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.sessions[sess.ID] = sess
		for id, o := range s.sessions {
			if time.Since(o.Created) > 6*time.Hour {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
		for _, it := range sess.Items {
			j.Logf("%s → %s/%s (%s, %s)", it.DisplayName, it.System, it.DestName, it.Action, it.Confidence)
		}
		return map[string]any{"session": sess.ID, "items": sess.Items, "summary": sess.Summary()}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) session(id string) *roms.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) hRomsSet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Session string  `json:"session"`
		ID      string  `json:"id"`
		System  *string `json:"system"`
		Core    *string `json:"core"`
		Skip    *bool   `json:"skip"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	sess := s.session(req.Session)
	if sess == nil {
		return nil, errors.New("sessione scaduta: rifai l'analisi")
	}
	return sess.Set(s.analyzer(), req.ID, req.System, req.Core, req.Skip)
}

func (s *Server) hRomsApply(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Session string `json:"session"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	sess := s.session(req.Session)
	if sess == nil {
		return nil, errors.New("sessione scaduta: rifai l'analisi")
	}
	j := s.jobs.Start("Copia ROM", func(j *jobs.Job) (any, error) {
		ap := &roms.Applier{SSH: s.ssh, ES: s.es, Tmp: filepath.Join(s.tmpDir, "rebuild-"+j.ID), Log: func(m string) { j.Logf("%s", m) }, Step: func(p float64, st string) { j.SetProgress(p, st) }}
		ap.PostInstall = func(ctx context.Context, kind string) error {
			if kind == "spacecadet" {
				return s.installSpaceCadet(ctx, func(f string, a ...any) { j.Logf(f, a...) })
			}
			if kind == "pinmame" {
				return s.vpxGlobalPinmame(ctx, func(f string, a ...any) { j.Logf(f, a...) })
			}
			return nil
		}
		defer os.RemoveAll(ap.Tmp)
		res, err := ap.Apply(j.Context(), sess)
		s.invalidateInfo()
		return res, err
	})
	return map[string]any{"job": j.ID}, nil
}

// ---------- upload dal telefono ----------

func (s *Server) hUpload(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.tmpDir, "upload", time.Now().Format("20060102-150405"))
	_ = os.MkdirAll(dir, 0o755)
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error()})
		return
	}
	var paths []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		if p.FileName() == "" {
			continue
		}
		dst := filepath.Join(dir, filepath.Base(p.FileName()))
		f, err := os.Create(dst)
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		_, err = io.Copy(f, p)
		f.Close()
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		paths = append(paths, dst)
	}
	writeJSON(w, 200, map[string]any{"paths": paths})
}

// ---------- finestra di scelta file (Windows) ----------

func (s *Server) hPick(w http.ResponseWriter, r *http.Request) (any, error) {
	kind := r.URL.Query().Get("kind")
	if runtime.GOOS != "windows" {
		return nil, errors.New("la finestra di scelta file è disponibile solo su Windows: incolla il percorso")
	}
	var ps string
	filter := map[string]string{"zip": "Archivi (*.zip)|*.zip|Tutti i file (*.*)|*.*", "video": "Video (*.mp4;*.mkv;*.webm)|*.mp4;*.mkv;*.webm|Tutti i file (*.*)|*.*", "image": "Immagini (*.png;*.jpg)|*.png;*.jpg;*.jpeg|Tutti i file (*.*)|*.*", "dat": "DAT (*.dat;*.xml;*.zip)|*.dat;*.xml;*.zip|Tutti i file (*.*)|*.*"}[kind]
	if filter == "" {
		filter = "Tutti i file (*.*)|*.*"
	}
	multi := "$false"
	if kind == "files" || kind == "bios" {
		multi = "$true"
	}
	if kind == "folder" {
		// finestra proprietaria invisibile e "sempre in primo piano": il dialogo
		// eredita la posizione sopra la finestra del Companion invece di finire dietro
		ps = `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $o = New-Object System.Windows.Forms.Form; $o.TopMost = $true; $o.ShowInTaskbar = $false; $o.StartPosition = 'Manual'; $o.Size = New-Object System.Drawing.Size(1,1); $o.Location = New-Object System.Drawing.Point(-2000,-2000); $o.Opacity = 0; $o.Show(); $o.Activate(); $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = 'Scegli la cartella'; $d.ShowNewFolderButton = $false; if ($d.ShowDialog($o) -eq 'OK') { Write-Output $d.SelectedPath }; $o.Close()`
	} else {
		ps = `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $o = New-Object System.Windows.Forms.Form; $o.TopMost = $true; $o.ShowInTaskbar = $false; $o.StartPosition = 'Manual'; $o.Size = New-Object System.Drawing.Size(1,1); $o.Location = New-Object System.Drawing.Point(-2000,-2000); $o.Opacity = 0; $o.Show(); $o.Activate(); $d = New-Object System.Windows.Forms.OpenFileDialog; $d.Multiselect = ` + multi + `; $d.Filter = '` + filter + `'; $d.Title = 'Batocera Companion'; if ($d.ShowDialog($o) -eq 'OK') { $d.FileNames | ForEach-Object { Write-Output $_ } }; $o.Close()`
	}
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-NonInteractive", "-Command", ps)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("finestra di scelta: %v", err)
	}
	var paths []string
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return map[string]any{"paths": paths}, nil
}

// ---------- DAT ----------

func (s *Server) hDats(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.dats.List(false), nil
}

func (s *Server) hDatsDownload(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	j := s.jobs.Start("Download DAT", func(j *jobs.Job) (any, error) {
		var errs []string
		for i, id := range req.IDs {
			e := dat.Lookup(id)
			if e == nil {
				continue
			}
			if e.Generate {
				j.SetProgress(float64(i)/float64(len(req.IDs)), "Genero "+e.Name)
				if err := s.generateMameDat(j); err != nil {
					errs = append(errs, e.Name+": "+err.Error())
					j.Logf("✗ %s: %v", e.Name, err)
				} else {
					j.Logf("✓ %s", e.Name)
				}
				continue
			}
			j.SetProgress(float64(i)/float64(len(req.IDs)), "Scarico "+e.Name)
			if err := s.dats.Download(j.Context(), id, func(m string) { j.Logf("✓ %s", m) }); err != nil {
				errs = append(errs, e.Name+": "+err.Error())
				j.Logf("✗ %s: %v", e.Name, err)
			}
		}
		if len(errs) == len(req.IDs) && len(errs) > 0 {
			return nil, errors.New(strings.Join(errs, "; "))
		}
		return map[string]any{"errors": errs}, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) generateMameDat(j *jobs.Job) error {
	j.SetStep("mame -listxml sul cabinato (2-4 minuti)")
	res, err := s.ssh.RunTimeout(`M=$(ls /usr/bin/mame/mame /usr/bin/mame 2>/dev/null | head -1); [ -n "$M" ] || { echo "MAME non trovato"; exit 2; }; "$M" -listxml 2>/dev/null | gzip -1 > /tmp/mame-listxml.xml.gz && ls -la /tmp/mame-listxml.xml.gz`, 20*time.Minute)
	if err != nil {
		return err
	}
	if res.Exit != 0 {
		return fmt.Errorf("%s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	j.SetStep("Scarico il listxml sul PC")
	tmp := filepath.Join(s.tmpDir, "mame-listxml.xml.gz")
	if err := s.ssh.Download(j.Context(), "/tmp/mame-listxml.xml.gz", tmp, func(d, t int64) {
		if t > 0 {
			j.SetProgress(float64(d)/float64(t), "Scarico il listxml")
		}
	}); err != nil {
		return err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	j.SetStep("Leggo il listxml (può richiedere un minuto)")
	db, err := s.dats.SaveGenerated("mame", b)
	if err != nil {
		return err
	}
	j.Logf("MAME %s: %d set", db.Version, db.Count())
	return nil
}

func (s *Server) hDatsGenerate(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Start("DAT MAME", func(j *jobs.Job) (any, error) {
		j.SetProgress(-1, "")
		return nil, s.generateMameDat(j)
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) hDatsImport(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if dat.Lookup(req.ID) == nil {
		return nil, errors.New("database sconosciuto")
	}
	db, err := s.dats.Import(req.ID, req.Path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"machines": db.Count(), "version": db.Version}, nil
}

// ---------- BIOS ----------

// biosExpected: nome file (minuscolo) → percorsi relativi a /userdata richiesti da
// batocera-systems (tabella nel suo sorgente python). Vuoto se non leggibile.
func (s *Server) biosExpected() map[string][]string {
	out := map[string][]string{}
	res, err := s.ssh.RunTimeout(`grep -oE '"file" *: *"[^"]+"' /usr/bin/batocera-systems 2>/dev/null | sed 's/.*: *"//; s/"$//'`, 30*time.Second)
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(res.Stdout, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		b := strings.ToLower(path.Base(l))
		out[b] = append(out[b], l)
	}
	return out
}

func (s *Server) hBiosUpload(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Paths     []string `json:"paths"`
		Overwrite bool     `json:"overwrite"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	j := s.jobs.Start("Copia BIOS", func(j *jobs.Job) (any, error) {
		type file struct{ local, rel string }
		var files []file
		for _, p := range req.Paths {
			st, err := os.Stat(p)
			if err != nil {
				j.Logf("non trovato: %s", p)
				continue
			}
			if st.IsDir() {
				_ = filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						rel, _ := filepath.Rel(p, q)
						files = append(files, file{q, filepath.ToSlash(rel)})
					}
					return nil
				})
			} else {
				files = append(files, file{p, filepath.Base(p)})
			}
		}
		if len(files) == 0 {
			return nil, errors.New("nessun file")
		}
		// Percorsi attesi da batocera-systems (relativi a /userdata): non tutto sta in
		// bios/, es. saves/dolphin-emu/Triforce/segaboot.gcm. Un file con quel nome va lì.
		expected := s.biosExpected()
		type target struct{ local, dest string }
		var targets []target
		for _, f := range files {
			base := strings.ToLower(path.Base(f.rel))
			dests := expected[base]
			if len(dests) == 0 {
				targets = append(targets, target{f.local, s.cfg.BiosPath + "/" + f.rel})
				continue
			}
			for _, d := range dests {
				targets = append(targets, target{f.local, "/userdata/" + d})
				if !strings.HasPrefix(d, "bios/") {
					j.Logf("%s → /userdata/%s (percorso richiesto da batocera-systems)", f.rel, d)
				}
			}
		}
		files = nil
		for _, t := range targets {
			files = append(files, file{t.local, t.dest})
		}
		// dimensioni già presenti
		var cmds []string
		for _, f := range files {
			cmds = append(cmds, "stat -c '%s' "+cab.ShellQuote(f.rel)+" 2>/dev/null || echo -")
		}
		res, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 2*time.Minute)
		if err != nil {
			return nil, err
		}
		sizes := strings.Split(strings.TrimSpace(res.Stdout), "\n")
		cl, err := s.ssh.SFTP()
		if err != nil {
			return nil, err
		}
		defer cl.Close()
		copied, skipped := 0, 0
		for i, f := range files {
			if j.Cancelled() {
				return nil, errors.New("annullato")
			}
			st, _ := os.Stat(f.local)
			if !req.Overwrite && i < len(sizes) && sizes[i] != "-" && st != nil && sizes[i] == fmt.Sprint(st.Size()) {
				skipped++
				continue
			}
			j.SetProgress(float64(i)/float64(len(files)), f.rel)
			_ = cl.MkdirAll(path.Dir(f.rel))
			if err := cab.UploadWith(j.Context(), cl, f.local, f.rel, nil); err != nil {
				j.Logf("✗ %s: %v", f.rel, err)
				continue
			}
			copied++
			j.Logf("✓ %s", f.rel)
		}
		s.invalidateInfo()
		return map[string]any{"copied": copied, "skipped": skipped}, nil
	})
	return map[string]any{"job": j.ID}, nil
}
