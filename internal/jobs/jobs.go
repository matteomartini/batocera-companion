// Package jobs: lavori in sottofondo interrogati dall'interfaccia (/api/job?id=).
// Ogni job gira nella propria goroutine: più job (es. copia ROM + analisi cartella) procedono in parallelo.
package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"` // running | done | error | cancelled
	Progress float64   `json:"progress"`
	Step     string    `json:"step"`
	Log      []string  `json:"log"`
	Result   any       `json:"result,omitempty"`
	Error    string    `json:"error,omitempty"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended,omitempty"`

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

type Manager struct {
	mu   sync.Mutex
	jobs map[string]*Job
	seq  int
}

func NewManager() *Manager { return &Manager{jobs: map[string]*Job{}} }

func (m *Manager) Start(name string, fn func(j *Job) (any, error)) *Job {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("j%d-%d", time.Now().Unix()%100000, m.seq)
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: id, Name: name, Status: "running", Progress: -1, Started: time.Now(), ctx: ctx, cancel: cancel}
	m.jobs[id] = j
	// pulizia dei job vecchi
	for k, o := range m.jobs {
		if o.Status != "running" && time.Since(o.Ended) > 2*time.Hour {
			delete(m.jobs, k)
		}
	}
	m.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				j.finish(nil, fmt.Errorf("errore interno: %v", r))
			}
		}()
		res, err := fn(j)
		j.finish(res, err)
	}()
	return j
}

func (j *Job) finish(res any, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.Status != "running" {
		return
	}
	j.Ended = time.Now()
	if err != nil {
		if j.ctx.Err() != nil {
			j.Status = "cancelled"
			j.Error = "annullato"
		} else {
			j.Status = "error"
			j.Error = err.Error()
		}
		return
	}
	j.Status = "done"
	j.Progress = 1
	j.Result = res
}

func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func (m *Manager) Cancel(id string) bool {
	j := m.Get(id)
	if j == nil {
		return false
	}
	j.cancel()
	return true
}

func (j *Job) Context() context.Context { return j.ctx }
func (j *Job) Cancelled() bool          { return j.ctx.Err() != nil }

func (j *Job) SetProgress(p float64, step string) {
	j.mu.Lock()
	j.Progress = p
	if step != "" {
		j.Step = step
	}
	j.mu.Unlock()
}

func (j *Job) SetStep(step string) { j.SetProgress(j.Progress, step) }

func (j *Job) Logf(format string, a ...any) {
	j.mu.Lock()
	j.Log = append(j.Log, fmt.Sprintf(format, a...))
	if len(j.Log) > 400 {
		j.Log = j.Log[len(j.Log)-400:]
	}
	j.mu.Unlock()
}

// Snapshot: copia sicura per la serializzazione JSON.
func (j *Job) Snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	lg := make([]string, len(j.Log))
	copy(lg, j.Log)
	return map[string]any{"id": j.ID, "name": j.Name, "status": j.Status, "progress": j.Progress, "step": j.Step, "log": lg, "result": j.Result, "error": j.Error}
}
