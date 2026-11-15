// Package roms: analisi dei file ROM sul PC e copia sul cabinato.
package roms

import (
	"archive/zip"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

// Entry: un file dentro un archivio (o un file sciolto).
type Entry struct {
	Name string // nome dentro l'archivio (senza cartelle)
	Size int64
	CRC  uint32
	// origine per la ricostruzione
	Archive string // percorso dell'archivio ("" = file sciolto in Path)
	Path    string // file sciolto
	Inner   string // nome completo dentro l'archivio
}

func IsArchive(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".zip", ".7z", ".rar":
		return true
	}
	return false
}

// ListArchive elenca il contenuto con CRC32 (senza estrarre, dove possibile).
func ListArchive(p string) ([]Entry, error) {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".zip":
		return listZip(p)
	case ".7z":
		return list7z(p)
	case ".rar":
		return listRar(p)
	}
	return nil, nil
}

func listZip(p string) ([]Entry, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []Entry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		out = append(out, Entry{Name: filepath.Base(f.Name), Size: int64(f.UncompressedSize64), CRC: f.CRC32, Archive: p, Inner: f.Name})
	}
	return out, nil
}

func list7z(p string) ([]Entry, error) {
	r, err := sevenzip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Entry
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		out = append(out, Entry{Name: filepath.Base(f.Name), Size: int64(f.UncompressedSize), CRC: f.CRC32, Archive: p, Inner: f.Name})
	}
	return out, nil
}

func listRar(p string) ([]Entry, error) {
	rr, err := rardecode.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []Entry
	for {
		h, err := rr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		if h.IsDir {
			continue
		}
		// rardecode non espone il CRC in modo affidabile per tutti i formati: lo calcolo leggendo
		hs := crc32.NewIEEE()
		n, _ := io.Copy(hs, rr)
		out = append(out, Entry{Name: filepath.Base(h.Name), Size: n, CRC: hs.Sum32(), Archive: p, Inner: h.Name})
	}
	return out, nil
}

// OpenEntry restituisce il contenuto di un file (dentro un archivio o sciolto).
func OpenEntry(e Entry) (io.ReadCloser, error) {
	if e.Archive == "" {
		return os.Open(e.Path)
	}
	switch strings.ToLower(filepath.Ext(e.Archive)) {
	case ".zip":
		zr, err := zip.OpenReader(e.Archive)
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name == e.Inner {
				rc, err := f.Open()
				if err != nil {
					zr.Close()
					return nil, err
				}
				return &closer{rc, func() { zr.Close() }}, nil
			}
		}
		zr.Close()
		return nil, os.ErrNotExist
	case ".7z":
		r, err := sevenzip.OpenReader(e.Archive)
		if err != nil {
			return nil, err
		}
		for _, f := range r.File {
			if f.Name == e.Inner {
				rc, err := f.Open()
				if err != nil {
					r.Close()
					return nil, err
				}
				return &closer{rc, func() { r.Close() }}, nil
			}
		}
		r.Close()
		return nil, os.ErrNotExist
	case ".rar":
		rr, err := rardecode.OpenReader(e.Archive)
		if err != nil {
			return nil, err
		}
		for {
			h, err := rr.Next()
			if err != nil {
				rr.Close()
				return nil, os.ErrNotExist
			}
			if h.Name == e.Inner {
				return &closer{io.NopCloser(rr), func() { rr.Close() }}, nil
			}
		}
	}
	return nil, os.ErrNotExist
}

type closer struct {
	io.ReadCloser
	after func()
}

func (c *closer) Close() error { err := c.ReadCloser.Close(); c.after(); return err }

// FileCRC calcola il CRC32 di un file (e la variante senza header per i sistemi che lo hanno).
func FileCRC(p string, skip int64) (uint32, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	st, _ := f.Stat()
	if skip > 0 {
		if _, err := f.Seek(skip, io.SeekStart); err != nil {
			return 0, 0, err
		}
	}
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return 0, 0, err
	}
	return h.Sum32(), st.Size(), nil
}

// HeaderSkip: byte di header da saltare per confrontare con i DAT headerless.
func HeaderSkip(system string, size int64, head []byte) int64 {
	switch system {
	case "nes":
		if len(head) >= 4 && string(head[:4]) == "NES\x1a" {
			return 16
		}
	case "fds":
		if len(head) >= 4 && string(head[:4]) == "FDS\x1a" {
			return 16
		}
	case "snes":
		if size%1024 == 512 {
			return 512
		}
	case "lynx":
		if len(head) >= 4 && string(head[:4]) == "LYNX" {
			return 64
		}
	case "atari7800":
		if len(head) >= 10 && strings.Contains(string(head[:10]), "ATARI7800") {
			return 128
		}
	}
	return 0
}
