package dml

import (
	"bytes"
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var updateInfoSchemaGolden = flag.Bool("update", false, "rewrite the information_schema DML goldens under testdata/ with the current generator output")

// generatorLocation matches the "// code generator" comment block the
// generator stamps before each section: the function and the file:line that
// emitted it. Those move whenever the generator's source does, so the
// comparison ignores them; the golden still records them as written.
var generatorLocation = regexp.MustCompile(`(?m)^// code generator\n// .*\n// .*:\d+\n`)

func normaliseGeneratorLocations(code []byte) (out []byte) {
	return generatorLocation.ReplaceAll(code, []byte("// code generator\n"))
}

// TestInformationSchemaDmlMatchesGolden generates the DML classes for the
// information_schema facts and vcsmanaged mappings and compares them with the
// goldens under testdata/. The output is kept as testdata rather than as a
// compiled package because nothing in this module imports it; the generator's
// compile-ability is covered by the packages it writes elsewhere
// (dml/example, leeway/test/singledecl). Rerun with -update after an
// intentional generator or mapping change.
func TestInformationSchemaDmlMatchesGolden(t *testing.T) {
	cases := []struct {
		name    string
		mapping func() (common.TableDesc, error)
	}{
		{"facts", mapping.NewInformationSchemaFactsMapping},
		{"vcsmanaged", mapping.NewInformationSchemaVcsManagedDimensionMapping},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tblDesc, err := c.mapping()
			require.NoError(t, err)

			conv, err := ddl.NewHumanReadableNamingConvention(":")
			require.NoError(t, err)
			driver := NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())

			const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
			namer := gocodegen.NewDefaultGoClassNamer()
			generated, _, err := driver.GenerateGoClasses(c.name, naming.MustBeValidStylableName(c.name), tblDesc, tableRowConfig, namer)
			require.NoError(t, err)
			checkCodeInvariants(generated, t)
			_, err = parser.ParseFile(token.NewFileSet(), c.name+".go", generated, parser.SkipObjectResolution)
			require.NoError(t, err, "the generated source does not parse")

			goldenPath := filepath.Join("testdata", "infoschema_"+c.name+"_dml.go.golden")
			if *updateInfoSchemaGolden {
				require.NoError(t, os.WriteFile(goldenPath, generated, 0o644))
				t.Logf("golden updated: %s (%d bytes)", goldenPath, len(generated))
				return
			}
			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "read golden; rerun with -update to create it")
			if bytes.Equal(normaliseGeneratorLocations(generated), normaliseGeneratorLocations(want)) {
				return
			}
			newPath := goldenPath + ".new"
			_ = os.WriteFile(newPath, generated, 0o644)
			t.Fatalf("generator output differs from %s; wrote %s (diff with `diff -u`); rerun with -update to overwrite the golden", goldenPath, newPath)
		})
	}
}
