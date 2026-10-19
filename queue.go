package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type printTarget struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// printer may be "Name", {"name":..} or {"host":"192.168.1.50","port":9100}
func (t *printTarget) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		t.Name = s
		return nil
	}
	type alias printTarget
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*t = printTarget(a)
	return nil
}

func targetKey(t printTarget) string {
	if t.Host != "" {
		p := t.Port
		if p == 0 {
			p = 9100
		}
		return fmt.Sprintf("net:%s:%d", t.Host, p)
	}
	return "win:" + strings.ToLower(t.Name)
}

// sendNow does a single immediate attempt (no queue).
func sendNow(t printTarget, job string, data []byte) error {
	if t.Host != "" {
		return rawPrintNetwork(t.Host, t.Port, data)
	}
	return rawPrintWindows(t.Name, job, data)
}

type Job struct {
	ID        string      `json:"id"`
	Target    printTarget `json:"target"`
	Name      string      `json:"name"`
	Data      []byte      `json:"data"`
	Attempts  int         `json:"attempts"`
	Created   time.Time   `json:"created"`
	NextTry   time.Time   `json:"next_try"`
	LastError string      `json:"last_error"`
	State     string      `json:"state"` // queued | printing | printed | failed | cancelled

	done     chan struct{}
	doneOnce sync.Once
}

func (j *Job) summary() map[string]any {
	return map[string]any{
		"id": j.ID, "state": j.State, "attempts": j.Attempts, "error": j.LastError,
		"printer": targetKey(j.Target), "name": j.Name, "created": j.Created,
	}
}

// Queue keeps jobs on disk, prints them in order per printer, and retries
// while the printer is off, out of paper, has the cover open, etc.
type Queue struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	history []*Job
	busy    map[string]bool
	kick    chan struct{}
	emit    func(map[string]any)
}

var queue = &Queue{jobs: map[string]*Job{}, busy: map[string]bool{}, kick: make(chan struct{}, 1)}

func (q *Queue) dir() string {
	d := filepath.Join(dataDir(), "queue")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func (q *Queue) persist(j *Job) {
	b, _ := json.Marshal(j)
	_ = os.WriteFile(filepath.Join(q.dir(), j.ID+".json"), b, 0o644)
}

func (q *Queue) forget(j *Job) { _ = os.Remove(filepath.Join(q.dir(), j.ID+".json")) }

func (q *Queue) wake() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

func (q *Queue) pushHistory(j *Job) {
	q.history = append(q.history, j)
	if len(q.history) > 50 {
		q.history = q.history[len(q.history)-50:]
	}
}

func (q *Queue) load() {
	entries, _ := os.ReadDir(q.dir())
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(q.dir(), e.Name()))
		if err != nil {
			continue
		}
		var j Job
		if json.Unmarshal(b, &j) != nil || j.ID == "" {
			continue
		}
		j.State = "queued"
		j.done = make(chan struct{})
		j.doneOnce.Do(func() { close(j.done) })
		q.jobs[j.ID] = &j
	}
}

func (q *Queue) Pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// Submit enqueues a job and waits for the first attempt (max 8 s).
func (q *Queue) Submit(t printTarget, name string, data []byte) map[string]any {
	if name == "" {
		name = appName + " ticket"
	}
	j := &Job{
		ID: newNonce()[:12], Target: t, Name: name, Data: data,
		Created: time.Now(), State: "queued", done: make(chan struct{}),
	}
	q.mu.Lock()
	q.jobs[j.ID] = j
	q.mu.Unlock()
	q.persist(j)
	q.wake()
	select {
	case <-j.done:
	case <-time.After(8 * time.Second):
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return j.summary()
}

func (q *Queue) Start() {
	go func() {
		t := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-t.C:
			case <-q.kick:
			}
			q.dispatch()
		}
	}()
}

func (q *Queue) dispatch() {
	now := time.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	oldest := map[string]*Job{}
	for _, j := range q.jobs {
		if j.State != "queued" {
			continue
		}
		k := targetKey(j.Target)
		if cur, ok := oldest[k]; !ok || j.Created.Before(cur.Created) {
			oldest[k] = j
		}
	}
	for k, j := range oldest {
		if q.busy[k] || j.NextTry.After(now) {
			continue
		}
		q.busy[k] = true
		j.State = "printing"
		go q.attempt(k, j)
	}
}

func (q *Queue) attempt(key string, j *Job) {
	st := printerStatus(j.Target)
	var err error
	if blocking(st) {
		err = fmt.Errorf("%s", statusText(st))
	} else {
		err = sendNow(j.Target, j.Name, j.Data)
	}

	q.mu.Lock()
	j.Attempts++
	finished := false
	if err == nil {
		j.State, j.LastError = "printed", ""
		finished = true
	} else {
		j.LastError = err.Error()
		if time.Since(j.Created) > time.Duration(cfg.MaxAgeHours)*time.Hour {
			j.State = "failed"
			finished = true
		} else {
			j.State = "queued"
			j.NextTry = time.Now().Add(time.Duration(cfg.RetryEverySeconds) * time.Second)
		}
	}
	if finished {
		delete(q.jobs, j.ID)
		q.pushHistory(j)
	}
	delete(q.busy, key)
	ev := map[string]any{"event": "job", "id": j.ID, "state": j.State, "error": j.LastError, "attempts": j.Attempts, "printer": key}
	q.mu.Unlock()

	if finished {
		q.forget(j)
	} else {
		q.persist(j)
	}
	j.doneOnce.Do(func() { close(j.done) })
	if q.emit != nil {
		q.emit(ev)
	}
	q.wake()
}

func (q *Queue) List() []map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []map[string]any{}
	for _, j := range q.jobs {
		out = append(out, j.summary())
	}
	for _, j := range q.history {
		out = append(out, j.summary())
	}
	return out
}

func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	j, ok := q.jobs[id]
	if !ok || j.State != "queued" {
		q.mu.Unlock()
		return false
	}
	j.State = "cancelled"
	delete(q.jobs, id)
	q.pushHistory(j)
	q.mu.Unlock()
	q.forget(j)
	return true
}

func (q *Queue) RetryNow(id string) bool {
	q.mu.Lock()
	j, ok := q.jobs[id]
	if ok && j.State == "queued" {
		j.NextTry = time.Time{}
	}
	q.mu.Unlock()
	q.wake()
	return ok
}
