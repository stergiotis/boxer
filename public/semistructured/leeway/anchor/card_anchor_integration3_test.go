package anchor

import (
	"embed"
	_ "embed"
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

//go:embed *.txt
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
			require.NoError(t, os.WriteFile(p, sink.Bytes(), os.ModePerm))
		} else {
			require.Equal(t, getTxtContent(p, t), sink.String())
		}
	}
}
