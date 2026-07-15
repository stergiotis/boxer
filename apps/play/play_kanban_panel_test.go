package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0122 §Validation: the board's schema contract (§SD1), the dot vocabulary
// and its rejections (§SD2), and the fold's positional identities.

// kanbanSchema builds a schema from name/type pairs, in order.
func kanbanSchema(fields ...arrow.Field) *arrow.Schema { return arrow.NewSchema(fields, nil) }

// strField lives in play_timeline_panel_test.go.
func u64Field(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Uint64} }
func f64Field(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Float64} }
func i32Field(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Int32} }

// kanbanRec builds a board record: lane/title strings plus one uint64 dot
// column per dotCols entry, values supplied per row.
func kanbanRec(t *testing.T, lanes, titles []string, dotCols []string, dots [][]uint64) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	mkStr := func(vs []string) arrow.Array {
		b := array.NewStringBuilder(alloc)
		defer b.Release()
		b.AppendValues(vs, nil)
		return b.NewStringArray()
	}
	fields := []arrow.Field{strField("lane"), strField("title")}
	cols := []arrow.Array{mkStr(lanes), mkStr(titles)}
	for i, name := range dotCols {
		fields = append(fields, u64Field(name))
		b := array.NewUint64Builder(alloc)
		vals := make([]uint64, len(lanes))
		for r := range lanes {
			vals[r] = dots[r][i]
		}
		b.AppendValues(vals, nil)
		cols = append(cols, b.NewUint64Array())
		b.Release()
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(lanes)))
}

// The contract: lane + title by name, everything else optional.
func TestKanbanAcceptContract(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}

	_, reason := p.AcceptForChannel(chMain, nil, sigNone())
	assert.NotEmpty(t, reason, "nil schema is rejected")

	// Missing columns name themselves, and the hint teaches the contract.
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("status"), strField("name")), sigNone())
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "`lane`")
	assert.Contains(t, reason, "`title`")
	assert.Contains(t, reason, "AS lane", "the reject shows a query that satisfies it")

	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("lane"), strField("x")), sigNone())
	assert.Contains(t, reason, "`title`")
	assert.NotContains(t, reason, "`lane`", "only the missing column is named")

	// The minimum board.
	claim, reason := p.AcceptForChannel(chMain, kanbanSchema(strField("lane"), strField("title")), sigNone())
	require.Empty(t, reason)
	k := claim.(kanbanClaim)
	assert.Equal(t, 0, k.laneCol)
	assert.Equal(t, 1, k.titleCol)
	assert.Equal(t, -1, k.subCol, "absent subtitle is -1, not 0")
	assert.Empty(t, k.dots)

	// Column order is free; subtitle is picked up when present.
	claim, reason = p.AcceptForChannel(chMain,
		kanbanSchema(strField("subtitle"), strField("title"), strField("other"), strField("lane")), sigNone())
	require.Empty(t, reason)
	k = claim.(kanbanClaim)
	assert.Equal(t, 3, k.laneCol)
	assert.Equal(t, 1, k.titleCol)
	assert.Equal(t, 0, k.subCol)
}

// lane/title carry no type requirement — formatCell is total, so a numeric
// lane is a board with numbered lanes rather than an error.
func TestKanbanAcceptTypePermissiveOnText(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}
	_, reason := p.AcceptForChannel(chMain, kanbanSchema(i32Field("lane"), f64Field("title")), sigNone())
	assert.Empty(t, reason, "any type renders through formatCell")
}

// The dot vocabulary: bare names take the positional ramp, `@token` names a
// colour, and both may mix.
func TestKanbanAcceptDotColumns(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}

	claim, reason := p.AcceptForChannel(chMain, kanbanSchema(
		strField("lane"), strField("title"),
		u64Field("dot_done@success"), u64Field("dot_cited@warning"), u64Field("dot_todo@disabled"),
	), sigNone())
	require.Empty(t, reason)
	k := claim.(kanbanClaim)
	require.Len(t, k.dots, 3)
	assert.Equal(t, "done", k.dots[0].label, "the label is the name minus prefix and token")
	assert.Equal(t, "cited", k.dots[1].label)
	assert.Equal(t, "todo", k.dots[2].label)
	assert.Equal(t, kanbanTokenColor(styletokens.SuccessDefault), k.dots[0].color)
	assert.Equal(t, "dot_done@success", k.dots[0].name, "the legend tooltip names the physical column")

	// Bare names fall back to the ramp, by position.
	claim, reason = p.AcceptForChannel(chMain, kanbanSchema(
		strField("lane"), strField("title"), u64Field("dot_a"), u64Field("dot_b"),
	), sigNone())
	require.Empty(t, reason)
	k = claim.(kanbanClaim)
	require.Len(t, k.dots, 2)
	assert.Equal(t, "a", k.dots[0].label)
	assert.Equal(t, kanbanTokenColor(kanbanDotRamp[0]), k.dots[0].color)
	assert.Equal(t, kanbanTokenColor(kanbanDotRamp[1]), k.dots[1].color)

	// Mixed: the ramp index is the dot's position, not a count of bare ones.
	claim, reason = p.AcceptForChannel(chMain, kanbanSchema(
		strField("lane"), strField("title"), u64Field("dot_a@error"), u64Field("dot_b"),
	), sigNone())
	require.Empty(t, reason)
	k = claim.(kanbanClaim)
	assert.Equal(t, kanbanTokenColor(kanbanDotRamp[1]), k.dots[1].color, "position 1 takes ramp[1]")
}

// The rejections §SD2 turns into messages rather than a quietly wrong board.
func TestKanbanAcceptDotRejections(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}
	base := []arrow.Field{strField("lane"), strField("title")}

	// Four dot kinds: the widget would silently drop the fourth.
	_, reason := p.AcceptForChannel(chMain, kanbanSchema(append(base,
		u64Field("dot_a"), u64Field("dot_b"), u64Field("dot_c"), u64Field("dot_d"))...), sigNone())
	require.NotEmpty(t, reason, "four dot kinds is a rejection, not a truncation")
	assert.Contains(t, reason, "dot_d", "the reject names the columns it cannot paint")

	// A float tally.
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(append(base, f64Field("dot_a"))...), sigNone())
	assert.Contains(t, reason, "integer tally")

	// An unknown token, and the message lists the vocabulary.
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(append(base, u64Field("dot_a@chartreuse"))...), sigNone())
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "@chartreuse")
	assert.Contains(t, reason, "success")

	// An empty token is a typo, not a request for the ramp.
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(append(base, u64Field("dot_a@"))...), sigNone())
	assert.NotEmpty(t, reason, "`dot_a@` is rejected")

	// A dot column with no label.
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(append(base, u64Field("dot_"))...), sigNone())
	assert.Contains(t, reason, "no label")
}

// The *Subtle background tones are excluded from the vocabulary by
// construction: a dot painted in one is invisible on the card.
func TestKanbanDotTokensAreForegroundOnly(t *testing.T) {
	for name := range kanbanDotTokens {
		assert.NotContains(t, name, "subtle", "the vocabulary must not expose a background tone")
	}
	for _, want := range []string{"success", "warning", "error", "info", "accent", "neutral", "disabled"} {
		_, ok := kanbanDotTokens[want]
		assert.True(t, ok, "token %q is part of the vocabulary", want)
	}
	// The reject message lists them in a stable order (map order is not).
	assert.Equal(t, kanbanTokenNames(), kanbanTokenNames())
	assert.Equal(t, "accent, disabled, error, info, neutral, success, warning", kanbanTokenNames())
}

func TestParseKanbanDot(t *testing.T) {
	for _, tc := range []struct {
		name         string
		label, token string
		hasToken     bool
	}{
		{"dot_done", "done", "", false},
		{"dot_done@success", "done", "success", true},
		{"dot_all_done@success", "all_done", "success", true},
		{"dot_done@", "done", "", true},
		{"dot_", "", "", false},
		{"dot_a@b@c", "a", "b@c", true},
	} {
		label, token, hasToken := parseKanbanDot(tc.name)
		assert.Equal(t, tc.label, label, "label of %q", tc.name)
		assert.Equal(t, tc.token, token, "token of %q", tc.name)
		assert.Equal(t, tc.hasToken, hasToken, "hasToken of %q", tc.name)
	}
}

// The fold: lanes in first-seen row order, positional identities, zero tallies
// dropped.
func TestKanbanFold(t *testing.T) {
	rec := kanbanRec(t,
		[]string{"proposed", "accepted", "proposed", ""},
		[]string{"A", "B", "C", "D"},
		[]string{"dot_done@success", "dot_todo@disabled"},
		[][]uint64{{2, 1}, {0, 0}, {3, 0}, {0, 5}})
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, reason := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	require.Empty(t, reason)
	d.rebuild(rec, rec.Schema(), claim.(kanbanClaim), nil)
	m := d.model
	require.NotNil(t, m)

	// Lanes: first-seen order, so ORDER BY controls the board.
	require.Len(t, m.Columns, 3)
	assert.Equal(t, "proposed", m.Columns[0].Title)
	assert.Equal(t, "accepted", m.Columns[1].Title)
	assert.Equal(t, kanbanNoLane, m.Columns[2].Title, "an empty lane cell gets a visible title")

	require.Len(t, m.Cards, 4)
	for i, card := range m.Cards {
		assert.Equal(t, uint64(i+1), card.ID, "card ids are positional and non-zero")
	}
	assert.Equal(t, m.Columns[0].ID, m.Cards[0].ColumnID)
	assert.Equal(t, m.Columns[0].ID, m.Cards[2].ColumnID, "row 2 rejoins the first lane")
	assert.Equal(t, m.Columns[2].ID, m.Cards[3].ColumnID)
	assert.Equal(t, "A", m.Cards[0].Title)

	// Zero tallies carry no dot at all.
	require.Len(t, m.Cards[0].Dots, 2)
	assert.Equal(t, 2, m.Cards[0].Dots[0].Count)
	assert.Equal(t, 1, m.Cards[0].Dots[1].Count)
	assert.Empty(t, m.Cards[1].Dots, "a card with nothing to report stays clean")
	require.Len(t, m.Cards[2].Dots, 1)
	assert.Equal(t, uint64(1), m.Cards[2].Dots[0].ID, "the surviving dot keeps its kind id")
	require.Len(t, m.Cards[3].Dots, 1)
	assert.Equal(t, uint64(2), m.Cards[3].Dots[0].ID)

	// Legend ids match what the cards tally against — a mismatch is skipped
	// silently by the widget.
	require.Len(t, m.DotLegend, 2)
	assert.Equal(t, uint64(1), m.DotLegend[0].ID)
	assert.Equal(t, "done", m.DotLegend[0].Label)
	assert.Contains(t, m.DotLegend[1].Tooltip, "dot_todo@disabled")
}

// The fold is cached on (executed, schema) — not only for the allocation, but
// because kanban.Model owns the widget's selection.
func TestKanbanFoldCachePreservesSelection(t *testing.T) {
	rec := kanbanRec(t, []string{"a", "b"}, []string{"A", "B"}, nil, [][]uint64{{}, {}})
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, _ := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	k := claim.(kanbanClaim)

	d.rebuild(rec, rec.Schema(), k, nil)
	first := d.model
	d.model.SetSelected(2)
	d.rebuild(rec, rec.Schema(), k, nil)
	assert.Same(t, first, d.model, "an unchanged result must not rebuild the model")
	assert.Equal(t, uint64(2), d.model.Selected(), "a rebuild would have cleared the selection")

	// A new result identity does rebuild.
	d.noteExecuted(d.pendingExecuted.Add(1))
	d.rebuild(rec, rec.Schema(), k, nil)
	assert.NotSame(t, first, d.model, "a fresh result rebuilds")
}

// The selection claim rides in from the signal env, so the board can follow a
// cursor another panel moved.
func TestKanbanAcceptCarriesSelection(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}
	schema := kanbanSchema(strField("lane"), strField("title"))

	claim, reason := p.AcceptForChannel(chMain, schema, sigWith(7))
	require.Empty(t, reason)
	assert.Equal(t, int64(7), claim.(kanbanClaim).selRow)

	claim, reason = p.AcceptForChannel(chMain, schema, sigNone())
	require.Empty(t, reason)
	assert.Equal(t, int64(-1), claim.(kanbanClaim).selRow, "no selection is -1, not row 0")
}

// The lanes channel (§SD6) is optional, and claims the `lane` column of the
// lanes node.
func TestKanbanAcceptLanesChannel(t *testing.T) {
	p := kanbanPanel{driver: NewKanbanDriver(nil, nil)}

	specs := p.Channels()
	require.Len(t, specs, 2)
	assert.Equal(t, chMain, specs[0].ID)
	assert.True(t, specs[0].Required)
	assert.Equal(t, chLanes, specs[1].ID)
	assert.False(t, specs[1].Required, "a board without a lanes CTE still renders")

	claim, reason := p.AcceptForChannel(chLanes, kanbanSchema(strField("lane")), sigNone())
	require.Empty(t, reason)
	assert.Equal(t, 0, claim.(int))

	// Column order is free; other columns are ignored.
	claim, reason = p.AcceptForChannel(chLanes, kanbanSchema(strField("ord"), strField("lane")), sigNone())
	require.Empty(t, reason)
	assert.Equal(t, 1, claim.(int))

	_, reason = p.AcceptForChannel(chLanes, kanbanSchema(strField("name")), sigNone())
	assert.Contains(t, reason, "`lane` column")
	_, reason = p.AcceptForChannel(chLanes, nil, sigNone())
	assert.Equal(t, "no lanes result", reason)
}

func TestKanbanDeclaredLanes(t *testing.T) {
	rec := kanbanRec(t, []string{"b", "a", "b", ""}, []string{"1", "2", "3", "4"}, nil, make([][]uint64, 4))
	defer rec.Release()
	// Row order, de-duplicated; the empty cell is a lane like any other.
	assert.Equal(t, []string{"b", "a", ""}, kanbanDeclaredLanes(rec, 0))
	assert.Nil(t, kanbanDeclaredLanes(rec, -1))
	assert.Nil(t, kanbanDeclaredLanes(nil, 0))
}

// The point of §SD6: a declared lane with no cards renders, so the board can
// say "nothing is withdrawn" — which lanes-off-the-rows structurally cannot.
func TestKanbanDeclaredLanesRenderEmpty(t *testing.T) {
	rec := kanbanRec(t, []string{"accepted", "proposed"}, []string{"A", "B"}, nil, make([][]uint64, 2))
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, reason := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	require.Empty(t, reason)
	declared := []string{"proposed", "accepted", "superseded", "withdrawn", "deferred"}
	d.rebuild(rec, rec.Schema(), claim.(kanbanClaim), declared)

	require.Len(t, d.model.Columns, 5, "every declared lane exists, cards or not")
	for i, want := range declared {
		assert.Equal(t, want, d.model.Columns[i].Title, "declared order is the board order")
	}
	// The two cards land in their declared lanes; the other three lanes are empty.
	counts := map[string]int{}
	for _, card := range d.model.Cards {
		for _, col := range d.model.Columns {
			if col.ID == card.ColumnID {
				counts[col.Title]++
			}
		}
	}
	assert.Equal(t, map[string]int{"accepted": 1, "proposed": 1}, counts)
	assert.Equal(t, 0, counts["withdrawn"], "the lane exists and is empty — the §SD6 point")
}

// A lane no inventory names is appended rather than dropped — adrboard's
// unknown-status behaviour.
func TestKanbanUndeclaredLaneIsAppended(t *testing.T) {
	rec := kanbanRec(t, []string{"accepted", "rejected"}, []string{"A", "B"}, nil, make([][]uint64, 2))
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, _ := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	d.rebuild(rec, rec.Schema(), claim.(kanbanClaim), []string{"proposed", "accepted"})

	require.Len(t, d.model.Columns, 3)
	assert.Equal(t, "proposed", d.model.Columns[0].Title)
	assert.Equal(t, "accepted", d.model.Columns[1].Title)
	assert.Equal(t, "rejected", d.model.Columns[2].Title, "an unnamed lane is appended, never dropped")
}

// The declared lanes are an input to the fold, so they must key its cache —
// they can change while the result does not.
func TestKanbanLanesKeyTheFoldCache(t *testing.T) {
	rec := kanbanRec(t, []string{"a"}, []string{"A"}, nil, make([][]uint64, 1))
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, _ := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	k := claim.(kanbanClaim)

	d.rebuild(rec, rec.Schema(), k, []string{"a"})
	first := d.model
	d.rebuild(rec, rec.Schema(), k, []string{"a"})
	assert.Same(t, first, d.model, "unchanged lanes must not rebuild")
	d.rebuild(rec, rec.Schema(), k, []string{"a", "b"})
	assert.NotSame(t, first, d.model, "a changed inventory rebuilds")
	assert.Len(t, d.model.Columns, 2)
}

// A failed lanes query must not read as "nothing was declared": the board
// falls back to row-derived lanes, which looks like a working board.
func TestKanbanLanesErrorSurfaces(t *testing.T) {
	rec := kanbanRec(t, []string{"a"}, []string{"A"}, nil, make([][]uint64, 1))
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, _ := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	d.rebuild(rec, rec.Schema(), claim.(kanbanClaim), nil)

	assert.NotContains(t, d.statusLine(), "lanes query failed")
	d.lanesErr = eh.Errorf("boom")
	assert.Contains(t, d.statusLine(), "lanes query failed", "the failure is on the board, not just in a log")
	d.lanesErr = nil
	d.lanesLoading = true
	assert.Contains(t, d.statusLine(), "lanes…")
}

// The board caps its fold and says so rather than laying out a million cards.
func TestKanbanFoldTruncates(t *testing.T) {
	n := kanbanMaxCards + 5
	lanes := make([]string, n)
	titles := make([]string, n)
	for i := range lanes {
		lanes[i] = "lane"
		titles[i] = "card"
	}
	rec := kanbanRec(t, lanes, titles, nil, make([][]uint64, n))
	defer rec.Release()

	d := NewKanbanDriver(nil, nil)
	claim, _ := kanbanPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	d.rebuild(rec, rec.Schema(), claim.(kanbanClaim), nil)

	assert.Len(t, d.model.Cards, kanbanMaxCards)
	assert.Equal(t, int64(5), d.truncated)
	assert.Contains(t, d.statusLine(), "5 more rows not shown", "the drop is surfaced, not silent")
	assert.True(t, strings.Contains(d.statusLine(), "LIMIT"), "the status says what to do about it")
}
