package firsttable

import (
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stretchr/testify/require"
)

// TestGenerate writes the generated write side (events_dml.out.go) and read
// side (events_ra.out.go) of the events table. Running the package's tests
// regenerates both; a diff after `go test` means the schema or a generator
// changed.
func TestGenerate(t *testing.T) {
	manip, err := NewSchema()
	require.NoError(t, err)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	name := naming.MustBeValidStylableName("events")

	code, _, err := dml.NewGoCodeGeneratorDriver(conv, tech).GenerateGoClasses("firsttable", name, tbl, TableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("events_dml.out.go", code, 0o644))

	code, _, err = readaccess.NewGoCodeGeneratorDriver(conv, tech, true).GenerateGoClasses("firsttable", name, tbl, TableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("events_ra.out.go", code, 0o644))
}
