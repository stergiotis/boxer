package play

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

func sameName(alias string) string { return alias }

// The notice says why each alias waits as the follower heard it, and what
// binding it does: a launch alias runs the window again, one a task asked
// for runs nothing. An alias not asked about yet is left out.
func TestDatasetWaitNoticeSaysWhyAndWhatFollows(t *testing.T) {
	launch := func(local string) bool { return local == "declared" }
	assert.Nil(t, datasetWaitNotice(nil, sameName, launch))
	assert.Nil(t, datasetWaitNotice(map[string]string{"x": adhocdata.WaitNotAsked}, sameName, launch),
		"an alias the service was not asked about yet does not flash a notice")

	assert.Equal(t, "**Waiting for dataset `declared`.** No dataset is live under the alias; it binds when one is published. The window runs again once it is bound.",
		string(datasetWaitNotice(map[string]string{"declared": adhocdata.WaitNoLive}, sameName, launch)))
	assert.Equal(t, "**Waiting for dataset `asked`.** Its dataset was withdrawn; it binds when one is published again. A task asked for it here; binding it runs nothing.",
		string(datasetWaitNotice(map[string]string{"asked": adhocdata.WaitWithdrawn}, sameName, launch)))

	md := string(datasetWaitNotice(map[string]string{
		"declared": adhocdata.WaitNoLive,
		"orders":   adhocdata.WaitUnanswered + ": timeout",
		"pending":  adhocdata.WaitNotAsked,
	}, func(alias string) string {
		if alias == "orders" {
			return "o"
		}
		return alias
	}, launch))
	assert.Equal(t, "**Waiting for datasets.**\n"+
		"\n- `declared`: No dataset is live under the alias; it binds when one is published. The window runs again once it is bound."+
		"\n- `orders` (read as `o`): The dataset service did not answer: timeout. A task asked for it here; binding it runs nothing.", md)
}

// The bundle's notice and the follower's share the strip: neither wipes the
// other, and play reparses only when the composed text changes.
func TestDatasetNoticesComposeAndReparseOnlyOnChange(t *testing.T) {
	p := &PlayApp{}
	var n datasetNotices
	n.show(p)
	assert.Nil(t, p.datasetNotice)

	n.bundle = []byte("**Waiting for bundle `sales`.**")
	n.datasets = []byte("**Waiting for dataset `x`.**")
	n.show(p)
	require.NotNil(t, p.datasetNotice)
	assert.Equal(t, "**Waiting for bundle `sales`.**\n\n**Waiting for dataset `x`.**", string(n.shown))
	parsed := p.datasetNotice
	n.bundle = []byte("**Waiting for bundle `sales`.**")
	n.show(p)
	assert.Same(t, parsed, p.datasetNotice, "the same text is not parsed again")

	n.bundle = nil
	n.show(p)
	assert.Equal(t, "**Waiting for dataset `x`.**", string(n.shown), "the bundle arriving leaves the dataset's notice")
	n.datasets = nil
	n.show(p)
	assert.Nil(t, p.datasetNotice)
}

// A query that gives up on an alias stops following what it asked for, so
// the window does not say it waits for it for the rest of its life; an
// alias followed before the call stays followed.
func TestQueryGivingUpUnfollowsWhatItAskedFor(t *testing.T) {
	l, h, _ := bindLauncher(t)
	_, err := bindDataset(t, h, "earlier")
	require.NoError(t, err)
	args, err := buscodec.Encode(QueryArgs{Sql: "SELECT * FROM keelson('nothing_here') JOIN keelson('earlier') USING (v)"})
	require.NoError(t, err)
	call := app.OperationCall{Writer: "task:t", Key: "q", OnBehalfOf: &app.OnBehalfOf{Task: "t"}}
	_, err = h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	q := l.inner.queryRuns["q"]
	require.Equal(t, []string{"nothing_here"}, q.followed, "earlier was followed already")
	require.Eventually(t, func() bool {
		l.syncDatasets()
		return l.follower.Waiting()["nothing_here"] == adhocdata.WaitNoLive
	}, 5*time.Second, 5*time.Millisecond)
	assert.Contains(t, string(l.notices.shown), "`nothing_here`")

	q.bindBy = time.Now().Add(-time.Second)
	l.inner.queryRuns["q"] = q
	settled, _, err := h.(app.OperationsAsyncI).SettleAsync(call, opQuery, nil)
	require.True(t, settled)
	require.ErrorContains(t, err, "no live dataset under nothing_here")
	assert.Equal(t, []string{"earlier"}, l.follower.Pending())
	l.syncDatasets()
	assert.NotContains(t, string(l.notices.shown), "nothing_here", "the window stops saying it waits for it")
	assert.Contains(t, string(l.notices.shown), "`earlier`")
}

// keelson('<name>') of a table the host's plane serves is a table, not an
// alias to bind: the host serves its own registry, not introspect.Default.
func TestQueryReadsAPlaneTableWithoutBindingIt(t *testing.T) {
	l, _, _ := bindLauncher(t)
	reg := introspect.NewRegistry()
	require.NoError(t, introspect.RegisterCatalog(reg))
	introspect.SetLocalCatalog(reg)
	t.Cleanup(func() { introspect.SetLocalCatalog(nil) })

	assert.Empty(t, l.inner.unboundDatasets("SELECT * FROM keelson('tables') JOIN keelson('columns') USING (name)"))
	assert.False(t, isAdhocName("tables"))
	assert.Equal(t, []string{"orders"}, l.inner.unboundDatasets("SELECT * FROM keelson('tables'), keelson('orders')"))
	assert.True(t, isAdhocName("orders"))
}

// A window that waits for a dataset asks for a frame after a pause, so the
// follower's answers land and a query's wait ends without input: briefly
// while a query waits for its bind, once a second while an alias waits for
// a publish, and not at all when nothing waits.
func TestAWaitingWindowAsksForFrames(t *testing.T) {
	l, h, _ := bindLauncher(t)
	_, waits := l.inner.waitRepaint()
	assert.False(t, waits)

	_, err := bindDataset(t, h, "later")
	require.NoError(t, err)
	l.syncDatasets()
	assert.True(t, l.inner.dataWait)
	after, waits := l.inner.waitRepaint()
	assert.True(t, waits)
	assert.Equal(t, dataWaitRepaintSeconds, after)

	l.inner.queryRuns = map[string]queryRun{"q": {binding: []string{"later"}}}
	after, _ = l.inner.waitRepaint()
	assert.Equal(t, queryBindRepaintSeconds, after, "a query's bind is waited for briefly")
}
