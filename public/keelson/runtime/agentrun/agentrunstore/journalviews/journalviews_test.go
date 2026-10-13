package journalviews

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every view composes, is stamped, and holds no sugar the server would not
// understand.
func TestComposeShape(t *testing.T) {
	views, err := Compose("")
	require.NoError(t, err)
	require.Len(t, views, len(AllViewNames()))
	stamp := "\nCOMMENT " + Stamp().Literal()
	for i, v := range views {
		require.Equal(t, AllViewNames()[i], v.Name)
		require.True(t, strings.HasPrefix(v.Sql, "CREATE OR REPLACE VIEW boxer."+v.Name+" AS\n"), v.Name)
		require.True(t, strings.HasSuffix(v.Sql, stamp), v.Name)
		require.NotContains(t, v.Sql, ";", v.Name)
		for _, sugar := range []string{"LW_GET", "LW_PLAIN", "`"} {
			require.NotContains(t, v.Sql, sugar, v.Name)
		}
	}
}
