package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// Aspetto: schermata di avvio e temi di EmulationStation.

const lookScript = `
echo "@@splash"; ls /userdata/splash 2>/dev/null
echo "@@conf"; for k in splash.length splash.sound splash.subtitle; do echo "$k=$(batocera-settings-get $k 2>/dev/null)"; done
echo "@@bootlogo"; ls /boot/boot-custom.png 2>/dev/null
echo "@@eslogo"; ls /userdata/system/configs/emulationstation/splash.png /userdata/system/configs/emulationstation/logo.png 2>/dev/null
echo "@@ffprobe"; which ffprobe
echo "@@theme"; grep -o 'name="ThemeSet" value="[^"]*"' ` + esSettings + ` 2>/dev/null | sed 's/.*value="//;s/"//'
echo "@@installed"; ls /userdata/themes 2>/dev/null; ls /usr/share/emulationstation/themes 2>/dev/null
`

func (s *Server) hLook(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := s.ssh.RunTimeout(lookScript, time.Minute)
	if err != nil {
		return nil, err
	}
	sec := cab.SplitSections(res.Stdout)
	out := map[string]any{}
	out["splash_files"] = cab.Lines(sec["splash"])
	for _, l := range cab.Lines(sec["conf"]) {
		if k, v, ok := strings.Cut(l, "="); ok {
			out[strings.ReplaceAll(k, ".", "_")] = strings.TrimSpace(v)
		}
	}
	if v, _ := out["splash_length"].(string); v == "" || v == "-1" {
		out["splash_length"] = "auto"
	}
	out["bootlogo_custom"] = strings.TrimSpace(sec["bootlogo"]) != ""
	out["eslogo_custom"] = strings.TrimSpace(sec["eslogo"]) != ""
	out["ffprobe"] = strings.TrimSpace(sec["ffprobe"]) != ""
	out["current_theme"] = strings.TrimSpace(sec["theme"])
	inst := uniqStr(cab.Lines(sec["installed"]))
	out["installed"] = inst
	if r.URL.Query().Get("themes") == "1" {
		av, err := s.themeCatalog(inst)
		if err != nil {
			out["available_error"] = err.Error()
		}
		out["available"] = av
	}
	return out, nil
}

func uniqStr(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range l {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) themeCatalog(installed []string) ([]map[string]any, error) {
	res, err := s.ssh.RunTimeout("batocera-es-theme list 2>/dev/null", 3*time.Minute)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, l := range cab.Lines(res.Stdout) {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		name := f[0]
		isInst := strings.Contains(l, "[installed]") || strings.Contains(l, "[I]")
		folder := name
		for _, i := range installed {
			if i == name || i == name+"-master" || i == name+"-main" || strings.EqualFold(i, name) {
				isInst = true
				folder = i
			}
		}
		url := ""
		for _, x := range f[1:] {
			if strings.HasPrefix(x, "http") {
				url = x
			}
		}
		if url == "" {
			url = "https://github.com/batocera-linux/" + name
		}
		out = append(out, map[string]any{"name": name, "folder": folder, "installed": isInst, "url": url, "screenshot": "https://raw.githubusercontent.com/batocera-linux/batocera-themes/master/screenshots/" + name + ".jpg", "author": strings.TrimPrefix(strings.TrimSuffix(url, "/"+name), "https://github.com/")})
	}
	if len(out) == 0 {
		return nil, errors.New("catalogo vuoto: il cabinato ha internet?")
	}
	return out, nil
}

func (s *Server) hLookSplash(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Video, BootLogo, ESLogo  string
		Length                   int
		Sound, Subtitle, Replace bool
	}
	var raw struct {
		Video    string `json:"video"`
		BootLogo string `json:"boot_logo"`
		ESLogo   string `json:"es_logo"`
		Length   int    `json:"length"`
		Sound    bool   `json:"sound"`
		Subtitle bool   `json:"subtitle"`
		Replace  bool   `json:"replace"`
	}
	if err := readJSON(r, &raw); err != nil {
		return nil, err
	}
	req.Video, req.BootLogo, req.ESLogo, req.Length, req.Sound, req.Subtitle, req.Replace = raw.Video, raw.BootLogo, raw.ESLogo, raw.Length, raw.Sound, raw.Subtitle, raw.Replace
	j := s.jobs.Start("Schermata di avvio", func(j *jobs.Job) (any, error) {
		ts := time.Now().Format("20060102-150405")
		cmds := []string{"mkdir -p " + backupDir + "/splash-" + ts + " /userdata/splash"}
		if req.Video != "" {
			if _, err := os.Stat(req.Video); err != nil {
				return nil, fmt.Errorf("video non trovato: %s", req.Video)
			}
			if req.Replace {
				cmds = append(cmds, "mv /userdata/splash/*.mp4 /userdata/splash/*.png /userdata/splash/*.jpg "+backupDir+"/splash-"+ts+"/ 2>/dev/null; true")
			}
			j.SetProgress(-1, "Carico il video")
			dest := "/userdata/splash/" + filepath.Base(req.Video)
			if err := s.ssh.Upload(j.Context(), req.Video, dest, func(d, t int64) {
				if t > 0 {
					j.SetProgress(float64(d)/float64(t), "Carico il video")
				}
			}); err != nil {
				return nil, err
			}
			j.Logf("video: %s", dest)
		}
		if req.BootLogo != "" {
			j.SetProgress(-1, "Carico il boot-logo")
			if err := s.ssh.Upload(j.Context(), req.BootLogo, "/tmp/boot-custom.png", nil); err != nil {
				return nil, err
			}
			cmds = append(cmds, "mount -o remount,rw /boot 2>/dev/null; cp /boot/boot-custom.png "+backupDir+"/splash-"+ts+"/ 2>/dev/null; cp /tmp/boot-custom.png /boot/boot-custom.png && echo 'boot-logo ok'; mount -o remount,ro /boot 2>/dev/null; true")
		}
		if req.ESLogo != "" {
			j.SetProgress(-1, "Carico il logo di EmulationStation")
			if err := s.ssh.Upload(j.Context(), req.ESLogo, "/userdata/system/configs/emulationstation/splash.png", nil); err != nil {
				return nil, err
			}
			j.Logf("logo ES caricato")
		}
		length := "-1"
		if req.Length > 0 {
			length = fmt.Sprint(req.Length)
		}
		cmds = append(cmds, "batocera-settings-set splash.length "+length)
		cmds = append(cmds, "batocera-settings-set splash.sound "+b01(req.Sound))
		cmds = append(cmds, "batocera-settings-set splash.subtitle "+b01(req.Subtitle))
		res, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 2*time.Minute)
		if err != nil {
			return nil, err
		}
		for _, l := range cab.Lines(res.Stdout + res.Stderr) {
			j.Logf("%s", l)
		}
		j.Logf("fatto: la nuova schermata si vede al prossimo avvio")
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *Server) hLookSplashReset(w http.ResponseWriter, r *http.Request) (any, error) {
	j := s.jobs.Start("Ripristino schermata di avvio", func(j *jobs.Job) (any, error) {
		ts := time.Now().Format("20060102-150405")
		cmd := "mkdir -p " + backupDir + "/splash-" + ts + "; mv /userdata/splash/* " + backupDir + "/splash-" + ts + "/ 2>/dev/null; mount -o remount,rw /boot 2>/dev/null; mv /boot/boot-custom.png " + backupDir + "/splash-" + ts + "/ 2>/dev/null; mount -o remount,ro /boot 2>/dev/null; mv /userdata/system/configs/emulationstation/splash.png " + backupDir + "/splash-" + ts + "/ 2>/dev/null; for k in splash.length splash.sound splash.subtitle; do sed -i \"/^$k=/d\" /userdata/system/batocera.conf; done; echo ok"
		res, err := s.ssh.RunTimeout(cmd, time.Minute)
		if err != nil {
			return nil, err
		}
		j.Logf("%s", strings.TrimSpace(res.Stdout))
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func (s *Server) hLookTheme(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		URL    string `json:"url"`
		Path   string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "set":
		if req.Name == "" {
			return nil, errors.New("tema mancante")
		}
		n := req.Name
		_, err := s.ssh.RunTimeout("mkdir -p "+backupDir+"; cp "+esSettings+" "+backupDir+"/es_settings.cfg."+time.Now().Format("20060102-150405")+"; "+esSettingsSetCmd("ThemeSet", &n, false)+"; batocera-es-swissknife --restart >/dev/null 2>&1 &", time.Minute)
		return nil, err
	case "remove":
		if req.Name == "" || strings.Contains(req.Name, "/") || strings.Contains(req.Name, "..") {
			return nil, errors.New("nome non valido")
		}
		_, err := s.ssh.RunTimeout("rm -rf /userdata/themes/"+cab.ShellQuote(req.Name)+"; batocera-es-theme remove "+cab.ShellQuote(req.Name)+" >/dev/null 2>&1; true", 2*time.Minute)
		return nil, err
	case "install":
		j := s.jobs.Start("Installazione tema", func(j *jobs.Job) (any, error) {
			j.SetProgress(-1, "Scarico il tema sul cabinato (può richiedere qualche minuto)")
			var cmd string
			if req.URL != "" {
				u := strings.TrimSuffix(strings.TrimSpace(req.URL), "/")
				u = strings.TrimSuffix(u, ".git")
				name := path.Base(u)
				cmd = fmt.Sprintf("cd /userdata/themes && rm -rf %[1]s && (git clone --depth 1 %[2]s %[1]s 2>&1 || (curl -Ls %[2]s/archive/refs/heads/master.zip -o /tmp/theme.zip || curl -Ls %[2]s/archive/refs/heads/main.zip -o /tmp/theme.zip) && unzip -q -o /tmp/theme.zip -d /userdata/themes && rm -f /tmp/theme.zip) && ls /userdata/themes", cab.ShellQuote(name), cab.ShellQuote(u))
			} else if req.Name != "" {
				cmd = "batocera-es-theme install " + cab.ShellQuote(req.Name) + " 2>&1"
			} else {
				return nil, errors.New("nome o indirizzo mancante")
			}
			res, err := s.ssh.RunCtx(j.Context(), cmd, 20*time.Minute)
			if err != nil {
				return nil, err
			}
			for _, l := range cab.Lines(res.Stdout) {
				j.Logf("%s", l)
			}
			if res.Exit != 0 {
				return nil, fmt.Errorf("installazione fallita (codice %d)", res.Exit)
			}
			return nil, nil
		})
		return map[string]any{"job": j.ID}, nil
	case "upload":
		if req.Path == "" {
			return nil, errors.New("file mancante")
		}
		j := s.jobs.Start("Caricamento tema", func(j *jobs.Job) (any, error) {
			if err := s.ssh.Upload(j.Context(), req.Path, "/tmp/theme-upload.zip", func(d, t int64) {
				if t > 0 {
					j.SetProgress(float64(d)/float64(t), "Carico lo zip")
				}
			}); err != nil {
				return nil, err
			}
			j.SetProgress(-1, "Estraggo in /userdata/themes")
			res, err := s.ssh.RunTimeout("mkdir -p /userdata/themes && cd /userdata/themes && unzip -q -o /tmp/theme-upload.zip && rm -f /tmp/theme-upload.zip && ls /userdata/themes", 10*time.Minute)
			if err != nil {
				return nil, err
			}
			for _, l := range cab.Lines(res.Stdout) {
				j.Logf("%s", l)
			}
			return nil, nil
		})
		return map[string]any{"job": j.ID}, nil
	}
	return nil, errors.New("azione sconosciuta")
}
