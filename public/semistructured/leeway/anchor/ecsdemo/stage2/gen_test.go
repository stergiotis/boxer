package stage2

import (
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

// These generation tests emit the bespoke drone card the same way anchor emits
// its own (card_anchor_*_test.go): via the leeway code-generator libraries, no
// app CLI. Run them to (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateDrone ./public/semistructured/leeway/anchor/ecsdemo/stage2/

func droneTableDesc(t *testing.T) common.TableDesc {
	t.Helper()
	manip, err := GetDroneSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	return td
}

func writeGenFile(t *testing.T, path, code string) {
	t.Helper()
	_ = os.Remove(path)
	require.NoError(t, os.WriteFile(path, unsafeperf.UnsafeStringToBytes(code), os.ModePerm))
}

// TestGenerateDroneDML emits the Arrow write target (InDroneTableTable + section
// builders) that marshalling drives.
func TestGenerateDroneDML(t *testing.T) {
	td := droneTableDesc(t)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := dml.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	code, _, err := driver.GenerateGoClasses("stage2", naming.MustBeValidStylableName("drone_table"), td, TableRowConfig, namer)
	require.NoError(t, err)
	writeGenFile(t, "./drone_dml.out.go", unsafeperf.UnsafeBytesToString(code))
}

// TestGenerateDroneDDL emits the ClickHouse CREATE TABLE for the bespoke schema.
func TestGenerateDroneDDL(t *testing.T) {
	td := droneTableDesc(t)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	b.WriteString("CREATE OR REPLACE TABLE drone.facts (\n")
	tech.SetCodeBuilder(b)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, tech))
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	gen := ddl.NewGeneratorDriver()
	require.NoError(t, gen.GenerateColumnsCode(ir.IterateColumnProps(), TableRowConfig, conv, tech,
		func(hint encodingaspects.AspectE) (bool, string) { return true, "" }))
	b.WriteString("\n) ENGINE = Memory SETTINGS allow_suspicious_low_cardinality_types=1;\n")
	writeGenFile(t, "./drone_ddl_clickhouse.out.sql", b.String())
}

// TestGenerateDroneDTOCodec emits the marshallgen codec for DroneEntity.
func TestGenerateDroneDTOCodec(t *testing.T) {
	_, err := marshallgen.Generate("./dto.go", "./dto.out.go", marshallgen.NoOpWrapper{}, marshallgen.EmitOpts{})
	require.NoError(t, err)
}

// TestGenerateDroneRA emits the read-access classes (per-section Attributes /
// Memberships readers + plain id reader) used to extract typed components from a
// marshalled Arrow batch.
func TestGenerateDroneRA(t *testing.T) {
	td := droneTableDesc(t)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := readaccess.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator(), true)
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	code, _, err := driver.GenerateGoClasses("stage2", naming.MustBeValidStylableName("drone_table"), td, TableRowConfig, namer)
	require.NoError(t, err)
	writeGenFile(t, "./drone_ra.out.go", unsafeperf.UnsafeBytesToString(code))
}
