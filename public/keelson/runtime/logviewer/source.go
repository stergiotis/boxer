package logviewer

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// Entry is one log row as the viewer reads it back from boxer.facts: the
// row, and the run it was stamped with (ADR-0191 §SD3).
type Entry struct {
	Row   factsstore.LogRow
	RunId string
}

// Query is what one refresh asks of the store. Every filter is applied by
// the store; the zero value of a field is "no constraint".
type Query struct {
	// Since bounds the look-back.
	Since time.Time
	// Levels keeps rows whose level is one of these; empty keeps every row.
	Levels []string
	// App and Message are case-insensitive substrings.
	App     string
	Message string
	RunId   string
	// InstanceKey keeps one window's rows; 0 keeps every window's.
	InstanceKey uint64
	Limit       int
}

// ReadFunc returns the rows matching q, newest first. It runs on the
// viewer's background refresh, never on the render goroutine.
type ReadFunc func(ctx context.Context, q Query) (rows []Entry, err error)

// Source is what the host hands the viewer: where log rows are read from
// and the bridge whose counters the strip shows. A host whose log lines do
// not reach boxer.facts leaves Read nil and says why in NotPersisted.
type Source struct {
	// Sink is the bridge writing log rows; nil when the host installed
	// none, and then the counters read zero.
	Sink *logbridge.Sink
	// Read reads rows back from boxer.facts; nil when log lines are not
	// persisted.
	Read ReadFunc
	// Run returns this process's run, the default scope of a window. It
	// is read at each refresh, not when the source is registered: the
	// host adopts its run after the bridge is installed. nil, or "",
	// means the run is not known yet, and a This run scope then reads
	// every run.
	Run func() string
	// NotPersisted is why Read is nil, as the window's one line says it.
	NotPersisted string
}

func (inst *Source) persisted() bool { return inst != nil && inst.Read != nil }

// run is the source's current run; "" when unknown.
func (inst *Source) run() (runId string) {
	if inst != nil && inst.Run != nil {
		runId = inst.Run()
	}
	return
}

// sourceRef holds the host-registered source. The host writes it once
// during start-up; every frame and every refresh reads it.
var sourceRef atomic.Pointer[Source]

// Register hands the viewer its source. Call it when the log bridge is
// installed, before the first frame of any viewer window; a later call
// replaces the source for every window, and nil leaves the viewer with
// none, which it reports as not persisted.
func Register(s *Source) {
	sourceRef.Store(s)
}

// notPersistedReason is what a window and a refused operation say when no
// source reads boxer.facts.
func notPersistedReason(src *Source) (reason string) {
	why := "the host did not install the log bridge"
	if src != nil && src.NotPersisted != "" {
		why = src.NotPersisted
	}
	reason = "log lines are not persisted on this host (" + why + "); set BOXER_LOG_FACTS=1 with a reachable ClickHouse to read them here"
	return
}
