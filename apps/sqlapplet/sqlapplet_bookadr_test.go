package sqlapplet

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

func adrDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("adr", help.MustSub(bookadrFS, "bookadr"))
	require.Empty(t, errs)
	require.Len(t, defs, 1)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	return bySlug
}

// TestAdrBookCorpus is the ADR-0132 §SD6 gate over the decision-corpus book.
func TestAdrBookCorpus(t *testing.T) {
	bySlug := adrDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
		assert.Equal(t, []app.TopicT{app.TopicAbout}, d.Topics, slug)
	}

	tl := bySlug["adr-timeline"]
	require.NotNil(t, tl)
	assert.Equal(t, []TabSel{{ID: "timeline"}, {ID: "table"}, {ID: "detail"}}, tl.Tabs)
	assert.NotEmpty(t, tl.Preamble, "the preamble strip explains the two encodings")

	// The SET prelude must be the first thing in the buffer. A `--` comment
	// above it costs the applet every run: mounted live, the identical buffer
	// with its comment block hoisted over the two SET lines fails with
	// `keelsonsql: parse: syntax error: 1:0`, and moving the comments back
	// below them fixes it. Verified by capture in both directions; the
	// mechanism is not the client-side split/fuse/param-extract chain, which
	// handles either shape, so this pins the shape that was seen to work
	// rather than a rule anyone can derive.
	require.True(t, strings.HasPrefix(tl.SQL, "SET "),
		"the SET prelude must lead the buffer — a comment above it breaks every run")

	// The Timeline contract (ADR-0097): `_tl_time` plus `_tl_time_end` is the
	// Intervals mode. A `_tl_label` alongside the end column is not a richer
	// bar — the panel refuses the pair as ambiguous and renders nothing.
	assert.Contains(t, tl.SQL, " AS _tl_time,", "the events contract needs _tl_time")
	assert.Contains(t, tl.SQL, " AS _tl_time_end", "intervals need _tl_time_end")
	assert.Contains(t, tl.SQL, " AS _tl_intensity", "colour is the evidence count")
	assert.NotContains(t, tl.SQL, "_tl_label", "a label beside _tl_time_end is rejected as ambiguous")
	// A Timestamp column is what the panel demands, and only DateTime64
	// crosses Arrow as one — DateTime arrives as a bare integer and the panel
	// rejects the schema.
	assert.Contains(t, tl.SQL, "toDateTime64(proposed, 3, 'UTC')")
	assert.Contains(t, tl.SQL, "toDateTime64(until, 3, 'UTC')")

	// The bands lane has its own disjoint slot inventory.
	require.NotEmpty(t, tl.BandsSQL, "the review-day bands ride the `sql bands` fence")
	for _, col := range []string{" AS _tl_band_from", " AS _tl_band_to", " AS _tl_band_color"} {
		assert.Contains(t, tl.BandsSQL, col, "the bands contract needs%s", col)
	}
	// The colour is a design-system token name resolved by play's band-colour
	// map, not a hex literal; an unknown name drops the row.
	assert.Contains(t, tl.BandsSQL, "'success.default'")
}

// TestAdrBookQueries runs the buffers verbatim against the live keelson ADR
// tables, which read this repository's own corpus off disk.
func TestAdrBookQueries(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(60 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))

	caller := bus.NewClient("test.adr.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	query := func(sql string) string {
		t.Helper()
		body, _, qErr := e.Query(context.Background(), sql, "TabSeparated")
		require.NoError(t, qErr, "query failed:\n%s", sql)
		return strings.TrimSpace(string(body))
	}

	// The corpus is found by walking up from the working directory, which is
	// this package inside the checkout. Zero decisions would make every
	// assertion below vacuous rather than failing.
	corpus, err := strconv.Atoi(query("SELECT count() FROM keelson('adr')"))
	require.NoError(t, err)
	require.Greater(t, corpus, 0, "keelson('adr') found no corpus to read")

	tl := adrDefsBySlug(t)["adr-timeline"]
	pre, body := splitPrelude(tl.SQL)
	assert.NotEmpty(t, query(tl.SQL), "the buffer produced no rows")
	assert.NotEmpty(t, query(tl.BandsSQL), "the bands query produced no rows")

	// Only a decision whose date does not parse is left off the axis. Anything
	// else silently dropped would be a decision missing from the picture.
	dated := query("SELECT count() FROM keelson('adr') WHERE toDateOrNull(date) IS NOT NULL")
	assert.Equal(t, dated, query(pre+"\nSELECT count() FROM ("+body+")"))

	// The panel demands a Timestamp column and reads it as one; DateTime would
	// cross Arrow as an integer and be rejected with the schema. Matched by
	// pattern rather than compared: TabSeparated escapes the quotes around the
	// timezone, which is noise this assertion does not need.
	assert.Equal(t, "1\t1", query(pre+"\nSELECT DISTINCT"+
		" toTypeName(_tl_time)     LIKE 'Nullable(DateTime64(3,%UTC%',"+
		" toTypeName(_tl_time_end) LIKE 'Nullable(DateTime64(3,%UTC%'"+
		" FROM ("+body+")"))

	// An inverted interval is dropped by the panel, and intensity outside
	// [0,1] clamps — both would lose information without saying so.
	assert.Equal(t, "0\t0", query(pre+"\nSELECT countIf(until_on < proposed_on),"+
		" countIf(_tl_intensity < 0 OR _tl_intensity > 1) FROM ("+body+")"))

	// A decision still under consideration runs to today; every settled one
	// ends on a day that has already happened.
	assert.Equal(t, "0", query(pre+"\nSELECT countIf(status = 'proposed' AND until_on != toDate(now('UTC')))"+
		" FROM ("+body+")"), "an open decision must run to today")

	// The status knob is a filter over the same rows, not a different query.
	proposed := query("SELECT count() FROM keelson('adr') WHERE status = 'proposed' AND toDateOrNull(date) IS NOT NULL")
	assert.Equal(t, proposed+"\t1", query("SET param_span = 'review';\nSET param_status = 'proposed';\n"+
		"SELECT count(), uniqExact(status) FROM ("+body+")"))

	// The colour scale is pinned to the whole corpus before the filter: the
	// same decision must keep its colour when the view narrows to its own
	// status, or a filtered view would quietly re-rank the evidence.
	num := query("SELECT num FROM keelson('adr') WHERE status = 'proposed' ORDER BY code_refs DESC, num ASC LIMIT 1")
	require.NotEmpty(t, num)
	pick := "SELECT round(_tl_intensity, 6) FROM (" + body + ") WHERE adr = concat('ADR-', leftPad('" + num + "', 4, '0'))"
	assert.Equal(t,
		query("SET param_span = 'review';\nSET param_status = 'all';\n"+pick),
		query("SET param_span = 'review';\nSET param_status = 'proposed';\n"+pick),
		"the evidence colour must not depend on the status filter")

	// The span knob measures a different thing, not a rescaled one: activity
	// ends on the last dated edit, which for a still-open decision is on or
	// before today rather than today by construction.
	assert.Equal(t, "1", query("SET param_span = 'activity';\nSET param_status = 'all';\n"+
		"SELECT countIf(until_on <= toDate(now('UTC'))) = count() FROM ("+body+")"))

	// Bands are one day wide, on the days the corpus records several reviews.
	sweeps := query("SELECT count() FROM (SELECT reviewed_date FROM keelson('adr')" +
		" WHERE reviewed_date != '' GROUP BY reviewed_date HAVING count() >= 3)")
	assert.Equal(t, sweeps+"\t0", query("SELECT count(), countIf(dateDiff('day', _tl_band_from, _tl_band_to) != 1)"+
		" FROM ("+tl.BandsSQL+")"))
}
