package adhocdemo

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/sqlapplet"
)

func TestItemsDocParses(t *testing.T) {
	def, err := sqlapplet.ParseDocSource(string(ManifestId), "items.md", []byte(itemsDoc))
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, "items", def.Slug)
	assert.Equal(t, sqlapplet.EndpointIntrospection, def.Endpoint)
	assert.Equal(t, []string{datasetAlias}, def.Datasets)
	assert.Contains(t, def.SQL, "keelson('items')")
}

func TestSeriesEncodes(t *testing.T) {
	inst := &App{log: zerolog.Nop()}
	b0 := inst.series(0)
	require.NotEmpty(t, b0)

	rdr, err := ipc.NewReader(bytes.NewReader(b0))
	require.NoError(t, err)
	defer rdr.Release()
	require.Len(t, rdr.Schema().Fields(), 2)
	var rows int64
	for rdr.Next() {
		rows += rdr.RecordBatch().NumRows()
	}
	assert.Equal(t, int64(24), rows)

	// Each generation differs, so Regenerate is visible.
	assert.NotEqual(t, b0, inst.series(1))
}
