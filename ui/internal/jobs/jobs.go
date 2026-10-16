// Package jobs tracks the long-running work a page starts and watches: a
// render, a template download, a list resolve, a batch run. A job keeps every
// event it emits, numbered, so a watcher that attaches late replays what it
// missed
package jobs

import (
	"context"
	"image"
	"strconv"
	"sync"
	"time"

	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/batch"
	"github.com/odevine/mimic/ui/internal/cardlist"
)

// ttl is how long a finished job is kept so a slightly late watcher can still
// read its events and image before it is reaped
const ttl = 10 * time.Minute

// Event is one message in a job's progress stream. Step and Frac drive a
// progress bar, and the terminal event sets Done, with ArtMissing on a render
// whose art could not be fetched or Err on a failure. A list resolve carries one
// resolved Row per event and a batch run carries one Card update, so a single
// stream reports on every item in the job. Seq numbers a job's events from 1
type Event struct {
	Seq        int                `json:"seq"`
	Step       string             `json:"step,omitempty"`
	Frac       float64            `json:"frac"`
	Done       bool               `json:"done,omitempty"`
	ArtMissing bool               `json:"artMissing,omitempty"`
	Err        string             `json:"error,omitempty"`
	Row        *cardlist.Resolved `json:"row,omitempty"`
	Card       *batch.Card        `json:"card,omitempty"`
	Log        string             `json:"log,omitempty"`
	// Warmup and WarmupCards ride on the event of a batch run that ends its
	// warm-up, in milliseconds and cards, and are zero on every other
	Warmup      int64 `json:"warmupMs,omitempty"`
	WarmupCards int   `json:"warmupCards,omitempty"`
	// Imported rides on the done event of a template import that succeeded
	Imported *Imported `json:"imported,omitempty"`
}

// Imported is the template an import installed, and the notes the conversion
// made about what it left out or draws differently
type Imported struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Notes   []string `json:"notes"`
}

// Emitter receives every event of every job as it is emitted. A transport
// implements it to push events to the page
type Emitter interface {
	Emit(jobID string, e Event)
}

// Job is one unit of work in flight. It records every event so a watcher that
// attaches a moment late still replays from the start, and wakes waiting
// watchers as events arrive. A render job also holds its finished image and the
// card name for the save filename
type Job struct {
	id  string
	reg *Registry

	mu       sync.Mutex
	events   []Event
	finished bool
	waiters  []chan struct{}
	// sinkMu orders Emit from numbering an event to handing it on
	sinkMu  sync.Mutex
	img     image.Image
	name    string
	created time.Time
	cancel  context.CancelFunc
	// pinned keeps the job out of the reaper, for a run that stays the latest
	// until the next replaces it
	pinned bool
}

// ID is the id the page uses to name the job
func (j *Job) ID() string { return j.id }

// Emit numbers and records an event, wakes watchers and hands it to the
// registry's emitter. A Done event marks the job finished, which ends every
// stream after the backlog drains
func (j *Job) Emit(e Event) {
	// The event is numbered and handed to the sink under one lock, so two
	// goroutines emitting at once reach the sink in the order of their numbers.
	// The page applies an event only when its number is above the last it applied,
	// so one that arrived late would be dropped
	j.sinkMu.Lock()
	defer j.sinkMu.Unlock()
	j.mu.Lock()
	e.Seq = len(j.events) + 1
	j.events = append(j.events, e)
	if e.Done {
		j.finished = true
	}
	for _, w := range j.waiters {
		close(w)
	}
	j.waiters = nil
	j.mu.Unlock()

	if sink := j.reg.sinkOrNil(); sink != nil {
		sink.Emit(j.id, e)
	}
}

// SetResult records a finished render's image and card name
func (j *Job) SetResult(img image.Image, name string) {
	j.mu.Lock()
	j.img, j.name = img, name
	j.mu.Unlock()
}

// Result returns the finished image and card name, or a nil image when the job
// has none yet
func (j *Job) Result() (image.Image, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.img, j.name
}

// SetCancel records how to stop the job's work. Jobs that cannot be stopped this
// way never set it
func (j *Job) SetCancel(cancel context.CancelFunc) {
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
}

// Cancel stops the job's work, and does nothing on a job that cannot be stopped
func (j *Job) Cancel() {
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Backlog returns the events numbered after seq, and whether the job has
// finished. Pass 0 for everything
func (j *Job) Backlog(seq int) ([]Event, bool) {
	events, finished, _ := j.Stream(seq)
	return events, finished
}

// Stream returns the events after index from, whether the job has finished, and
// a channel that closes when more events arrive. The caller writes the returned
// events, then either returns (finished) or waits on the channel before calling
// again. This replays the backlog to a late watcher and blocks only between
// events
func (j *Job) Stream(from int) (events []Event, finished bool, wait chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if from < 0 || from > len(j.events) {
		from = len(j.events)
	}
	events = append([]Event(nil), j.events[from:]...)
	if j.finished {
		return events, true, nil
	}
	wait = make(chan struct{})
	j.waiters = append(j.waiters, wait)
	return events, false, wait
}

// Registry holds the jobs in flight and the recently finished
type Registry struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	nextID uint64
	sink   Emitter
}

// NewRegistry returns an empty registry
func NewRegistry() *Registry { return &Registry{jobs: make(map[string]*Job)} }

// SetEmitter sets where every job's events are forwarded, or nil for nowhere
func (r *Registry) SetEmitter(e Emitter) {
	r.mu.Lock()
	r.sink = e
	r.mu.Unlock()
}

func (r *Registry) sinkOrNil() Emitter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sink
}

// New creates and registers a job, reaping any finished jobs past their TTL so
// the registry does not grow unbounded
func (r *Registry) New() *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reap()
	r.nextID++
	return r.add(strconv.FormatUint(r.nextID, 10), false)
}

// NewNamed creates a job under an id the caller chose, such as a run's own. The
// job stays until Forget, so the page can still replay the latest run however
// long ago it finished
func (r *Registry) NewNamed(id string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reap()
	return r.add(id, true)
}

func (r *Registry) add(id string, pinned bool) *Job {
	j := &Job{id: id, reg: r, created: time.Now(), pinned: pinned}
	r.jobs[id] = j
	return j
}

// reap drops finished jobs older than the TTL. The caller holds r.mu
func (r *Registry) reap() {
	now := time.Now()
	for id, j := range r.jobs {
		j.mu.Lock()
		expired := j.finished && !j.pinned && now.Sub(j.created) > ttl
		j.mu.Unlock()
		if expired {
			delete(r.jobs, id)
		}
	}
}

// Forget drops a job
func (r *Registry) Forget(id string) {
	r.mu.Lock()
	delete(r.jobs, id)
	r.mu.Unlock()
}

// Lookup returns the job with the given id
func (r *Registry) Lookup(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	return j, ok
}

// Service is the bound service the page calls to catch up on a job
type Service struct{ reg *Registry }

// NewService returns the service over a registry
func NewService(r *Registry) *Service { return &Service{reg: r} }

// Backlog returns the events of a job numbered after seq and whether it has
// finished, so a page that registered its listener late or reloaded misses
// nothing
func (s *Service) Backlog(id string, seq int) (Backlog, error) {
	j, ok := s.reg.Lookup(id)
	if !ok {
		return Backlog{}, apierr.New(apierr.NotFound, "no such job")
	}
	events, finished := j.Backlog(seq)
	return Backlog{Events: events, Finished: finished}, nil
}

// Backlog is the answer to Service.Backlog
type Backlog struct {
	Events   []Event `json:"events"`
	Finished bool    `json:"finished"`
}
