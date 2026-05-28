package anchor

import (
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

func GetAnchorTableDesc() (tableDesc common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = GetSchemaInManipulator()
	if err != nil {
		err = eh.Errorf("unable to get schema")
		return
	}
	tableDesc, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build table desc")
		return
	}
	return
}

func writeFile(path string, code string, t *testing.T) {
	_ = os.Remove(path)
	err := os.WriteFile(path, unsafeperf.UnsafeStringToBytes(code), os.ModePerm)
	require.NoError(t, err)
}

func TestReadAccessGoClassBuilderGeneration(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := readaccess.NewGoCodeGeneratorDriver(conv, chTech, true)

	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingConvention := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("anchor", naming.MustBeValidStylableName("test_table"), tblDesc, tableRowConfig, namingConvention)
	require.NoError(t, err)

	writeFile("./card_anchor_ra.out.go", unsafeperf.UnsafeBytesToString(sourceCode), t)
}
func TestDmlGeneration(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

	var sourceCode []byte
	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("anchor", naming.MustBeValidStylableName("test_table"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)

	writeFile("./card_anchor_dml.out.go", unsafeperf.UnsafeBytesToString(sourceCode), t)
}
func TestDdlClickHouseGeneration(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	b.WriteString(`CREATE OR REPLACE TABLE anchor.facts (
`)
	tech.SetCodeBuilder(b)

	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	require.NoError(t, err)
	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	var conv common.NamingConventionI
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	generator := ddl.NewGeneratorDriver()
	err = generator.GenerateColumnsCode(ir.IterateColumnProps(), tableRowConfig, conv, tech, func(hint encodingaspects.AspectE) (ok bool, msg string) {
		return true, ""
	})
	require.NoError(t, err)
	b.WriteString(`
) ENGINE = Memory SETTINGS min_bytes_to_keep = 1000000, max_bytes_to_keep = 100000000
SETTINGS allow_suspicious_low_cardinality_types=1;
`)
	writeFile("./card_anchor_ddl_clickhouse.out.sql", b.String(), t)
}
