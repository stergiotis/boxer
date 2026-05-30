package facts

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

// TestFactsInfoSchemaDmlGeneration regenerates lw_info_facts.out.go from the
// information_schema facts mapping. Driven from the repo-root generate.sh
// (step 6) via `go test -run`, mirroring the anchor/card_anchor_gen_test.go
// convention.
//
// This replaces the pebble2impl pipeline
//
//	leeway ddl table mappings informationschema facts --format cbor \
//	  | leeway dml table generate go --packageName facts --tableName facts
//
// whose CBOR producer subcommand (`ddl table mappings informationschema`) was
// not ported to boxer. mapping.NewInformationSchemaFactsMapping is the same
// TableDesc that producer emitted; feeding it straight into the dml driver
// (the consumer half, with the CLI's NewDefaultGoClassNamer) reproduces the
// output without the CBOR round-trip the pipe needed.
func TestFactsInfoSchemaDmlGeneration(t *testing.T) {
	tblDesc, err := mapping.NewInformationSchemaFactsMapping()
	require.NoError(t, err)

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	namer := gocodegen.NewDefaultGoClassNamer()
	sourceCode, _, err := driver.GenerateGoClasses("facts", naming.MustBeValidStylableName("facts"), tblDesc, tableRowConfig, namer)
	require.NoError(t, err)

	err = os.WriteFile("./lw_info_facts.out.go", sourceCode, 0o644)
	require.NoError(t, err)
}
