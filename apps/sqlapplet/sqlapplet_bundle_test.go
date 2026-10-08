package sqlapplet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bundle's document reaches play as what a window applies: the SQL, the
// local names, the first pane, the endpoint and whether it may run on open
// (ADR-0288 §SD4).
func TestParseBundleDoc(t *testing.T) {
	src := "---\ntype: reference\nstatus: draft\ntitle: Sales\nsummary: \"orders by region\"\nendpoint: introspection\ndatasets: [orders, regions]\ntabs: [chart, table]\n---\n\n# Sales\n\n```sql\nSELECT region, count() AS n FROM keelson('orders') GROUP BY region\n```\n"
	doc, err := parseBundleDoc("sales.md", []byte(src))
	require.NoError(t, err)
	assert.Equal(t, "Sales", doc.Title)
	assert.Equal(t, []string{"orders", "regions"}, doc.Datasets)
	assert.Equal(t, "chart", doc.Tab)
	assert.True(t, doc.Introspection)
	assert.True(t, doc.Runnable)
	assert.Contains(t, doc.Sql, "keelson('orders')")

	_, err = parseBundleDoc("broken.md", []byte("no frontmatter"))
	assert.Error(t, err)
}

// A bundle alias may hold capitals and underscores, which an applet slug
// may not; the document parses whatever the alias.
func TestParseBundleDocTakesAnyAlias(t *testing.T) {
	src := "---\ntype: reference\nstatus: draft\ntitle: Q3\nsummary: \"s\"\nendpoint: introspection\ndatasets: [result]\ntabs: [table]\n---\n\n```sql\nSELECT * FROM keelson('result')\n```\n"
	for _, alias := range []string{"Sales_Q3", "_x", "counts", "A__"} {
		_, err := parseBundleDoc(alias+".md", []byte(src))
		assert.NoError(t, err, alias)
	}
	assert.Equal(t, "sales-q3.md", bundleDocPath("Sales_Q3.md"))
	assert.Equal(t, "bundle.md", bundleDocPath("__.md"))
}
