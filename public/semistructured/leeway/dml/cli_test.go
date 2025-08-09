package dml

import (
	"os"
	"testing"

	common2 "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	tblDesc, err := mapping.NewJsonMapping()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := NewGoCodeGeneratorDriver(conv, chTech)

	var sourceCode []byte
	tableRowConfig := common2.TableRowConfigMultiAttributesPerRow
	namingStyle := NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", common2.MustBeValidStylableName("json"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)
	checkCodeInvariants(sourceCode, t)

	p := "./example/dml_json.gen.go"
	_ = os.Remove(p)
	err = os.WriteFile(p, sourceCode, os.ModePerm)
	require.NoError(t, err)
}
