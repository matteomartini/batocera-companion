// Package scrape: fonti di immagini/metadati per i giochi (libretro-thumbnails, ScreenScraper).
package scrape

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LibretroSystem: cartella Batocera → nome del repository libretro-thumbnails.
var LibretroSystem = map[string]string{
	"nes": "Nintendo - Nintendo Entertainment System", "fds": "Nintendo - Family Computer Disk System", "snes": "Nintendo - Super Nintendo Entertainment System",
	"n64": "Nintendo - Nintendo 64", "gb": "Nintendo - Game Boy", "gbc": "Nintendo - Game Boy Color", "gba": "Nintendo - Game Boy Advance", "nds": "Nintendo - Nintendo DS",
	"virtualboy": "Nintendo - Virtual Boy", "gamecube": "Nintendo - GameCube", "wii": "Nintendo - Wii", "3ds": "Nintendo - Nintendo 3DS", "pokemini": "Nintendo - Pokemon Mini",
	"megadrive": "Sega - Mega Drive - Genesis", "mastersystem": "Sega - Master System - Mark III", "gamegear": "Sega - Game Gear", "sega32x": "Sega - 32X",
	"segacd": "Sega - Mega-CD - Sega CD", "saturn": "Sega - Saturn", "dreamcast": "Sega - Dreamcast", "sg1000": "Sega - SG-1000", "naomi": "Sega - Naomi", "atomiswave": "Atomiswave",
	"psx": "Sony - PlayStation", "ps2": "Sony - PlayStation 2", "psp": "Sony - PlayStation Portable",
	"pcengine": "NEC - PC Engine - TurboGrafx 16", "pcenginecd": "NEC - PC Engine CD - TurboGrafx-CD", "supergrafx": "NEC - PC Engine SuperGrafx",
	"atari2600": "Atari - 2600", "atari5200": "Atari - 5200", "atari7800": "Atari - 7800", "lynx": "Atari - Lynx", "atarist": "Atari - ST", "jaguar": "Atari - Jaguar",
	"ngp": "SNK - Neo Geo Pocket", "ngpc": "SNK - Neo Geo Pocket Color", "neogeo": "SNK - Neo Geo", "wswan": "Bandai - WonderSwan", "wswanc": "Bandai - WonderSwan Color",
	"colecovision": "Coleco - ColecoVision", "mame": "MAME", "fbneo": "FBNeo - Arcade Games", "3do": "The 3DO Company - 3DO", "amiga500": "Commodore - Amiga", "amiga1200": "Commodore - Amiga",
	"c64": "Commodore - 64", "msx1": "Microsoft - MSX", "msx2": "Microsoft - MSX2", "amstradcpc": "Amstrad - CPC", "zxspectrum": "Sinclair - ZX Spectrum",
	"prboom": "DOOM", "gzdoom": "DOOM", "scummvm": "ScummVM", "intellivision": "Mattel - Intellivision", "vectrex": "GCE - Vectrex",
}

var LibretroKinds = map[string]string{"thumbnail": "Named_Boxarts", "image": "Named_Snaps", "titleshot": "Named_Titles"}

// LibretroName: nome file secondo le regole dei thumbnails (caratteri vietati → _).
func LibretroName(n string) string {
	return strings.NewReplacer("&", "_", "*", "_", "/", "_", ":", "_", "`", "_", "<", "_", ">", "_", "?", "_", "\\", "_", "|", "_", "\"", "_").Replace(strings.TrimSpace(n))
}

var httpCl = &http.Client{Timeout: 40 * time.Second}

// FetchLibretro scarica un'immagine (nil, nil se non esiste).
func FetchLibretro(system, kind, name string) ([]byte, error) {
	repo, ok := LibretroSystem[system]
	if !ok {
		return nil, fmt.Errorf("sistema %s non presente su libretro-thumbnails", system)
	}
	dir, ok := LibretroKinds[kind]
	if !ok {
		return nil, fmt.Errorf("tipo %s non disponibile", kind)
	}
	u := "https://thumbnails.libretro.com/" + url.PathEscape(repo) + "/" + dir + "/" + url.PathEscape(LibretroName(name)) + ".png"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "BatoceraCompanion")
	resp, err := httpCl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if len(b) < 100 {
		return nil, nil
	}
	return b, nil
}
