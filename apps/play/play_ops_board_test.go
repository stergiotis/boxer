package play

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cardgrid"
)

func TestBoardOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	k, ok := m.Operations.Lookup(opGetKanban)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectNone, k.Effect)
	assert.True(t, k.Untrusted)
	assert.True(t, k.Agents)
	assert.Equal(t, []string{opsResResult, opsResPanes}, k.Reads, "the board has no options, so no resource")
	_, ok = m.Operations.Lookup("set_kanban_options")
	assert.False(t, ok)

	get, ok := m.Operations.Lookup(opGetCards)
	require.True(t, ok)
	assert.True(t, get.Untrusted)
	assert.Equal(t, []string{opsResCards, opsResResult, opsResPanes}, get.Reads)
	set, ok := m.Operations.Lookup(opSetCardsOptions)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectDocument, set.Effect)
	assert.Equal(t, []string{opsResCards}, set.Writes)
	assert.NotEmpty(t, set.Gesture)
}

// kanbanBoard folds four cards over a declared todo/doing lane set and a
// lane only the rows name.
func kanbanBoard(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	rec := kanbanRec(t, []string{"todo", "extra", "todo", "doing"}, []string{"a", "b", "c", "d"},
		[]string{"dot_open"}, [][]uint64{{2}, {1}, {3}, {0}})
	k, reason := resolveKanbanColumns(rec.Schema())
	require.Empty(t, reason)
	d := p.kanbanDriver
	d.noteExecuted(time.Unix(1, 0))
	d.rebuild(rec, rec.Schema(), k, []string{"todo", "doing", "done"})
	d.lanesNode = true
	drawnPane(p, kanbanPaneId, 2, rec.Schema())
	return rec
}

func TestGetKanbanCountsLanesAndSumsDots(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := kanbanBoard(t, p)
	defer rec.Release()

	r := queryOp[KanbanReading](t, h, opGetKanban, GetKanbanArgs{})
	assert.Equal(t, uint64(2), r.Drawn.ResultId)
	assert.Equal(t, int64(4), r.Cards)
	assert.Equal(t, []string{"open"}, r.DotLabels)
	assert.Equal(t, "declared", r.LanesQuery)
	assert.Equal(t, []KanbanLaneReading{
		{Lane: "todo", Declared: true, Cards: 2, Dots: []int64{5}},
		{Lane: "doing", Declared: true, Cards: 1, Dots: []int64{0}},
		{Lane: "done", Declared: true, Cards: 0, Dots: []int64{0}},
		{Lane: "extra", Cards: 1, Dots: []int64{1}},
	}, r.Lanes)
	assert.Nil(t, r.Lane)

	r = queryOp[KanbanReading](t, h, opGetKanban, GetKanbanArgs{Lane: "todo", Offset: 1})
	require.NotNil(t, r.Lane)
	assert.Equal(t, []KanbanCardReading{{Row: 2, Title: "c", Dots: []int64{3}}}, r.Lane.Cards)

	require.Error(t, queryErr(t, h, opGetKanban, GetKanbanArgs{Lane: "nope"}))

	p.kanbanDriver.lanesErr = errors.New("agent limit: no grant for this destination")
	r = queryOp[KanbanReading](t, h, opGetKanban, GetKanbanArgs{})
	assert.Equal(t, "failed", r.LanesQuery)
	assert.Contains(t, r.LanesError, "agent limit")
}

// cardsPane folds the first page (12 cards) of a 30-row result whose hero
// has no media type and whose rows carry an owner fact.
func cardsPane(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	titles, heroes, owners := make([]any, 30), make([]any, 30), make([]any, 30)
	for i := range 30 {
		titles[i], heroes[i], owners[i] = "card "+strconv.Itoa(i), []byte("xyz"), "ann"
	}
	rec := cardgridRec(t,
		cardgridCol{strField("card_title"), titles},
		cardgridCol{binField("card_hero"), heroes},
		cardgridCol{strField("owner"), owners},
	)
	d := p.cardgridDriver
	k, reason := d.claimOf(rec.Schema())
	require.Empty(t, reason)
	d.pager.Configure(rec.NumRows())
	d.rows = rec.NumRows()
	d.pager.SetPageSize(12) // keeps row 12, mid-page at 24, in view
	d.pager.GoToIndex(0)
	start, end := d.pager.Range()
	d.refold(p, rec, rec.Schema(), p.glossColumns(rec.Schema()), ResultID(9), k, start, end, false)
	drawnPane(p, cardsPaneId, 9, rec.Schema())
	return rec
}

func TestGetCardsReadsThePageAsDrawn(t *testing.T) {
	l, h := opsLauncher(t)
	rec := cardsPane(t, l.inner)
	defer rec.Release()

	r := queryOp[CardsReading](t, h, opGetCards, GetCardsArgs{Limit: 2})
	assert.Equal(t, uint64(9), r.Drawn.ResultId)
	assert.Equal(t, int64(30), r.Rows)
	assert.Equal(t, int64(1), r.Page)
	assert.Equal(t, int64(3), r.Pages)
	assert.Equal(t, int64(12), r.PageSize)
	assert.Equal(t, "M", r.Density)
	assert.Equal(t, "16:9", r.Aspect)
	require.Len(t, r.Cards, 2)
	assert.Equal(t, int32(10), r.More)
	c := r.Cards[1]
	assert.Equal(t, int64(1), c.Row)
	assert.Equal(t, "card 1", c.Title)
	assert.Equal(t, []CardFactReading{{Label: "owner", Value: "ann"}}, c.Facts)
	assert.Empty(t, c.Hero)
	assert.Contains(t, c.HeroNote, "no media type")

	r = queryOp[CardsReading](t, h, opGetCards, GetCardsArgs{Offset: 11})
	require.Len(t, r.Cards, 1)
	assert.Equal(t, int64(11), r.Cards[0].Row)
	require.Error(t, queryErr(t, h, opGetCards, GetCardsArgs{Offset: 12}))
}

func TestSetCardsOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := cardsPane(t, p)
	defer rec.Release()
	d := p.cardgridDriver
	before := h.ResourceValue(opsResCards)

	density, aspect := "l", "1:1"
	page, size := int64(2), int64(24)
	require.NoError(t, applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{Density: &density, Aspect: &aspect, PageSize: &size, Page: &page}))
	assert.Equal(t, cardgrid.DensityLarge, d.state.Density())
	assert.Equal(t, cardgrid.Aspect1x1, d.state.Aspect())
	assert.Equal(t, int64(24), d.pager.PageSize())
	assert.Equal(t, int64(1), d.pager.CurrentPage(), "page 2 at the new size")
	assert.NotEqual(t, before, h.ResourceValue(opsResCards))

	far, odd, bad := int64(3), int64(20), "XL"
	require.Error(t, applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{Page: &far}), "two pages at 24 a page")
	require.Error(t, applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{PageSize: &odd}))
	require.Error(t, applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{Density: &bad}))
	require.Error(t, applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{}))
	assert.Equal(t, cardgrid.DensityLarge, d.state.Density(), "a refused call applies nothing")

	// No hero, no hero box to shape.
	norec := cardgridRec(t, cardgridCol{strField("card_title"), []any{"x"}})
	defer norec.Release()
	_, reason := d.claimOf(norec.Schema())
	require.Empty(t, reason)
	err := applyOp(t, h, opSetCardsOptions, SetCardsOptionsArgs{Aspect: &aspect})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "card_hero")
}

// The toolbar's and the pager's changes go through set_cards_options as the
// person's.
func TestCardsControlsGoThroughTheirCommand(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	rec := cardsPane(t, p)
	defer rec.Release()
	page := int64(3)
	p.cardgridDriver.requestOptions(SetCardsOptionsArgs{Page: &page})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetCardsOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, []string{opsResCards}, e.Resources)
	assert.Equal(t, int64(2), p.cardgridDriver.pager.CurrentPage())
}
