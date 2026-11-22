package roms

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"batocompanion/internal/cab"
)

type Applier struct {
	SSH  *cab.SSH
	ES   *cab.ES
	Log  func(string)
	Step func(float64, string)
	Tmp  string
	// PostInstall viene chiamato una volta per ogni Kind speciale copiato (es. "spacecadet":
	// installa il gioco in Ports dopo i file dati).
	PostInstall func(ctx context.Context, kind string) error
}

type ApplyResult struct {
	Copied int      `json:"copied"`
	Failed int      `json:"failed"`
	Errors []string `json:"errors"`
}

func (ap *Applier) logf(f string, a ...any) {
	if ap.Log != nil {
		ap.Log(fmt.Sprintf(f, a...))
	}
}

// Apply copia sul cabinato gli elementi selezionati della sessione. Usa un client SFTP proprio:
// non interferisce con le altre operazioni (analisi, comandi) che girano nel frattempo.
func (ap *Applier) Apply(ctx context.Context, s *Session) (*ApplyResult, error) {
	var todo []*Item
	var total int64
	for _, it := range s.Items {
		if it.Skip || it.Action == "skip" || it.System == "" {
			continue
		}
		todo = append(todo, it)
		total += it.Size
	}
	if len(todo) == 0 {
		return nil, fmt.Errorf("niente da copiare")
	}
	cl, err := ap.SSH.SFTP()
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	res := &ApplyResult{}
	var done int64
	perGame := map[string]string{} // chiave batocera.conf → valore
	var chdJobs []string
	kinds := map[string]bool{}
	for i, it := range todo {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		name := it.DisplayName
		ap.Step(float64(done)/float64(total+1), fmt.Sprintf("%d/%d  %s", i+1, len(todo), name))
		destDir := path.Join("/userdata/roms", it.System)
		prog := func(d, _ int64) {
			if total > 0 {
				ap.Step(float64(done+d)/float64(total), fmt.Sprintf("%d/%d  %s  %s", i+1, len(todo), name, human(done+d)+" / "+human(total)))
			}
		}
		var err error
		switch it.Action {
		case "copy":
			err = ap.doCopy(ctx, cl, it, destDir, prog)
		case "rebuild":
			err = ap.doRebuild(ctx, cl, it, destDir, prog)
		case "extract":
			err = ap.doExtract(ctx, cl, it, destDir, prog)
		case "chd":
			var tmpCue string
			tmpCue, err = ap.doChdUpload(ctx, cl, it, destDir, prog)
			if err == nil {
				chdJobs = append(chdJobs, fmt.Sprintf("%s|%s|%s", tmpCue, path.Join(destDir, it.DestName), it.System))
			}
		default:
			err = fmt.Errorf("azione %q sconosciuta", it.Action)
		}
		done += it.Size
		if err != nil {
			res.Failed++
			res.Errors = append(res.Errors, name+": "+err.Error())
			ap.logf("✗ %s: %v", name, err)
			continue
		}
		res.Copied++
		kinds[it.Kind] = true
		ap.logf("✓ %s → %s/%s", name, it.System, it.DestName)
		if it.PerGameCore && it.Core != "" && it.Kind == "arcade" {
			perGame[fmt.Sprintf(`%s["%s"].core`, it.System, it.DestName)] = it.Core
		}
	}
	// conversioni CHD sul cabinato
	for i, j := range chdJobs {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		f := strings.Split(j, "|")
		ap.Step(-1, fmt.Sprintf("Conversione in CHD %d/%d: %s", i+1, len(chdJobs), path.Base(f[1])))
		if err := ap.convertCHD(ctx, f[0], f[1], f[2]); err != nil {
			res.Errors = append(res.Errors, path.Base(f[1])+": CHD: "+err.Error())
			res.Failed++
			res.Copied--
			ap.logf("✗ CHD %s: %v", path.Base(f[1]), err)
		} else {
			ap.logf("✓ CHD %s", path.Base(f[1]))
		}
	}
	// installazioni collegate (Space Cadet Pinball)
	if ap.PostInstall != nil && kinds["pinmame"] {
		if err := ap.PostInstall(ctx, "pinmame"); err != nil {
			ap.logf("⚠ VPinballX.ini: %v", err)
		}
	}
	if ap.PostInstall != nil && kinds["spacecadet"] {
		ap.Step(-1, "Installo Space Cadet Pinball in Ports")
		if err := ap.PostInstall(ctx, "spacecadet"); err != nil {
			res.Errors = append(res.Errors, "Space Cadet Pinball: "+err.Error())
			ap.logf("✗ installazione Space Cadet Pinball: %v", err)
		}
	}
	// core per gioco
	if len(perGame) > 0 {
		var cmds []string
		cmds = append(cmds, "mkdir -p /userdata/system/companion_backup; cp /userdata/system/batocera.conf /userdata/system/companion_backup/batocera.conf."+time.Now().Format("20060102-150405"))
		for k, v := range perGame {
			esc := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `.`, `\.`, `"`, `\"`, `/`, `\/`).Replace(k)
			cmds = append(cmds, fmt.Sprintf("sed -i '/^%s=/d' /userdata/system/batocera.conf; echo %s >> /userdata/system/batocera.conf", esc, cab.ShellQuote(k+"="+v)))
			ap.logf("core per gioco: %s=%s", k, v)
		}
		if _, err := ap.SSH.RunTimeout(strings.Join(cmds, "\n"), time.Minute); err != nil {
			res.Errors = append(res.Errors, "batocera.conf: "+err.Error())
		}
	}
	ap.Step(1, "Rigenero la lista giochi")
	if ap.ES != nil {
		if err := ap.ES.ReloadGames(); err != nil {
			ap.logf("lista giochi: %v (EmulationStation spento? la rilegge all'avvio)", err)
		} else {
			ap.logf("lista giochi rigenerata")
		}
	}
	if res.Failed > 0 && res.Copied == 0 {
		return res, fmt.Errorf("nessun file copiato: %s", strings.Join(res.Errors, "; "))
	}
	return res, nil
}

func human(b int64) string {
	f := float64(b)
	u := []string{"B", "KB", "MB", "GB"}
	i := 0
	for f >= 1024 && i < 3 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, u[i])
}

func (ap *Applier) doCopy(ctx context.Context, cl *sftp.Client, it *Item, destDir string, prog func(int64, int64)) error {
	_ = cl.MkdirAll(destDir)
	_ = cl.MkdirAll(path.Join(destDir, path.Dir(it.DestName)))
	files := append([]string{it.Sources[0]}, it.Extra...)
	var done int64
	for i, f := range files {
		dest := path.Join(destDir, it.DestName)
		if i > 0 {
			dest = path.Join(destDir, path.Dir(it.DestName), filepath.Base(f))
		}
		if it.Kind == "doom" {
			dest = path.Join(destDir, filepath.Base(f))
		}
		st, _ := os.Stat(f)
		base := done
		if err := cab.UploadWith(ctx, cl, f, dest, func(d, _ int64) { prog(base+d, 0) }); err != nil {
			return err
		}
		if st != nil {
			done += st.Size()
		}
	}
	return nil
}

func (ap *Applier) doRebuild(ctx context.Context, cl *sftp.Client, it *Item, destDir string, prog func(int64, int64)) error {
	_ = os.MkdirAll(ap.Tmp, 0o755)
	tmp := filepath.Join(ap.Tmp, it.DestName)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, e := range it.Files {
		if ctx.Err() != nil {
			zw.Close()
			f.Close()
			os.Remove(tmp)
			return ctx.Err()
		}
		rc, err := OpenEntry(e)
		if err != nil {
			zw.Close()
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: time.Now()})
		if err == nil {
			_, err = io.Copy(w, rc)
		}
		rc.Close()
		if err != nil {
			zw.Close()
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("%s: %w", e.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	defer os.Remove(tmp)
	_ = cl.MkdirAll(destDir)
	return cab.UploadWith(ctx, cl, tmp, path.Join(destDir, it.DestName), prog)
}

func (ap *Applier) doExtract(ctx context.Context, cl *sftp.Client, it *Item, destDir string, prog func(int64, int64)) error {
	target := destDir
	if strings.Contains(it.DestName, "/") {
		target = path.Join(destDir, path.Dir(it.DestName))
	}
	_ = cl.MkdirAll(target)
	var done int64
	for _, e := range it.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = os.MkdirAll(ap.Tmp, 0o755)
		tmp := filepath.Join(ap.Tmp, filepath.Base(e.Name))
		rc, err := OpenEntry(e)
		if err != nil {
			return err
		}
		w, err := os.Create(tmp)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			os.Remove(tmp)
			return err
		}
		dest := path.Join(target, e.Name)
		if len(it.Files) == 1 && !strings.Contains(it.DestName, "/") {
			dest = path.Join(destDir, it.DestName)
		}
		base := done
		err = cab.UploadWith(ctx, cl, tmp, dest, func(d, _ int64) { prog(base+d, 0) })
		os.Remove(tmp)
		if err != nil {
			return err
		}
		done += e.Size
	}
	return nil
}

// doChdUpload carica cue/bin (o iso) in una cartella temporanea del cabinato; la conversione avviene dopo.
func (ap *Applier) doChdUpload(ctx context.Context, cl *sftp.Client, it *Item, destDir string, prog func(int64, int64)) (string, error) {
	tmpDir := path.Join(destDir, ".companion_tmp", strings.TrimSuffix(it.DestName, ".chd"))
	_ = cl.MkdirAll(tmpDir)
	files := append([]string{it.Sources[0]}, it.Extra...)
	var done int64
	for _, f := range files {
		st, _ := os.Stat(f)
		base := done
		if err := cab.UploadWith(ctx, cl, f, path.Join(tmpDir, filepath.Base(f)), func(d, _ int64) { prog(base+d, 0) }); err != nil {
			return "", err
		}
		if st != nil {
			done += st.Size()
		}
	}
	return path.Join(tmpDir, filepath.Base(it.Sources[0])), nil
}

func (ap *Applier) convertCHD(ctx context.Context, src, dest, system string) error {
	cmd := "createcd"
	if system == "ps2" || system == "gamecube" || system == "wii" {
		cmd = "createdvd"
	}
	sh := fmt.Sprintf(`set -e; CH=$(ls /usr/bin/mame/chdman /usr/bin/chdman 2>/dev/null | head -1); [ -n "$CH" ] || { echo "chdman non trovato sul cabinato"; exit 2; }
rm -f %[2]s; "$CH" %[3]s -i %[1]s -o %[2]s >/tmp/chd.log 2>&1 || { tail -5 /tmp/chd.log; exit 3; }
rm -rf %[4]s`, cab.ShellQuote(src), cab.ShellQuote(dest), cmd, cab.ShellQuote(path.Dir(src)))
	res, err := ap.SSH.RunCtx(ctx, sh, 60*time.Minute)
	if err != nil {
		return err
	}
	if res.Exit != 0 {
		return fmt.Errorf("%s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return nil
}
