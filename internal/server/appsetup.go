package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"batocompanion/internal/cab"
	"batocompanion/internal/jobs"
)

// Configurazione con un clic: Steam (flatpak) e Chiaki (PS5 Remote Play).

const appStatusScript = `
echo "@@flatpak"; flatpak list --app --columns=application 2>/dev/null | grep -c valvesoftware.Steam
echo "@@rootcheck"; F=$(find /userdata/saves/flatpak/binaries/app/com.valvesoftware.Steam -name bin_steam.sh 2>/dev/null | head -1); if [ -z "$F" ]; then echo none; elif grep -q "Don't allow running as root" "$F"; then echo present; else echo removed; fi
echo "@@essteam"; grep -c 'steam-es.sh' /userdata/system/configs/emulationstation/es_systems_steam.cfg 2>/dev/null
echo "@@launcher"; ls /userdata/system/steam-es.sh /userdata/system/steam-hotkey.py 2>/dev/null | wc -l
echo "@@steamfile"; ls /userdata/roms/steam/*.steam 2>/dev/null | wc -l
echo "@@override"; flatpak override --show com.valvesoftware.Steam 2>/dev/null | grep -c SDL_GAMECONTROLLERCONFIG
echo "@@steamlogin"; ls /userdata/saves/flatpak/data/.var/app/com.valvesoftware.Steam/.local/share/Steam/userdata 2>/dev/null | grep -vc '^0$'
echo "@@chiaki"; ls /userdata/system/add-ons/chiaki/Chiaki.AppImage 2>/dev/null | wc -l
echo "@@chiakish"; grep -c 'XDG_CONFIG_HOME=/userdata' /userdata/roms/ports/Chiaki.sh 2>/dev/null
echo "@@chiakireg"; grep -c 'registered_hosts\|rp_key\|RegistKey' /userdata/system/.config/Chiaki/Chiaki.conf 2>/dev/null
echo "@@chiakihosts"; grep -oE 'server_nickname=[^\r\n]*' /userdata/system/.config/Chiaki/Chiaki.conf 2>/dev/null | head -3 | sed 's/server_nickname=//'
echo "@@scbin"; ls /userdata/roms/ports/SpaceCadetPinball/SpaceCadetPinball 2>/dev/null | wc -l
echo "@@scdat"; ls /userdata/roms/ports/SpaceCadetPinball/ 2>/dev/null | grep -i -c '\.dat$'
echo "@@scl2"; grep -c 'leftstick' "/userdata/roms/ports/Space Cadet Pinball.sh" 2>/dev/null
`

func (s *Server) hAppSetup(w http.ResponseWriter, r *http.Request) (any, error) {
	if r.Method != http.MethodPost {
		res, err := s.ssh.RunTimeout(appStatusScript, time.Minute)
		if err != nil {
			return nil, err
		}
		sec := cab.SplitSections(res.Stdout)
		n := func(k string) int { var v int; fmt.Sscanf(strings.TrimSpace(sec[k]), "%d", &v); return v }
		st := map[string]any{"installed": n("flatpak") > 0, "rootcheck": strings.TrimSpace(sec["rootcheck"]), "entry": n("essteam") > 0 && n("steamfile") > 0, "launcher": n("launcher") == 2, "padmap": n("override") > 0, "logged_in": n("steamlogin") > 0}
		st["configured"] = st["installed"].(bool) && st["rootcheck"] == "removed" && st["entry"].(bool) && st["launcher"].(bool) && st["padmap"].(bool)
		ch := map[string]any{"installed": n("chiaki") > 0, "persistent": n("chiakish") > 0, "registered": n("chiakireg") > 0, "hosts": cab.Lines(sec["chiakihosts"])}
		sc := map[string]any{"installed": n("scbin") > 0, "data": n("scdat") > 0, "l2r2": n("scl2") > 0}
		ch["configured"] = ch["installed"].(bool) && ch["persistent"].(bool)
		return map[string]any{"steam": st, "chiaki": ch, "spacecadet": sc}, nil
	}
	var req struct {
		App string `json:"app"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	switch req.App {
	case "steam":
		j := s.jobs.Start("Configurazione Steam", func(j *jobs.Job) (any, error) { return nil, s.setupSteam(j) })
		return map[string]any{"job": j.ID}, nil
	case "chiaki":
		j := s.jobs.Start("Configurazione Chiaki", func(j *jobs.Job) (any, error) { return nil, s.setupChiaki(j) })
		return map[string]any{"job": j.ID}, nil
	case "spacecadet":
		j := s.jobs.Start("Space Cadet Pinball", func(j *jobs.Job) (any, error) {
			j.SetProgress(0.2, "Aggiorno gioco, launcher e comandi (i file dati restano)")
			return nil, s.installSpaceCadet(context.Background(), func(f string, a ...any) { j.Logf(f, a...) })
		})
		return map[string]any{"job": j.ID}, nil
	}
	return nil, errors.New("app sconosciuta")
}

func (s *Server) run(j *jobs.Job, cmd string, timeout time.Duration) (string, error) {
	res, err := s.ssh.RunCtx(j.Context(), cmd, timeout)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout + res.Stderr)
	if res.Exit != 0 {
		return out, fmt.Errorf("codice %d: %s", res.Exit, lastLine(out))
	}
	return out, nil
}

func (s *Server) setupSteam(j *jobs.Job) error {
	// 1. installato?
	j.SetProgress(0.05, "Controllo Steam")
	out, _ := s.run(j, "flatpak list --app --columns=application 2>/dev/null | grep -c valvesoftware.Steam; true", time.Minute)
	if strings.TrimSpace(out) == "0" {
		j.SetProgress(0.1, "Installo Steam da Flathub (circa 1 GB)")
		if _, err := s.run(j, "flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo 2>/dev/null; flatpak install -y --noninteractive flathub com.valvesoftware.Steam 2>&1 | tail -5", 60*time.Minute); err != nil {
			return fmt.Errorf("installazione Steam: %v", err)
		}
		j.Logf("✓ Steam installato")
	} else {
		j.Logf("✓ Steam già installato")
	}
	// 2. blocco root
	j.SetProgress(0.4, "Tolgo il blocco 'non come root'")
	out, _ = s.run(j, `F=$(find /userdata/saves/flatpak/binaries/app/com.valvesoftware.Steam -name bin_steam.sh 2>/dev/null | head -1); [ -n "$F" ] || { echo NOFILE; exit 0; }
if grep -q "# Don't allow running as root" "$F"; then cp "$F" "$F.bak"; awk "/# Don't allow running as root/{n=5} !/# Don't allow running as root/{if(n>0){n--}else{print}}" "$F.bak" > "$F"; chmod +x "$F"; echo REMOVED; else echo ALREADY; fi`, time.Minute)
	switch strings.TrimSpace(out) {
	case "REMOVED":
		j.Logf("✓ blocco root tolto da bin_steam.sh")
	case "ALREADY":
		j.Logf("✓ blocco root già tolto")
	default:
		j.Logf("⚠ bin_steam.sh non trovato: Steam deve fare il primo avvio (scarica l'aggiornamento) e poi rilancia la configurazione")
	}
	// 3. script di avvio + hotkey
	j.SetProgress(0.6, "Scrivo lo script di avvio e il gestore hotkey")
	for _, f := range []struct{ asset, dest string }{{"steam-es.sh", "/userdata/system/steam-es.sh"}, {"steam-hotkey.py", "/userdata/system/steam-hotkey.py"}} {
		b, err := s.asset(f.asset)
		if err != nil {
			return err
		}
		if err := s.ssh.UploadBytes(f.dest, b, 0o755); err != nil {
			return err
		}
	}
	// 4. sezione Steam di ES con il nostro comando
	j.SetProgress(0.7, "Configuro la sezione Steam di EmulationStation")
	block, err := s.esSystemBlock("steam")
	if err != nil {
		return err
	}
	block = reCommand.ReplaceAllString(block, "<command>/userdata/system/steam-es.sh %ROM%</command>")
	cfg := "<?xml version=\"1.0\"?>\n<systemList>\n" + block + "\n</systemList>\n"
	if err := s.ssh.UploadBytes("/userdata/system/configs/emulationstation/es_systems_steam.cfg", []byte(cfg), 0o644); err != nil {
		return err
	}
	if _, err := s.run(j, `mkdir -p /userdata/roms/steam /userdata/system/companion_trash/steam-bua
[ -s /userdata/roms/steam/Steam.steam ] && : > /userdata/roms/steam/Steam.steam; touch /userdata/roms/steam/Steam.steam
mv /userdata/roms/steam/Steam_Big_Picture.sh* /userdata/roms/ports/*[Ss]team* /userdata/system/companion_trash/steam-bua/ 2>/dev/null
for f in /userdata/roms/flatpak/*.flatpak; do [ -f "$f" ] && grep -q valvesoftware.Steam "$f" 2>/dev/null && mv "$f" /userdata/system/companion_trash/steam-bua/; done; true`, time.Minute); err != nil {
		return err
	}
	j.Logf("✓ sezione Steam: voce Steam.steam (eventuali doppioni BUA/flatpak spostati nel cestino)")
	// 5. plancia come controller SDL (leva = levetta analogica)
	j.SetProgress(0.85, "Mappo la plancia per Steam")
	out, _ = s.run(j, `L=$(grep -h DragonRise /userdata/system/configs/PCSX2/game_controller_db.txt /userdata/system/configs/*/game_controller_db.txt /userdata/system/configs/*/gamecontrollerdb.txt 2>/dev/null | head -1)
[ -n "$L" ] || { echo NOMAP; exit 0; }
L=$(echo "$L" | sed -E 's/(leftx|lefty|rightx|righty|dpup|dpdown|dpleft|dpright):[^,]*,//g')
flatpak override --env=SDL_GAMECONTROLLERCONFIG="${L}leftx:a0,lefty:a1," com.valvesoftware.Steam && echo OK`, time.Minute)
	if strings.Contains(out, "OK") {
		j.Logf("✓ plancia: SHORT=A, FORWARD=B, JAB=X, STRONG=Y, FIERCE=LB, ROUNDHOUSE=RB, leva = levetta analogica sinistra")
	} else {
		j.Logf("⚠ mappa SDL della plancia non trovata (avvia prima un gioco PS2/Dolphin per generarla): Steam userà la mappa generica")
	}
	j.SetProgress(0.95, "Riavvio EmulationStation")
	_, _ = s.run(j, "batocera-es-swissknife --restart >/dev/null 2>&1 &", 20*time.Second)
	j.Logf("Primo avvio: Steam scarica il suo aggiornamento (qualche minuto), poi login. In Steam → Impostazioni → Controller attiva il supporto 'controller generici'. Hotkey+Start chiude Steam.")
	return nil
}

var reCommand = regexpMust(`(?s)<command>.*?</command>`)

// esSystemBlock estrae il blocco <system>…</system> di un sistema da es_systems.cfg.
func (s *Server) esSystemBlock(name string) (string, error) {
	res, err := s.ssh.RunTimeout("cat /usr/share/emulationstation/es_systems.cfg", time.Minute)
	if err != nil {
		return "", err
	}
	x := res.Stdout
	needle := "<name>" + name + "</name>"
	i := strings.Index(x, needle)
	if i < 0 {
		return "", fmt.Errorf("sistema %s non presente in es_systems.cfg", name)
	}
	start := strings.LastIndex(x[:i], "<system>")
	end := strings.Index(x[i:], "</system>")
	if start < 0 || end < 0 {
		return "", errors.New("es_systems.cfg non leggibile")
	}
	return x[start : i+end+len("</system>")], nil
}

func (s *Server) setupChiaki(j *jobs.Job) error {
	j.SetProgress(0.05, "Controllo Chiaki")
	out, _ := s.run(j, "ls /userdata/system/add-ons/chiaki/Chiaki.AppImage 2>/dev/null | wc -l", time.Minute)
	if strings.TrimSpace(out) == "0" {
		j.SetProgress(0.1, "Scarico chiaki-ng (AppImage, ~100 MB)")
		b, err := s.asset("chiaki-install.sh")
		if err != nil {
			return err
		}
		if err := s.ssh.UploadBytes("/tmp/chiaki-install.sh", b, 0o755); err != nil {
			return err
		}
		o, err := s.run(j, "bash /tmp/chiaki-install.sh 2>&1 | tail -15", 30*time.Minute)
		for _, l := range cab.Lines(o) {
			j.Logf("%s", l)
		}
		if err != nil {
			return fmt.Errorf("installazione Chiaki: %v", err)
		}
	} else {
		j.Logf("✓ Chiaki installato")
	}
	j.SetProgress(0.6, "Rendo persistente la configurazione (registrazione PS5 in /userdata)")
	if err := s.writeChiakiLauncher(); err != nil {
		return err
	}
	if _, err := s.run(j, "mkdir -p /userdata/system/.config /userdata/system/.local/share /userdata/system/.cache; [ -d /root/.config/Chiaki ] && [ ! -d /userdata/system/.config/Chiaki ] && cp -r /root/.config/Chiaki /userdata/system/.config/; true", time.Minute); err != nil {
		return err
	}
	j.Logf("✓ Chiaki.sh in Ports con configurazione persistente")
	_ = s.es.ReloadGames()
	j.Logf("Prima volta: avvia Chiaki dal cabinato, inserisci l'Account ID PSN (dal link 'Login PSN' nella scheda Add-on, poi incolla l'indirizzo in Chiaki con Ctrl+V dalla vista live) e registra la PS5 con il PIN.")
	return nil
}

const chiakiLauncher = `#!/bin/bash
export $(cat /proc/1/environ | tr '\0' '\n')
# configurazione e registrazione PS5 su /userdata (persistente), non su / (si azzera a ogni riavvio)
export HOME=/userdata/system
export XDG_CONFIG_HOME=/userdata/system/.config
export XDG_DATA_HOME=/userdata/system/.local/share
export XDG_CACHE_HOME=/userdata/system/.cache
export DISPLAY=:0.0
export QTWEBENGINE_DISABLE_SANDBOX=1
export QT_QPA_PLATFORM=xcb
app_dir="/userdata/system/add-ons/chiaki"
log="/userdata/system/logs/chiaki.log"
mkdir -p /userdata/system/logs
# un Chiaki rimasto appeso (sessione bloccata) impedirebbe l'avvio: lo chiudo prima di ripartire
if pgrep -f 'Chiaki.AppImage' >/dev/null; then echo "$(date): chiudo un Chiaki rimasto in esecuzione" >> "$log"; pkill -9 -f 'Chiaki.AppImage'; sleep 1; fi
# cabinato: il joystick (digitale) fa anche da levetta sinistra, cosi' nei giochi PS5 il personaggio si muove
if [ -n "$SDL_GAMECONTROLLERCONFIG" ] && [ -f "$app_dir/padmap.py" ]; then
  m="$(printf '%s' "$SDL_GAMECONTROLLERCONFIG" | python3 "$app_dir/padmap.py" 2>>"$log")"
  [ -n "$m" ] && export SDL_GAMECONTROLLERCONFIG="$m"
fi
# mappatura tasti dedicata a Chiaki (pad virtuale del Companion): encoder presi in esclusiva
padpid=""
if [ -f /userdata/system/companion/chiaki-pad.json ] && [ -f "$app_dir/chiaki-pad.py" ]; then
  rm -f /tmp/bc_chiaki_pad.map
  pkill -f '[c]hiaki-pad.py' 2>/dev/null
  python3 "$app_dir/chiaki-pad.py" & padpid=$!
  trap '[ -n "$padpid" ] && kill "$padpid" 2>/dev/null' EXIT INT TERM HUP
  for i in 1 2 3 4 5 6 7 8 9 10; do [ -f /tmp/bc_chiaki_pad.map ] && break; sleep 0.3; done
  if [ -f /tmp/bc_chiaki_pad.map ]; then
    export SDL_GAMECONTROLLERCONFIG="$(cat /tmp/bc_chiaki_pad.map)
$SDL_GAMECONTROLLERCONFIG"
    echo "$(date): pad virtuale Companion attivo" >> "$log"
  else
    echo "$(date): pad virtuale non partito, uso i controller normali" >> "$log"
  fi
fi
echo "$(date): avvio Chiaki" >> "$log"
cd "$app_dir" && ./Chiaki.AppImage >> "$log" 2>&1
[ -n "$padpid" ] && kill "$padpid" 2>/dev/null
`

// mappatura SDL: se il pad non ha una levetta sinistra, la ricava dal d-pad (assi del joystick)
const chiakiPadmap = `import sys, re
out = []
for line in sys.stdin.read().splitlines():
    if not line.strip():
        continue
    parts = line.split(',')
    keys = {p.split(':')[0]: p.split(':', 1)[1] for p in parts[2:] if ':' in p}
    add = []
    if 'leftx' not in keys:
        v = keys.get('dpleft', '')
        m = re.match(r'[-+]a(\d+)', v)
        if m: add.append('leftx:a' + m.group(1))
        elif v.startswith('h'): add.append('leftx:' + v.split('.')[0] + '.8')  # hat → non supportato come asse: lascia
    if 'lefty' not in keys:
        v = keys.get('dpup', '')
        m = re.match(r'[-+]a(\d+)', v)
        if m: add.append('lefty:a' + m.group(1))
    add = [a for a in add if not a.endswith('.8')]
    if add:
        line = line.rstrip(',') + ',' + ','.join(add) + ','
    out.append(line)
print('\n'.join(out))
`

// writeChiakiLauncher scrive Chiaki.sh in Ports e gli script di supporto (padmap, pad virtuale).
func (s *Server) writeChiakiLauncher() error {
	if err := s.ssh.UploadBytes("/userdata/system/add-ons/chiaki/padmap.py", []byte(chiakiPadmap), 0o644); err != nil {
		return err
	}
	if b, err := s.asset("chiaki-pad.py"); err == nil {
		if err := s.ssh.UploadBytes("/userdata/system/add-ons/chiaki/chiaki-pad.py", b, 0o755); err != nil {
			return err
		}
	}
	return s.ssh.UploadBytes("/userdata/roms/ports/Chiaki.sh", []byte(chiakiLauncher), 0o755)
}
