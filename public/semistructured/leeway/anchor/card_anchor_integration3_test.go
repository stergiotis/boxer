package anchor

import (
	"bytes"
	"embed"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"encoding/json/jsontext"

	card2 "github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

//go:embed *.txt *.json
var txtFileContent embed.FS

func getTxtContent(path string, t *testing.T) string {
	b, err := txtFileContent.ReadFile(path)
	require.NoError(t, err, path)
	return unsafeperf.UnsafeBytesToString(b)
}

const rewriteGold = false

func TestCardE2e(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()

	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	fmts := streamreadaccess.DefaultFormatters()
	var cardDriver *streamreadaccess.Driver
	cardDriver, err = streamreadaccess.NewDriver(&tblDesc, ir, fmts)
	require.NoError(t, err)

	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)

	{
		sink := streamreadaccess.NewStructuredOutputRecorder()
		err = cardDriver.DriveRecordBatch(sink, records[0])
		require.NoError(t, err)
		p := "card_anchor_integration3_test_e2e_gold.out.txt"
		if rewriteGold {
			os.WriteFile(p, sink.Bytes(), os.ModePerm)
		} else {
			require.Equal(t, getTxtContent(p, t), sink.String())
		}
	}
}
func TestCardE2eText(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	fmts := streamreadaccess.DefaultFormatters()
	var cardDriver *streamreadaccess.Driver
	cardDriver, err = streamreadaccess.NewDriver(&tblDesc, ir, fmts)
	require.NoError(t, err)

	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)

	b := bytes.NewBuffer(nil)
	sink := card2.NewUnicodeCardEmitter(b, 200)
	for i, r := range records {
		b.Reset()
		err = cardDriver.DriveRecordBatch(sink, r)
		require.NoError(t, err)
		p := fmt.Sprintf("card_anchor_integration3_test_e2etext_gold_%02d.out.txt", i)
		if rewriteGold {
			os.WriteFile(p, b.Bytes(), os.ModePerm)
		} else {
			require.Equal(t, getTxtContent(p, t), b.String())
		}
	}
}

func TestCardE2eJson(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	fmts := streamreadaccess.DefaultFormatters()
	var cardDriver *streamreadaccess.Driver
	cardDriver, err = streamreadaccess.NewDriver(&tblDesc, ir, fmts)
	require.NoError(t, err)

	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)

	b := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(b, jsontext.Multiline(true), jsontext.WithIndent("  "))
	sink := card2.NewJsonCardEmitter(enc, ir)
	for i, r := range records {
		b.Reset()
		err = cardDriver.DriveRecordBatch(sink, r)
		require.NoError(t, err)
		p := fmt.Sprintf("card_anchor_integration3_test_e2ejson_gold_%02d.out.json", i)
		if rewriteGold {
			os.WriteFile(p, b.Bytes(), os.ModePerm)
		} else {
			require.Equal(t, getTxtContent(p, t), b.String())
		}
	}
}

// TestCardE2eSchema exercises Driver.DriveSchema against the anchor
// TableDesc and asserts the emitted schema document carries a stable
// blake3 fingerprint that matches across re-emissions.
func TestCardE2eSchema(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	fmts := streamreadaccess.DefaultFormatters()
	cardDriver, err := streamreadaccess.NewDriver(&tblDesc, ir, fmts)
	require.NoError(t, err)

	emit := func() (out []byte, fp string) {
		buf := bytes.NewBuffer(nil)
		enc := jsontext.NewEncoder(buf)
		sink := card2.NewJsonCardSchemaEmitter(enc)
		err := cardDriver.DriveSchema(sink)
		require.NoError(t, err)
		out = append([]byte(nil), buf.Bytes()...)
		fp = sink.Fingerprint()
		return
	}

	bytesA, fpA := emit()
	bytesB, fpB := emit()
	require.Equal(t, fpA, fpB, "schema fingerprint must match across re-emissions")
	require.Equal(t, bytesA, bytesB, "schema bytes must match across re-emissions")
	require.NotEmpty(t, fpA, "schema fingerprint should be populated")
	require.Contains(t, string(bytesA), `"leewayCardSchema":"1"`)
	require.Contains(t, string(bytesA), fpA, "fingerprint must appear in document")
}

// TestCardE2eSparks pins the three text topology emitters. Each renders shape
// only — no values — so the golden is a compact statement of what the anchor
// batch's structure looks like, and a structural regression in the driver shows
// up here as a changed glyph run rather than as a silently different picture.
//
// These had no coverage until they gained a venue in play's Experiments pane;
// before that they were unreachable and unverified.
func TestCardE2eSparks(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	fmts := streamreadaccess.DefaultFormatters()
	cardDriver, err := streamreadaccess.NewDriver(&tblDesc, ir, fmts)
	require.NoError(t, err)

	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)
	require.NotEmpty(t, records)

	for _, tc := range []struct {
		name string
		make func(w *bytes.Buffer) streamreadaccess.SinkI
	}{
		{"topo", func(w *bytes.Buffer) streamreadaccess.SinkI { return card2.NewTopologySpark(w) }},
		{"braille", func(w *bytes.Buffer) streamreadaccess.SinkI { return card2.NewBrailleSpark(w) }},
		{"treemap", func(w *bytes.Buffer) streamreadaccess.SinkI { return card2.NewTreemapSpark(w) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.NewBuffer(nil)
			// One batch is enough: the sparks emit per entity, and the goldens
			// stay readable.
			err := cardDriver.DriveRecordBatch(tc.make(b), records[0])
			require.NoError(t, err)
			require.NotEmpty(t, b.String(), "spark produced no output")
			p := fmt.Sprintf("card_anchor_integration3_test_e2espark_%s_gold.out.txt", tc.name)
			if rewriteGold {
				os.WriteFile(p, b.Bytes(), os.ModePerm)
			} else {
				require.Equal(t, getTxtContent(p, t), b.String())
			}
		})
	}
}

// TestTreemapSparkRowsAlign pins the width invariant of TreemapSpark's box
// drawing: every entity renders as three lines (top rule, fill row, bottom
// rule) that must occupy the same number of columns, or the box visibly
// shears.
//
// A golden alone would catch a regression but not explain it. The emitter's
// borders go through strings.Repeat, which repeats whole runes, while the fill
// row was measured with strings.Builder.Len() — bytes — and every fill glyph
// (█ ▓ ░ ·) is multi-byte, so the padding computed negative and was skipped.
// Counting runes here states the property that was actually violated.
func TestTreemapSparkRowsAlign(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)

	cardDriver, err := streamreadaccess.NewDriver(&tblDesc, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)

	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)

	b := bytes.NewBuffer(nil)
	err = cardDriver.DriveRecordBatch(card2.NewTreemapSpark(b), records[0])
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.NotEmpty(t, lines)
	require.Zero(t, len(lines)%3, "each entity renders as exactly three lines")
	for i := 0; i < len(lines); i += 3 {
		top, mid, bot := lines[i], lines[i+1], lines[i+2]
		wTop := utf8.RuneCountInString(top)
		require.Equal(t, wTop, utf8.RuneCountInString(mid),
			"entity %d: fill row is %d columns against a %d-column top rule\ntop: %s\nmid: %s",
			i/3, utf8.RuneCountInString(mid), wTop, top, mid)
		require.Equal(t, wTop, utf8.RuneCountInString(bot),
			"entity %d: bottom rule is %d columns against a %d-column top rule",
			i/3, utf8.RuneCountInString(bot), wTop)
	}
}
