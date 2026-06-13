package golang_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	canonicaltypes "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	encodingaspects "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

// Regression for review D-1/D-2: the Go-struct backend emitted field names that
// included the structural separator (Tv:symbol:value… — not a Go identifier)
// and a malformed, tag-swallowing struct tag. The emitted struct fields must
// now parse as valid Go.
func TestGenerateColumnCode_EmitsValidGo(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	hints, err := encodingaspects.EncodeAspects(encodingaspects.AspectLightGeneralCompression)
	require.NoError(t, err)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id",
		canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64},
		hints, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("symbol", "value",
		canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8},
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet, common.MembershipSpecLowCardRef, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	const trc = common.TableRowConfigMultiAttributesPerRow
	var phys []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, trc)
		require.NoError(t, err)
	}
	require.NotEmpty(t, phys)

	gen := golang.NewTechnologySpecificCodeGenerator()
	var b strings.Builder
	gen.SetCodeBuilder(&b)
	for i, phy := range phys {
		require.NoError(t, gen.GenerateColumnCode(i, phy))
	}

	// Wrap the emitted fields in a struct and parse — invalid identifiers or a
	// malformed tag would fail here.
	src := "package p\ntype T struct {\n" + b.String() + "\n}\n"
	_, perr := parser.ParseFile(token.NewFileSet(), "emitted.go", src, parser.AllErrors)
	require.NoErrorf(t, perr, "generated struct must be valid Go, got:\n%s", src)
}
