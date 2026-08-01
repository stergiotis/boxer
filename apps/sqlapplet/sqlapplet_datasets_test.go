package sqlapplet

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// bindOK / bindFails stand in for PlayApp.BindDataset, which is exercised
// against a real instance in the book test.
func bindOK(string, string) error { return nil }

func bindFails(string, string) error { return eh.Errorf("rejected") }

// TestRenderDatasetNotice pins the text the applet shows over empty panes:
// the alias, because that is what the failing query and the catalog name,
// and the author's hint, because nothing else can say how to produce it.
func TestRenderDatasetNotice(t *testing.T) {
	assert.Nil(t, renderDatasetNotice(nil, "hint"), "a cleared condition renders nothing")

	one := string(renderDatasetNotice([]string{"pprof_cpu"}, "Capture one from imzrt → Profiles → Capture CPU."))
	assert.Contains(t, one, "Waiting for dataset `pprof_cpu`")
	assert.Contains(t, one, "Capture one from imzrt")
	assert.Contains(t, one, "no need to reopen")

	many := string(renderDatasetNotice([]string{"a", "b"}, ""))
	assert.Contains(t, many, "Waiting for datasets `a`, `b`")
}

// TestNewDatasetRebinderAbsent covers the cases that must not allocate a
// rebinder: nothing missed, or no bus to retry against.
func TestNewDatasetRebinderAbsent(t *testing.T) {
	assert.Nil(t, newDatasetRebinder(&app.NoopBus{}, zerolog.Nop(), "", nil))
	assert.Nil(t, newDatasetRebinder(nil, zerolog.Nop(), "", []string{"items"}))
}

// TestDatasetRebinderNoticeHandoff drives the render-thread half: the notice
// is pushed once and not again, and the rebinder stays pending while nothing
// resolves.
func TestDatasetRebinderNoticeHandoff(t *testing.T) {
	reb := newDatasetRebinder(&app.NoopBus{}, zerolog.Nop(), "Capture it.", []string{"pprof_cpu"})
	require.NotNil(t, reb)

	// The first sync hands over the notice built at construction, so the
	// window explains itself on the frame after Mount rather than only once
	// a resolve attempt has come back.
	bound, notice, changed, done := reb.sync(bindOK)
	assert.False(t, bound)
	assert.True(t, changed, "the first frame must push the notice")
	assert.Contains(t, string(notice), "`pprof_cpu`")
	assert.Contains(t, string(notice), "Capture it.")
	assert.False(t, done)

	// Re-set only on change: SetDatasetNotice reparses, and sync runs every
	// frame.
	_, _, changed, done = reb.sync(bindOK)
	assert.False(t, changed, "an unchanged notice must not be re-pushed")
	assert.False(t, done)
}

// TestDatasetRebinderBackoff pins that construction holds the first retry off
// for an interval — the caller has just asked and missed — and that sync does
// not stack a second resolve on an in-flight one.
func TestDatasetRebinderBackoff(t *testing.T) {
	reb := newDatasetRebinder(&app.NoopBus{}, zerolog.Nop(), "", []string{"items"})
	require.NotNil(t, reb)

	reb.mu.Lock()
	notYet := time.Now().Before(reb.nextAt)
	reb.nextAt = time.Time{}
	reb.inFlight = true // pretend a worker is out
	reb.mu.Unlock()
	assert.True(t, notYet, "the open-time miss already asked; the first retry waits an interval")

	reb.sync(bindOK)

	reb.mu.Lock()
	stillOne := reb.inFlight
	reb.mu.Unlock()
	assert.True(t, stillOne, "sync must not stack a second resolve on an in-flight one")
}

// TestDatasetRebinderSettles walks a resolved alias through the render-thread
// handoff, including the bind-rejected branch: a bad handle is the service's
// problem, not a transient one, so it must settle rather than spin forever.
func TestDatasetRebinderSettles(t *testing.T) {
	reb := newDatasetRebinder(&app.NoopBus{}, zerolog.Nop(), "", []string{"items"})
	require.NotNil(t, reb)
	_, _, _, done := reb.sync(bindFails)
	require.False(t, done, "nothing has resolved yet")

	reb.mu.Lock()
	reb.resolved = map[string]string{"items": "adhoc_deadbeef01234567"}
	reb.mu.Unlock()

	bound, notice, changed, done := reb.sync(bindFails)
	assert.False(t, bound, "a rejected bind is not a bind")
	assert.True(t, changed)
	assert.Nil(t, notice, "with nothing pending the notice clears")
	assert.True(t, done, "a settled alias drops the rebinder")

	// The happy branch reports the bind, which is what makes the caller
	// re-run the buffer.
	reb = newDatasetRebinder(&app.NoopBus{}, zerolog.Nop(), "", []string{"items"})
	require.NotNil(t, reb)
	reb.mu.Lock()
	reb.resolved = map[string]string{"items": "adhoc_deadbeef01234567"}
	reb.mu.Unlock()
	bound, notice, _, done = reb.sync(bindOK)
	assert.True(t, bound)
	assert.Nil(t, notice)
	assert.True(t, done)
}
