package chstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
)

func TestSchemaContractReadsTheSetupColumns(t *testing.T) {
	c, err := SchemaContract(Config{Database: "db", Table: "t"})
	require.NoError(t, err)
	require.Equal(t, "db.t", c.Table)
	require.Equal(t, factsschema.TableName, c.Kind)
	require.NotEmpty(t, c.Columns)
	require.Empty(t, c.Stamp)
}
