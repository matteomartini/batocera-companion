package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"batocompanion/internal/roms"
)

// Space Cadet Pinball: port SDL2 (k4zmu2a/SpaceCadetPinball, MIT) compilato per Linux x86_64
// e incluso nel Companion. I file dati originali (PINBALL.DAT ecc.) li porta Aggiungi ROM in
// ports/SpaceCadetPinball/; qui si installano binario, launcher per Ports e i comandi
// preimpostati per il cabinato.

const spaceCadetRoot = "/userdata/roms/ports/" + roms.SpaceCadetDir
const spaceCadetSh = "/userdata/roms/ports/Space Cadet Pinball.sh"

const spaceCadetLauncher = `#!/bin/bash
# 3D Pinball - Space Cadet: port SDL2 di k4zmu2a (MIT), installato da Batocera Companion.
# File dati (PINBALL.DAT, .MID, .WAV) in ` + spaceCadetRoot + `
export HOME=/userdata/system
export XDG_DATA_HOME=/userdata/system/.local/share
export XDG_CONFIG_HOME=/userdata/system/.config
export DISPLAY="${DISPLAY:-:0.0}"
d="` + spaceCadetRoot + `"
log=/userdata/system/logs/spacecadet.log
mkdir -p /userdata/system/logs "$XDG_DATA_HOME/SpaceCadetPinball"
# comandi preimpostati per il cabinato (solo la prima volta: poi valgono le modifiche fatte in gioco, F8)
ini="$XDG_DATA_HOME/SpaceCadetPinball/imgui_pb.ini"
[ -f "$ini" ] || cp "$d/imgui_pb.ini.default" "$ini" 2>/dev/null
cd "$d" || exit 1
echo "$(date): avvio Space Cadet Pinball" >> "$log"
exec ./SpaceCadetPinball -fullscreen >> "$log" 2>&1
`

// Preimpostazioni comandi (formato imgui_pb.ini del gioco):
// type: 1 tastiera, 3 controller; controller: A=0 B=1 X=2 Y=3 Back=4 Start=6 LB=9 RB=10.
// Layout a 6 tasti Street Fighter (Y X L1 / B A R1): colonna sinistra = flipper sinistro,
// colonna destra = flipper destro, colonna centrale = lancio pallina. Start = nuova partita.
// Select non esce dal gioco (sarebbe la moneta): si esce con hotkey+start.
const spaceCadetIni = `[Pinball][Settings]
FullScreen=1
Sounds=1
Left Flipper key 0 type=1
Left Flipper key 0 input=122
Left Flipper key 1 type=3
Left Flipper key 1 input=3
Left Flipper key 2 type=3
Left Flipper key 2 input=1
Right Flipper key 0 type=1
Right Flipper key 0 input=47
Right Flipper key 1 type=3
Right Flipper key 1 input=9
Right Flipper key 2 type=3
Right Flipper key 2 input=10
Plunger key 0 type=1
Plunger key 0 input=32
Plunger key 1 type=3
Plunger key 1 input=2
Plunger key 2 type=3
Plunger key 2 input=0
New Game 0 type=1
New Game 0 input=1073741883
New Game 1 type=3
New Game 1 input=6
Toggle Pause 0 type=1
Toggle Pause 0 input=1073741884
Toggle Pause 1 type=1
Toggle Pause 1 input=1073741884
Exit 0 type=1
Exit 0 input=27
Exit 1 type=1
Exit 1 input=27

`

// installSpaceCadet mette binario, launcher e preimpostazioni sul cabinato e verifica che il
// binario giri (librerie SDL2 presenti, file dati trovati).
func (s *Server) installSpaceCadet(ctx context.Context, logf func(string, ...any)) error {
	bin, err := s.asset("spacecadet/SpaceCadetPinball.bin")
	if err != nil {
		return err
	}
	lic, _ := s.asset("spacecadet/LICENSE.txt")
	if _, err := s.ssh.RunCtx(ctx, "mkdir -p "+spaceCadetRoot, time.Minute); err != nil {
		return err
	}
	if err := s.ssh.UploadBytes(spaceCadetRoot+"/SpaceCadetPinball", bin, 0o755); err != nil {
		return fmt.Errorf("binario: %v", err)
	}
	if lic != nil {
		_ = s.ssh.UploadBytes(spaceCadetRoot+"/LICENSE-SpaceCadetPinball.txt", lic, 0o644)
	}
	if err := s.ssh.UploadBytes(spaceCadetRoot+"/imgui_pb.ini.default", []byte(spaceCadetIni), 0o644); err != nil {
		return err
	}
	if err := s.ssh.UploadBytes(spaceCadetSh, []byte(spaceCadetLauncher), 0o755); err != nil {
		return fmt.Errorf("launcher: %v", err)
	}
	logf("✓ Space Cadet Pinball installato in Ports (%s)", spaceCadetSh)
	// verifica: librerie e file dati
	chk := `cd ` + spaceCadetRoot + ` && echo "@@ldd"; ldd ./SpaceCadetPinball 2>&1 | grep -i "not found"; echo "@@dat"; ls -1 2>/dev/null | grep -i -c '\.dat$'; echo "@@run"; SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy timeout 4 ./SpaceCadetPinball -noaudio 2>&1 | head -3; echo "@@end"`
	res, err := s.ssh.RunCtx(ctx, chk, 30*time.Second)
	if err != nil {
		logf("verifica non eseguita: %v", err)
		return nil
	}
	out := res.Stdout
	sec := func(name string) string {
		_, after, ok := strings.Cut(out, "@@"+name+"\n")
		if !ok {
			return ""
		}
		before, _, _ := strings.Cut(after, "@@")
		return strings.TrimSpace(before)
	}
	if miss := sec("ldd"); miss != "" {
		logf("⚠ librerie mancanti sul cabinato: %s", strings.ReplaceAll(miss, "\n", "; "))
		return fmt.Errorf("librerie mancanti: %s", strings.ReplaceAll(miss, "\n", "; "))
	}
	if sec("dat") == "0" {
		logf("⚠ nessun file .DAT in ports/%s: il gioco non parte senza PINBALL.DAT", roms.SpaceCadetDir)
	}
	if run := sec("run"); strings.Contains(run, "Could not load game data") {
		logf("⚠ il gioco non riesce a caricare i dati (PINBALL.DAT mancante o non valido in ports/%s)", roms.SpaceCadetDir)
	} else if run != "" && !strings.Contains(run, "Game version") && !strings.Contains(run, "SDL") {
		logf("avvio di prova: %s", strings.ReplaceAll(run, "\n", " "))
	}
	logf("Comandi: colonna sinistra dei tasti = flipper sinistro, colonna destra = flipper destro, tasti centrali = lancio, joystick = scossa, Start = nuova partita, hotkey+Start = esci. Si cambiano in gioco con F8.")
	return nil
}
