// Package xclip: appunti di X11 (CLIPBOARD) via xgb, per il cabinato (niente xclip su Batocera).
package xclip

import (
	"errors"
	"os"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
)

func atom(c *xgb.Conn, name string) (xproto.Atom, error) {
	r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return r.Atom, nil
}

// Get legge il testo negli appunti (UTF8_STRING, poi STRING).
func Get() (string, error) {
	c, err := xgb.NewConn()
	if err != nil {
		return "", err
	}
	defer c.Close()
	screen := xproto.Setup(c).DefaultScreen(c)
	win, _ := xproto.NewWindowId(c)
	xproto.CreateWindow(c, screen.RootDepth, win, screen.Root, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange})
	clip, _ := atom(c, "CLIPBOARD")
	prop, _ := atom(c, "BC_CLIP")
	for _, target := range []string{"UTF8_STRING", "STRING"} {
		t, _ := atom(c, target)
		xproto.ConvertSelection(c, win, clip, t, prop, xproto.TimeCurrentTime)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			ev, err := c.WaitForEvent()
			if err != nil {
				return "", err
			}
			if sn, ok := ev.(xproto.SelectionNotifyEvent); ok {
				if sn.Property == 0 {
					break
				}
				r, err := xproto.GetProperty(c, true, win, prop, xproto.GetPropertyTypeAny, 0, 1<<20).Reply()
				if err != nil {
					return "", err
				}
				return string(r.Value), nil
			}
		}
	}
	return "", errors.New("appunti vuoti")
}

// Serve diventa proprietario degli appunti con il testo dato e resta in attesa delle richieste
// (termina quando un altro programma prende gli appunti).
func Serve(text string) error {
	c, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer c.Close()
	screen := xproto.Setup(c).DefaultScreen(c)
	win, _ := xproto.NewWindowId(c)
	xproto.CreateWindow(c, screen.RootDepth, win, screen.Root, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil)
	clip, _ := atom(c, "CLIPBOARD")
	primary := xproto.Atom(xproto.AtomPrimary)
	utf8, _ := atom(c, "UTF8_STRING")
	targets, _ := atom(c, "TARGETS")
	textA, _ := atom(c, "TEXT")
	xproto.SetSelectionOwner(c, win, clip, xproto.TimeCurrentTime)
	xproto.SetSelectionOwner(c, win, primary, xproto.TimeCurrentTime)
	data := []byte(text)
	for {
		ev, err := c.WaitForEvent()
		if err != nil {
			return err
		}
		switch e := ev.(type) {
		case xproto.SelectionRequestEvent:
			prop := e.Property
			if prop == 0 {
				prop = e.Target
			}
			switch e.Target {
			case targets:
				buf := make([]byte, 0, 16)
				for _, a := range []xproto.Atom{targets, utf8, xproto.Atom(xproto.AtomString), textA} {
					buf = append(buf, byte(a), byte(a>>8), byte(a>>16), byte(a>>24))
				}
				xproto.ChangeProperty(c, xproto.PropModeReplace, e.Requestor, prop, xproto.Atom(xproto.AtomAtom), 32, 4, buf)
			case utf8, xproto.Atom(xproto.AtomString), textA:
				xproto.ChangeProperty(c, xproto.PropModeReplace, e.Requestor, prop, e.Target, 8, uint32(len(data)), data)
			default:
				prop = 0
			}
			nev := xproto.SelectionNotifyEvent{Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: prop}
			xproto.SendEvent(c, false, e.Requestor, 0, string(nev.Bytes()))
		case xproto.SelectionClearEvent:
			return nil
		}
	}
}

func ReadFileOrStdin(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}
