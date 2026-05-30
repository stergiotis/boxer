package vcsmanaged

import (
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

// TestVcsManagedInfoSchemaDmlGeneration regenerates lw_info_vcsmanaged.out.go
// from the information_schema vcsmanaged dimension mapping. Driven from the
// repo-root generate.sh (step 6) via `go test -run`, mirroring the
// anchor/card_anchor_gen_test.go convention.
//
// This replaces the pebble2impl pipeline
//
//	leeway ddl table mappings informationschema vcsmanaged --format cbor \
//	  | leeway dml table generate go --packageName vcsmanaged --tableName vcsmanaged
//
// whose CBOR producer subcommand (`ddl table mappings informationschema`) was
// not ported to boxer. See the facts sibling test for the full rationale.
func TestVcsManagedInfoSchemaDmlGeneration(t *testing.T) {
	tblDesc, err := mapping.NewInformationSchemaVcsManagedDimensionMapping()
	require.NoError(t, err)

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	namer := gocodegen.NewDefaultGoClassNamer()
	sourceCode, _, err := driver.GenerateGoClasses("vcsmanaged", naming.MustBeValidStylableName("vcsmanaged"), tblDesc, tableRowConfig, namer)
	require.NoError(t, err)

	err = os.WriteFile("./lw_info_vcsmanaged.out.go", sourceCode, 0o644)
	require.NoError(t, err)
}
