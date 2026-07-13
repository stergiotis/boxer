package lwsql

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

const testTable = "mytable"

// buildKnownNames constructs a small leeway table and returns its generated
// physical column names — the same names a real ClickHouse table would carry:
//   - plain id (backbone)
//   - string:value      — single-value section
//   - timeRange:value   — single-value section (multi-word, exercises folding)
//   - symbol:value, :ref — a two-value-column section (bare is ambiguous); this
//     is the shipped addSymbol/addSymbolRef shape, guaranteed to validate
func buildKnownNames(t *testing.T) []string {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(testTable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	const memb = common.MembershipSpecMixedLowCardVerbatimHighCardParameters
	manip.MergeTaggedValueColumn("string", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("timeRange", "value", ctabb.U64, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	manip.MergeTaggedValueColumn("symbol", "ref", ctabb.U64, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names := make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, strings.Join(p.NameComponents, ""))
	}
	require.NotEmpty(t, names)
	return names
}

func newTestResolver(names []string) *Resolver {
	return NewResolver(passes.NewStaticSchemaProvider(map[string][]string{testTable: names}))
}

func TestResolver_KnownTable(t *testing.T) {
	names := buildKnownNames(t)
	r := newTestResolver(names)
	resolves := func(handle string) string {
		t.Helper()
		p, ok := r.Resolve("", testTable, handle)
		require.Truef(t, ok, "handle %q should resolve", handle)
		require.Containsf(t, names, p, "resolved name %q not among the table's columns", p)
		return p
	}
	notResolves := func(handle string) {
		t.Helper()
		_, ok := r.Resolve("", testTable, handle)
		require.Falsef(t, ok, "handle %q should NOT resolve", handle)
	}

	// Single-value section: bare and section:column both resolve to it.
	require.Equal(t, resolves("string"), resolves("string:value"))

	// Multi-word section name folds across every naming style.
	require.Equal(t, resolves("timeRange"), resolves("time_range"))
	require.Equal(t, resolves("timeRange"), resolves("TIME-RANGE"))
	require.Equal(t, resolves("timeRange"), resolves("timeRange:value"))

	// Two-value-column section: bare is ambiguous; the specific columns resolve
	// and are distinct.
	notResolves("symbol")
	require.Equal(t, resolves("symbol:value"), resolves("Symbol:Value"))
	require.NotEqual(t, resolves("symbol:value"), resolves("symbol:ref"))

	// Plain / backbone column.
	resolves("id")

	// Non-handles are left for the server.
	notResolves("value")       // a bare column name is not a section handle
	notResolves("nonexistent") // unknown
	notResolves("string:nope") // known section, unknown column
}

func TestResolver_NonLeewayTable(t *testing.T) {
	r := NewResolver(passes.NewStaticSchemaProvider(map[string][]string{
		"plain": {"user_id", "amount", "created_at"},
	}))
	_, ok := r.Resolve("", "plain", "user_id")
	require.False(t, ok, "plain SQL columns are not leeway handles")
	_, ok = r.Resolve("", "unknown", "x")
	require.False(t, ok, "unknown table has no handles")
}

func TestBuildLabels_RoundTrip(t *testing.T) {
	names := buildKnownNames(t)
	labels := BuildLabels(names)
	require.NotEmpty(t, labels)

	r := newTestResolver(names)
	// Labels cover value AND support columns. The value-column labels round-trip
	// through the resolver (resolving one yields exactly the column it labels);
	// support-column labels are display-only and do not resolve — the resolver's
	// input vocabulary is deliberately value-only.
	resolvable := 0
	for phys, label := range labels {
		got, ok := r.Resolve("", testTable, label)
		if !ok {
			continue // support-column label — display only
		}
		resolvable++
		require.Equalf(t, phys, got, "label %q should round-trip to its column", label)
	}
	require.Positive(t, resolvable, "value-column labels must resolve")
	require.Greater(t, len(labels), resolvable, "support columns should also be labelled (display-only)")

	// A single default `value` column labels as the bare section; a named
	// column labels as section:column. Compare by folded identity so the
	// assertion is independent of stored casing.
	folded := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		folded[foldHandle(label)] = struct{}{}
	}
	require.Contains(t, folded, foldHandle("string"), "expected a bare 'string' section label")
	require.Contains(t, folded, foldHandle("symbol:value"), "expected a 'symbol:value' label")
	require.Contains(t, folded, foldHandle("symbol:ref"), "expected a 'symbol:ref' label")
}
