package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// glossRec builds a two-row record: a declared temperature column, a declared
// Luhn column, an undeclared float, a declaration with a typo, and a
// temperature declared over a string column (kind refused).
func glossRec(t *testing.T) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fb := array.NewFloat64Builder(mem)
	defer fb.Release()
	fb.AppendValues([]float64{21.5, -3}, nil)
	temps := fb.NewArray()
	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.AppendValues([]string{"4111111111111111", "4111111111111112"}, nil)
	pans := sb.NewArray()
	pb := array.NewFloat64Builder(mem)
	defer pb.Release()
	pb.AppendValues([]float64{1, 2}, nil)
	plain := pb.NewArray()
	tb := array.NewFloat64Builder(mem)
	defer tb.Release()
	tb.AppendValues([]float64{7, 8}, nil)
	typo := tb.NewArray()
	wb := array.NewStringBuilder(mem)
	defer wb.Release()
	wb.AppendValues([]string{"hot", "cold"}, nil)
	wrongKind := wb.NewArray()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t@gloss/temperature;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "pan@gloss/luhn", Type: arrow.BinaryTypes.String},
		{Name: "plain", Type: arrow.PrimitiveTypes.Float64},
		{Name: "x@gloss/temperatur;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "w@gloss/temperature;unit=C", Type: arrow.BinaryTypes.String},
	}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{temps, pans, plain, typo, wrongKind}, 2)
}

func TestGlossColumnsResolvePerSchema(t *testing.T) {
	app := &PlayApp{}
	rec := glossRec(t)
	defer rec.Release()
	cols := app.glossColumns(rec.Schema())
	require.Len(t, cols, 5)

	assert.Equal(t, "t", cols[0].label)
	assert.Equal(t, gloss.MediaTypeTemperature, cols[0].mediaType)
	assert.Equal(t, glossSourceAlias, cols[0].source)
	assert.True(t, cols[0].glossed())
	assert.Contains(t, glossHover(&cols[0]), "glossed as gloss/temperature;unit=C (alias) — spec: name:t@gloss/temperature;unit=C arrow:float64")

	assert.True(t, cols[1].glossed())
	assert.Empty(t, cols[2].mediaType, "an undeclared column is plain")
	assert.Equal(t, "spec: name:plain arrow:float64", glossHover(&cols[2]), "…and its hover shows only the spec line")

	assert.Equal(t, "gloss/temperatur", cols[3].mediaType)
	assert.Nil(t, cols[3].inst)
	assert.Contains(t, cols[3].reason, "unknown media type")
	assert.Contains(t, glossHover(&cols[3]), "refused")

	assert.NotNil(t, cols[4].inst, "the declaration is fine — the column kind is not")
	assert.False(t, cols[4].glossed())
	assert.Contains(t, cols[4].rowReason, "expects numeric, got text")
	assert.Contains(t, glossHover(&cols[4]), "not applied")

	// Same schema pointer → same slice, no re-resolution.
	again := app.glossColumns(rec.Schema())
	assert.Equal(t, &cols[0], &again[0])

	// An aliased column is never offered to the rules: a directive that
	// matches its spec line is shadowed by the alias — whether the alias
	// binds or is refused — and a plain column takes it.
	app.sql = "SELECT 1\n-- play: gloss gloss/raw name:\n"
	cols = app.glossColumns(rec.Schema())
	assert.Equal(t, glossSourceAlias, cols[0].source)
	assert.Equal(t, gloss.MediaTypeTemperature, cols[0].mediaType, "the alias still binds")
	assert.Equal(t, "directive line 2: gloss/raw ← name:", glossShadowedLine(cols[0].shadowed))
	assert.Contains(t, cols[3].reason, "unknown media type")
	assert.Equal(t, "directive line 2: gloss/raw ← name:", glossShadowedLine(cols[3].shadowed), "refused alias: the rule is still not offered")
	assert.Equal(t, gloss.MediaTypeRaw, cols[2].mediaType, "the plain column takes the directive")
	assert.Empty(t, cols[2].shadowed)
}

// The catalog listing: every default gloss binds with its sample token,
// renders a sample face where a sample fits, and spells its accepted kinds;
// the Insert buttons write a directive line's token and a gloss(…) call
// that expands back to the same token.
func TestGlossCatalogListing(t *testing.T) {
	cat := gloss.Default()
	for g := range cat.All() {
		token, params := glossSampleToken(g)
		sample, bound := bindGlossSample(g, token)
		require.True(t, bound, token)
		assert.NotEmpty(t, glossKindsLine(sample.inst), token)
		call := glosssql.Call("expr", g.MediaType(), params)
		mt, p, _, err := cat.BindToken(token)
		require.NoError(t, err, token)
		assert.Equal(t, gloss.CompactMediaType(mt, p), token, "the sample token binds as written")
		out, err := glosssql.Expand("SELECT "+call, cat)
		require.NoError(t, err, call)
		assert.Equal(t, `SELECT expr AS "expr@`+token+`"`, out, "Insert call and Insert rule agree on the token")
	}

	temp, _ := glossSampleToken(mustLookup(t, cat, gloss.MediaTypeTemperature))
	assert.Equal(t, "gloss/temperature;unit=K", temp, "a required closed-set parameter lands spelled, first value")
	sample, _ := bindGlossSample(mustLookup(t, cat, gloss.MediaTypeTemperature), temp)
	assert.True(t, sample.hasFace)
	assert.Equal(t, "numeric", glossKindsLine(sample.inst))
	sample, _ = bindGlossSample(mustLookup(t, cat, gloss.MediaTypeMasked), gloss.MediaTypeMasked)
	assert.Equal(t, "any", glossKindsLine(sample.inst))
	assert.Equal(t, gloss.MaskedFace, sample.face.Text)
	sample, _ = bindGlossSample(mustLookup(t, cat, gloss.MediaTypeMarkdown), gloss.MediaTypeMarkdown)
	assert.Equal(t, "text · bytes", glossKindsLine(sample.inst))
	sample, _ = bindGlossSample(mustLookup(t, cat, gloss.MediaTypePNG), gloss.MediaTypePNG)
	assert.Equal(t, "text · bytes", glossKindsLine(sample.inst), "an image column arrives as ClickHouse String: text is accepted")
	assert.Equal(t, "[image/png · 24 B]", sample.face.Text)
}

func mustLookup(t *testing.T, cat *gloss.Catalog, mediaType string) gloss.GlossI {
	t.Helper()
	g, ok := cat.Lookup(mediaType)
	require.True(t, ok, mediaType)
	return g
}

func TestGlossCellRendersFaceOrPlain(t *testing.T) {
	app := &PlayApp{}
	rec := glossRec(t)
	defer rec.Release()
	cols := app.glossColumns(rec.Schema())

	text, tone := app.glossCell(&cols[0], rec.Column(0), 0, false)
	assert.Equal(t, "21.5 °C", text)
	assert.Equal(t, gloss.ToneNeutral, tone)

	text, tone = app.glossCell(&cols[1], rec.Column(1), 1, false)
	assert.Equal(t, "4111 •••• •••• 1112 ✗", text)
	assert.Equal(t, gloss.ToneError, tone)

	text, _ = app.glossCell(&cols[2], rec.Column(2), 0, false)
	assert.Equal(t, "1", text, "plain column, plain text")
	text, _ = app.glossCell(&cols[3], rec.Column(3), 0, false)
	assert.Equal(t, "7", text, "a refused declaration renders plain")
	text, _ = app.glossCell(&cols[4], rec.Column(4), 0, false)
	assert.Equal(t, "hot", text, "a refused kind renders plain")

	// The raw toggle bypasses every gloss.
	app.tableOpts.rawCells = true
	text, tone = app.glossCell(&cols[0], rec.Column(0), 0, false)
	assert.Equal(t, "21.5", text)
	assert.Equal(t, gloss.ToneNeutral, tone)
	assert.True(t, app.anyGlossed(rec.Schema()), "the toggle is offered while a column is glossed")

	// The text-backed variant (per-attribute grid) applies the same face.
	app.tableOpts.rawCells = false
	assert.Equal(t, "-3.0 °C", app.glossText(&cols[0], "-3", gloss.ValueKindNumeric))
	assert.Equal(t, "", app.glossText(&cols[0], "", gloss.ValueKindNumeric), "an absent item stays blank")
}

// leewayGlossRec mints a leeway-shaped schema with the ADR-0181 Composer —
// an id, a `sensor.temperature` value carrying sem:secret, a `sensor.reading`
// value carrying sem:url — so the rule route has aspects to match.
func leewayGlossRec(t *testing.T) arrow.RecordBatch {
	t.Helper()
	comp, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	require.NoError(t, err)
	idName, err := comp.PlainColumn("id", "u64", []string{"item:id"})
	require.NoError(t, err)
	tempName, err := comp.TaggedValueColumn("sensor", "temperature", "f64", []string{"sem:secret"})
	require.NoError(t, err)
	urlName, err := comp.TaggedValueColumn("sensor", "reading", "s", []string{"sem:url"})
	require.NoError(t, err)
	membName, err := comp.MembershipColumn("sensor", "low-card-ref")
	require.NoError(t, err)

	mem := memory.NewGoAllocator()
	ib := array.NewUint64Builder(mem)
	defer ib.Release()
	ib.AppendValues([]uint64{1, 2}, nil)
	ids := ib.NewArray()
	// Tagged values arrive as lists (one item per attribute).
	tb := array.NewListBuilder(mem, arrow.PrimitiveTypes.Float64)
	defer tb.Release()
	tvb := tb.ValueBuilder().(*array.Float64Builder)
	tb.Append(true)
	tvb.Append(21.5)
	tb.Append(true)
	tvb.Append(-3)
	temps := tb.NewArray()
	ub := array.NewListBuilder(mem, arrow.BinaryTypes.String)
	defer ub.Release()
	uvb := ub.ValueBuilder().(*array.StringBuilder)
	ub.Append(true)
	uvb.Append("https://example.com/1")
	ub.Append(true)
	uvb.Append("https://example.com/2")
	urls := ub.NewArray()
	mb := array.NewListBuilder(mem, arrow.PrimitiveTypes.Uint64)
	defer mb.Release()
	mvb := mb.ValueBuilder().(*array.Uint64Builder)
	mb.Append(true)
	mvb.Append(7)
	mb.Append(true)
	mvb.Append(7)
	membs := mb.NewArray()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: idName, Type: arrow.PrimitiveTypes.Uint64},
		{Name: tempName, Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
		{Name: urlName, Type: arrow.ListOf(arrow.BinaryTypes.String)},
		{Name: membName, Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{ids, temps, urls, membs}, 2)
}

func TestScanGlossDirectives(t *testing.T) {
	sql := "SELECT 1\n-- play: gloss gloss/temperature;unit=K name:.*temp\\b\n  --   PLAY: GLOSS gloss/raw sem:secret  \n-- play: enum x a,b\n-- play: gloss\n"
	ds := scanGlossDirectives(sql)
	require.Len(t, ds, 3)
	assert.Equal(t, glossDirective{line: 2, token: "gloss/temperature;unit=K", pattern: `name:.*temp\b`}, ds[0])
	assert.Equal(t, glossDirective{line: 3, token: "gloss/raw", pattern: "sem:secret"}, ds[1], "keyword case-insensitive, padding trimmed")
	assert.Equal(t, glossDirective{line: 5, token: "", pattern: ""}, ds[2], "an incomplete line is kept so the compiler can name what is missing")
	assert.Empty(t, scanGlossDirectives("SELECT 1"))
}

// The rule route end to end on a leeway result: affinities apply to aspects,
// a directive outranks an affinity, an alias outranks both, and every source
// is named on hover with the spec line.
func TestGlossRulesOnLeewaySchema(t *testing.T) {
	app := &PlayApp{}
	rec := leewayGlossRec(t)
	defer rec.Release()
	schema := rec.Schema()

	// No directives: affinities alone.
	cols := app.glossColumns(schema)
	require.Len(t, cols, 4)
	assert.Empty(t, cols[0].mediaType, "the id has no affinity")
	assert.Contains(t, glossHover(&cols[0]), "spec: name:id item:id ct:u64 arrow:uint64", "a plain column still shows its spec line")

	assert.Equal(t, gloss.MediaTypeMasked, cols[1].mediaType)
	assert.Equal(t, `affinity: \bsem:secret\b`, cols[1].source)
	assert.True(t, cols[1].glossed(), "a secret accepts any kind: the per-row cell of the list column is masked whole")
	assert.True(t, cols[1].glossedElem(), "…and so are its items in the per-attribute grid")
	assert.Contains(t, cols[1].specLine, "name:temperature section:sensor role:val ct:f64 sem:secret arrow:list<item: float64")

	assert.Equal(t, gloss.MediaTypeURL, cols[2].mediaType)
	assert.Equal(t, "https://example.com/1", app.glossText(&cols[2], "https://example.com/1", gloss.KindOfArrow(arrow.BinaryTypes.String)))
	assert.Equal(t, gloss.MaskedFace, app.glossText(&cols[1], "21.5", gloss.ValueKindNumeric))
	assert.Empty(t, cols[3].mediaType, "the membership lane carries no matching aspect")

	assert.Empty(t, cols[1].shadowed, "one affinity matches: nothing behind it")

	// A directive outranks the affinity, and gloss/raw suppresses it.
	app.sql = "SELECT *\n-- play: gloss gloss/raw sem:secret\n-- play: gloss gloss/temperature;unit=C name:temperature\n"
	cols = app.glossColumns(schema)
	assert.Equal(t, gloss.MediaTypeRaw, cols[1].mediaType, "first matching directive wins, in buffer order")
	assert.Equal(t, "directive line 2: sem:secret", cols[1].source)
	assert.Equal(t, "21.5", app.glossText(&cols[1], "21.5", gloss.ValueKindNumeric))
	assert.Empty(t, app.glossNotes())
	// The Glosses tab lists what the winner shadowed (§SD3): the later
	// directive, then the affinity — in the order they would have applied.
	assert.Equal(t, `directive line 3: gloss/temperature;unit=C ← name:temperature · affinity: gloss/masked ← \bsem:secret\b`,
		glossShadowedLine(cols[1].shadowed))
	assert.Empty(t, cols[2].shadowed, "the url column matches its affinity alone")

	// A second directive set: the temperature rule now reaches the column —
	// on the items (per-attribute grid); the whole list is not a number, so
	// the per-row cell stays [len=1].
	app.sql = "SELECT *\n-- play: gloss gloss/temperature;unit=C name:temperature\n"
	cols = app.glossColumns(schema)
	assert.Equal(t, gloss.MediaTypeTemperature, cols[1].mediaType)
	assert.Equal(t, "C", cols[1].params[gloss.ParamUnit])
	assert.False(t, cols[1].glossed())
	assert.True(t, cols[1].glossedElem())
	assert.Equal(t, "21.5 °C", app.glossText(&cols[1], "21.5", gloss.ValueKindNumeric))
	text, _ := app.glossCell(&cols[1], rec.Column(1), 0, false)
	assert.Equal(t, "[len=1]", text)

	// A directive that does not compile is a note, and the rest still apply.
	app.sql = "SELECT *\n-- play: gloss gloss/temperatur;unit=C name:temperature\n-- play: gloss gloss/temperature name:temperature\n-- play: gloss\n"
	cols = app.glossColumns(schema)
	notes := app.glossNotes()
	require.Len(t, notes, 3)
	assert.Contains(t, notes[0], "line 2")
	assert.Contains(t, notes[0], "unknown media type")
	assert.Contains(t, notes[1], "requires unit=")
	assert.Contains(t, notes[2], "no slash")
	assert.Equal(t, gloss.MediaTypeMasked, cols[1].mediaType, "back to the affinity")

	// The cache keys on the directive set: same schema, same lines → no work.
	again := app.glossColumns(schema)
	assert.Equal(t, &cols[0], &again[0])
}

// A gloss/url cell is a hyperlink to its value; every other cell is not.
func TestGlossLink(t *testing.T) {
	app := &PlayApp{}
	mem := memory.NewGoAllocator()
	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.AppendValues([]string{" https://example.com/a ", ""}, []bool{true, false})
	urls := sb.NewArray()
	defer urls.Release()
	fb := array.NewFloat64Builder(mem)
	defer fb.Release()
	fb.Append(1)
	nums := fb.NewArray()
	defer nums.Release()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "link@gloss/url", Type: arrow.BinaryTypes.String},
		{Name: "t@gloss/temperature;unit=C", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	cols := app.glossColumns(schema)
	assert.Equal(t, "https://example.com/a", app.glossLink(&cols[0], urls, 0), "trimmed value")
	assert.Equal(t, "", app.glossLink(&cols[0], urls, 1), "a null cell links nowhere")
	assert.Equal(t, "", app.glossLink(&cols[1], nums, 0), "only gloss/url links")
	app.tableOpts.rawCells = true
	assert.Equal(t, "", app.glossLink(&cols[0], urls, 0), "raw cells: no link either")
}

// fitRunes bounds what a re-fit cell hands egui_table to measure.
func TestFitRunes(t *testing.T) {
	assert.Equal(t, "abc", fitRunes("abc", 3))
	assert.Equal(t, "ab…", fitRunes("abcd", 2))
	assert.Equal(t, "••…", fitRunes("••••", 2), "runes, not bytes")
	assert.Equal(t, "", fitRunes("", 5))
}

// The card's block seam (ADR-0186 Update): only a column whose gloss has a
// block face in this pane is claimed — gloss/url's hyperlink, the content
// family — with a height the card reserves; values are counted per column
// so the row's artifact cache keys each; the raw toggle and a bare app
// (no cache) yield no seam at all.
func TestCardCellBlock(t *testing.T) {
	app := &PlayApp{richCells: newRichCellCache(nil)}
	rec := leewayGlossRec(t)
	defer rec.Release()
	schema := rec.Schema()

	// Affinities alone: the url column qualifies (a hyperlink face), the
	// masked one does not (inline only), the id is plain.
	fn := app.cardCellBlock(schema)
	require.NotNil(t, fn)
	_, ok := fn(0, "1")
	assert.False(t, ok, "plain")
	_, ok = fn(1, "21.5")
	assert.False(t, ok, "gloss/masked has no block face")
	b, ok := fn(2, " https://example.com/1 ")
	require.True(t, ok)
	assert.NotNil(t, b.Render)
	assert.Zero(t, b.Height, "a hyperlink is one line: the card's minimum")
	_, ok = fn(9, "x")
	assert.False(t, ok, "out of range")

	// A directive binds markdown to the text column: a scrolling text face,
	// its height estimated from the source lines and capped, one cache entry
	// per value in drive order.
	app.sql = "SELECT *\n-- play: gloss text/markdown name:reading\n"
	fn = app.cardCellBlock(schema)
	require.NotNil(t, fn)
	b, ok = fn(2, "# title\nline two\nline three")
	require.True(t, ok)
	assert.InDelta(t, 3*cardBlockLineHeight+cardBlockPad, b.Height, 0.01)
	b, ok = fn(2, strings.Repeat("x\n", 40))
	require.True(t, ok)
	assert.InDelta(t, cardBlockMaxLines*cardBlockLineHeight+cardBlockPad, b.Height, 0.01, "capped")
	assert.Contains(t, app.richCells.entries, richKey{col: 2, ord: 0})
	assert.Contains(t, app.richCells.entries, richKey{col: 2, ord: 1})
	assert.NotNil(t, app.richCells.entries[richKey{col: 2, ord: 0}].doc, "parsed once, retained for the frame's draw")

	// The raw toggle bypasses the seam; so does an app without the cache.
	app.tableOpts.rawCells = true
	assert.Nil(t, app.cardCellBlock(schema))
	app.tableOpts.rawCells = false
	assert.Nil(t, (&PlayApp{}).cardCellBlock(schema), "no artifact cache: no block faces")

	// A schema without any block-faced column: nil, so the emitter skips it.
	plain := glossRec(t)
	defer plain.Release()
	assert.Nil(t, app.cardCellBlock(plain.Schema()), "temperature, luhn: inline faces only")
}
