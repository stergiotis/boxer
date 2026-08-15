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

	// The prelude no longer has to lead the buffer, and this test used to
	// require that it did.
	//
	// A `--` comment above the two SET lines cost the applet every run: it
	// failed with `keelsonsql: parse: syntax error: 1:0`, verified live in both
	// directions on 2026-08-06. The mechanism was in `dsl/env`, whose prelude
	// harvest took only a LEADING run of SET lines, so a comment above them
	// ended the prelude before it started and the bindings never reached the
	// wire. ADR-0006's 2026-08-15 Update made the harvest comment-tolerant.
	//
	// Re-tested live the same day, same applet, comment block hoisted over both
	// SET lines: 182 rows, `span` and `status` both prelude-bound (the pane
	// offers `unpin` for each), tree byte-identical to the as-shipped shape
	// except for frame timings. So the placement is now free, and the buffer
	// keeps its comments below the prelude because that reads better — not
	// because moving them breaks it.

	// The Timeline contract (ADR-0097): `_tl_time` plus `_tl_time_end` is the
	// Intervals mode. A `_tl_label` alongside the end column is not a richer
	// bar — the panel refuses the pair as ambiguous and renders nothing.
	assert.Contains(t, tl.SQL, " AS _tl_time,", "the events contract needs _tl_time")
	assert.Contains(t, tl.SQL, " AS _tl_time_end", "intervals need _tl_time_end")
	assert.NotContains(t, tl.SQL, "_tl_label", "a label beside _tl_time_end is rejected as ambiguous")
	// No _tl_intensity, deliberately. Present, it turns every bar a different
	// colour off a sequential ramp indexed by packing order — thirteen fills
	// over two-thirds of the pane — and the ramp's low end sits at the panel's
	// own luminance, hiding exactly the decisions with no code behind them.
	// Measured on captures of both; the flat bar is the one that reads.
	assert.NotContains(t, tl.SQL, "_tl_intensity",
		"colour by evidence draws a quilt; the evidence columns carry it instead")
	// The lane hint is equally deliberate: it puts every bar of a status on ONE
	// row, where they overpaint each other and the count is lost.
	assert.NotContains(t, tl.SQL, "_tl_lane", "a hinted lane overpaints its own bars")
	// A Timestamp column is what the panel demands, and only DateTime64
	// crosses Arrow as one — DateTime arrives as a bare integer and the panel
	// rejects the schema.
	assert.Contains(t, tl.SQL, "toDateTime64(proposed, 3, 'UTC')")
	assert.Contains(t, tl.SQL, "toDateTime64(greatest(until, addDays(proposed, 1)), 3, 'UTC')")

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

	// An inverted interval is dropped by the panel without saying so.
	assert.Equal(t, "0", query(pre+"\nSELECT countIf(until_on < proposed_on) FROM ("+body+")"))

	// Every drawn bar covers at least a whole day — at their true width the
	// same-day decisions are one pixel — while `days` keeps reporting the real
	// elapsed number, zero included.
	assert.Equal(t, "0", query(pre+"\nSELECT countIf(dateDiff('day', _tl_time, _tl_time_end) < 1)"+
		" FROM ("+body+")"), "a bar narrower than a day is a one-pixel mark")
	assert.Equal(t, "1", query(pre+"\nSELECT countIf(days = 0) > 0 FROM ("+body+")"),
		"widening the drawn bar must not inflate the reported duration")
	// …and the widening is only ever a floor: a bar that already spans days
	// keeps its own end.
	assert.Equal(t, "0", query(pre+"\nSELECT countIf(days >= 1 AND toDate(_tl_time_end) != until_on)"+
		" FROM ("+body+")"))

	// A decision still under consideration runs to today; every settled one
	// ends on a day that has already happened.
	assert.Equal(t, "0", query(pre+"\nSELECT countIf(status = 'proposed' AND until_on != toDate(now('UTC')))"+
		" FROM ("+body+")"), "an open decision must run to today")

	// The status knob is a filter over the same rows, not a different query.
	proposed := query("SELECT count() FROM keelson('adr') WHERE status = 'proposed' AND toDateOrNull(date) IS NOT NULL")
	assert.Equal(t, proposed+"\t1", query("SET param_span = 'review';\nSET param_status = 'proposed';\n"+
		"SELECT count(), uniqExact(status) FROM ("+body+")"))

	// Identity leads the projection, and the drawn pair trails it. The Detail
	// card lifts the temporal columns into a section of their own and lists
	// the rest in column order, so a buffer opening with `_tl_time` puts a
	// chart of four timestamps where the decision's number and title belong.
	header, _, hErr := e.Query(context.Background(), pre+"\nSELECT * FROM ("+body+") LIMIT 0", "TSVWithNames")
	require.NoError(t, hErr)
	cols := strings.Split(strings.TrimSpace(string(header)), "\t")
	require.GreaterOrEqual(t, len(cols), 3)
	assert.Equal(t, []string{"adr", "title", "status"}, cols[:3], "identity first")
	assert.Equal(t, []string{"_tl_time", "_tl_time_end"}, cols[len(cols)-2:], "the drawn pair last")

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
