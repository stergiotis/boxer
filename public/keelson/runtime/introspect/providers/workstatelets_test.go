package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
)

// The provider serves what the catalog holds, and a nil catalog is an empty
// table with the same columns rather than an absent one.
func TestWorkstateletsProviderReadsCatalog(t *testing.T) {
	empty, err := workstateletsProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer empty.Release()
	assert.EqualValues(t, 0, empty.NumRows())

	reg, cat := app.NewRegistry(), workstatelet.NewCatalog()
	composed, err := workstatelet.Compose(workstatelet.Doc{
		Slug: "desk", Title: "Desk", Summary: "Two windows", Topics: []app.TopicT{app.TopicRuntime},
		Desktop: workstatelet.Desktop{Layout: "columns"},
		Windows: []workstatelet.Window{{App: "play", Kind: "playLaunch", Config: []byte("0123456789")}, {App: "appcenter"}},
	})
	require.NoError(t, err)
	id, err := cat.Mint(reg, composed, workstatelet.OriginStored, "")
	require.NoError(t, err)

	rec, err := workstateletsProvider{cat: cat}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows())
	assert.Equal(t, empty.Schema().String(), rec.Schema().String())
	assert.Equal(t, string(id), firstString(t, rec, "app_id"))
	assert.Equal(t, "desk", firstString(t, rec, "slug"))
	assert.Equal(t, "stored", firstString(t, rec, "origin"))
	assert.Equal(t, "columns", firstString(t, rec, "layout"))
	assert.Equal(t, string(composed.Source), firstString(t, rec, "doc"), "the document's text is what a book would commit")
	for col, want := range map[string]int64{"windows": 2, "configured_windows": 1, "config_bytes": 10} {
		idx := rec.Schema().FieldIndices(col)
		require.Len(t, idx, 1, col)
		assert.Equal(t, want, rec.Column(idx[0]).(*array.Int64).Value(0), col)
	}
}
