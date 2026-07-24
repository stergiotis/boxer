package play

// Tests for the tier-aware PARAMETERS pane (ADR-0124's 2026-07-22 §SD4
// amendment, ADR-0097's same-day Update): a name the buffer SET-binds is
// PINNED and its drift rebuilds the prelude, a name without a SET is LIVE and
// its drift is a `param-widget` store write. The pane's phases are UI-free
// (renderParamSlots only dispatches), so the value path is exercised directly:
// refreshParamSlotsFromParse for phase 1, a draft assignment standing in for a
// widget's phase-2 mutation, syncParamDriftToPrelude for phase 3.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// paneApp builds an app whose pane state reflects sql, as the debounced parse
// would leave it, with a frame snapshot taken.
func paneApp(t *testing.T, sql string) *PlayApp {
	t.Helper()
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.sql = sql
	app.formattedFor = sql
	slots, vals, err := extractSlotsAndParams(sql)
	require.NoError(t, err)
	app.refreshParamSlotsFromParse(slots, vals)
	app.frameSig = app.graph.signals()
	return app
}

// reparse re-runs the debounced parse against the current buffer — the frame
// after a buffer rewrite, once the debounce has caught up.
func reparse(t *testing.T, app *PlayApp) {
	t.Helper()
	slots, vals, err := extractSlotsAndParams(app.sql)
	require.NoError(t, err)
	app.refreshParamSlotsFromParse(slots, vals)
	app.frameSig = app.graph.signals()
}

func heldSignal(t *testing.T, app *PlayApp, name string) signalRow {
	t.Helper()
	for _, r := range app.graph.signalRows() {
		if r.Name == name {
			return r
		}
	}
	require.Failf(t, "signal not held", "no signal %q in the store", name)
	return signalRow{}
}

// The live tier: a widget's drift on a name with no SET is a store write with
// `param-widget` provenance, and the buffer is not touched — the flipped fill
// default. Filling a picker no longer pins it.
func TestLiveParamDriftWritesStoreNotBuffer(t *testing.T) {
	sql := "SELECT * FROM t WHERE q = {q:String}"
	app := paneApp(t, sql)
	require.False(t, app.paramPinned("q"), "no SET ⇒ live")

	*app.paramDrafts["q"] = "abc" // phase 2: the widget's write
	app.syncParamDriftToPrelude()

	require.Equal(t, sql, app.sql, "a live name's drift leaves the buffer byte-identical")
	row := heldSignal(t, app, "q")
	require.Equal(t, "abc", row.Raw)
	require.Equal(t, signalWriterParamWidget, row.Writer,
		"provenance distinguishes the typed pane from the raw Signals editor")

	// Idempotent: a settled draft writes nothing more, so no revision churn.
	rev := app.graph.signals().Revision()
	app.syncParamDriftToPrelude()
	require.Equal(t, rev, app.graph.signals().Revision(), "an unmoved draft is quiet")
}

// The pinned tier is unchanged: drift on a SET-bound name rebuilds the
// prelude through the same path as before and never reaches the store.
func TestPinnedParamDriftAuthorsPreludeOnly(t *testing.T) {
	app := paneApp(t, "SET param_q = 'x';\nSELECT {q:String}")
	require.True(t, app.paramPinned("q"), "a SET ⇒ pinned")

	*app.paramDrafts["q"] = "y"
	app.syncParamDriftToPrelude()

	require.Equal(t, "SET param_q = 'y';\nSELECT {q:String}", app.sql)
	require.Empty(t, app.graph.signalRows(), "a pinned name's drift never touches the store")
}

// A buffer whose prelude binds every slot behaves exactly as it did before
// the amendment: every drift authors its SET, nothing is written live, and
// slots that did not move keep their lines.
func TestFullyBoundBufferIsBehaviourIdentical(t *testing.T) {
	app := paneApp(t, "SET param_a = 'p';\nSET param_b = 2;\nSELECT {a:String}, {b:UInt64}")

	*app.paramDrafts["a"] = "q"
	app.syncParamDriftToPrelude()

	require.Equal(t, "SET param_a = 'q';\nSET param_b = 2;\nSELECT {a:String}, {b:UInt64}", app.sql,
		"the moved slot re-authors; the untouched one keeps its line and its encoding")
	require.Empty(t, app.graph.signalRows())
	require.False(t, app.hasUnboundSlots(), "a fully bound buffer offers no Live toggle")
}

// Mixed tiers in one buffer: the pinned half authors its SET, the live half
// writes the store, and neither leaks into the other.
func TestMixedTierBufferForksBothWays(t *testing.T) {
	app := paneApp(t, "SET param_a = 'p';\nSELECT {a:String}, {b:UInt64}")

	*app.paramDrafts["a"] = "q"
	*app.paramDrafts["b"] = "7"
	app.syncParamDriftToPrelude()

	require.Equal(t, "SET param_a = 'q';\nSELECT {a:String}, {b:UInt64}", app.sql,
		"the live name gets no SET authored for it")
	require.Equal(t, "7", heldSignal(t, app, "b").Raw)
	require.Len(t, app.graph.signalRows(), 1, "only the live name is in the store")
}

// A live draft is born from the store, so a name a panel already publishes
// shows its value the first frame its widget appears — and that seed does not
// then read as drift and write the empty draft back over it.
func TestLiveDraftBornFromStore(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.graph.setSignalRawFrom("tl_min", "2026-01-01 00:00:00", signalWriterApp)
	app.sql = "SELECT {tl_min:DateTime64(3, 'UTC')}"
	reparse(t, app)

	require.Equal(t, "2026-01-01 00:00:00", *app.paramDrafts["tl_min"],
		"the widget opens on the published value, not an empty field")

	rev := app.graph.signals().Revision()
	app.syncParamDriftToPrelude()
	require.Equal(t, rev, app.graph.signals().Revision(),
		"the seeded draft is not drift — the pane must not clobber the publisher")
	require.Equal(t, "2026-01-01 00:00:00", heldSignal(t, app, "tl_min").Raw)
}

// An unfilled live name is left unfilled: an untouched empty draft is not
// drift, so it does not fill the store with "" and quietly unblock a Run that
// D3 means to block.
func TestUntouchedLiveDraftDoesNotSeedEmpty(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}, {lim:UInt64}")
	app.syncParamDriftToPrelude()
	require.Empty(t, app.graph.signalRows(), "nothing typed ⇒ nothing written")
	require.Equal(t, []string{"q", "lim"}, app.unfilledInputs(), "both still block the Run")
}

// The staleness witness stays in lockstep with Run resolution across the
// fork: a live widget write diverges the shown result from its inputs exactly
// as a panel write does, and reverting the value clears it.
func TestLiveWidgetWriteFlipsStalenessWitness(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}")
	app.lastSentSql = app.sql
	app.graph.setSignalRawFrom("q", "abc", signalWriterParamWidget)
	app.paramLiveSeeded["q"] = "abc"
	*app.paramDrafts["q"] = "abc"
	app.frameSig = app.graph.signals()

	sig, _, _ := app.resolveRunSignals(app.sql)
	app.lastSentSigParams = sig // the Run shipped q=abc
	require.False(t, app.runSignalsDiverged())

	*app.paramDrafts["q"] = "def" // the user types in the pane
	app.syncParamDriftToPrelude()
	app.frameSig = app.graph.signals()
	require.True(t, app.runSignalsDiverged(), "a live widget write makes the result stale")

	*app.paramDrafts["q"] = "abc" // …and types it back
	app.syncParamDriftToPrelude()
	app.frameSig = app.graph.signals()
	require.False(t, app.runSignalsDiverged(), "reverting clears it, symmetric with a buffer edit")
}

// Deleting a SET line by hand is the same gesture as unpinning: the next
// parse drops the name from the prelude mirror, so its tier flips to live and
// the pane stops re-authoring the line it just lost.
func TestHandDeletedSetFlipsTierToLive(t *testing.T) {
	app := paneApp(t, "SET param_q = 'x';\nSELECT {q:String}")
	require.True(t, app.paramPinned("q"))

	app.sql = "SELECT {q:String}" // the user deletes the prelude line
	reparse(t, app)
	require.False(t, app.paramPinned("q"), "no SET ⇒ live")

	*app.paramDrafts["q"] = "z"
	app.syncParamDriftToPrelude()
	require.Equal(t, "SELECT {q:String}", app.sql, "the deleted SET is not re-authored")
	require.Equal(t, "z", heldSignal(t, app, "q").Raw)
}

// An idle live draft follows the store: the Timeline publishes an extent and
// the pane's picker shows it on the next frame, without the pane writing
// anything back.
func TestIdleLiveDraftFollowsStore(t *testing.T) {
	app := paneApp(t, "SELECT {tl_min:DateTime64(3, 'UTC')}")
	*app.paramDrafts["tl_min"] = "" // never touched

	app.graph.setSignalRawFrom("tl_min", "2026-03-01 00:00:00", "timeline")
	app.frameSig = app.graph.signals()
	app.syncLiveParamDrafts()

	require.Equal(t, "2026-03-01 00:00:00", *app.paramDrafts["tl_min"],
		"an idle draft follows an external write")

	rev := app.graph.signals().Revision()
	app.syncParamDriftToPrelude()
	require.Equal(t, rev, app.graph.signals().Revision(), "following is not drift")
	require.Equal(t, "timeline", heldSignal(t, app, "tl_min").Writer,
		"the publisher keeps its provenance — the pane did not rewrite the value")
}

// A draft the user has moved since the last agreed value survives a
// simultaneous external write, and the pane's write is the one that lands —
// last-writer-wins, with `param-widget` provenance saying so.
func TestMidEditLiveDraftSurvivesExternalWrite(t *testing.T) {
	app := paneApp(t, "SELECT {tl_min:DateTime64(3, 'UTC')}")
	app.graph.setSignalRawFrom("tl_min", "2026-01-01 00:00:00", "timeline")
	app.frameSig = app.graph.signals()
	app.syncLiveParamDrafts() // draft and store agree

	// The same frame: the user types, and the Timeline republishes.
	*app.paramDrafts["tl_min"] = "2026-06-06 06:06:06"
	app.graph.setSignalRawFrom("tl_min", "2026-02-02 00:00:00", "timeline")
	app.frameSig = app.graph.signals()

	app.syncLiveParamDrafts()
	require.Equal(t, "2026-06-06 06:06:06", *app.paramDrafts["tl_min"],
		"typing wins — the uncommitted edit is not torn out from under the user")

	app.syncParamDriftToPrelude()
	row := heldSignal(t, app, "tl_min")
	require.Equal(t, "2026-06-06 06:06:06", row.Raw)
	require.Equal(t, signalWriterParamWidget, row.Writer)
}

// A settled co-writer — a panel re-emitting the value it already published —
// causes no draft churn, because the store dedups identical re-sets and the
// reseed guard keys on the value, not on the write.
func TestSettledCoWriterCausesNoDraftChurn(t *testing.T) {
	app := paneApp(t, "SELECT {tl_min:DateTime64(3, 'UTC')}")
	app.graph.setSignalRawFrom("tl_min", "2026-01-01 00:00:00", "timeline")
	app.frameSig = app.graph.signals()
	app.syncLiveParamDrafts()

	rev := app.graph.signals().Revision()
	for range 3 {
		app.graph.setSignalRawFrom("tl_min", "2026-01-01 00:00:00", "timeline")
		app.frameSig = app.graph.signals()
		app.syncLiveParamDrafts()
		app.syncParamDriftToPrelude()
	}
	require.Equal(t, rev, app.graph.signals().Revision(), "a settled pair is quiet")
	require.Equal(t, "2026-01-01 00:00:00", *app.paramDrafts["tl_min"])
}

// A pinned draft is the parser's; the reseed pass must not touch it, or a
// same-named signal would fight the prelude the user authored.
func TestReseedLeavesPinnedDraftsAlone(t *testing.T) {
	app := paneApp(t, "SET param_q = 'buffer';\nSELECT {q:String}")
	app.graph.setSignalRawFrom("q", "store", signalWriterEditor)
	app.frameSig = app.graph.signals()

	app.syncLiveParamDrafts()
	require.Equal(t, "buffer", *app.paramDrafts["q"], "a SET-bound draft follows the buffer, not the store")
}
