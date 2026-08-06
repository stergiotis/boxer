package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// panel_shapes registers with the rest of the static set, so the table name is
// there whatever else a host wires.
func TestPanelShapes_Registered(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	_, has := r.Lookup("panel_shapes")
	assert.True(t, has, "panel_shapes is not registered")
}

// The served rows are the same battery the catalog run evaluates — one
// definition, two faces (ADR-0170 §SD5). This test is what keeps them one.
func TestPanelShapes_MatchesTheBattery(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	p, has := r.Lookup("panel_shapes")
	require.True(t, has)

	batch, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer batch.Release()

	want := 0
	for _, s := range panelshapes.Shapes() {
		want += len(s.Patterns)
	}
	assert.EqualValues(t, want, batch.NumRows())

	schema := batch.Schema()
	for _, col := range []string{"shape", "pattern", "ordinal", "note"} {
		assert.Truef(t, len(schema.FieldIndices(col)) == 1, "column %q missing", col)
	}
	assert.Equal(t, introspect.FreshnessStatic, p.Freshness())
}
