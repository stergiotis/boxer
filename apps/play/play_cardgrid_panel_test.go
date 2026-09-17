package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cardgrid"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0245 §Verification: the card contract and its rejects (§SD1), row-value
// precedence, the token cap and both gates (§SD2), and the page fold.

func binField(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.BinaryTypes.Binary} }
func i64Field(n string) arrow.Field { return arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Int64} }
func strListField(n string) arrow.Field {
	return arrow.Field{Name: n, Type: arrow.ListOf(arrow.BinaryTypes.String)}
}

// cardgridCol is one column of a test record; nil is NULL, and the Go type of
// a value follows the field's type (string, []byte, int64, []string).
type cardgridCol struct {
	field arrow.Field
	vals  []any
}

func cardgridRec(t *testing.T, cols ...cardgridCol) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	n := -1
	for _, col := range cols {
		if n < 0 {
			n = len(col.vals)
		}
		require.Len(t, col.vals, n)
		fields = append(fields, col.field)
		switch col.field.Type.ID() {
		case arrow.BINARY:
			b := array.NewBinaryBuilder(alloc, arrow.BinaryTypes.Binary)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.([]byte))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		case arrow.INT64:
			b := array.NewInt64Builder(alloc)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(int64))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		case arrow.LIST:
			b := array.NewListBuilder(alloc, arrow.BinaryTypes.String)
			vb := b.ValueBuilder().(*array.StringBuilder)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
					continue
				}
				b.Append(true)
				for _, s := range v.([]string) {
					vb.Append(s)
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		default:
			b := array.NewStringBuilder(alloc)
			for _, v := range col.vals {
				if v == nil {
					b.AppendNull()
				} else {
					b.Append(v.(string))
				}
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
		}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(n))
	for _, a := range arrs {
		a.Release()
	}
	return rec
}

func cardgridSchema(fields ...arrow.Field) *arrow.Schema { return arrow.NewSchema(fields, nil) }

func TestCardgridAcceptContract(t *testing.T) {
	t.Run("the sketch query claims", func(t *testing.T) {
		k, reason := resolveCardgridColumns(cardgridSchema(
			strField("card_title"), strField("card_overline"), binField("card_hero"), strField("card_hero_gloss"),
			strField("card_body@text/markdown"), i64Field("length@gloss/duration;unit=ms"), i64Field("sample_rate")))
		require.Empty(t, reason)
		assert.Equal(t, 0, k.title)
		assert.Equal(t, 1, k.overline)
		assert.Equal(t, 2, k.hero)
		assert.Equal(t, 4, k.body, "the slot is matched on the gloss label")
		assert.Equal(t, map[int]int{2: 3}, k.companionOf)
		assert.Equal(t, []int{5, 6}, k.factCols, "unclaimed columns are facts; the companion is not one")
		assert.True(t, k.slots().Has(cardgrid.SlotsHero|cardgrid.SlotsTitle|cardgrid.SlotsBody|cardgrid.SlotsFacts))
		assert.False(t, k.slots().Has(cardgrid.SlotsFooter))
	})
	t.Run("a hero alone is a gallery", func(t *testing.T) {
		k, reason := resolveCardgridColumns(cardgridSchema(binField("card_hero@image/png")))
		require.Empty(t, reason)
		assert.Equal(t, cardgrid.SlotsHero, k.slots())
	})

	rejects := []struct {
		name   string
		schema *arrow.Schema
		want   string
	}{
		{"no schema", nil, "need a `card_title` or a `card_hero`"},
		{"neither required slot", cardgridSchema(strField("card_subtitle"), strField("x")), "need a `card_title` or a `card_hero`"},
		{"a typo is not a fact", cardgridSchema(strField("card_title"), strField("card_titel")), "`card_titel` is not a card slot"},
		{"a slot claimed twice", cardgridSchema(strField("card_title"), strField("card_title@text/plain")), "two columns claim `card_title`"},
		{"a gloss for an unglossed slot", cardgridSchema(strField("card_title"), strField("card_tone"), strField("card_tone_gloss")), "does not render through one"},
		{"a companion that is not text", cardgridSchema(strField("card_title"), binField("card_hero"), i64Field("card_hero_gloss")), "must be text"},
		{"a companion with no slot beside it", cardgridSchema(strField("card_title"), strField("card_hero_gloss")), "`card_hero_gloss` is not a card slot"},
	}
	for _, tc := range rejects {
		_, reason := resolveCardgridColumns(tc.schema)
		assert.Contains(t, reason, tc.want, tc.name)
	}
}

func TestRowGlossCompanions(t *testing.T) {
	assert.Nil(t, rowGlossCompanions(cardgridSchema(strField("a"), strField("b"))))
	assert.Equal(t, map[int]int{0: 1}, rowGlossCompanions(cardgridSchema(binField("payload"), strField("payload_gloss"))))
	assert.Equal(t, map[int]int{0: 1}, rowGlossCompanions(cardgridSchema(binField("payload@image/png"), strField("payload_gloss"))),
		"the pairing is by gloss label")
	assert.Nil(t, rowGlossCompanions(cardgridSchema(strField("lip"), i64Field("lip_gloss"))), "a companion carries text")
	assert.Nil(t, rowGlossCompanions(cardgridSchema(strField("lip_gloss"))), "and sits beside its column")
}

func TestRowGlossResolve(t *testing.T) {
	rec := cardgridRec(t,
		cardgridCol{binField("card_hero@image/png"), []any{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")}},
		cardgridCol{strField("card_hero_gloss"), []any{"image/jpeg", nil, "", "image/pgn", "jpeg"}},
		cardgridCol{i64Field("ms"), []any{int64(1), int64(2), int64(3), int64(4), int64(5)}},
		cardgridCol{strField("ms_gloss"), []any{"gloss/duration;unit=ms", "high", "gloss/duration;unti=ms", "text/markdown", nil}},
	)
	defer rec.Release()
	app := &PlayApp{}
	cols := app.glossColumns(rec.Schema())
	b := newRowGlossBinder(app.glossCatalog())

	// The row value outranks the alias; a NULL or empty one falls through.
	gc := b.resolve(rec, 0, 1, 0, &cols[0], true)
	assert.Equal(t, gloss.MediaTypeJPEG, gc.mediaType)
	assert.Equal(t, "card_hero", gc.label)
	assert.Equal(t, "row value: card_hero_gloss", gc.source)
	assert.True(t, gc.rowOK)
	assert.Same(t, &cols[0], b.resolve(rec, 0, 1, 1, &cols[0], true), "NULL falls through to the column's resolution")
	assert.Same(t, &cols[0], b.resolve(rec, 0, 1, 2, &cols[0], true), "so does the empty string")
	assert.Same(t, gc, b.resolve(rec, 0, 1, 0, &cols[0], true), "a token binds once")

	// Inside the reserved namespace everything that does not bind is loud.
	assert.Contains(t, b.resolve(rec, 0, 1, 3, &cols[0], true).reason, "unknown media type")
	assert.Contains(t, b.resolve(rec, 0, 1, 4, &cols[0], true).reason, "no slash")

	// Outside it the slash gate applies per value: no slash, no declaration.
	assert.Equal(t, gloss.MediaTypeDuration, b.resolve(rec, 2, 3, 0, &cols[2], false).mediaType)
	assert.Same(t, &cols[2], b.resolve(rec, 2, 3, 1, &cols[2], false), "`high` is a value, not a declaration")
	assert.NotEmpty(t, b.resolve(rec, 2, 3, 2, &cols[2], false).reason, "an undeclared parameter is loud")
	refused := b.resolve(rec, 2, 3, 3, &cols[2], false)
	assert.Empty(t, refused.reason)
	assert.False(t, refused.rowOK, "markdown refuses a number, with the reason")
	assert.NotEmpty(t, refused.rowReason)

	b.reset()
	assert.NotSame(t, gc, b.resolve(rec, 0, 1, 0, &cols[0], true), "a new result binds afresh")
}

func TestRowGlossTokenCap(t *testing.T) {
	n := rowGlossMaxTokens + 5
	vals, tokens := make([]any, n), make([]any, n)
	for i := range n {
		vals[i] = "v"
		tokens[i] = "text/plain; charset=c" + strings.Repeat("x", i)
	}
	rec := cardgridRec(t, cardgridCol{strField("card_title"), vals}, cardgridCol{strField("card_title_gloss"), tokens})
	defer rec.Release()
	b := newRowGlossBinder((&PlayApp{}).glossCatalog())
	for i := range rowGlossMaxTokens {
		assert.Empty(t, b.resolve(rec, 0, 1, int64(i), nil, true).reason, "token %d is within the cap", i)
	}
	over := b.resolve(rec, 0, 1, int64(rowGlossMaxTokens), nil, true)
	assert.Contains(t, over.reason, "more than")
	assert.Same(t, over, b.resolve(rec, 0, 1, int64(n-1), nil, true), "past the cap every row shares one resolution")
	assert.Len(t, b.tokens[1], rowGlossMaxTokens)
	assert.Empty(t, b.resolve(rec, 0, 1, 0, nil, true).reason, "a token bound before the cap keeps its binding")
}

func cardgridFold(t *testing.T, rec arrow.RecordBatch, start, end int64) (*CardGridDriver, *cardgrid.Model) {
	t.Helper()
	app := &PlayApp{}
	k, reason := resolveCardgridColumns(rec.Schema())
	require.Empty(t, reason)
	d := NewCardGridDriver(nil, nil, app.glossCatalog())
	d.refold(app, rec, rec.Schema(), app.glossColumns(rec.Schema()), ResultID(1), k, start, end, false)
	require.NoError(t, d.model.Validate())
	return d, d.model
}

func TestCardgridFold(t *testing.T) {
	long := strings.Repeat("/very/long/path", 400)
	rec := cardgridRec(t,
		cardgridCol{strField("card_title"), []any{"Sweep", long, nil, "four"}},
		cardgridCol{binField("card_hero"), []any{[]byte("x"), nil, []byte("y"), []byte("z")}},
		cardgridCol{strField("card_hero_gloss"), []any{"audio/nope", "image/png", "image/png", nil}},
		cardgridCol{strField("card_body@text/markdown"), []any{"# hi", "plain", nil, "x"}},
		cardgridCol{strField("card_body_gloss"), []any{nil, "text/x-unknown", nil, nil}},
		cardgridCol{strField("card_tone"), []any{"success", "Warning ", "chartreuse", nil}},
		cardgridCol{strListField("card_tags"), []any{[]string{"a", "b"}, nil, []string{}, []string{"c"}}},
		cardgridCol{i64Field("length@gloss/duration;unit=ms"), []any{int64(65000), nil, int64(12), int64(1)}},
		cardgridCol{i64Field("bytes"), []any{int64(40858), int64(1), nil, int64(2)}},
		cardgridCol{strField("bytes_gloss"), []any{"gloss/bytes", "large", nil, "gloss/bytez"}},
	)
	defer rec.Release()
	d, m := cardgridFold(t, rec, 0, 4)

	assert.Equal(t, 4, m.Count)
	assert.Equal(t, "Sweep", m.Title[0])
	assert.LessOrEqual(t, len([]rune(m.Title[1])), cardgrid.MaxTitleRunes+1, "a long title is bounded before it enters the model")
	assert.Equal(t, "", m.Title[2], "NULL is an unfilled slot")

	// The body: a block-faced one leaves the text to the block; one whose
	// row value does not bind keeps its plain text.
	assert.Equal(t, "", m.Body[0], "markdown is drawn by the body block")
	assert.Equal(t, "plain", m.Body[1])

	// Tones: the ADR-0122 vocabulary, case- and space-insensitive; unknown
	// ones draw neutral and are counted.
	assert.NotEqual(t, color.ColorKindNone, m.Tone[0].Kind())
	assert.NotEqual(t, color.ColorKindNone, m.Tone[1].Kind())
	assert.Equal(t, color.ColorKindNone, m.Tone[2].Kind())
	assert.Equal(t, 1, d.unknownTones)

	assert.Equal(t, []int32{0, 2, 2, 2, 3}, m.TagOff)
	assert.Equal(t, []string{"a", "b", "c"}, m.Tag)

	facts := func(i int) (out []string) {
		for j := m.FactOff[i]; j < m.FactOff[i+1]; j++ {
			out = append(out, m.FactLabel[j]+"="+m.FactValue[j])
		}
		return
	}
	// Row 0: both facts through their glosses — one by alias, one by row value.
	assert.Equal(t, []string{"length=1m 05s", "bytes=40 KiB"}, facts(0))
	// Row 1: the body's unbound row value is reported as a fact, ahead of the
	// result's own; NULL facts are skipped; `large` has no slash, so outside
	// the card_ namespace it is not a declaration.
	f1 := facts(1)
	require.Len(t, f1, 2)
	assert.True(t, strings.HasPrefix(f1[0], "card_body=unknown media type"), f1[0])
	assert.Equal(t, "bytes=1", f1[1])
	// Row 3: a row value with a slash that does not bind is loud as the
	// fact's value.
	f3 := facts(3)
	require.Len(t, f3, 2)
	assert.Contains(t, f3[1], "unknown media type")
}

func TestCardgridFoldIsPerPageAndCached(t *testing.T) {
	rec := cardgridRec(t, cardgridCol{strField("card_title"), []any{"a", "b", "c", "d", "e"}})
	defer rec.Release()
	app := &PlayApp{}
	k, _ := resolveCardgridColumns(rec.Schema())
	cols := app.glossColumns(rec.Schema())
	d := NewCardGridDriver(nil, nil, app.glossCatalog())

	d.refold(app, rec, rec.Schema(), cols, ResultID(1), k, 2, 4, false)
	first := d.model
	assert.Equal(t, []string{"c", "d"}, first.Title, "only the page is folded")
	gen := d.generation
	d.refold(app, rec, rec.Schema(), cols, ResultID(1), k, 2, 4, false)
	assert.Same(t, first, d.model, "unchanged inputs keep the fold")
	assert.Equal(t, gen, d.generation, "and the page's artifacts")

	d.refold(app, rec, rec.Schema(), cols, ResultID(1), k, 4, 5, false)
	assert.Equal(t, []string{"e"}, d.model.Title)
	assert.Greater(t, d.generation, gen, "a page turn drops the page's artifacts")
	gen = d.generation
	d.refold(app, rec, rec.Schema(), cols, ResultID(2), k, 4, 5, false)
	assert.Greater(t, d.generation, gen, "so does a new result")
}

func TestCardgridSlotBlockStandIns(t *testing.T) {
	png := tinyPNG(t, 40, 10)
	rec := cardgridRec(t,
		cardgridCol{strField("card_title"), []any{"ok", "null", "bad type", "not an image", "plain bytes"}},
		cardgridCol{binField("card_hero"), []any{png, nil, png, []byte("garbage"), png}},
		cardgridCol{strField("card_hero_gloss"), []any{"image/png", "image/png", "image/pgn", "image/png", nil}},
	)
	defer rec.Release()
	app := &PlayApp{}
	k, reason := resolveCardgridColumns(rec.Schema())
	require.Empty(t, reason)
	cols := app.glossColumns(rec.Schema())
	d := NewCardGridDriver(nil, nil, app.glossCatalog())
	box := cardgrid.Box{W: 320, H: 180}
	block := func(row int64) (cardgrid.Block, bool) {
		d.spent, d.builds = 0, 0
		return d.slotBlock(app, rec, rec.Schema(), cols, k, k.hero, row, box, false, true)
	}

	b, ok := block(0)
	require.True(t, ok)
	assert.NotNil(t, b.Render)
	assert.Equal(t, float32(40), b.W, "laid out by the source's size, never scaled up")
	assert.Equal(t, float32(10), b.H)

	_, ok = block(1)
	assert.False(t, ok, "a NULL hero is declined: the widget draws its placeholder")

	b, _ = block(2)
	assert.Contains(t, b.Reason, "unknown media type")
	b, _ = block(3)
	assert.NotEmpty(t, b.Reason, "an undecodable image says why")
	b, _ = block(4)
	assert.Contains(t, b.Reason, "no media type", "bytes are not guessed at")

	// The frame budget: once it is spent, an unbuilt artifact is a skeleton.
	d.dropArtifacts()
	d.spent, d.builds = cardgridFrameBudget, 1
	b, _ = d.slotBlock(app, rec, rec.Schema(), cols, k, k.hero, 0, box, false, true)
	assert.True(t, b.Pending)
	assert.True(t, d.pending)
	d.spent, d.builds = cardgridFrameBudget, 0
	b, _ = d.slotBlock(app, rec, rec.Schema(), cols, k, k.hero, 0, box, false, true)
	assert.False(t, b.Pending, "the first build of a frame always runs")
	assert.LessOrEqual(t, len(d.thumbs[richKey{col: k.hero, ord: 0}].pixels), cardgridThumbMaxSide*cardgridThumbMaxSide)
}

// ADR-0245 §SD8 M6: the grids, Detail and Chat read the companion through the
// app's resolution — the row value where there is one, the column's own
// otherwise, nothing at all with raw cells on.
func TestAppRowGloss(t *testing.T) {
	rec := cardgridRec(t,
		cardgridCol{binField("payload"), []any{[]byte("a"), []byte("b"), []byte("c")}},
		cardgridCol{strField("payload_gloss"), []any{"image/png", nil, "image/pgn"}},
		cardgridCol{strField("name"), []any{"x", "y", "z"}},
	)
	defer rec.Release()
	app := &PlayApp{}
	cols := app.glossColumns(rec.Schema())

	gc := app.rowGloss(rec, cols, 0, 0)
	require.NotNil(t, gc)
	assert.Equal(t, gloss.MediaTypePNG, gc.mediaType)
	assert.True(t, gc.fromRowValue())
	d, ok := gc.declaration("payload")
	assert.True(t, ok)
	assert.Equal(t, gloss.MediaTypePNG, d.MediaType)

	assert.Same(t, &cols[0], app.rowGloss(rec, cols, 0, 1), "no row value: the column's own resolution")
	assert.Same(t, &cols[2], app.rowGloss(rec, cols, 2, 0), "no companion: likewise")
	assert.True(t, app.isRowGlossCompanion(rec.Schema(), 1))
	assert.False(t, app.isRowGlossCompanion(rec.Schema(), 0))

	// Outside the card_ namespace a slashed value that does not bind is
	// still loud: the grid marks the cell.
	bad := app.rowGloss(rec, cols, 0, 2)
	assert.NotEmpty(t, bad.reason)
	_, tone := app.glossCell(bad, rec.Column(0), 2, false)
	assert.Equal(t, gloss.ToneWarning, tone)

	// The inline face reaches a grid cell through the row value.
	text, _ := app.glossCell(gc, rec.Column(0), 0, false)
	assert.Equal(t, "[image/png · 1 B]", text)

	app.tableOpts.rawCells = true
	assert.Same(t, &cols[0], app.rowGloss(rec, cols, 0, 0), "raw cells bypasses the row value with every other gloss")
	app.tableOpts.rawCells = false

	// A new result binds afresh.
	app.frameResult = ResultID(9)
	assert.NotSame(t, gc, app.rowGloss(rec, cols, 0, 0))

	var none *glossColumn
	_, ok = none.declaration("x")
	assert.False(t, ok, "a nil resolution declares nothing")
	assert.False(t, none.fromRowValue())
}
