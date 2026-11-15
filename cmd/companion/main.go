// Batocera Companion: app Windows (e server sul cabinato) per gestire Batocera.
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"batocompanion/internal/config"
	"batocompanion/internal/server"
	"batocompanion/internal/xclip"
)

var version = "dev"

//go:embed web
var webFS embed.FS

//go:embed assets
var assetsFS embed.FS

//go:embed payload
var payloadFS embed.FS

func main() {
	port := flag.Int("port", 0, "porta locale (0 = casuale)")
	noBrowser := flag.Bool("no-browser", false, "non aprire la finestra")
	keep := flag.Bool("keep", false, "non chiudere quando la finestra viene chiusa")
	serverMode := flag.Bool("server", false, "modalità server (sul cabinato): ascolta sulla rete con PIN, nessuna finestra")
	showVer := flag.Bool("version", false, "stampa la versione")
	clipGet := flag.Bool("clip-get", false, "(sul cabinato) stampa il testo negli appunti di X")
	clipSet := flag.String("clip-set", "", "(sul cabinato) mette negli appunti di X il contenuto del file e resta a servirlo")
	flag.Parse()
	if *showVer {
		fmt.Println(version)
		return
	}
	if *clipGet {
		t, err := xclip.Get()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(t)
		return
	}
	if *clipSet != "" {
		t, err := xclip.ReadFileOrStdin(*clipSet)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := xclip.Serve(t); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(*serverMode)
	if err != nil {
		log.Printf("configurazione: %v (uso i valori predefiniti)", err)
	}
	_ = os.MkdirAll(cfg.Dir(), 0o755)
	logf, err := os.OpenFile(filepath.Join(cfg.Dir(), "companion.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		if st, _ := logf.Stat(); st != nil && st.Size() > 2<<20 {
			logf.Truncate(0)
		}
		log.SetOutput(logf)
	}
	log.Printf("Batocera Companion %s avvio (server=%v)", version, *serverMode)

	payload, _ := payloadFS.ReadFile("payload/companion-linux-amd64")
	srv := server.New(version, cfg, webFS, assetsFS, payload, *keep || *noBrowser)

	if *serverMode {
		addr := fmt.Sprintf(":%d", cfg.LanPort)
		log.Printf("in ascolto su %s", addr)
		h := &http.Server{Addr: addr, Handler: srv.Handler(false), ReadHeaderTimeout: 20 * time.Second}
		log.Fatal(h.ListenAndServe())
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
	fmt.Println("Interfaccia:", url)
	go func() { _ = http.Serve(ln, srv.Handler(true)) }()
	srv.StartLAN()
	if !*noBrowser {
		openWindow(url)
	}
	go srv.WatchHeartbeat()
	<-srv.Quit()
	log.Println("chiusura")
}

// openWindow apre l'interfaccia in una finestra app (Edge/Chrome) o nel browser predefinito.
func openWindow(url string) {
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				profile := filepath.Join(config.DataDir(false), "window-profile")
				cmd := exec.Command(c, "--app="+url, "--user-data-dir="+profile, "--window-size=1280,860", "--no-first-run", "--disable-features=Translate,msEdgeShoppingAssistant,msHubApps")
				if err := cmd.Start(); err == nil {
					return
				}
			}
		}
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}
