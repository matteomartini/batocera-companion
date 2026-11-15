package server

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"batocompanion/internal/cab"
)

// Report PDF "stato del cabinato", generato sul PC.
func (s *Server) hReport(w http.ResponseWriter, r *http.Request) {
	in, err := s.collectInfo(r.URL.Query().Get("refresh") == "1")
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	rep, _ := s.runCheckup(false)
	systems, _ := s.es.Systems()
	ups, _ := s.updatesStatus()
	upm, _ := ups.(map[string]any)
	sec := map[string]string{}
	if extra, err := s.ssh.RunTimeout(`echo "@@flatpak"; which flatpak >/dev/null 2>&1 && flatpak list --app --columns=name,version 2>/dev/null
echo "@@backups"; ls -1t /media/*/batocera-backup/backup-info.json 2>/dev/null | head -3
echo "@@guns"; for e in /sys/class/input/event*; do udevadm info -q property -p $e 2>/dev/null | grep -q 'ID_INPUT_GUN=1' && cat $e/device/name; done
echo "@@saves"; find /userdata/saves -type f 2>/dev/null | wc -l
echo "@@media"; du -sh /userdata/roms 2>/dev/null | cut -f1`, 2*time.Minute); err == nil {
		sec = cab.SplitSections(extra.Stdout)
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 6, tr(fmt.Sprintf("Batocera Companion %s · report del %s · pagina %d", s.Version, time.Now().Format("02/01/2006 15:04"), pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	h1 := func(t string) {
		pdf.SetFont("Helvetica", "B", 18)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(0, 10, tr(t), "", 1, "L", false, 0, "")
	}
	h2 := func(t string) {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(230, 120, 20)
		pdf.CellFormat(0, 7, tr(t), "", 1, "L", false, 0, "")
		pdf.SetDrawColor(230, 120, 20)
		pdf.Line(15, pdf.GetY(), 195, pdf.GetY())
		pdf.Ln(2)
		pdf.SetTextColor(20, 20, 20)
	}
	kv := func(k, v string) {
		pdf.SetFont("Helvetica", "B", 9.5)
		pdf.CellFormat(48, 5.5, tr(k), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.MultiCell(0, 5.5, tr(v), "", "L", false)
	}
	para := func(t string) {
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.MultiCell(0, 5, tr(t), "", "L", false)
	}
	gb := func(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/1e9) }

	h1("Stato del cabinato")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	host := in.Hostname
	if host == "" {
		host = s.cfg.Host
	}
	pdf.CellFormat(0, 6, tr(fmt.Sprintf("%s · Batocera %s (%s) · rilevato il %s", host, in.Version, in.Arch, in.CollectedAt.Format("02/01/2006 15:04"))), "", 1, "L", false, 0, "")

	h2("Hardware")
	kv("Processore", fmt.Sprintf("%s (%d core)", in.CPUModel, in.CPUCount))
	kv("Memoria", fmt.Sprintf("%s usati su %s", gb(in.MemUsed), gb(in.MemTotal)))
	kv("Disco /userdata", fmt.Sprintf("%s usati su %s (liberi %s)", gb(in.DiskUsed), gb(in.DiskTotal), gb(in.DiskFree)))
	if in.CPUTempC > 0 {
		kv("Temperatura CPU", fmt.Sprintf("%.0f °C", in.CPUTempC))
	}
	if in.ResolutionCur != "" {
		kv("Schermo", fmt.Sprintf("%s a %.2f Hz", in.ResolutionCur, in.RefreshHz))
	}
	if in.GPU != "" {
		kv("Grafica", in.GPU)
	}
	if in.UptimeSec > 0 {
		kv("Acceso da", fmt.Sprintf("%.1f ore", in.UptimeSec/3600))
	}
	kv("Indirizzi IP", strings.Join(in.IPs, ", "))
	if gm := strings.TrimSpace(sec["media"]); gm != "" {
		kv("Spazio ROM", gm)
	}

	h2("Versioni e aggiornamenti")
	kv("Batocera", in.Version+" · kernel "+in.Kernel)
	if upm != nil {
		if st, _ := upm["batocera_stable"].(string); st != "" {
			kv("Ultima stabile", fmt.Sprintf("%s (%v)", st, upm["batocera_stable_date"]))
		}
		if bt, _ := upm["batocera_beta"].(string); bt != "" {
			kv("Beta butterfly", fmt.Sprintf("%s (%v)", bt, upm["batocera_beta_date"]))
		}
		if items, ok := upm["items"].([]map[string]any); ok && len(items) > 0 {
			for _, it := range items {
				para("• " + it["title"].(string))
			}
		} else {
			para("Nessun aggiornamento in sospeso.")
		}
	}
	kv("Companion", s.Version)
	if in.MameVersion != "" {
		kv("MAME", in.MameVersion)
	}
	kv("Core libretro", fmt.Sprintf("%d installati", len(in.Cores)))

	h2("Giochi per sistema")
	sort.Slice(systems, func(i, j int) bool { return systems[i].Count > systems[j].Count })
	total := 0
	pdf.SetFont("Helvetica", "", 9)
	col := 0
	nsys := 0
	for _, sy := range systems {
		n := strings.ToLower(sy.Name)
		if sy.Count == 0 || n == "all" || n == "favorites" || n == "recent" || n == "lastplayed" || strings.HasPrefix(n, "auto-") {
			continue
		}
		nsys++
		total += sy.Count
		name := sy.Fullname
		if name == "" {
			name = sy.Name
		}
		pdf.CellFormat(60, 5, tr(fmt.Sprintf("%s: %d", name, sy.Count)), "", 0, "L", false, 0, "")
		col++
		if col == 3 {
			pdf.Ln(5)
			col = 0
		}
	}
	if col != 0 {
		pdf.Ln(5)
	}
	pdf.SetFont("Helvetica", "B", 9.5)
	pdf.CellFormat(0, 6, tr(fmt.Sprintf("Totale: %d giochi in %d sistemi", total, nsys)), "", 1, "L", false, 0, "")

	h2("Controller e periferiche")
	if len(in.InputDevices) > 0 {
		for _, d := range in.InputDevices {
			para("• " + d)
		}
	} else {
		para("Nessun dispositivo di input rilevato.")
	}
	if g := strings.TrimSpace(sec["guns"]); g != "" {
		kv("Lightgun", strings.ReplaceAll(g, "\n", ", "))
	}
	labels := map[string]string{}
	for k, v := range planciaDefaultLabels {
		labels[k] = v
	}
	for k, v := range s.cfg.PlanciaLabels {
		labels[k] = v
	}
	for _, ic := range in.ESInput {
		if ic.Type != "joystick" {
			continue
		}
		var parts []string
		for _, n := range []string{"a", "b", "x", "y", "pageup", "pagedown", "select", "start", "hotkey"} {
			v, ok := ic.Inputs[n]
			if !ok {
				continue
			}
			f := strings.Split(v, ":")
			if len(f) >= 2 && f[0] == "button" {
				lab := labels[f[1]]
				if lab == "" {
					lab = "pulsante " + f[1]
				}
				parts = append(parts, fmt.Sprintf("%s = %s", strings.ToUpper(n), lab))
			}
		}
		if len(parts) > 0 {
			pdf.SetFont("Helvetica", "B", 9.5)
			pdf.MultiCell(0, 5.5, tr("Plancia ("+strings.Join(strings.Fields(ic.DeviceName), " ")+")"), "", "L", false)
			para(strings.Join(parts, " · "))
		}
		break
	}

	h2("BIOS")
	if len(in.BiosMissing) == 0 {
		para("Tutti i BIOS richiesti sono presenti.")
	} else {
		para(fmt.Sprintf("%d BIOS mancanti o errati:", len(in.BiosMissing)))
		for i, b := range in.BiosMissing {
			if i >= 40 {
				para(fmt.Sprintf("… e altri %d", len(in.BiosMissing)-40))
				break
			}
			para(fmt.Sprintf("• %s — %s (%s)", b.System, b.Path, b.Status))
		}
	}

	h2("App installate (Flathub)")
	if fp := strings.TrimSpace(sec["flatpak"]); fp != "" {
		for _, l := range cab.Lines(fp) {
			para("• " + strings.ReplaceAll(l, "\t", " "))
		}
	} else {
		para("Nessuna app Flatpak installata.")
	}

	h2("Backup")
	if bk := strings.TrimSpace(sec["backups"]); bk != "" {
		para("Backup su USB trovati: " + strings.ReplaceAll(bk, "\n", ", "))
	}
	if s.cfg.BackupDir != "" {
		kv("Cartella backup PC", s.cfg.BackupDir)
	} else {
		para("Nessuna cartella di backup sul PC configurata (scheda Backup).")
	}
	if sv := strings.TrimSpace(sec["saves"]); sv != "" {
		kv("File di salvataggio", sv)
	}

	if rep != nil {
		h2("Check-up")
		para(fmt.Sprintf("Punteggio %d/100 · %d controlli: %d errori, %d avvisi, %d ok.", rep.Score, len(rep.Findings), rep.Counts["error"], rep.Counts["warn"], rep.Counts["ok"]))
		for _, f := range rep.Findings {
			if f.Level == "ok" {
				continue
			}
			icon := map[string]string{"error": "[X]", "warn": "[!]", "info": "[i]"}[f.Level]
			pdf.SetFont("Helvetica", "B", 9.5)
			pdf.MultiCell(0, 5, tr(icon+" "+f.Title), "", "L", false)
			if f.Detail != "" {
				det := f.Detail
				if rs := []rune(det); len(rs) > 500 {
					det = string(rs[:500]) + " […]"
				}
				pdf.SetFont("Helvetica", "", 9)
				pdf.SetTextColor(80, 80, 80)
				pdf.MultiCell(0, 4.5, tr(det), "", "L", false)
				pdf.SetTextColor(20, 20, 20)
			}
			pdf.Ln(1)
		}
	}
	if s.cfg.StockActive {
		h2("Note")
		para("Modalità \"stock\" attiva: alcuni sistemi sono nascosti e i menu sono in modalità Kiosk (scheda Aspetto per disattivarla).")
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cabinato-%s.pdf"`, time.Now().Format("2006-01-02")))
	_, _ = w.Write(buf.Bytes())
}
