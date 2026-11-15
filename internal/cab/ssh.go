// Package cab: dialogo con il cabinato (SSH/SFTP + API HTTP di EmulationStation).
package cab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSH: una connessione condivisa su cui si aprono sessioni indipendenti. Ogni Run/Upload/Download
// usa la propria sessione, quindi comandi e trasferimenti procedono in parallelo (una copia ROM in
// corso non blocca l'analisi di un'altra cartella). Se la connessione cade viene riaperta;
// se l'indirizzo di casa non risponde si prova quello remoto (Tailscale).
type SSH struct {
	Host, RemoteHost string
	Port             int
	User, Pass       string

	mu        sync.Mutex
	client    *ssh.Client
	viaRemote bool
	dialing   bool      // single-flight: una sola connessione per volta
	failAt    time.Time // ultimo tentativo fallito: breve backoff per non intasare
	lastErr   string
	sem       chan struct{}
}

type Result struct {
	Stdout, Stderr string
	Exit           int
}

func NewSSH(host, remote string, port int, user, pass string) *SSH {
	return &SSH{Host: host, RemoteHost: remote, Port: port, User: user, Pass: pass, sem: make(chan struct{}, 8)}
}

func (s *SSH) Configure(host, remote string, port int, user, pass string) {
	s.mu.Lock()
	changed := s.Host != host || s.RemoteHost != remote || s.Port != port || s.User != user || s.Pass != pass
	s.Host, s.RemoteHost, s.Port, s.User, s.Pass = host, remote, port, user, pass
	c := s.client
	if changed {
		s.client = nil
	}
	s.mu.Unlock()
	if changed && c != nil {
		c.Close()
	}
}

func (s *SSH) ViaRemote() bool   { s.mu.Lock(); defer s.mu.Unlock(); return s.viaRemote }
func (s *SSH) LastError() string { s.mu.Lock(); defer s.mu.Unlock(); return s.lastErr }
func (s *SSH) ActiveHost() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.viaRemote && s.RemoteHost != "" {
		return s.RemoteHost
	}
	return s.Host
}

func (s *SSH) dial(host string, timeout time.Duration) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: s.User,
		Auth: []ssh.AuthMethod{ssh.Password(s.Pass), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range qs {
				a[i] = s.Pass
			}
			return a, nil
		})},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprint(s.Port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.(*net.TCPConn).SetKeepAlive(true)
	_ = conn.(*net.TCPConn).SetKeepAlivePeriod(15 * time.Second)
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Client restituisce la connessione condivisa, aprendola se serve.
func (s *SSH) Client() (*ssh.Client, error) {
	s.mu.Lock()
	for {
		if s.client != nil {
			c := s.client
			s.mu.Unlock()
			return c, nil
		}
		if s.Host == "" {
			s.mu.Unlock()
			return nil, errors.New("indirizzo del cabinato non impostato (Impostazioni)")
		}
		// backoff: dopo un fallimento, per 3 s rispondi subito con l'ultimo errore
		if s.lastErr != "" && time.Since(s.failAt) < 3*time.Second {
			e := s.lastErr
			s.mu.Unlock()
			return nil, errors.New(e)
		}
		if !s.dialing {
			break
		}
		// qualcun altro sta già connettendo: aspetta che finisca
		s.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		s.mu.Lock()
	}
	s.dialing = true
	host, remote := s.Host, s.RemoteHost
	s.mu.Unlock()

	c, err := s.dial(host, 4*time.Second)
	via := false
	if err != nil && remote != "" {
		if c2, err2 := s.dial(remote, 8*time.Second); err2 == nil {
			c, err, via = c2, nil, true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialing = false
	if err != nil {
		s.lastErr, s.failAt = humanErr(err), time.Now()
		return nil, errors.New(s.lastErr)
	}
	if s.client != nil { // nel frattempo qualcuno ha riconfigurato e connesso
		c.Close()
		return s.client, nil
	}
	s.client, s.viaRemote, s.lastErr = c, via, ""
	go func(c *ssh.Client) {
		// chiusura: dimentica il client così il prossimo comando riconnette
		_ = c.Wait()
		s.mu.Lock()
		if s.client == c {
			s.client = nil
		}
		s.mu.Unlock()
	}(c)
	go s.keepalive(c)
	return c, nil
}

// probe: keepalive con timeout. Un cabinato spento di colpo lascia la TCP
// "aperta" per minuti; senza timeout la richiesta resterebbe appesa.
func probe(c *ssh.Client, timeout time.Duration) bool {
	ch := make(chan error, 1)
	go func() { _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); ch <- err }()
	select {
	case err := <-ch:
		return err == nil
	case <-time.After(timeout):
		return false
	}
}

// Reconnect chiude la connessione attuale (se c'è) e azzera il backoff:
// il prossimo comando riconnette subito.
func (s *SSH) Reconnect() {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.lastErr = ""
	s.failAt = time.Time{}
	s.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

func (s *SSH) keepalive(c *ssh.Client) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		cur := s.client
		s.mu.Unlock()
		if cur != c {
			return
		}
		if !probe(c, 6*time.Second) {
			c.Close()
			return
		}
	}
}

func humanErr(err error) string {
	e := err.Error()
	switch {
	case strings.Contains(e, "unable to authenticate"):
		return "utente o password SSH errati"
	case strings.Contains(e, "i/o timeout"), strings.Contains(e, "no route"), strings.Contains(e, "unreachable"):
		return "il cabinato non risponde (spento o indirizzo errato)"
	case strings.Contains(e, "refused"):
		return "connessione rifiutata: SSH non attivo sul cabinato"
	}
	return e
}

func (s *SSH) Online() bool { _, err := s.Client(); return err == nil }

func (s *SSH) Close() {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

// Run esegue un comando (bash) e restituisce output e codice di uscita. Concorrente.
func (s *SSH) Run(cmd string) (*Result, error) { return s.RunTimeout(cmd, 2*time.Minute) }

func (s *SSH) RunTimeout(cmd string, timeout time.Duration) (*Result, error) {
	return s.RunCtx(context.Background(), cmd, timeout)
}

func (s *SSH) RunCtx(ctx context.Context, cmd string, timeout time.Duration) (*Result, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		c, err := s.Client()
		if err != nil {
			return nil, err
		}
		s.sem <- struct{}{}
		res, err := s.runOn(ctx, c, cmd, timeout)
		<-s.sem
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !isConnErr(err) {
			if ctx.Err() == nil && !probe(c, 3*time.Second) {
				// comando scaduto e connessione muta: il cabinato è sparito
				s.mu.Lock()
				if s.client == c {
					s.client = nil
				}
				s.mu.Unlock()
				c.Close()
				return nil, errors.New("il cabinato non risponde più (spento o riavviato)")
			}
			return nil, err
		}
		s.mu.Lock()
		if s.client == c {
			s.client = nil
		}
		s.mu.Unlock()
		c.Close()
	}
	return nil, lastErr
}

// Prelude eseguito prima di ogni comando: sostituisce batocera-settings-set con
// una versione che scrive sempre (il comando ufficiale fallisce con exit 11/12
// se la chiave è commentata o assente, caso normale in un batocera.conf di serie).
// bc_del toglie la riga attiva di una chiave. Modifica atomica via file temporaneo.
const Prelude = `bc_set(){ f=/userdata/system/batocera.conf; [ -f "$f" ] || touch "$f"; awk -v k="$1" -v v="$2" 'BEGIN{d=0}{l=$0;sub(/^[#; \t]+/,"",l);if(index(l,k"=")==1){if(!d){print k"="v;d=1};next}print}END{if(!d)print k"="v}' "$f" >"$f.bc" && cat "$f.bc" >"$f" && rm -f "$f.bc"; }
bc_del(){ f=/userdata/system/batocera.conf; [ -f "$f" ] || return 0; awk -v k="$1" '{l=$0;sub(/^[ \t]+/,"",l);if(index(l,k"=")==1)next;print}' "$f" >"$f.bc" && cat "$f.bc" >"$f" && rm -f "$f.bc"; }
batocera-settings-set(){ if [ "$1" = "-f" ]; then command batocera-settings-set "$@"; else bc_set "$1" "$2"; fi; }
`

func isConnErr(err error) bool {
	e := err.Error()
	return strings.Contains(e, "EOF") || strings.Contains(e, "closed") || strings.Contains(e, "broken pipe") || strings.Contains(e, "reset") || strings.Contains(e, "handshake") || strings.Contains(e, "open channel")
}

func (s *SSH) runOn(ctx context.Context, c *ssh.Client, cmd string, timeout time.Duration) (*Result, error) {
	sess, err := c.NewSession()
	if err != nil {
		return nil, fmt.Errorf("sessione SSH: %w", err)
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errb
	done := make(chan error, 1)
	go func() { done <- sess.Run("bash -c " + ShellQuote(Prelude+cmd)) }()
	var timer <-chan time.Time
	if timeout > 0 {
		timer = time.After(timeout)
	}
	select {
	case err = <-done:
	case <-timer:
		_ = sess.Signal(ssh.SIGKILL)
		sess.Close()
		return &Result{Stdout: out.String(), Stderr: errb.String(), Exit: 124}, fmt.Errorf("comando interrotto dopo %s", timeout)
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		sess.Close()
		return &Result{Stdout: out.String(), Stderr: errb.String(), Exit: 130}, ctx.Err()
	}
	res := &Result{Stdout: out.String(), Stderr: errb.String()}
	if err != nil {
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			res.Exit = ee.ExitStatus()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Output: comodo per comandi di sola lettura.
func (s *SSH) Output(cmd string) string {
	r, err := s.Run(cmd)
	if err != nil {
		return ""
	}
	return r.Stdout
}

// StreamSession avvia un comando e restituisce il suo stdout come flusso (per MJPEG, eventi…).
// Chiudere il ReadCloser termina il comando.
func (s *SSH) StreamSession(cmd string) (io.ReadCloser, error) {
	c, err := s.Client()
	if err != nil {
		return nil, err
	}
	sess, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = io.Discard
	if err := sess.Start("bash -c " + ShellQuote(Prelude+cmd)); err != nil {
		sess.Close()
		return nil, err
	}
	go func() {
		err := sess.Wait()
		pw.CloseWithError(err)
	}()
	return &streamRC{Reader: pr, close: func() { _ = sess.Signal(ssh.SIGKILL); sess.Close(); pr.Close() }}, nil
}

type streamRC struct {
	io.Reader
	close func()
	once  sync.Once
}

func (r *streamRC) Close() error { r.once.Do(r.close); return nil }

// SFTP apre un client SFTP dedicato (sessione propria): da chiudere a fine uso.
func (s *SSH) SFTP() (*sftp.Client, error) {
	c, err := s.Client()
	if err != nil {
		return nil, err
	}
	cl, err := sftp.NewClient(c, sftp.MaxPacketUnchecked(128*1024), sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		return nil, fmt.Errorf("SFTP: %w", err)
	}
	return cl, nil
}

// Upload copia un file locale sul cabinato (scrittura su .part + rename), con callback di avanzamento.
func (s *SSH) Upload(ctx context.Context, local, remote string, progress func(done, total int64)) error {
	cl, err := s.SFTP()
	if err != nil {
		return err
	}
	defer cl.Close()
	return UploadWith(ctx, cl, local, remote, progress)
}

func UploadWith(ctx context.Context, cl *sftp.Client, local, remote string, progress func(done, total int64)) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	st, _ := f.Stat()
	total := st.Size()
	_ = cl.MkdirAll(path.Dir(remote))
	tmp := remote + ".part"
	w, err := cl.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("creazione %s: %w", remote, err)
	}
	pr := &progressReader{r: f, total: total, cb: progress, ctx: ctx}
	_, err = w.ReadFrom(pr)
	w.Close()
	if err != nil {
		_ = cl.Remove(tmp)
		return err
	}
	_ = cl.Remove(remote)
	if err := cl.Rename(tmp, remote); err != nil {
		return err
	}
	_ = cl.Chtimes(remote, st.ModTime(), st.ModTime())
	return nil
}

// UploadBytes scrive dati in un file remoto.
func (s *SSH) UploadBytes(remote string, data []byte, mode os.FileMode) error {
	cl, err := s.SFTP()
	if err != nil {
		return err
	}
	defer cl.Close()
	_ = cl.MkdirAll(path.Dir(remote))
	w, err := cl.OpenFile(remote, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return err
	}
	w.Close()
	if mode != 0 {
		_ = cl.Chmod(remote, mode)
	}
	return nil
}

// Download copia un file dal cabinato sul PC.
func (s *SSH) Download(ctx context.Context, remote, local string, progress func(done, total int64)) error {
	cl, err := s.SFTP()
	if err != nil {
		return err
	}
	defer cl.Close()
	return DownloadWith(ctx, cl, remote, local, progress)
}

func DownloadWith(ctx context.Context, cl *sftp.Client, remote, local string, progress func(done, total int64)) error {
	r, err := cl.Open(remote)
	if err != nil {
		return err
	}
	defer r.Close()
	st, _ := r.Stat()
	var total int64
	if st != nil {
		total = st.Size()
	}
	_ = os.MkdirAll(path.Dir(local), 0o755)
	tmp := local + ".part"
	w, err := os.Create(tmp)
	if err != nil {
		return err
	}
	pr := &progressReader{r: r, total: total, cb: progress, ctx: ctx}
	_, err = io.Copy(w, pr)
	w.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(local)
	if err := os.Rename(tmp, local); err != nil {
		return err
	}
	if st != nil {
		_ = os.Chtimes(local, st.ModTime(), st.ModTime())
	}
	return nil
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	cb    func(done, total int64)
	ctx   context.Context
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	if p.ctx != nil && p.ctx.Err() != nil {
		return 0, p.ctx.Err()
	}
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.cb != nil && (time.Since(p.last) > 300*time.Millisecond || err != nil) {
		p.last = time.Now()
		p.cb(p.done, p.total)
	}
	return n, err
}

// ReadFile legge un file remoto (vuoto se assente).
func (s *SSH) ReadFile(remote string) ([]byte, error) {
	cl, err := s.SFTP()
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	f, err := cl.Open(remote)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// ShellQuote: quoting sicuro per bash.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
