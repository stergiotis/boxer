package chstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

func TestSchemaContractReadsTheSetupColumns(t *testing.T) {
	c, err := SchemaContract(Config{Database: "db", Table: "t"})
	require.NoError(t, err)
	require.Equal(t, "db.t", c.Table)
	require.Equal(t, factsschema.TableName, c.Kind)
	require.NotEmpty(t, c.Columns)
	s, ok := recordstore.ParseTableStamp(c.Stamp)
	require.True(t, ok, "the facts table is stamped (ADR-0397)")
	require.Equal(t, recordstore.ColumnsDigest(c.Columns), s.Columns)
	require.Equal(t, c.Layout.Digest(), s.Layout)
}
