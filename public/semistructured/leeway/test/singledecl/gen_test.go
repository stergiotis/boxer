package singledecl

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGenerateSingleDeclFixture ."

import (
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stretchr/testify/require"
)

// TestGenerateSingleDeclFixture (re)generates the fixture's DML and
// read-access classes for both schema variants. Run it to regenerate:
//
//	go test -run TestGenerateSingleDeclFixture ./public/semistructured/leeway/test/singledecl/
func TestGenerateSingleDeclFixture(t *testing.T) {
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	for _, declared := range []bool{true, false} {
		manip, merr := GetSchemaInManipulator(declared)
		require.NoError(t, merr)
		td, berr := manip.BuildTableDesc()
		require.NoError(t, berr)
		stem := string(td.DictionaryEntry.Name)
		stylable := naming.MustBeValidStylableName(stem)

		dmlDriver := dml.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
		code, _, gerr := dmlDriver.GenerateGoClasses("singledecl", stylable, td, common.TableRowConfigMultiAttributesPerRow, gocodegen.NewMultiTablePerPackageGoClassNamer())
		require.NoError(t, gerr)
		require.NoError(t, os.WriteFile("./"+stem+"_dml.out.go", code, 0o644))

		raDriver := readaccess.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator(), true)
		code, _, gerr = raDriver.GenerateGoClasses("singledecl", stylable, td, common.TableRowConfigMultiAttributesPerRow, gocodegen.NewMultiTablePerPackageGoClassNamer())
		require.NoError(t, gerr)
		require.NoError(t, os.WriteFile("./"+stem+"_ra.out.go", code, 0o644))
	}
}
