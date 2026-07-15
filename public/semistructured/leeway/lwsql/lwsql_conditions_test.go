package lwsql

import (
	"slices"
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

// buildKnownNamesSep is buildKnownNames with the naming convention's separator
// chosen, so the '_'-separated shape (a dumped table) can be exercised too.
func buildKnownNamesSep(t *testing.T, sep string) []string {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(testTable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	const memb = common.MembershipSpecMixedLowCardVerbatimHighCardParameters
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, memb, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err := ddl.NewHumanReadableNamingConvention(sep)
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
		names = append(names, p.String())
	}
	require.NotEmpty(t, names)
	return names
}

// TestNameConditions_DeclaredSection is the ADR-0121 §SD5 acceptance criterion:
// the condition names must not merely *look* leeway-shaped, they must read back as
// a genuine `conditions` section of the result, beside the table's real
// sections. That is what makes BuildLabels and handle resolution work on them
// with no further change.
func TestNameConditions_DeclaredSection(t *testing.T) {
	names := buildKnownNames(t)
	r := newTestResolver(names)

	conds, ok, err := r.NameConditions("", testTable, 2)
	require.NoError(t, err)
	require.True(t, ok, "a leeway table must be named by the leeway namer")
	require.Len(t, conds, 2)
	for _, w := range conds {
		require.True(t, strings.HasPrefix(w, "tv:conditions:"), "unexpected condition name %q", w)
	}

	// The result a rewritten query returns: the table's own columns plus the
	// condition columns. Discovery must reconstruct the section from it.
	result := append(slices.Clone(names), conds...)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	table, _, err := conv.DiscoverTableFromColumnNames(result)
	require.NoError(t, err, "conds must read back as part of the table")

	var found bool
	for _, sec := range table.TaggedValuesSections {
		if string(sec.Name) != DefaultConditionSection {
			continue
		}
		found = true
		cols := make([]string, 0, len(sec.ValueColumnNames))
		for _, c := range sec.ValueColumnNames {
			cols = append(cols, string(c))
		}
		require.Equal(t, []string{"c1", "c2"}, cols)
	}
	require.True(t, found, "discovery found no %q section in %v", DefaultConditionSection, table.TaggedValuesSections)

	// The table's real sections must survive alongside it.
	var haveSymbol bool
	for _, sec := range table.TaggedValuesSections {
		if string(sec.Name) == "symbol" {
			haveSymbol = true
		}
	}
	require.True(t, haveSymbol, "the table's own sections must be unaffected")

	// And the whole point of a declared section: it labels like any other.
	labels := BuildLabels(result)
	require.Equal(t, "conditions:c1", labels[conds[0]])
	require.Equal(t, "conditions:c2", labels[conds[1]])
}

// TestNameConditions_ResolvesAsHandle checks the other direction: a resolver over
// a result carrying condition columns resolves `conditions:c1` and `conditions:*`
// like any authored section.
func TestNameConditions_ResolvesAsHandle(t *testing.T) {
	names := buildKnownNames(t)
	conds, _, err := newTestResolver(names).NameConditions("", testTable, 2)
	require.NoError(t, err)
	result := append(slices.Clone(names), conds...)

	r := newTestResolver(result)
	res := r.Resolve("", testTable, "conditions:c1")
	require.Equal(t, passes.ResolveOK, res.Kind)
	require.Equal(t, []string{conds[0]}, res.Physical)

	star := r.Resolve("", testTable, "conditions:*")
	require.Equal(t, passes.ResolveOK, star.Kind)
	require.Equal(t, conds, star.Physical)
}

func TestNameConditions_NonLeewayTableDeclines(t *testing.T) {
	r := NewResolver(passes.NewStaticSchemaProvider(map[string][]string{
		"plain": {"a", "b", "c"},
	}))
	names, ok, err := r.NameConditions("", "plain", 2)
	require.NoError(t, err)
	require.False(t, ok, "a plain SQL table must fall through to plain naming")
	require.Nil(t, names)
}

func TestNameConditions_UnknownTableDeclines(t *testing.T) {
	r := newTestResolver(buildKnownNames(t))
	_, ok, err := r.NameConditions("", "nosuchtable", 2)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestNameConditions_ExistingSectionRefuses is §SD5's collision case: a table
// already carrying the section would have synthesized columns merged into an
// authored one.
func TestNameConditions_ExistingSectionRefuses(t *testing.T) {
	names := buildKnownNames(t)
	conds, _, err := newTestResolver(names).NameConditions("", testTable, 1)
	require.NoError(t, err)

	// A table that already has the section.
	r := newTestResolver(append(slices.Clone(names), conds...))
	_, ok, err := r.NameConditions("", testTable, 2)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConditionSectionExists)
	require.False(t, ok)
}

// TestNameConditions_SectionNameFolds: the configured name is style-insensitive
// (ADR-0116's folding rule), so all three spellings are one section. The default
// (`conditions`) is a single word and so folds to itself; a multi-word name is
// what actually exercises the rule.
func TestNameConditions_SectionNameFolds(t *testing.T) {
	names := buildKnownNames(t)
	for _, spelling := range []string{"my-audit", "myAudit", "my_audit"} {
		r, err := NewResolverWithConditionSection(passes.NewStaticSchemaProvider(map[string][]string{testTable: names}), spelling)
		require.NoError(t, err)
		got, ok, err := r.NameConditions("", testTable, 1)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, "tv:my-audit:c1:val:b:0:0:0:0::", got[0], "spelling %q", spelling)
	}
}

// TestNameConditions_DefaultSection pins the default section's physical form.
func TestNameConditions_DefaultSection(t *testing.T) {
	names := buildKnownNames(t)
	got, ok, err := newTestResolver(names).NameConditions("", testTable, 2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []string{
		"tv:conditions:c1:val:b:0:0:0:0::",
		"tv:conditions:c2:val:b:0:0:0:0::",
	}, got)
}

func TestNameConditions_CustomSection(t *testing.T) {
	names := buildKnownNames(t)
	r, err := NewResolverWithConditionSection(passes.NewStaticSchemaProvider(map[string][]string{testTable: names}), "audit")
	require.NoError(t, err)
	got, ok, err := r.NameConditions("", testTable, 1)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "tv:audit:c1:val:b:0:0:0:0::", got[0])
}

func TestNewResolverWithConditionSection_Rejects(t *testing.T) {
	_, err := NewResolverWithConditionSection(passes.NewStaticSchemaProvider(nil), "")
	require.Error(t, err)
}

// TestNameConditions_UnderscoreSeparator: a dumped table joins its name
// components with '_', and a condition name must follow suit or it will not parse
// back into the table (§SD5).
func TestNameConditions_UnderscoreSeparator(t *testing.T) {
	names := buildKnownNamesSep(t, "_")
	r := newTestResolver(names)
	got, ok, err := r.NameConditions("", testTable, 1)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "tv_conditions_c1_val_b_0_0_0_0__", got[0])

	// It reads back as part of the same table.
	conv, err := ddl.NewHumanReadableNamingConvention("_")
	require.NoError(t, err)
	_, _, err = conv.DiscoverTableFromColumnNames(append(slices.Clone(names), got...))
	require.NoError(t, err)
}

func TestNameConditions_ZeroIsNoop(t *testing.T) {
	r := newTestResolver(buildKnownNames(t))
	names, ok, err := r.NameConditions("", testTable, 0)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, names)
}
