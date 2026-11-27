package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"batocompanion/internal/cab"
)

// Visual Pinball (Batocera 43, VPX con PinMAME plugin): la ROM PinMAME si cerca in
// <cartella del tavolo>/pinmame/roms/, poi nella cartella impostata in VPinballX.ini
// ([Plugin.PinMAME] PinMAMEPath), poi in $HOME/.pinmame. Il Companion imposta PinMAMEPath su
// /userdata/roms/vpinball/pinmame così vpinball/pinmame/roms/ vale per tutti i tavoli.

const vpxIni = "/userdata/system/configs/vpinball/VPinballX.ini"
const vpxPinmameRoot = "/userdata/roms/vpinball/pinmame"

const vpxIniPy = `import os, configparser
p='/userdata/system/configs/vpinball/VPinballX.ini'
os.makedirs(os.path.dirname(p), exist_ok=True)
os.makedirs('/userdata/roms/vpinball/pinmame/roms', exist_ok=True)
os.makedirs('/userdata/roms/vpinball/pinmame/nvram', exist_ok=True)
c=configparser.RawConfigParser(allow_no_value=True, strict=False); c.optionxform=str
try: c.read(p)
except Exception as e: print('ERR', e); raise SystemExit(0)
if not c.has_section('Plugin.PinMAME'): c.add_section('Plugin.PinMAME')
cur=c.get('Plugin.PinMAME','PinMAMEPath',fallback='')
if cur.strip() in ('', './', '.'):
    c.set('Plugin.PinMAME','PinMAMEPath','/userdata/roms/vpinball/pinmame')
    c.set('Plugin.PinMAME','Enable','1')
    with open(p,'w') as f: c.write(f, space_around_delimiters=True)
    print('SET')
else:
    print('KEEP', cur)`

// vpxGlobalPinmame imposta la cartella PinMAME comune in VPinballX.ini (se non c'è già una
// cartella scelta dall'utente) e crea vpinball/pinmame/{roms,nvram}.
func (s *Server) vpxGlobalPinmame(ctx context.Context, logf func(string, ...any)) error {
	res, err := s.ssh.RunCtx(ctx, "python3 -c "+cab.ShellQuote(vpxIniPy), time.Minute)
	if err != nil {
		return err
	}
	out := strings.TrimSpace(res.Stdout)
	switch {
	case strings.HasPrefix(out, "SET"):
		logf("✓ VPinballX.ini: PinMAMEPath = %s (ROM in vpinball/pinmame/roms/ valide per tutti i tavoli)", vpxPinmameRoot)
	case strings.HasPrefix(out, "KEEP"):
		logf("VPinballX.ini: PinMAMEPath già impostato (%s), lasciato com'è", strings.TrimSpace(strings.TrimPrefix(out, "KEEP")))
	case strings.HasPrefix(out, "ERR"):
		return errors.New(strings.TrimSpace(strings.TrimPrefix(out, "ERR")))
	default:
		return errors.New("risposta inattesa: " + out + " " + res.Stderr)
	}
	return nil
}

// vpinballApply: sposta le ROM PinMAME trovate nel posto sbagliato ("src>dst", percorsi
// assoluti dentro vpinball o nelle vecchie cartelle configs/vpinball/pinmame) e sistema VPinballX.ini.
func (s *Server) vpinballApply(apply []string) (any, error) {
	q := cab.ShellQuote
	okSrc := func(p string) bool {
		return !strings.ContainsAny(p, "'\"\n") && !strings.Contains(p, "..") && (strings.HasPrefix(p, "/userdata/roms/vpinball/") || strings.HasPrefix(p, "/userdata/system/configs/vpinball/pinmame/") || strings.HasPrefix(p, "/userdata/system/.pinmame/"))
	}
	var cmds []string
	n := 0
	unz := 0
	for _, a := range apply {
		src, dst, ok := strings.Cut(a, ">")
		if ok && dst == "unzip" && okSrc(src) && strings.HasSuffix(strings.ToLower(src), ".vpx") {
			// pacchetto zip travestito da .vpx: estraggo nella cartella del tavolo, ROM in pinmame/roms, via il finto .vpx
			dir := strings.TrimSuffix(src, "/"+pathBase(src))
			cmds = append(cmds, "cd "+q(dir)+" && mv -f "+q(src)+" "+q(src+".pkg.zip")+" && unzip -o -q "+q(src+".pkg.zip")+" -d . && rm -f "+q(src+".pkg.zip")+" && mkdir -p pinmame/roms && for z in *.zip; do [ -f \"$z\" ] && mv -f \"$z\" pinmame/roms/; done; true")
			unz++
			continue
		}
		if !ok || !okSrc(src) || !okSrc(dst) || !strings.HasSuffix(src, ".zip") || !strings.HasSuffix(dst, "/roms") {
			continue
		}
		cmds = append(cmds, "mkdir -p "+q(dst)+" && mv -f "+q(src)+" "+q(dst+"/")+"; rmdir "+q(strings.TrimSuffix(src, "/"+pathBase(src)))+" 2>/dev/null; true")
		n++
	}
	if n > 0 || unz > 0 {
		if _, err := s.ssh.RunTimeout(strings.Join(cmds, "\n"), 20*time.Minute); err != nil {
			return nil, err
		}
	}
	_ = s.vpxGlobalPinmame(context.Background(), func(string, ...any) {})
	if unz > 0 {
		_ = s.es.ReloadGames()
	}
	return map[string]any{"applied": 0, "moved": n, "unzipped": unz}, nil
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
