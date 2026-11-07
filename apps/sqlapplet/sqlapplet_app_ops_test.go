package sqlapplet

import (
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// An applet's catalog is play's operable subset under play's own names:
// same version, class, effect, arguments and result, every one offered to
// agents, and nothing that changes the document or publishes.
func TestAnAppletsCatalogIsPlaysOperableSubset(t *testing.T) {
	catalog := appletOps.Catalog()
	require.NoError(t, catalog.Validate())
	ops := play.OperableOperations()
	require.NotEmpty(t, ops)
	for _, o := range ops {
		got, ok := catalog.Lookup(o.Spec.Name)
		require.True(t, ok, o.Spec.Name)
		assert.Equal(t, o.Spec.Version, got.Version, o.Spec.Name)
		assert.Equal(t, o.Spec.Class, got.Class, o.Spec.Name)
		assert.Equal(t, o.Spec.Effect, got.Effect, o.Spec.Name)
		assert.Equal(t, o.Spec.Args, got.Args, o.Spec.Name)
		assert.Equal(t, o.Spec.Result, got.Result, o.Spec.Name)
		assert.Equal(t, o.Spec.Reads, got.Reads, o.Spec.Name)
		assert.Equal(t, o.Spec.Writes, got.Writes, o.Spec.Name)
		assert.True(t, got.Agents, o.Spec.Name)
	}
	for _, name := range []string{"set_sql", "publish_result"} {
		_, ok := catalog.Lookup(name)
		assert.False(t, ok, name+": the document is the author's")
	}
}

// A minted manifest carries the catalog and registers with it, and the
// window it opens serves it: before Mount every operation is unavailable
// for the one reason there is.
func TestAMintedAppletRegistersItsCatalog(t *testing.T) {
	fsys := fstest.MapFS{"reader.md": docMD("title: Reader", sqlFence)}
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "t", fsys: fsys, topics: []app.TopicT{app.TopicRuntime}}})
	require.Empty(t, errs)
	require.Equal(t, 1, minted)
	id := app.AppIdT(appletIdPrefix + "reader")
	assert.Empty(t, reg.OperationsDiagnostic(id), "the catalog was not withdrawn")
	m, ok := reg.LookupManifest(id)
	require.True(t, ok)
	require.NotNil(t, m.Operations)
	_, ok = m.Operations.Lookup("get_state")
	assert.True(t, ok)

	a, err := reg.Open(id)
	require.NoError(t, err)
	inst, ok := a.(*appletApp)
	require.True(t, ok)
	h := inst.Operations()
	require.NotNil(t, h)
	available, reason := h.Snapshot().Available("get_state")
	assert.False(t, available)
	assert.Contains(t, reason, "not mounted")
}
