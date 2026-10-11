package bagdemo

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
)

// TestGenerateBagTable (re)generates the table's DML and read-access
// classes in place.
func TestGenerateBagTable(t *testing.T) {
	manip, err := GetSchemaInManipulator()
	require.NoError(t, err)
	tblDesc, err := manip.BuildTableDesc()
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	table := naming.MustBeValidStylableName("bag_table")

	dmlCode, _, err := dml.NewGoCodeGeneratorDriver(conv, chTech).GenerateGoClasses("bagdemo", table, tblDesc, TableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("./bag_dml.out.go", dmlCode, 0o644))

	raCode, _, err := readaccess.NewGoCodeGeneratorDriver(conv, chTech, true).GenerateGoClasses("bagdemo", table, tblDesc, common.TableRowConfigMultiAttributesPerRow, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("./bag_ra.out.go", raCode, 0o644))
}
