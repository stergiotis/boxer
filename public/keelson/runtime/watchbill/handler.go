package watchbill

import (
	"context"
	"sort"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// HandlerI runs the jobs of one kind (ADR-0223 §SD5). RunE receives the
// job row as claimed and the keelson task the run is reported through —
// Report and Note for progress, Ctx for the cancel — and returns the
// outcome: nil succeeds, an error fails the attempt under the job's
// policy, and a return after ctx is done is read as the cancel or the
// timeout it was.
//
// Delivery is at-least-once; a handler that was interrupted mid-way will
// be run again and must tolerate it.
type HandlerI interface {
	Kind() string
	RunE(ctx context.Context, job watchbillstore.Job, h task.HandleI) (err error)
}

// Registry maps a kind to its handler. A binary that links a consumer's
// package has that consumer's handler registered at init, the way apps
// register into app.DefaultRegistry; a worker takes the registry's kinds
// as the set it drains.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]HandlerI
}

// DefaultRegistry is the process-wide registry [Register] fills.
var DefaultRegistry = NewRegistry()

// NewRegistry returns an empty registry.
func NewRegistry() (inst *Registry) {
	inst = &Registry{handlers: make(map[string]HandlerI, 8)}
	return
}

// Register adds h under its kind; a second handler for one kind is
// refused rather than replacing the first.
func (inst *Registry) Register(h HandlerI) (err error) {
	if h == nil || h.Kind() == "" {
		return eh.Errorf("watchbill: nil handler or empty kind")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.handlers[h.Kind()]; dup {
		return eb.Build().Str("kind", h.Kind()).Errorf("watchbill: kind already has a handler")
	}
	inst.handlers[h.Kind()] = h
	return
}

// Lookup returns the handler for kind.
func (inst *Registry) Lookup(kind string) (h HandlerI, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	h, ok = inst.handlers[kind]
	return
}

// Kinds lists the registered kinds, sorted.
func (inst *Registry) Kinds() (kinds []string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	kinds = make([]string, 0, len(inst.handlers))
	for k := range inst.handlers {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return
}

// Register adds h to [DefaultRegistry].
func Register(h HandlerI) (err error) { return DefaultRegistry.Register(h) }

// HandlerFunc adapts a function to [HandlerI].
type HandlerFunc struct {
	KindName string
	Run      func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error
}

var _ HandlerI = HandlerFunc{}

func (inst HandlerFunc) Kind() (kind string) { return inst.KindName }

func (inst HandlerFunc) RunE(ctx context.Context, job watchbillstore.Job, h task.HandleI) (err error) {
	return inst.Run(ctx, job, h)
}
