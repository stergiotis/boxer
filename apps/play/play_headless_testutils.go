package play

// Test support for lanes outside this package that drive a play window
// through its operations with no client process attached — the agent
// dispatcher's end-to-end test of ADR-0288 M5 among them. It is
// production code by Go's rules (a _test.go symbol is invisible to other
// packages), so it holds only what such a lane needs and nothing a window
// uses.

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// NewHeadlessLauncherForTest builds a play window that is mounted as far as
// its operations need — a buffer, a query graph, a client on url, the
// window's bus — and draws nothing. Its frames are the caller's: the
// operations host applies queued commands, and SyncForTest runs the
// window's own per-frame work.
func NewHeadlessLauncherForTest(bus app.BusI, log zerolog.Logger, url string) (inst *PlayLauncher) {
	inner := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	inner.client = NewClient(ClientConfig{URL: url}, nil)
	return &PlayLauncher{inner: inner, bus: bus, log: log}
}

// SyncForTest is the window's frame minus the drawing: the bundle it
// follows and the dataset aliases its follower keeps bound.
func (inst *PlayLauncher) SyncForTest() {
	inst.syncBundle()
	if inst.follower != nil {
		inst.follower.Sync(inst.inner)
	}
}

// SetMainResultForTest seats rec as the window's main result, as a run of
// sql that finished whole; the window takes ownership of rec.
func (inst *PlayLauncher) SetMainResultForTest(rec arrow.RecordBatch, sql string) {
	inst.inner.graph.mainLane.finish(sql, nil, time.Now(), rec, rec.Schema(), rec.NumRows(), Summary{}, nil, runstream.Terminal{})
}

// DatasetBindingsForTest is the window's bound names and their handles.
func (inst *PlayLauncher) DatasetBindingsForTest() (bindings map[string]string) {
	return inst.inner.DatasetBindingsForTest()
}

// LastPublishForTest is the window's last publish_result.
func (inst *PlayLauncher) LastPublishForTest() (last LastPublish) { return inst.lastPublish() }

// BufferForTest is the window's SQL buffer.
func (inst *PlayLauncher) BufferForTest() (sql string) { return inst.inner.sql }

// DatasetBindingsForTest is an instance's bound names and their handles.
func (inst *PlayApp) DatasetBindingsForTest() (bindings map[string]string) {
	inst.client.mu.RLock()
	defer inst.client.mu.RUnlock()
	bindings = make(map[string]string, len(inst.client.datasetBindings))
	for k, v := range inst.client.datasetBindings {
		bindings[k] = v
	}
	return
}

// BufferForTest is an instance's SQL buffer.
func (inst *PlayApp) BufferForTest() (sql string) { return inst.sql }

// RunRequestedForTest says a run is queued for the next frame.
func (inst *PlayApp) RunRequestedForTest() (requested bool) { return inst.requestRun }

// OpenPlaygroundBundleForTest is the bundle Open in Playground opens.
func (inst *PlayApp) OpenPlaygroundBundleForTest() (alias string) { return inst.openPlaygroundBundle }
