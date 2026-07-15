package play

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0123 §Validation: the name parser and its two gates (§SD2), cellRaw over
// the Arrow string/binary types (§SD4), and the vocabulary's diagnostics
// (§SD3).

// The '@' gate and the slash gate. The slash is what separates a declaration
// from a column that merely has an '@' in its name.
func TestParseRichColumnGates(t *testing.T) {
	tests := []struct {
		name     string
		declared bool
		label    string
		mime     string
		kind     richKindE
	}{
		// Declarations.
		{"notes@text/markdown", true, "notes", mimeMarkdown, richKindMarkdown},
		{"shot@image/png", true, "shot", mimePNG, richKindImage},
		{"req@application/json", true, "req", mimeJSON, richKindJSON},
		{"q@application/sql", true, "q", mimeSQL, richKindSQL},
		{"src@text/x-go", true, "src", mimeGo, richKindGo},
		{"stack@text/plain", true, "stack", mimePlain, richKindPlain},

		// Not declarations: no '@' at all.
		{"lane", false, "", "", richKindNone},
		{"title", false, "", "", richKindNone},
		{"cond_0", false, "", "", richKindNone},
		// A '/' with no '@' is not a declaration either.
		{"a/b", false, "", "", richKindNone},

		// Not declarations: an '@' but no '/' after it. These are the names
		// the slash gate exists to protect — ADR-0122's dot vocabulary above
		// all, which shares the separator but not the meaning.
		{"dot_done@success", false, "", "", richKindNone},
		{"dot_cited@warning", false, "", "", richKindNone},
		{"user@example.com", false, "", "", richKindNone},
		{"weird@", false, "", "", richKindNone},

		// Leeway physical names carry many colons and no '@' — untouched.
		{"tv:symbol:value:val:u64:g:1d0DV72:0:0::", false, "", "", richKindNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, declared := parseRichColumn(tc.name)
			require.Equal(t, tc.declared, declared)
			if !declared {
				return
			}
			assert.Equal(t, tc.label, d.label)
			assert.Equal(t, tc.mime, d.mime)
			assert.Equal(t, tc.kind, d.kind)
			assert.Empty(t, d.reason, "a known type carries no reason")
		})
	}
}

// mime.ParseMediaType does the case-folding and parameter-splitting, so the
// parser does not have to.
func TestParseRichColumnCanonicalises(t *testing.T) {
	d, declared := parseRichColumn("notes@TEXT/Markdown")
	require.True(t, declared)
	assert.Equal(t, mimeMarkdown, d.mime, "the media type is case-insensitive")
	assert.Equal(t, richKindMarkdown, d.kind)

	d, declared = parseRichColumn("notes@text/markdown; charset=utf-8")
	require.True(t, declared)
	assert.Equal(t, mimeMarkdown, d.mime, "parameters are split off the type")
	assert.Equal(t, richKindMarkdown, d.kind)
	assert.Empty(t, d.reason)
}

// A declaration this pane cannot honour is declared-with-a-reason, never
// silently plain. This is the §SD2 rule: the typo mode of a convention must
// not be a wrong-but-plausible render.
func TestParseRichColumnDiagnostics(t *testing.T) {
	// A typo in a known type.
	d, declared := parseRichColumn("notes@text/markdwn")
	require.True(t, declared, "it has a slash, so it meant to declare something")
	assert.Equal(t, richKindNone, d.kind)
	assert.Contains(t, d.reason, "unknown content type")
	assert.Contains(t, d.reason, "text/markdwn")
	assert.Contains(t, d.reason, mimeMarkdown, "the reason lists the vocabulary")

	// A type we deliberately do not carry a decoder for.
	d, declared = parseRichColumn("logo@image/svg+xml")
	require.True(t, declared)
	assert.Equal(t, richKindNone, d.kind)
	assert.Contains(t, d.reason, "unknown content type")

	// Malformed past ParseMediaType's tolerance.
	d, declared = parseRichColumn("x@a/b/c")
	require.True(t, declared)
	assert.Equal(t, richKindNone, d.kind)
	assert.Contains(t, d.reason, "not a media type")

	// ';base64' is not a media parameter — a parameter is key=value, and the
	// data-URI spelling is a data-URI-ism. Recorded as a diagnostic rather
	// than quietly accepted.
	d, declared = parseRichColumn("logo@image/png;base64")
	require.True(t, declared)
	assert.Equal(t, richKindNone, d.kind)
	assert.Contains(t, d.reason, "not a media type")
}

// A declaration with nothing before the '@' still needs a caption.
func TestParseRichColumnEmptyLabel(t *testing.T) {
	d, declared := parseRichColumn("@text/markdown")
	require.True(t, declared)
	assert.Equal(t, "@text/markdown", d.label, "an empty caption reads as a fault")
	assert.Equal(t, richKindMarkdown, d.kind)
}

// Declared columns land in the ad-hoc pane's `data` section, which is where
// the section loop routes them.
func TestRichColumnsSectionAsData(t *testing.T) {
	assert.Equal(t, sectionData, sectionForColumn("notes@text/markdown"))
	assert.Equal(t, sectionData, sectionForColumn("shot@image/png"))
}

// oneColRec builds a single-column, single-row record from an arrow.Array.
func oneColRec(name string, dt arrow.DataType, col arrow.Array) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: dt, Nullable: true}}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{col}, int64(col.Len()))
}

// cellRaw reaches the bytes for every string and binary type, because a
// ClickHouse String is byte-arbitrary and lands as either depending on
// output_format_arrow_string_as_string.
func TestCellRawStringAndBinary(t *testing.T) {
	alloc := memory.NewGoAllocator()
	const payload = "# hi\n\nbody"

	t.Run("String", func(t *testing.T) {
		b := array.NewStringBuilder(alloc)
		defer b.Release()
		b.AppendValues([]string{payload}, nil)
		rec := oneColRec("x@text/markdown", arrow.BinaryTypes.String, b.NewStringArray())
		raw, ok := cellRaw(rec, 0, 0)
		require.True(t, ok)
		assert.Equal(t, payload, raw)
	})

	t.Run("LargeString", func(t *testing.T) {
		b := array.NewLargeStringBuilder(alloc)
		defer b.Release()
		b.AppendValues([]string{payload}, nil)
		rec := oneColRec("x@text/markdown", arrow.BinaryTypes.LargeString, b.NewLargeStringArray())
		raw, ok := cellRaw(rec, 0, 0)
		require.True(t, ok)
		assert.Equal(t, payload, raw)
	})

	t.Run("Binary", func(t *testing.T) {
		b := array.NewBinaryBuilder(alloc, arrow.BinaryTypes.Binary)
		defer b.Release()
		b.AppendValues([][]byte{[]byte(payload)}, nil)
		rec := oneColRec("x@text/markdown", arrow.BinaryTypes.Binary, b.NewBinaryArray())
		raw, ok := cellRaw(rec, 0, 0)
		require.True(t, ok)
		// The point of cellRaw: formatCell would hex-encode this.
		assert.Equal(t, payload, raw)
		assert.NotEqual(t, formatCell(rec, 0, 0), raw)
	})

	t.Run("LargeBinary", func(t *testing.T) {
		b := array.NewBinaryBuilder(alloc, arrow.BinaryTypes.LargeBinary)
		defer b.Release()
		b.AppendValues([][]byte{[]byte(payload)}, nil)
		rec := oneColRec("x@text/markdown", arrow.BinaryTypes.LargeBinary, b.NewLargeBinaryArray())
		raw, ok := cellRaw(rec, 0, 0)
		require.True(t, ok)
		assert.Equal(t, payload, raw)
	})
}

// Raw image bytes survive a String column, which is how ClickHouse hands back
// a stored blob under output_format_arrow_string_as_string=1. They are not
// valid UTF-8, and must not be sanitised on the way through.
func TestCellRawKeepsNonUTF8Bytes(t *testing.T) {
	alloc := memory.NewGoAllocator()
	blob := string([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe})
	b := array.NewStringBuilder(alloc)
	defer b.Release()
	b.AppendValues([]string{blob}, nil)
	rec := oneColRec("shot@image/png", arrow.BinaryTypes.String, b.NewStringArray())

	raw, ok := cellRaw(rec, 0, 0)
	require.True(t, ok)
	assert.Equal(t, blob, raw, "the decoder needs the bytes, not a sanitised rendering")
}

// Null and out-of-range cells report no content, so the section loop skips
// them exactly as it skips an empty formatCell.
func TestCellRawNullAndRange(t *testing.T) {
	alloc := memory.NewGoAllocator()
	b := array.NewStringBuilder(alloc)
	defer b.Release()
	b.AppendValues([]string{"", ""}, []bool{false, true}) // row 0 null, row 1 empty
	rec := oneColRec("x@text/markdown", arrow.BinaryTypes.String, b.NewStringArray())

	_, ok := cellRaw(rec, 0, 0)
	assert.False(t, ok, "a null cell has no content")

	raw, ok := cellRaw(rec, 0, 1)
	assert.True(t, ok)
	assert.Equal(t, "", raw, "an empty cell is skipped by the caller's raw == \"\"")

	_, ok = cellRaw(rec, 0, 99)
	assert.False(t, ok, "out of range")
}

// A declared column on a non-string type is odd but total — it renders the
// number rather than nothing.
func TestCellRawFallsBackForOtherTypes(t *testing.T) {
	alloc := memory.NewGoAllocator()
	b := array.NewInt64Builder(alloc)
	defer b.Release()
	b.AppendValues([]int64{42}, nil)
	rec := oneColRec("x@text/markdown", arrow.PrimitiveTypes.Int64, b.NewInt64Array())

	raw, ok := cellRaw(rec, 0, 0)
	require.True(t, ok)
	assert.Equal(t, "42", raw)
}

// The artifact builder's reason paths. Each renders the ordinary truncated
// label plus the reason, rather than an empty box.
func TestBuildRichEntryReasons(t *testing.T) {
	t.Run("carries the declaration's own reason", func(t *testing.T) {
		d, _ := parseRichColumn("x@text/markdwn")
		e := buildRichEntry(d, "whatever")
		assert.Contains(t, e.reason, "unknown content type")
		assert.Nil(t, e.doc)
	})

	t.Run("oversized text declines in writing", func(t *testing.T) {
		d, _ := parseRichColumn("x@text/markdown")
		e := buildRichEntry(d, strings.Repeat("a", richMaxTextBytes+1))
		assert.Contains(t, e.reason, "over the")
		assert.Nil(t, e.doc, "no parse is attempted past the limit")
	})

	t.Run("undecodable image names the failure", func(t *testing.T) {
		d, _ := parseRichColumn("x@image/png")
		e := buildRichEntry(d, "not a png")
		assert.NotEmpty(t, e.reason)
		assert.Empty(t, e.pixels)
	})
}

// The happy paths build their artifact and no reason.
func TestBuildRichEntryArtifacts(t *testing.T) {
	t.Run("markdown parses", func(t *testing.T) {
		d, _ := parseRichColumn("x@text/markdown")
		e := buildRichEntry(d, "# Title\n\nsome *body*")
		require.Empty(t, e.reason)
		require.NotNil(t, e.doc)
		headings := e.doc.Headings()
		require.Len(t, headings, 1)
		assert.Equal(t, "Title", headings[0].Text)
	})

	t.Run("plain text is kept whole", func(t *testing.T) {
		d, _ := parseRichColumn("x@text/plain")
		e := buildRichEntry(d, "line one\nline two")
		require.Empty(t, e.reason)
		assert.Equal(t, "line one\nline two", e.text)
	})

	t.Run("json is highlighted", func(t *testing.T) {
		d, _ := parseRichColumn("x@application/json")
		e := buildRichEntry(d, `{"a":1}`)
		require.Empty(t, e.reason)
		assert.True(t, e.hasJob)
	})

	t.Run("image decodes to pixels", func(t *testing.T) {
		d, _ := parseRichColumn("x@image/png")
		e := buildRichEntry(d, string(tinyPNG(t, 3, 2)))
		require.Empty(t, e.reason)
		assert.Equal(t, uint32(3), e.widthPx)
		assert.Equal(t, uint32(2), e.heightPx)
		assert.Len(t, e.pixels, 6)
	})
}

// Malformed JSON still highlights — the pretty-printer falls back to the
// source rather than dropping the cell.
func TestRichIndentJSONFallsBack(t *testing.T) {
	assert.Equal(t, "{not json", richIndentJSON("{not json"))
	assert.Contains(t, richIndentJSON(`{"a":1}`), "\n", "valid JSON is indented")
}

// The fallback label cuts at the first newline: Truncate() clips on width, so
// without this a multi-megabyte blob would cross the wire to draw forty
// visible characters.
func TestFirstLineOf(t *testing.T) {
	assert.Equal(t, "first", firstLineOf("first\nsecond\nthird"))
	assert.Equal(t, "no newline", firstLineOf("no newline"))
	assert.Len(t, firstLineOf(strings.Repeat("x", 4096)), 256)
}

// The cache is keyed on (executed, row): a new row must not show the old row's
// artifact, and re-running a query must not show the old result's.
func TestRichCellCacheKey(t *testing.T) {
	cache := newRichCellCache(nil)
	d, _ := parseRichColumn("x@text/plain")

	cache.noteExecuted(time.Unix(100, 0))
	cache.syncTo(0)
	first := cache.entryFor(0, d, "row zero")
	assert.Equal(t, "row zero", first.text)
	assert.Same(t, first, cache.entryFor(0, d, "row zero"), "built once per row")

	gen := cache.generation

	// Same result, different row.
	cache.syncTo(1)
	assert.Greater(t, cache.generation, gen, "a new row is new image content")
	assert.Equal(t, "row one", cache.entryFor(0, d, "row one").text)

	// Same row, re-run query: the bytes may differ under an unchanged index.
	cache.syncTo(1)
	assert.Equal(t, "row one", cache.entryFor(0, d, "row one").text, "no churn on a steady frame")
	cache.noteExecuted(time.Unix(200, 0))
	cache.syncTo(1)
	assert.Equal(t, "fresh", cache.entryFor(0, d, "fresh").text, "a re-run drops the old bytes")
}

// tinyPNG encodes a w×h opaque PNG for the decode tests.
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
