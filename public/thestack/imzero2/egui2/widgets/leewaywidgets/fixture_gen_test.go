//go:build llm_generated_opus47

package leewaywidgets

import (
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../../tags)\" -run TestFixtureDmlGeneration ."

// TestFixtureDmlGeneration regenerates fixture_dml.out.go from the fixture
// TableDesc. Driven by `//go:generate` above; run via `go generate ./...`
// from this directory (or anywhere up the tree). Mirrors the
// boxerstaging/leeway/anchor/card_anchor_gen_test.go convention.
func TestFixtureDmlGeneration(t *testing.T) {
	tblDesc, err := BuildFixtureTableDesc()
	require.NoError(t, err)

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, tech)

	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err := driver.GenerateGoClasses("leewaywidgets",
		naming.MustBeValidStylableName(FixtureTableName),
		tblDesc, FixtureTableRowConfig, namer)
	require.NoError(t, err)

	const outPath = "./fixture_dml.out.go"
	_ = os.Remove(outPath)
	err = os.WriteFile(outPath, sourceCode, 0o644)
	require.NoError(t, err)
}
