package datacatalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// buildFixtureTable returns a small but structurally complete leeway table:
// plain backbone columns and two tagged sections, so the naming grammar has to
// reconstruct both halves rather than just the easy one.
func buildFixtureTable(t *testing.T) (tbl common.TableDesc) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	ctp := canonicaltypes.NewParser()
	ctId := ctp.MustParsePrimitiveTypeAst("bh")
	ctStr := ctp.MustParsePrimitiveTypeAst("s")
	ctU64 := ctp.MustParsePrimitiveTypeAst("u64")

	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctId, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "observedAt", ctU64, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("metric", "name", ctStr,
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))
	manip.MergeTaggedValueColumn("metric", "value", ctU64,
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))
	manip.MergeTaggedValueColumn("label", "text", ctStr,
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecHighCardRef, naming.Key(""), naming.Key(""))

	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	return
}

// physicalNames renders a table's physical column names under sep — the DDL
// path, so the names the test feeds back through Classify are the ones a real
// CREATE TABLE would carry.
func physicalNames(t *testing.T, tbl *common.TableDesc, sep string) (names []string) {
	t.Helper()
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(tbl, tech))
	conv, err := ddl.NewHumanReadableNamingConvention(sep)
	require.NoError(t, err)
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names = make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, p.String())
	}
	require.NotEmpty(t, names)
	return
}

func TestSniffSeparator(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, "_"},
		{"no colon anywhere", []string{"a", "b_c"}, "_"},
		{"colon in the first column", []string{"x:y", "z"}, ":"},
		// Only the first non-underscore column is evidence: a later colon does
		// not overturn it, or the two conventions would mix within one table.
		{"colon only after the first column", []string{"a_b", "x:y"}, "_"},
		{"leading underscore columns skipped", []string{"_part", "_rowid", "x:y"}, ":"},
		{"all columns underscore-prefixed", []string{"_part", "_rowid"}, "_"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, datacatalog.SniffSeparator(c.in))
		})
	}
}

// Both separators must classify: `:` is the canonical spelling and `_` the one
// ClickHouse table dumps mangle it to, and a catalog run sees both.
func TestClassify_LeewayUnderBothSeparators(t *testing.T) {
	tbl := buildFixtureTable(t)
	for _, sep := range []string{":", "_"} {
		t.Run("sep="+sep, func(t *testing.T) {
			names := physicalNames(t, &tbl, sep)
			cl := datacatalog.Classify(names)
			require.NoError(t, cl.Err)
			assert.Equal(t, datacatalog.KindLeeway, cl.Kind)
			assert.Equal(t, sep, cl.Separator)
			assert.Equal(t, common.TableRowConfigMultiAttributesPerRow, cl.RowConfig)
			require.NotNil(t, cl.Table)
			require.NotNil(t, cl.Convention)
			assert.Empty(t, cl.Detail())
			// The reconstruction is a table, not a husk: both tagged sections
			// come back.
			secs := make([]string, 0, len(cl.Table.TaggedValuesSections))
			for _, s := range cl.Table.TaggedValuesSections {
				secs = append(secs, string(s.Name))
			}
			assert.ElementsMatch(t, []string{"metric", "label"}, secs)
		})
	}
}

func TestClassify_Opaque(t *testing.T) {
	cases := [][]string{
		{"ts", "label", "value"},
		{"source", "target", "value"},
		{"lane", "title"},
		nil,
	}
	for _, names := range cases {
		cl := datacatalog.Classify(names)
		assert.Equal(t, datacatalog.KindOpaque, cl.Kind)
		assert.Nil(t, cl.Table)
		assert.Error(t, cl.Err)
		// The failure is a readable row rather than an absence, so it must
		// render to something.
		assert.NotEmpty(t, cl.Detail())
	}
}

// One leeway-looking column among opaque ones does not make the table leeway:
// discovery fails on the first name that does not parse, and that failure is
// the classification.
func TestClassify_PartiallyLeewayIsOpaque(t *testing.T) {
	tbl := buildFixtureTable(t)
	names := physicalNames(t, &tbl, ":")
	cl := datacatalog.Classify(append(names[:1:1], "an_ordinary_column"))
	assert.Equal(t, datacatalog.KindOpaque, cl.Kind)
	assert.Error(t, cl.Err)
}

func TestKindE_String(t *testing.T) {
	assert.Equal(t, "opaque", datacatalog.KindOpaque.String())
	assert.Equal(t, "leeway", datacatalog.KindLeeway.String())
	assert.Equal(t, "invalid", datacatalog.KindE(7).String())
	assert.Len(t, datacatalog.AllKinds, 2)
}
