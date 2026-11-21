package ddl_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/ddl"
)

// The layout names the timestamp by its physical name (ADR-0397); a change
// of the column's aspects renames it, and the clause must follow.
func TestTheLayoutNamesTheTimestampColumn(t *testing.T) {
	require.Contains(t, ddl.ColumnsSQL, "\t\""+factsschema.TsColumn+"\" DateTime64(9,'UTC')")
}
