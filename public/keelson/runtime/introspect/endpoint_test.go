package introspect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A host serves its own registry, not Default: the published catalog is
// what tells a co-resident app that keelson('tables') names a table of the
// plane rather than an ad-hoc dataset alias.
func TestLocalCatalogAnswersForTheHostsRegistry(t *testing.T) {
	t.Cleanup(func() { SetLocalCatalog(nil) })
	reg := NewRegistry()
	require.NoError(t, RegisterCatalog(reg))

	SetLocalCatalog(nil)
	_, inDefault := Default.Lookup("tables")
	assert.Equal(t, inDefault, IsLocalTable("tables"), "with no plane published, Default answers")

	SetLocalCatalog(reg)
	assert.True(t, IsLocalTable("tables"))
	assert.True(t, IsLocalTable("columns"))
	assert.False(t, IsLocalTable("sales_orders"), "a name no provider holds is not a table")
	assert.Equal(t, []string{"columns", "tables"}, LocalTableNames())

	SetLocalCatalog(nil)
	assert.Equal(t, Default.Names(), LocalTableNames(), "clearing it is how a stopped plane says so")
}
