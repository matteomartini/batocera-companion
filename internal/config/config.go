// Package config: impostazioni del Companion (JSON nella cartella dati dell'app).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

type Config struct {
	Host          string   `json:"host"`
	RemoteHost    string   `json:"remote_host"`
	SSHUser       string   `json:"ssh_user"`
	SSHPass       string   `json:"ssh_pass"`
	SSHPort       int      `json:"ssh_port"`
	ESPort        int      `json:"es_port"`
	RomsPath      string   `json:"roms_path"`
	BiosPath      string   `json:"bios_path"`
	ScreenshotDir string   `json:"screenshot_dir"`
	ArcadePref    []string `json:"arcade_preference"`
	ConvertToCHD  bool     `json:"convert_to_chd"`
	LanEnabled    bool     `json:"lan_enabled"`
	LanPin        string   `json:"lan_pin"`
	LanPort       int      `json:"lan_port"`
	BackupDir     string   `json:"backup_dir"`

	// ScreenScraper
	SSUser    string `json:"ss_user"`
	SSPass    string `json:"ss_pass"`
	SSDevID   string `json:"ss_devid"`
	SSDevPass string `json:"ss_devpass"`
	SSRegion  string `json:"ss_region"`
	SSLang    string `json:"ss_lang"`

	// Plancia
	PlanciaLabels map[string]string `json:"plancia_labels"`
	PlanciaSwap   bool              `json:"plancia_swap"`
	// Modalità stock
	StockActive bool              `json:"stock_active"`
	StockPrev   map[string]string `json:"stock_prev"`
	StockKeep   string            `json:"stock_keep"`
	// Promemoria aggiornamenti
	UpdatesSeen map[string]string `json:"updates_seen"`
	UpdatesLast string            `json:"updates_last"`

	// Sessioni LAN (token → nome dispositivo) e chiave per i link senza PIN (Home Assistant)
	Sessions map[string]string `json:"sessions"`
	LinkKey  string            `json:"link_key"`

	// Modalità server sul cabinato
	ServerMode bool   `json:"-"`
	Path       string `json:"-"`
}

var mu sync.Mutex

// DataDir: cartella dati dell'app (Windows: %APPDATA%\BatoceraCompanion; cabinato: /userdata/system/companion).
func DataDir(serverMode bool) string {
	if serverMode || (runtime.GOOS == "linux" && fileExists("/userdata/system")) {
		return "/userdata/system/companion"
	}
	if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, "BatoceraCompanion")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".batocera-companion")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func Default(serverMode bool) *Config {
	c := &Config{
		Host: "", SSHUser: "root", SSHPass: "linux", SSHPort: 22, ESPort: 1234,
		RomsPath: "/userdata/roms", BiosPath: "/userdata/bios",
		ArcadePref: []string{"fbneo", "mame", "mame2015", "mame2010", "mame2003_plus"},
		LanPort:    8765,
	}
	dir := DataDir(serverMode)
	c.ScreenshotDir = filepath.Join(dir, "screenshots")
	c.Path = filepath.Join(dir, "config.json")
	c.ServerMode = serverMode
	if serverMode {
		c.Host = "127.0.0.1"
	}
	return c
}

func Load(serverMode bool) (*Config, error) {
	c := Default(serverMode)
	b, err := os.ReadFile(c.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	p := c.Path
	if err := json.Unmarshal(b, c); err != nil {
		return c, err
	}
	c.Path = p
	c.ServerMode = serverMode
	if c.SSHPort == 0 {
		c.SSHPort = 22
	}
	if c.ESPort == 0 {
		c.ESPort = 1234
	}
	if c.LanPort == 0 {
		c.LanPort = 8765
	}
	if c.RomsPath == "" {
		c.RomsPath = "/userdata/roms"
	}
	if c.BiosPath == "" {
		c.BiosPath = "/userdata/bios"
	}
	if len(c.ArcadePref) == 0 {
		c.ArcadePref = Default(serverMode).ArcadePref
	}
	if c.ScreenshotDir == "" {
		c.ScreenshotDir = Default(serverMode).ScreenshotDir
	}
	if serverMode && c.Host == "" {
		c.Host = "127.0.0.1"
	}
	return c, nil
}

func Save(c *Config) error {
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

func (c *Config) Dir() string { return filepath.Dir(c.Path) }
