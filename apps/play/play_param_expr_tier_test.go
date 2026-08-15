package play

// The two tiers of a SQL-valued slot (ADR-0187 (proposed) §SD3, milestone M3):
// the tier bit's second source, the live path through the signal store, and
// pin/unpin as the migration between them.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The bit has two mirrors and one predicate: a caller asking "is this pinned"
// must not have to know which kind of slot it holds.
func TestExprTierBitReadsTheDeclaration(t *testing.T) {
	declared := paneApp(t, "-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}")
	require.True(t, declared.paramPinned("cond"), "a declaration is what pins a SQL-valued slot")

	bare := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	require.False(t, bare.paramPinned("cond"), "no declaration is the live tier")

	// The prelude mirror still answers for an ordinary slot, unchanged.
	pinned := paneApp(t, "SET param_lim = 5;\nSELECT x FROM t LIMIT {lim:UInt64}")
	require.True(t, pinned.paramPinned("lim"))
}

// A live expression follows the store, so a panel publishing the name shows its
// predicate in the field. Two routes, and both matter: the birth seed for a
// field that appears after the value exists, and phase 1b for one that was
// already on screen.
func TestLiveExprDraftFollowsTheStore(t *testing.T) {
	born := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	born.graph.setSignalRawFrom("cond", "b = 2", signalWriterParamWidget)
	born.paramDrafts = nil               // the field has not been seen yet
	born.frameSig = born.graph.signals() // the parse runs on a current snapshot
	reparse(t, born)
	require.Equal(t, "b = 2", *born.paramDrafts["cond"], "born from the store")
	require.False(t, born.paramPinned("cond"), "seeding from the store does not pin it")

	idle := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	idle.graph.setSignalRawFrom("cond", "c = 3", signalWriterParamWidget)
	idle.frameSig = idle.graph.signals()
	idle.syncLiveParamDrafts()
	require.Equal(t, "c = 3", *idle.paramDrafts["cond"], "an idle draft follows the store")
}

// At the live tier the drift goes to the store and the buffer is untouched —
// the inverse of the pinned tier, and the reason a panel can drive the value
// without rewriting the document.
func TestLiveExprDriftWritesTheStoreNotTheBuffer(t *testing.T) {
	sql := "SELECT x FROM t WHERE {cond:Expr}"
	app := paneApp(t, sql)

	*app.paramDrafts["cond"] = "c = 3"
	app.syncParamDriftToPrelude()

	require.Equal(t, sql, app.sql, "a live expression never touches the buffer")
	p, held := app.graph.signals().Get("cond")
	require.True(t, held)
	require.Equal(t, "c = 3", p.Raw)
}

// Pin is the migration to buffer-owned: it authors the declaration the prelude
// would otherwise have been, and the store KEEPS its value so the panel that
// publishes it is not wiped.
func TestPinExprClaimAuthorsTheDeclaration(t *testing.T) {
	app := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	app.graph.setSignalRawFrom("cond", "b = 2", signalWriterParamWidget)
	reparse(t, app)
	require.False(t, app.paramPinned("cond"))

	app.pinParamClaim([]paramSlot{{Name: "cond", Type: "Expr"}})

	require.Contains(t, app.sql, "-- play: expr cond = b = 2")
	require.NotContains(t, app.sql, "SET param_cond", "an expression is never prelude-bound")
	require.True(t, app.paramPinned("cond"), "the tier bit flips now, not on the next parse")
	p, held := app.graph.signals().Get("cond")
	require.True(t, held, "the store keeps its value — the declaration shadows it")
	require.Equal(t, "b = 2", p.Raw)
}

// An empty value is not a declaration, so there is nothing to pin.
func TestPinExprClaimDeclinesAnEmptyValue(t *testing.T) {
	sql := "SELECT x FROM t WHERE {cond:Expr}"
	app := paneApp(t, sql)
	app.pinParamClaim([]paramSlot{{Name: "cond", Type: "Expr"}})
	require.Equal(t, sql, app.sql)
	require.False(t, app.paramPinned("cond"))
}

// Unpin is the same migration in reverse: the declaration goes and the value it
// carried is seeded into the store, so the field keeps showing it and the name
// is immediately live rather than unfilled.
func TestUnpinExprClaimSeedsTheStore(t *testing.T) {
	app := paneApp(t, "-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}")
	require.True(t, app.paramPinned("cond"))

	app.unpinParamClaim([]paramSlot{{Name: "cond", Type: "Expr"}})

	require.NotContains(t, app.sql, "-- play: expr cond")
	require.False(t, app.paramPinned("cond"))
	p, held := app.graph.signals().Get("cond")
	require.True(t, held)
	require.Equal(t, "a = 1", p.Raw)

	// The frame snapshot is taken per frame (ADR-0097 5a), so a value written
	// during a frame reads back on the NEXT one — the same one-frame lag an
	// ordinary unpin has, not something this tier adds.
	app.frameSig = app.graph.signals()
	require.Empty(t, app.unfilledInputs(), "live and filled, not unfilled")
}

// A live expression is filled by the store, and the gate must see that through
// both of its independent derivations.
func TestLiveExprIsNotUnfilled(t *testing.T) {
	app := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	require.Equal(t, []string{"cond"}, app.unfilledInputs(), "no store value yet")

	app.graph.setSignalRawFrom("cond", "b = 2", signalWriterParamWidget)
	reparse(t, app)
	app.frameSig = app.graph.signals()
	require.Empty(t, app.unfilledInputs())

	app.caretByte = len(app.sql)
	runSQL, _, _ := app.runBuffer()
	sigParams, _, unfilled := app.resolveRunSignals(runSQL)
	require.Empty(t, unfilled, "the run gate agrees")
	// The predicate reaches the query through the splice; sending it as a URL
	// parameter as well would ship a string nothing reads.
	require.NotContains(t, sigParams, "param_cond")
}

// The declaration shadows the store, which is §SD4's rule for a SET applied to
// this tier pair — a panel co-writing a name must not silently override what
// the document states.
func TestDeclarationShadowsTheLiveValue(t *testing.T) {
	cl := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	cl.SetExprValues(map[string]string{"cond": "live = 1", "other": "x = 2"})

	values := cl.exprValuesFor("-- play: expr cond = declared = 1\nSELECT 1")
	require.Equal(t, "declared = 1", values["cond"], "the buffer wins")
	require.Equal(t, "x = 2", values["other"], "a name the buffer does not declare stays live")
}

// End to end for the live tier: a value that exists only in the store still
// reaches the wire body, because the splice is the only route a predicate has.
func TestLiveExprReachesTheWireBody(t *testing.T) {
	cl := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	const sql = "SELECT number FROM numbers(10) WHERE {cond:Expr}"

	body, params := cl.BuildStatement(sql)
	require.Contains(t, body, "{cond:Expr}", "nothing to substitute yet")

	cl.SetExprValues(map[string]string{"cond": "number % 2 = 0"})
	body, params = cl.BuildStatement(sql)
	require.Contains(t, body, "(number % 2 = 0)")
	require.NotContains(t, body, "{cond:Expr}")
	require.NotContains(t, params, "param_cond")

	// Whole-map replacement: a name that left the live tier stops being
	// substituted rather than lingering as a stale binding.
	cl.SetExprValues(nil)
	body, _ = cl.BuildStatement(sql)
	require.Contains(t, body, "{cond:Expr}")
}
