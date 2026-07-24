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

// Pin authors the SET from the store value and flips the tier immediately —
// the frame after it must not write the value straight back into the store it
// just left.
func TestPinClaimAuthorsSetAndFlipsTier(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}")
	*app.paramDrafts["q"] = "abc"
	app.syncParamDriftToPrelude() // the live value reaches the store
	app.frameSig = app.graph.signals()

	app.pinParamClaim(app.paramSlots)

	require.Equal(t, "SET param_q = 'abc';\nSELECT {q:String}", app.sql)
	require.True(t, app.paramPinned("q"), "the tier flips now, not at the next parse")
	require.NotContains(t, app.paramLiveSeeded, "q", "the live baseline goes with the tier")
	require.Equal(t, "abc", heldSignal(t, app, "q").Raw,
		"the store keeps its value — a SET shadows it, it does not replace it")

	// The frame after: neither a re-author nor a store write.
	before := app.sql
	rev := app.graph.signals().Revision()
	app.syncLiveParamDrafts()
	app.syncParamDriftToPrelude()
	require.Equal(t, before, app.sql)
	require.Equal(t, rev, app.graph.signals().Revision())
}

// Unpin removes the SET, seeds the store with the same value, and moves the
// baseline — so the frame after neither re-authors nor tears the draft.
func TestUnpinClaimSeedsStoreAndDoesNotReAuthor(t *testing.T) {
	app := paneApp(t, "SET param_q = 'abc';\nSELECT {q:String}")
	require.True(t, app.paramPinned("q"))

	app.unpinParamClaim(app.paramSlots)

	require.Equal(t, "SELECT {q:String}", app.sql, "the SET line is gone")
	require.False(t, app.paramPinned("q"))
	require.Equal(t, "abc", heldSignal(t, app, "q").Raw, "the value survives the migration")
	require.Equal(t, signalWriterParamWidget, heldSignal(t, app, "q").Writer)
	require.Equal(t, "abc", *app.paramDrafts["q"], "the draft is not torn")

	before := app.sql
	rev := app.graph.signals().Revision()
	app.frameSig = app.graph.signals()
	app.syncLiveParamDrafts()
	app.syncParamDriftToPrelude()
	require.Equal(t, before, app.sql, "the removed SET is not re-authored")
	require.Equal(t, rev, app.graph.signals().Revision(), "and the store is not rewritten")
	require.Equal(t, "abc", *app.paramDrafts["q"])

	// …and it survives the debounced parse catching up with the new buffer.
	reparse(t, app)
	require.False(t, app.paramPinned("q"))
	require.Equal(t, "abc", *app.paramDrafts["q"])
}

// Unpinning restores the Live toggle: a fully SET-bound buffer has no unbound
// slots, which is the dead end the selection_country fix documented — the only
// affordance for "give this name a value" was the one that broke the reactive
// path.
func TestUnpinRestoresLiveToggle(t *testing.T) {
	app := paneApp(t, "SET param_q = 'abc';\nSELECT {q:String}")
	require.False(t, app.hasUnboundSlots(), "fully bound ⇒ no Live toggle")

	app.unpinParamClaim(app.paramSlots)
	require.True(t, app.hasUnboundSlots(), "unpinning gives the buffer a signal input again")
}

// A folded pair migrates as a unit: one buffer rewrite carries both SETs, and
// both live values are seeded in the same frame, so every consumer sees the
// range move at one snapshot.
func TestPinUnpinFoldedPairIsBundleAtomic(t *testing.T) {
	app := paneApp(t, "SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}")
	*app.paramDrafts["tl_min"] = "2026-01-01 00:00:00"
	*app.paramDrafts["tl_max"] = "2026-02-01 00:00:00"
	app.syncParamDriftToPrelude()
	app.frameSig = app.graph.signals()

	pair, ok := matchRangePair(app.paramSlots)
	require.True(t, ok, "the stem rule folds tl_min/tl_max (§SD5)")
	claim := []paramSlot{app.paramSlots[pair[0]], app.paramSlots[pair[1]]}

	app.pinParamClaim(claim)
	require.Equal(t, "SET param_tl_min = '2026-01-01 00:00:00';\n"+
		"SET param_tl_max = '2026-02-01 00:00:00';\n"+
		"SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}", app.sql,
		"both halves pin in one rewrite — the pane cannot produce a half-pinned range")
	require.True(t, app.paramPinned("tl_min"))
	require.True(t, app.paramPinned("tl_max"))

	revBefore := app.graph.signals().Revision()
	app.unpinParamClaim(claim)
	require.False(t, app.paramPinned("tl_min"))
	require.False(t, app.paramPinned("tl_max"))
	require.Equal(t, "SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}", app.sql)
	// Both seeds land before the next frame's snapshot is taken, so no
	// consumer can observe one half moved and the other not.
	require.GreaterOrEqual(t, app.graph.signals().Revision(), revBefore)
	require.Equal(t, "2026-01-01 00:00:00", heldSignal(t, app, "tl_min").Raw)
	require.Equal(t, "2026-02-01 00:00:00", heldSignal(t, app, "tl_max").Raw)
}

// Pinning a name the store never held takes the draft — the pane's own empty
// field is a legitimate thing to write into the buffer.
func TestPinUnheldNameUsesDraft(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}")
	*app.paramDrafts["q"] = "typed"
	app.pinParamClaim(app.paramSlots)
	require.Equal(t, "SET param_q = 'typed';\nSELECT {q:String}", app.sql)
}

// A hand-authored half-pinned pair declines the fold: one picker cannot write
// two tiers. Both halves are withheld from the group widgets, so the tail
// claims them as two scalar fields, and §SD7's line names the halves and
// their tiers.
func TestHalfPinnedPairDeclinesFold(t *testing.T) {
	app := paneApp(t, "SET param_tl_min = '2026-01-01 00:00:00';\n"+
		"SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}")
	require.True(t, app.paramPinned("tl_min"))
	require.False(t, app.paramPinned("tl_max"))

	withheld, pairs := app.mixedTierRangeHalves(app.paramSlots)
	require.Len(t, pairs, 1)
	require.Equal(t, mixedTierPair{Pinned: "tl_min", Live: "tl_max"}, pairs[0])
	require.Equal(t, []bool{true, true}, withheld, "both halves are withheld from the group widgets")

	// What the group widgets are actually offered no longer folds…
	offered := unconsumedSlots(app.paramSlots, maskUnion(make([]bool, len(app.paramSlots)), withheld))
	_, folds := matchRangePair(offered)
	require.False(t, folds, "the pair is not offered, so no picker claims it")
	// …while the tail still sees both slots.
	require.Len(t, unconsumedSlots(app.paramSlots, make([]bool, len(app.paramSlots))), 2)

	note := mixedTierNote(pairs)
	require.Contains(t, note, "{tl_min}")
	require.Contains(t, note, "{tl_max}")
	require.Contains(t, note, "pinned")
	require.Contains(t, note, "live")
}

// A pair in one tier folds as before — the decline is about disagreement, not
// about the tiers themselves.
func TestUniformTierPairsStillFold(t *testing.T) {
	for _, sql := range []string{
		"SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}",
		"SET param_tl_min = '2026-01-01 00:00:00';\nSET param_tl_max = '2026-02-01 00:00:00';\n" +
			"SELECT {tl_min:DateTime64(3, 'UTC')}, {tl_max:DateTime64(3, 'UTC')}",
	} {
		app := paneApp(t, sql)
		withheld, pairs := app.mixedTierRangeHalves(app.paramSlots)
		require.Empty(t, pairs, "a same-tier pair is not a decline")
		require.Nil(t, withheld)
		_, folds := matchRangePair(app.paramSlots)
		require.True(t, folds)
	}
}

// Withholding is per pair: a half-pinned pair declining must not cost an
// unrelated, uniform pair its picker.
func TestDeclineDoesNotBlockAnotherPair(t *testing.T) {
	app := paneApp(t, "SET param_a_min = '2026-01-01 00:00:00';\n"+
		"SELECT {a_min:DateTime64(3, 'UTC')}, {a_max:DateTime64(3, 'UTC')}, "+
		"{b_min:DateTime64(3, 'UTC')}, {b_max:DateTime64(3, 'UTC')}")

	withheld, pairs := app.mixedTierRangeHalves(app.paramSlots)
	require.Len(t, pairs, 1, "only the half-pinned stem declines")
	require.Equal(t, "a_min", pairs[0].Pinned)

	offered := unconsumedSlots(app.paramSlots, maskUnion(make([]bool, len(app.paramSlots)), withheld))
	idx, folds := matchRangePair(offered)
	require.True(t, folds, "the uniform b_min/b_max pair still folds")
	require.Equal(t, []string{"b_min", "b_max"}, []string{offered[idx[0]].Name, offered[idx[1]].Name})
}

// The §SD7 line prefers the tier decline over the generic vocabulary note:
// telling the user their two DateTime slots "do not pair" would be false, and
// the rename it advises is not the fix.
func TestMixedTierNoteWinsOverGenericNearMiss(t *testing.T) {
	slots := []paramSlot{
		{Name: "tl_min", Type: "DateTime64(3, 'UTC')"},
		{Name: "tl_max", Type: "DateTime64(3, 'UTC')"},
	}
	generic := nearMissNote(slots, false)
	require.Contains(t, generic, "do not pair", "the generic note would misdescribe this")
	require.NotEqual(t, generic, mixedTierNote([]mixedTierPair{{Pinned: "tl_min", Live: "tl_max"}}))
}

// The pane's unfilled mark is derived per frame, so it appears for a
// referenced name nothing fills and retires as soon as one does — the same
// set the Run gate reads, so the hint and the mark cannot disagree.
func TestUnfilledMarkAppearsAndRetires(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}, {selection_country:String}")
	require.Equal(t, map[string]bool{"q": true}, app.unfilledSet(),
		"a reserved String signal defaults to empty and never reads as unfilled")

	*app.paramDrafts["q"] = "abc"
	app.syncParamDriftToPrelude()
	app.frameSig = app.graph.signals()
	require.Empty(t, app.unfilledSet(), "filling it in the pane retires the mark")
	require.Empty(t, app.unfilledInputs(), "…and unblocks the Run")
}

// Pinning is also a way to fill: the SET binds the name, so the mark retires
// without the store holding anything.
func TestPinRetiresUnfilledMark(t *testing.T) {
	app := paneApp(t, "SELECT {q:String}")
	require.Equal(t, map[string]bool{"q": true}, app.unfilledSet())

	*app.paramDrafts["q"] = "abc"
	app.pinParamClaim(app.paramSlots)
	require.Empty(t, app.unfilledSet(), "a SET fills the input (D1)")
}
