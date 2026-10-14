package dml

import (
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/code/synthesis/golang/align"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

// routingTableDesc is a table whose plain groups have several columns of one
// Go type, and whose id group has a column named like the group's setter.
func routingTableDesc(t *testing.T) common.TableDesc {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", ctabb.S)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "partitioningKey", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "routingKey", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "classification", ctabb.S)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "kind", ctabb.S)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		TaggedValueColumn("value", ctabb.S)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	return tbl
}

func generateRouting(t *testing.T, private bool) string {
	t.Helper()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
	if private {
		driver.EmitControlPrivate()
	}
	src, _, err := driver.GenerateGoClasses("example", naming.MustBeValidStylableName("routing"), routingTableDesc(t), common.TableRowConfigMultiAttributesPerRow, gocodegen.NewMultiTablePerPackageGoClassNamer())
	require.NoError(t, err)
	checkCodeInvariants(src, t)
	_, err = parser.ParseFile(token.NewFileSet(), "routing.go", src, parser.ParseComments)
	require.NoError(t, err)
	return string(src)
}

// TestGenerateDmlRouting writes example/dml_routing.out.go, whose per-column
// setters example's tests compare against the grouped ones.
func TestGenerateDmlRouting(t *testing.T) {
	src := generateRouting(t, false)
	for _, want := range []string{
		"func (inst *InEntityRouting) SetRouting(partitioningKey",
		"func (inst *InEntityRouting) SetPartitioningKey(partitioningKey",
		"func (inst *InEntityRouting) SetRoutingKey(routingKey",
		"func (inst *InEntityRouting) SetClassification(classification",
		"func (inst *InEntityRouting) SetKind(kind",
		"func (inst *InEntityRouting) SetId(id",
		"func (inst *InEntityRouting) SetIdColumn(id",
		"func (inst *InEntityRouting) SetNaturalKey(naturalKey",
		"// SetRouting sets the entity's partitioningKey, routingKey, classification,\n// kind columns",
		"// SetTimestamp sets the entity's ts column.",
	} {
		require.Contains(t, src, want)
	}
	// A single-column group's grouped setter already takes its one column.
	require.NotContains(t, src, "SetTs(")
	require.NoError(t, align.WriteAligned("example/dml_routing.out.go", []byte(src)))
}

// TestPlainColumnSettersPrivateControl checks that under private control the
// per-column setters are unexported and reached through exported drivers,
// like the grouped ones.
func TestPlainColumnSettersPrivateControl(t *testing.T) {
	src := generateRouting(t, true)
	for _, want := range []string{
		"func (inst *InEntityRouting) setKind(kind",
		"func (inst *InEntityRouting) setIdColumn(id",
		"func InEntityRoutingSetKind(e *InEntityRouting, kind",
		"func InEntityRoutingSetIdColumn(e *InEntityRouting, id",
		"return e.setKind(kind",
		"func InEntityRoutingSetRouting(e *InEntityRouting, ",
	} {
		require.Contains(t, src, want)
	}
	require.False(t, strings.Contains(src, ") SetKind("), "a per-column setter must not be exported under private control")
}

// TestPlainColumnSettersCollision checks that columns whose setter names
// collide with a grouped setter and with each other never yield two methods
// of one name: the generator either disambiguates them or refuses.
func TestPlainColumnSettersCollision(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "idColumn", ctabb.U64)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		TaggedValueColumn("value", ctabb.S)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
	src, _, err := driver.GenerateGoClasses("example", naming.MustBeValidStylableName("collide"), tbl, common.TableRowConfigMultiAttributesPerRow, gocodegen.NewMultiTablePerPackageGoClassNamer())
	if err != nil {
		return
	}
	seen := make(map[string]bool)
	for _, m := range regexp.MustCompile(`func \(inst \*InEntityCollide\) (\w+)\(`).FindAllStringSubmatch(string(src), -1) {
		require.Falsef(t, seen[m[1]], "method %s emitted twice", m[1])
		seen[m[1]] = true
	}
	require.True(t, seen["SetIdColumn"])
}
