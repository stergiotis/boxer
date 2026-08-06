package panelshapes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes"
)

// Every seed pattern compiles. This is the test the battery would otherwise
// fail at first use, in a CLI run against a live server.
func TestShapes_AllCompile(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)
	require.NotEmpty(t, b.Shapes())
	for _, s := range b.Shapes() {
		assert.NotEmptyf(t, s.Name, "shape has no name")
		assert.NotEmptyf(t, s.Note, "shape %q has no note", s.Name)
		assert.NotEmptyf(t, s.Patterns, "shape %q has no patterns", s.Name)
	}
}

// An empty conjunction is vacuously true and would claim every opaque table in
// the instance, so it is rejected rather than compiled.
func TestNewBatteryFrom_RejectsEmptyBattery(t *testing.T) {
	_, err := panelshapes.NewBatteryFrom([]panelshapes.Shape{{Name: "nothing"}})
	assert.Error(t, err)
}

func TestNewBatteryFrom_RejectsBadPattern(t *testing.T) {
	_, err := panelshapes.NewBatteryFrom([]panelshapes.Shape{{Name: "bad", Patterns: []string{"("}}})
	assert.Error(t, err)
}

// The pattern builders are pinned: their output is served as data over
// `keelson('panel_shapes')`, so a silent change to the spelling changes a
// published vocabulary.
func TestPatternBuilders(t *testing.T) {
	assert.Equal(t, `;lane:`, panelshapes.NamedColumn("lane"))
	assert.Equal(t, `;hist_lo:`, panelshapes.NamedColumn("hist_lo"))
	assert.Equal(t, `;value:`+panelshapes.NumericType+`\??;`, panelshapes.NamedColumnOfType("value", panelshapes.NumericType))
	assert.Equal(t, `;[^;]*:`+panelshapes.TemporalType+`\??;`, panelshapes.AnyColumnOfType(panelshapes.TemporalType))
}

// schemaOf is the path a real table takes: column metadata through
// NormalizedSchema, then the battery. Writing the fixtures as columns rather
// than as hand-built strings keeps the two halves honest about each other.
func schemaOf(cols ...datacatalog.ColumnMeta) (s string) {
	return datacatalog.NormalizedSchema(cols)
}

func col(name string, chType string) (c datacatalog.ColumnMeta) {
	return datacatalog.ColumnMeta{Name: name, Type: chType}
}

// One positive and one negative fixture per seed shape. The negatives are each
// one column away from matching, which is the case a battery gets wrong.
func TestBattery_MatchPerShape(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)

	cases := []struct {
		shape string
		yes   string
		no    string
	}{
		{
			shape: "series",
			yes:   schemaOf(col("t", "DateTime64(3)"), col("v", "Float64")),
			// A temporal column with nothing to plot against it.
			no: schemaOf(col("t", "DateTime64(3)"), col("v", "String")),
		},
		{
			shape: "sankey-flows",
			yes:   schemaOf(col("source", "String"), col("target", "String"), col("value", "Float64")),
			// A graph, not a flow: no quantity to give a ribbon its thickness.
			no: schemaOf(col("source", "String"), col("target", "String"), col("value", "String")),
		},
		{
			shape: "network-edges",
			yes:   schemaOf(col("source", "String"), col("target", "String")),
			no:    schemaOf(col("source", "String"), col("dest", "String")),
		},
		{
			shape: "kanban-cards",
			yes:   schemaOf(col("lane", "LowCardinality(String)"), col("title", "String")),
			no:    schemaOf(col("lane", "String"), col("name", "String")),
		},
		{
			shape: "hierarchy-nodes",
			yes:   schemaOf(col("id", "String"), col("parent", "Nullable(String)"), col("value", "UInt64")),
			// No parent: rows, not a tree.
			no: schemaOf(col("id", "String"), col("value", "UInt64")),
		},
		{
			shape: "hierarchy-folded",
			yes:   schemaOf(col("stack", "Array(String)"), col("value", "UInt64")),
			// A scalar stack is not a path.
			no: schemaOf(col("stack", "String"), col("value", "UInt64")),
		},
		{
			shape: "distribution",
			yes: schemaOf(col("series", "String"), col("n", "UInt64"),
				col("ps", "Array(Float64)"), col("qs", "Array(Float64)")),
			// The grid needs both halves.
			no: schemaOf(col("series", "String"), col("n", "UInt64"), col("ps", "Array(Float64)")),
		},
	}
	for _, c := range cases {
		t.Run(c.shape, func(t *testing.T) {
			assert.Containsf(t, b.Match(c.yes), c.shape, "positive fixture %q", c.yes)
			assert.NotContainsf(t, b.Match(c.no), c.shape, "negative fixture %q", c.no)
		})
	}
	// Every seed shape has a case; a new shape without one is a gap, not a
	// pass.
	assert.Len(t, cases, len(b.Shapes()))
}

// The sentinels are what make a name whole. Without them `;value:` would also
// claim a column called `myvalue` or `value_2`.
func TestBattery_NamesAreWholeColumns(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)
	near := schemaOf(col("mysource", "String"), col("targetish", "String"))
	assert.NotContains(t, b.Match(near), "network-edges")
}

// A Nullable numeric still reads as a number: normalization moved the
// nullability to a trailing `?`, and the patterns admit it.
func TestBattery_AdmitsNullableAndLowCardinality(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)
	s := schemaOf(col("source", "LowCardinality(String)"), col("target", "LowCardinality(String)"),
		col("value", "Nullable(Float64)"))
	assert.Contains(t, b.Match(s), "sankey-flows")
}

// `Float64` inside `Array(Float64)` must not satisfy a scalar-number demand.
func TestBattery_ScalarDemandRejectsArray(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)
	s := schemaOf(col("source", "String"), col("target", "String"), col("value", "Array(Float64)"))
	assert.NotContains(t, b.Match(s), "sankey-flows")
	assert.Contains(t, b.Match(s), "network-edges")
}

// A table with no columns satisfies nothing — the bare sentinel has no column
// for any pattern to find.
func TestBattery_EmptySchemaMatchesNothing(t *testing.T) {
	b, err := panelshapes.NewBattery()
	require.NoError(t, err)
	assert.Empty(t, b.Match(datacatalog.NormalizedSchema(nil)))
}
