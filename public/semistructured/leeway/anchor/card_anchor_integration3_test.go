package anchor

import (
	"bytes"
	"embed"
	_ "embed"
	"fmt"
	"os"
	"testing"

	"encoding/json/jsontext"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/unsafeperf"
	card2 "github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stretchr/testify/require"
)

//go:embed *.txt *.json *.html
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

func TestCardE2eHtml(t *testing.T) {
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
	sink := card2.NewHtmlCardEmitter(b, card2.ColorPaletteMagma)
	for i, r := range records {
		b.Reset()
		err = cardDriver.DriveRecordBatch(sink, r)
		require.NoError(t, err)
		p := fmt.Sprintf("card_anchor_integration3_test_e2ehtml_gold_%02d.out.html", i)
		if rewriteGold {
			os.WriteFile(p, b.Bytes(), os.ModePerm)
		} else {
			require.Equal(t, getTxtContent(p, t), b.String())
		}
	}
}
