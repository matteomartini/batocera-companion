package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func (s *Server) hOpen(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		What string `json:"what"`
		URL  string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	if s.cfg.ServerMode {
		return nil, errors.New("disponibile solo dall'app sul PC")
	}
	host := s.ssh.ActiveHost()
	switch req.What {
	case "share":
		return nil, openPath(`\\` + host + `\share`)
	case "web":
		return nil, openURL("http://" + host + "/")
	case "screens":
		_ = os.MkdirAll(s.cfg.ScreenshotDir, 0o755)
		return nil, openPath(s.cfg.ScreenshotDir)
	case "data":
		return nil, openPath(s.cfg.Dir())
	case "url":
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return nil, errors.New("indirizzo non valido")
		}
		return nil, openURL(req.URL)
	}
	return nil, fmt.Errorf("%q sconosciuto", req.What)
}
