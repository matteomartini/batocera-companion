package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/config"
	"batocompanion/internal/jobs"
)

// Backup incrementale del cabinato sul PC (SFTP) o su un disco USB collegato al cabinato (rsync).

type bkCat struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Desc    string   `json:"desc"`
	Default bool     `json:"default"`
	Rels    []string `json:"-"`
	Exclude []string `json:"-"`
}

var bkCats = []bkCat{
	{ID: "system", Label: "Configurazione", Desc: "batocera.conf, EmulationStation, controller, impostazioni degli emulatori, script e servizi del Companion", Default: true, Rels: []string{"system"}, Exclude: []string{"system/.cache", "system/logs", "system/companion_trash", "system/flatpak", "system/.local/share/flatpak", "system/add-ons", "system/.steam", "system/.var", "system/.config/chromium"}},
	{ID: "saves", Label: "Salvataggi", Desc: "partite salvate e stati (saves)", Default: true, Rels: []string{"saves"}, Exclude: []string{"saves/flatpak"}},
	{ID: "bios", Label: "BIOS", Desc: "/userdata/bios", Default: true, Rels: []string{"bios"}},
	{ID: "roms", Label: "ROM e giochi", Desc: "tutte le ROM con immagini, video e gamelist (la parte più grande)", Default: true, Rels: []string{"roms"}},
	{ID: "themes", Label: "Temi e decorazioni", Desc: "temi EmulationStation, bezel, musiche, splash", Default: true, Rels: []string{"themes", "decorations", "music", "splash", "screenshots"}},
	{ID: "addons", Label: "Add-on", Desc: "programmi aggiuntivi (Chiaki, Tailscale…)", Default: false, Rels: []string{"system/add-ons"}},
	{ID: "media", Label: "Registrazioni", Desc: "video registrati dal Companion", Default: false, Rels: []string{"recordings"}},
	{ID: "flatpak", Label: "Flatpak (Steam…)", Desc: "app flatpak e i loro dati: molto grande, si reinstallano da Flathub", Default: false, Rels: []string{"saves/flatpak", "system/flatpak"}},
}

func bkCat_(id string) *bkCat {
	for i := range bkCats {
		if bkCats[i].ID == id {
			return &bkCats[i]
		}
	}
	return nil
}

type bkInfo struct {
	Date       time.Time        `json:"date"`
	Categories []string         `json:"categories"`
	Bytes      map[string]int64 `json:"bytes"`
	Host       string           `json:"host"`
	Version    string           `json:"version"`
}

func (s *Server) hBackup(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		out := map[string]any{"dir": s.cfg.BackupDir, "cats": bkCats}
		if s.cfg.BackupDir != "" {
			if b, err := os.ReadFile(filepath.Join(s.cfg.BackupDir, "backup-info.json")); err == nil {
				var bi bkInfo
				if json.Unmarshal(b, &bi) == nil {
					out["last"] = bi
				}
			}
		}
		return out, nil
	}
	var req struct {
		Action string   `json:"action"`
		Dir    string   `json:"dir"`
		Cats   []string `json:"cats"`
		Mirror bool     `json:"mirror"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Dir == "" {
		return nil, errors.New("scegli la cartella di destinazione sul PC")
	}
	if s.cfg.ServerMode {
		return nil, errors.New("il backup sul PC si fa dall'app sul PC")
	}
	if req.Dir != s.cfg.BackupDir {
		s.cfg.BackupDir = req.Dir
		_ = config.Save(s.cfg)
	}
	if len(req.Cats) == 0 {
		return nil, errors.New("scegli almeno una categoria")
	}
	switch req.Action {
	case "scan", "backup", "restore":
	default:
		return nil, errors.New("azione sconosciuta")
	}
	j := s.jobs.Start("Backup", func(j *jobs.Job) (any, error) {
		return s.backupJob(j, req.Action, req.Dir, req.Cats, req.Mirror)
	})
	return map[string]any{"job": j.ID}, nil
}

type bkFile struct {
	Rel   string
	Size  int64
	Mtime float64
}

// remoteScan: elenco file sul cabinato per categoria.
func (s *Server) remoteScan(j *jobs.Job, cats []string) (map[string][]bkFile, error) {
	script, err := s.asset("backup-scan.py")
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}
	for _, c := range cats {
		if bc := bkCat_(c); bc != nil {
			spec[c] = map[string]any{"rels": bc.Rels, "exclude": bc.Exclude}
		}
	}
	sb, _ := json.Marshal(spec)
	res, err := s.ssh.RunCtx(j.Context(), "python3 -c "+cab.ShellQuote(string(script))+" "+cab.ShellQuote(string(sb)), 30*time.Minute)
	if err != nil {
		return nil, err
	}
	var raw map[string][][]any
	if err := json.Unmarshal([]byte(lastLine(res.Stdout)), &raw); err != nil {
		return nil, fmt.Errorf("lo script sul cabinato non ha risposto: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string][]bkFile{}
	for c, l := range raw {
		for _, f := range l {
			if len(f) < 3 {
				continue
			}
			out[c] = append(out[c], bkFile{Rel: f[0].(string), Size: int64(f[1].(float64)), Mtime: f[2].(float64)})
		}
	}
	return out, nil
}

func humanB(b int64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	}
	return fmt.Sprintf("%d MB", b>>20)
}

func localNeeds(dir string, f bkFile) bool {
	st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Rel)))
	if err != nil {
		return true
	}
	return st.Size() != f.Size || absF(float64(st.ModTime().Unix())-f.Mtime) > 2
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func (s *Server) backupJob(j *jobs.Job, action, dir string, cats []string, mirror bool) (any, error) {
	j.SetProgress(-1, "Leggo l'elenco dei file sul cabinato")
	if action == "restore" {
		return s.restoreJob(j, dir, cats)
	}
	files, err := s.remoteScan(j, cats)
	if err != nil {
		return nil, err
	}
	sizes := map[string]any{}
	var todo []bkFile
	var todoBytes, total int64
	for _, c := range cats {
		var cb, tb int64
		tf := 0
		for _, f := range files[c] {
			cb += f.Size
			if localNeeds(dir, f) {
				todo = append(todo, f)
				tb += f.Size
				tf++
			}
		}
		total += cb
		todoBytes += tb
		sizes[c] = map[string]any{"bytes": cb, "human": humanB(cb), "files": len(files[c]), "todo_files": tf, "todo_bytes": tb, "todo_human": humanB(tb)}
		j.Logf("%s: %d file, %s (da copiare: %d file, %s)", c, len(files[c]), humanB(cb), tf, humanB(tb))
	}
	if action == "scan" {
		return sizes, nil
	}
	_ = os.MkdirAll(dir, 0o755)
	cl, err := s.ssh.SFTP()
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	var done int64
	copied, failed := 0, 0
	for i, f := range todo {
		if j.Cancelled() {
			return nil, errors.New("annullato")
		}
		local := filepath.Join(dir, filepath.FromSlash(f.Rel))
		base := done
		err := cab.DownloadWith(j.Context(), cl, "/userdata/"+f.Rel, local, func(d, _ int64) {
			if todoBytes > 0 {
				j.SetProgress(float64(base+d)/float64(todoBytes), fmt.Sprintf("%d/%d  %s  (%s / %s)", i+1, len(todo), path.Base(f.Rel), humanB(base+d), humanB(todoBytes)))
			}
		})
		done += f.Size
		if err != nil {
			failed++
			j.Logf("✗ %s: %v", f.Rel, err)
			continue
		}
		copied++
	}
	removed := 0
	if mirror {
		for _, c := range cats {
			bc := bkCat_(c)
			if bc == nil {
				continue
			}
			keep := map[string]bool{}
			for _, f := range files[c] {
				keep[filepath.ToSlash(f.Rel)] = true
			}
			for _, rel := range bc.Rels {
				root := filepath.Join(dir, filepath.FromSlash(rel))
				_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					r, _ := filepath.Rel(dir, p)
					if !keep[filepath.ToSlash(r)] {
						os.Remove(p)
						removed++
					}
					return nil
				})
			}
		}
	}
	info := bkInfo{Date: time.Now(), Categories: cats, Bytes: map[string]int64{}, Host: s.cfg.Host}
	for _, c := range cats {
		if m, ok := sizes[c].(map[string]any); ok {
			info.Bytes[c] = m["bytes"].(int64)
		}
	}
	if s.info != nil {
		info.Version = s.info.Version
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "backup-info.json"), b, 0o644)
	if copied == 0 && failed == 0 {
		j.Logf("✓ backup già aggiornato: niente da copiare")
	} else {
		j.Logf("✓ %d file copiati, %d errori, %d rimossi dal backup · totale %s", copied, failed, removed, humanB(total))
	}
	return map[string]any{"copied": copied, "failed": failed, "removed": removed}, nil
}

func (s *Server) restoreJob(j *jobs.Job, dir string, cats []string) (any, error) {
	remote, err := s.remoteScan(j, cats)
	if err != nil {
		return nil, err
	}
	rmap := map[string]bkFile{}
	for _, l := range remote {
		for _, f := range l {
			rmap[f.Rel] = f
		}
	}
	var todo []string
	var todoBytes int64
	for _, c := range cats {
		bc := bkCat_(c)
		if bc == nil {
			continue
		}
		for _, rel := range bc.Rels {
			root := filepath.Join(dir, filepath.FromSlash(rel))
			_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || strings.HasSuffix(p, ".part") {
					return nil
				}
				r, _ := filepath.Rel(dir, p)
				r = filepath.ToSlash(r)
				st, _ := d.Info()
				if rf, ok := rmap[r]; ok && rf.Size == st.Size() && absF(float64(st.ModTime().Unix())-rf.Mtime) <= 2 {
					return nil
				}
				todo = append(todo, r)
				todoBytes += st.Size()
				return nil
			})
		}
	}
	if len(todo) == 0 {
		j.Logf("✓ il cabinato è già allineato al backup")
		return map[string]any{"copied": 0}, nil
	}
	cl, err := s.ssh.SFTP()
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	var done int64
	copied := 0
	for i, rel := range todo {
		if j.Cancelled() {
			return nil, errors.New("annullato")
		}
		local := filepath.Join(dir, filepath.FromSlash(rel))
		st, _ := os.Stat(local)
		base := done
		err := cab.UploadWith(j.Context(), cl, local, "/userdata/"+rel, func(d, _ int64) {
			j.SetProgress(float64(base+d)/float64(todoBytes), fmt.Sprintf("%d/%d  %s", i+1, len(todo), path.Base(rel)))
		})
		if st != nil {
			done += st.Size()
		}
		if err != nil {
			j.Logf("✗ %s: %v", rel, err)
			continue
		}
		copied++
	}
	j.Logf("✓ %d file riportati sul cabinato", copied)
	if containsStr(cats, "system") {
		j.Logf("· la configurazione è stata ripristinata: riavvia il cabinato (Dashboard → Riavvia) perché EmulationStation la rilegga, e non toccare i menu di ES prima")
	}
	_ = s.es.ReloadGames()
	return map[string]any{"copied": copied}, nil
}

// ---------- USB (rsync sul cabinato) ----------

var reLsblkKV = regexp.MustCompile(`([A-Z]+)="([^"]*)"`)

func (s *Server) hBackupUSB(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		res, err := s.ssh.RunTimeout(`echo "@@mounted"
for m in /media/*; do [ -d "$m" ] || continue; awk -v m="$m" '$2==m{f=1}END{exit !f}' /proc/mounts || continue; df -hP "$m" | awk -v m="$m" -v fs="$(findmnt -no FSTYPE "$m" 2>/dev/null)" 'NR==2{print m"\t"$2"\t"$4"\t"fs}'; done
echo "@@blk"
# partizioni (chiave=valore, i campi vuoti restano vuoti)
lsblk -Pno NAME,FSTYPE,SIZE,LABEL,MOUNTPOINT,TYPE,TRAN 2>/dev/null
echo "@@raw"
lsblk -o NAME,FSTYPE,SIZE,LABEL,MOUNTPOINT,TYPE,TRAN,MODEL 2>/dev/null; echo; cat /proc/partitions 2>/dev/null | tail -n +3; echo; lsusb 2>/dev/null; echo; dmesg 2>/dev/null | grep -iE 'usb|scsi|sd[a-z]' | tail -15
echo "@@dmesg"
dmesg 2>/dev/null | grep -iE 'ntfs|exfat|volume is dirty|hibernat|unclean|read-only|sd[a-z]: ' | tail -8`, time.Minute)
		if err != nil {
			return nil, err
		}
		sec := cab.SplitSections(res.Stdout)
		drives := []map[string]any{}
		for _, l := range cab.Lines(sec["mounted"]) {
			f := strings.Split(l, "\t")
			if len(f) < 4 {
				continue
			}
			drives = append(drives, map[string]any{"mount": f[0], "label": path.Base(f[0]), "size": f[1], "free": f[2], "fs": f[3]})
		}
		unmounted := []map[string]any{}
		for _, l := range cab.Lines(sec["blk"]) {
			kv := map[string]string{}
			for _, m := range reLsblkKV.FindAllStringSubmatch(l, -1) {
				kv[m[1]] = m[2]
			}
			// partizioni, e dischi senza tabella partizioni (filesystem direttamente sul disco)
			if kv["TYPE"] != "part" && kv["TYPE"] != "disk" {
				continue
			}
			if kv["TYPE"] == "disk" && kv["FSTYPE"] == "" {
				continue
			}
			switch kv["FSTYPE"] {
			case "swap", "LVM2_member", "linux_raid_member", "crypto_LUKS":
				continue
			}
			mp := kv["MOUNTPOINT"]
			if strings.HasPrefix(mp, "/media/") || mp == "/" || mp == "/boot" || strings.HasPrefix(mp, "/userdata") || strings.HasPrefix(mp, "/var") {
				continue
			}
			if mp != "" && kv["TRAN"] != "usb" {
				continue
			}
			if kv["FSTYPE"] == "" && kv["TRAN"] != "usb" {
				continue
			}
			unmounted = append(unmounted, map[string]any{"dev": "/dev/" + kv["NAME"], "fs": kv["FSTYPE"], "size": kv["SIZE"], "label": strings.ReplaceAll(kv["LABEL"], `\x20`, " "), "mounted_at": mp, "bus": kv["TRAN"]})
		}
		return map[string]any{"drives": drives, "unmounted": unmounted, "dmesg": strings.TrimSpace(sec["dmesg"]), "raw": strings.TrimSpace(sec["raw"])}, nil
	}
	var req struct {
		Action string   `json:"action"`
		Mount  string   `json:"mount"`
		Cats   []string `json:"cats"`
		Mirror bool     `json:"mirror"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Action == "mount" {
		if !strings.HasPrefix(req.Mount, "/dev/") || strings.ContainsAny(req.Mount, " ;&|$`'\"") {
			return nil, errors.New("dispositivo non valido")
		}
		dev := req.Mount
		script := `dev=` + dev + `
fs=$(lsblk -rno FSTYPE "$dev" | head -1); lb=$(lsblk -rno LABEL "$dev" | head -1 | sed 's/\\x20/ /g'); [ -n "$lb" ] || lb=$(basename "$dev")
mp="/media/$lb"; mkdir -p "$mp"
case "$fs" in
  ntfs) mount -t ntfs3 -o rw,noatime,uid=0,gid=0,umask=000 "$dev" "$mp" 2>/tmp/bc_mount.err || ntfs-3g -o rw,noatime,umask=000 "$dev" "$mp" 2>>/tmp/bc_mount.err || mount -t ntfs3 -o ro,uid=0,gid=0 "$dev" "$mp" 2>>/tmp/bc_mount.err;;
  exfat) mount -t exfat -o rw,noatime,umask=000 "$dev" "$mp" 2>/tmp/bc_mount.err;;
  *) mount -o rw,noatime "$dev" "$mp" 2>/tmp/bc_mount.err;;
esac
if awk -v m="$mp" '$2==m{f=1}END{exit !f}' /proc/mounts; then echo "OK $mp $(findmnt -no OPTIONS "$mp" | cut -d, -f1)"; else echo "FAIL"; cat /tmp/bc_mount.err; dmesg 2>/dev/null | tail -5; rmdir "$mp" 2>/dev/null; fi`
		res, err := s.ssh.RunTimeout(script, time.Minute)
		if err != nil {
			return nil, err
		}
		out := strings.TrimSpace(res.Stdout)
		if !strings.HasPrefix(out, "OK ") {
			return nil, fmt.Errorf("montaggio non riuscito: %s", strings.TrimPrefix(out, "FAIL\n"))
		}
		f := strings.Fields(out)
		ro := len(f) > 2 && f[2] == "ro"
		return map[string]any{"ok": true, "mount": f[1], "readonly": ro}, nil
	}
	if !strings.HasPrefix(req.Mount, "/media/") || strings.Contains(req.Mount, "..") {
		return nil, errors.New("disco non valido")
	}
	if req.Action != "scan" && req.Action != "backup" && req.Action != "restore" {
		return nil, errors.New("azione non valida")
	}
	j := s.jobs.Start("Backup USB", func(j *jobs.Job) (any, error) {
		dest := req.Mount + "/batocera-backup"
		type step struct {
			cat, rel, src, dst, del string
			ex                      []string
		}
		var steps []step
		for _, c := range req.Cats {
			bc := bkCat_(c)
			if bc == nil {
				continue
			}
			for _, rel := range bc.Rels {
				var ex []string
				for _, e := range bc.Exclude {
					if strings.HasPrefix(e, rel+"/") {
						ex = append(ex, "--exclude="+cab.ShellQuote("/"+strings.TrimPrefix(e, rel+"/")))
					}
				}
				del := ""
				if req.Mirror {
					del = " --delete"
				}
				src, dst := "/userdata/"+rel+"/", dest+"/"+rel+"/"
				if req.Action == "restore" {
					src, dst = dest+"/"+rel+"/", "/userdata/"+rel+"/"
					del = ""
				}
				steps = append(steps, step{c, rel, src, dst, del, ex})
			}
		}
		// 1) prova a secco: dimensione totale e byte da copiare per categoria
		j.SetProgress(-1, "Calcolo dimensioni sul cabinato")
		var dry []string
		dry = append(dry, "mkdir -p "+cab.ShellQuote(dest)+" || exit 5")
		for _, st := range steps {
			dry = append(dry, fmt.Sprintf("echo '@@ %s %s'; [ -d %s ] && rsync -rlt --modify-window=2 --dry-run --stats%s %s %s %s 2>&1 | grep -E '^(Number of regular files transferred|Total transferred file size|Total file size|Number of files)'; true", st.cat, st.rel, cab.ShellQuote(st.src), st.del, strings.Join(st.ex, " "), cab.ShellQuote(st.src), cab.ShellQuote(st.dst)))
		}
		res, err := s.ssh.RunCtx(j.Context(), strings.Join(dry, "\n"), time.Hour)
		if err != nil {
			return nil, err
		}
		if res.Exit == 127 {
			return nil, errors.New("rsync non presente su questa Batocera")
		}
		sizes := map[string]map[string]any{}
		todo := map[string]int64{} // byte da copiare per passo (cat/rel)
		var totalTodo int64
		cur := ""
		num := func(l string) int64 {
			f := strings.Fields(strings.ReplaceAll(l, ",", ""))
			for i := len(f) - 1; i >= 0; i-- {
				if n, err := strconv.ParseInt(strings.TrimSuffix(f[i], ","), 10, 64); err == nil {
					return n
				}
			}
			return 0
		}
		for _, l := range cab.Lines(res.Stdout) {
			if strings.HasPrefix(l, "@@ ") {
				f := strings.Fields(l)
				cur = f[1] + "/" + f[2]
				if sizes[f[1]] == nil {
					sizes[f[1]] = map[string]any{"bytes": int64(0), "files": int64(0), "todo_bytes": int64(0), "todo_files": int64(0)}
				}
				continue
			}
			if cur == "" {
				continue
			}
			cat := strings.SplitN(cur, "/", 2)[0]
			m := sizes[cat]
			switch {
			case strings.HasPrefix(l, "Total file size"):
				m["bytes"] = m["bytes"].(int64) + num(l)
			case strings.HasPrefix(l, "Number of files"):
				// "Number of files: 1,234 (reg: 1,200, dir: 34)"
				m["files"] = m["files"].(int64) + num(strings.SplitN(strings.ReplaceAll(l, ",", ""), "(", 2)[0])
			case strings.HasPrefix(l, "Total transferred file size"):
				n := num(l)
				m["todo_bytes"] = m["todo_bytes"].(int64) + n
				todo[cur] += n
				totalTodo += n
			case strings.HasPrefix(l, "Number of regular files transferred"):
				m["todo_files"] = m["todo_files"].(int64) + num(l)
			}
		}
		for c, m := range sizes {
			m["human"] = humanB(m["bytes"].(int64))
			m["todo_human"] = humanB(m["todo_bytes"].(int64))
			j.Logf("%s: %d file, %s (da copiare: %d file, %s)", c, m["files"], m["human"], m["todo_files"], m["todo_human"])
		}
		if req.Action == "scan" {
			out := map[string]any{}
			for c, m := range sizes {
				out[c] = m
			}
			return out, nil
		}
		// 2) rsync vero, con avanzamento in streaming
		var script []string
		for _, st := range steps {
			script = append(script, fmt.Sprintf("echo '@@ %s %s'; [ -d %s ] && { mkdir -p %s; rsync -rlt --modify-window=2 --info=progress2 --no-inc-recursive%s %s %s %s 2>&1 | tr '\\r' '\\n'; echo \"rc=${PIPESTATUS[0]}\"; }; true", st.cat, st.rel, cab.ShellQuote(st.src), cab.ShellQuote(st.dst), st.del, strings.Join(st.ex, " "), cab.ShellQuote(st.src), cab.ShellQuote(st.dst)))
		}
		if req.Action == "backup" {
			info, _ := json.Marshal(bkInfo{Date: time.Now(), Categories: req.Cats, Host: s.cfg.Host})
			script = append(script, "echo "+cab.ShellQuote(string(info))+" > "+cab.ShellQuote(dest+"/backup-info.json"))
		}
		rc, err := s.ssh.StreamSession(strings.Join(script, "\n"))
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		var doneBefore int64
		cur = ""
		bad := false
		started := time.Now()
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if j.Cancelled() {
				_, _ = s.ssh.RunTimeout("pkill -x rsync; true", 10*time.Second)
				return nil, errors.New("annullato")
			}
			l := strings.TrimSpace(sc.Text())
			if l == "" {
				continue
			}
			if strings.HasPrefix(l, "@@ ") {
				f := strings.Fields(l)
				cur = f[1] + "/" + f[2]
				j.Logf("▶ %s", cur)
				continue
			}
			if strings.HasPrefix(l, "rc=") {
				doneBefore += todo[cur]
				if l != "rc=0" {
					bad = true
					j.Logf("  %s", l)
				}
				continue
			}
			// riga progress2: "  1,234,567  45%  12.34MB/s  0:01:23 (xfr#12, to-chk=0/345)"
			f := strings.Fields(strings.ReplaceAll(l, ",", ""))
			if len(f) >= 4 && strings.HasSuffix(f[1], "%") {
				n, _ := strconv.ParseInt(f[0], 10, 64)
				done := doneBefore + n
				frac := float64(-1)
				eta := ""
				if totalTodo > 0 {
					frac = float64(done) / float64(totalTodo)
					if frac > 0.999 {
						frac = 0.999
					}
					if el := time.Since(started).Seconds(); el > 3 && done > 0 {
						rem := time.Duration(el * float64(totalTodo-done) / float64(done) * float64(time.Second))
						eta = " · mancano " + humanDur(rem)
					}
				}
				j.SetProgress(frac, fmt.Sprintf("%s · %s / %s · %s%s", cur, humanB(done), humanB(totalTodo), f[2], eta))
				continue
			}
			if strings.Contains(l, "error") || strings.Contains(l, "failed") {
				j.Logf("  %s", l)
			}
		}
		if bad {
			j.Logf("(alcuni file sono stati saltati)")
		}
		if req.Action == "restore" {
			_ = s.es.ReloadGames()
		}
		j.Logf("Completato in %s", humanDur(time.Since(started)))
		return nil, nil
	})
	return map[string]any{"job": j.ID}, nil
}

func humanDur(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		return fmt.Sprintf("%d min %02d s", s/60, s%60)
	default:
		return fmt.Sprintf("%d h %02d min", s/3600, (s%3600)/60)
	}
}
