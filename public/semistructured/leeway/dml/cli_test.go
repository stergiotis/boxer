package dml

import (
	"os"
	"testing"

	common2 "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
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
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("json"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)
	checkCodeInvariants(sourceCode, t)

	p := "./example/dml_json.out.go"
	_ = os.Remove(p)
	err = os.WriteFile(p, sourceCode, os.ModePerm)
	require.NoError(t, err)
}
