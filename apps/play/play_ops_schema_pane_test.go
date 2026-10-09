package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// schemaPaneNames is a leeway result whose sections the table spells in
// camelCase: one multi-word section, one single-word, and the backbone.
var schemaPaneNames = []string{
	"id:id:u64:47::0:",
	"ts:ts:z64:47::0:",
	"tv:f64:value:val:f64:4A:::0::data",
	"tv:f64:lv:lv:y:124:::0::data",
	"tv:f64:lvcard:lvcard:u64:4E:::0::data",
	"tv:u32Array:value:val:u32h:4:::0::data",
	"tv:u32Array:lv:lv:y:124:::0::data",
	"tv:u32Array:len:len:u64:4D:::0::data",
	"tv:u32Array:lvcard:lvcard:u64:4E:::0::data",
}

// finishMain lands a one-row result of the given column names, every
// column text, as the main result.
func finishMain(t *testing.T, l *PlayLauncher, names []string) {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(names))
	cols := make([]arrow.Array, 0, len(names))
	for _, n := range names {
		b := array.NewStringBuilder(mem)
		b.Append("")
		cols = append(cols, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: n, Type: arrow.BinaryTypes.String})
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(schema, cols, 1)
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, schema, 1, Summary{}, nil, runstream.Terminal{})
}

// get_schema reads the schema the Schema pane draws, spelled as the
// result stores it, with the handles describe_table would print.
func TestGetSchemaReadsThePanesSchema(t *testing.T) {
	l, h := opsLauncher(t)
	finishMain(t, l, schemaPaneNames)
	out := queryOp[SchemaReading](t, h, opGetSchema, SchemaArgs{})
	require.True(t, out.Leeway)
	require.Equal(t, len(schemaPaneNames), out.Columns)
	require.Len(t, out.Sections, 2)
	byName := map[string]SchemaSection{}
	for _, s := range out.Sections {
		byName[s.Name] = s
	}
	arr, ok := byName["u32Array"]
	require.True(t, ok, "the section keeps the table's spelling, not u32-array: %v", out.Sections)
	require.Equal(t, []string{"low-card-verbatim"}, arr.Memberships)
	require.Len(t, arr.Columns, 1)
	require.Equal(t, "u32Array:value", arr.Columns[0].Handle)
	require.Equal(t, "value", arr.Columns[0].Name)
	require.NotEmpty(t, arr.Columns[0].Type)
	require.Equal(t, "f64:value", byName["f64"].Columns[0].Handle)
	handles := map[string]string{}
	for _, c := range out.Plain {
		handles[c.Name] = c.Handle
	}
	require.Equal(t, "id:id", handles["id"])
	require.Equal(t, "timestamp:ts", handles["ts"])
	require.Zero(t, out.More)

	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opGetSchema)
	require.True(t, ok)
	require.True(t, spec.Untrusted)
	require.Equal(t, app.OperationEffectNone, spec.Effect)
	require.Equal(t, opGetSchema, paneOperations[schemaPaneId][0], "the pane's own reading comes first")
}

// A result that is not leeway-shaped reads as opaque plain columns, and a
// column bound counts what it leaves out.
func TestGetSchemaOpaqueAndBounded(t *testing.T) {
	l, h := opsLauncher(t)
	finishMain(t, l, []string{"tag", "n"})
	out := queryOp[SchemaReading](t, h, opGetSchema, SchemaArgs{})
	require.False(t, out.Leeway)
	require.Empty(t, out.Sections)
	require.Len(t, out.Plain, 2)
	require.Equal(t, "tag", out.Plain[0].Name)
	require.Equal(t, "opaque", out.Plain[0].ItemType)
	require.Empty(t, out.Plain[0].Handle)

	schema := arrow.NewSchema([]arrow.Field{{Name: "a", Type: arrow.BinaryTypes.String}, {Name: "b", Type: arrow.BinaryTypes.String}}, nil)
	td, leeway := resultSchemaDesc(schema)
	plain, _, more := schemaReading(td, leeway, schema.Fields(), 1)
	require.Len(t, plain, 1)
	require.Equal(t, 1, more)
}

// The pane's copy renames; the discovered TableDesc keeps leeway's
// canonical spelling.
func TestSpelledAsStoredLeavesTheDiscoveryAlone(t *testing.T) {
	fields := make([]arrow.Field, 0, len(schemaPaneNames))
	for _, n := range schemaPaneNames {
		fields = append(fields, arrow.Field{Name: n, Type: arrow.BinaryTypes.String})
	}
	r, ok := discoverCardRecipe(arrow.NewSchema(fields, nil))
	require.NotNil(t, r.table, "discovered (ok=%v)", ok)
	var canonical []string
	for _, s := range r.table.TaggedValuesSections {
		canonical = append(canonical, s.Name.String())
	}
	require.Contains(t, canonical, "u32-array")
	spelled := spelledAsStored(r.table, fields)
	var stored []string
	for _, s := range spelled.TaggedValuesSections {
		stored = append(stored, s.Name.String())
	}
	require.Contains(t, stored, "u32Array")
	require.Equal(t, "u32-array", r.table.TaggedValuesSections[len(canonical)-1].Name.String(), "the original is not renamed")
}
