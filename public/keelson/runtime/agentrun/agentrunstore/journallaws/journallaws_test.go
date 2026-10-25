package journallaws

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
)

// Every law composes over the views' physical columns and spells none of
// them itself; the names are distinct and in order.
func TestComposeShape(t *testing.T) {
	out, err := Compose("")
	require.NoError(t, err)
	require.Equal(t, Names(), func() (n []string) {
		for _, l := range out {
			n = append(n, l.Name)
		}
		return
	}())
	seen := map[string]bool{}
	for _, l := range out {
		require.False(t, seen[l.Name], l.Name)
		seen[l.Name] = true
		require.True(t, strings.HasPrefix(l.Sql, "WITH e AS (SELECT "), l.Name)
		require.Contains(t, l.Sql, "boxer."+journalviews.ViewEntries, l.Name)
		require.Contains(t, l.Sql, " AS why", l.Name)
		require.NotContains(t, l.Sql, ";", l.Name)
		require.NotEmpty(t, l.Statement, l.Name)
	}
	named, err := Compose("lane")
	require.NoError(t, err)
	require.Contains(t, named[0].Sql, "lane."+journalviews.ViewEntries)
}
