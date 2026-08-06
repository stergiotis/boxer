package test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/code/synthesis/golang/align"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

func getSystemTableColumnsDesc() (tableDesc common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.GetSystemTableColumnsManipulator()
	if err != nil {
		err = eh.Errorf("unable to get schema: %w", err)
		return
	}
	tableDesc, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build table desc: %w", err)
		return
	}
	return
}

// writeFileSystemTableColumns regenerates a source file belonging to ANOTHER
// package (../common). Under `go test ./...` that package may be compiling
// while this runs, so the file must never be observably absent: the unlink
// that used to precede the write left it missing for the whole align-and-
// format pass, and a concurrent package load — capslock's, which loads every
// package in the repo — failed with "no such file or directory" and an
// undefined symbol from ../common. align.WriteAligned replaces the file
// atomically, which needs no unlink.
func writeFileSystemTableColumns(path string, code string, t *testing.T) {
	err := align.WriteAligned(path, unsafeperf.UnsafeStringToBytes(code))
	require.NoError(t, err)
}

func TestSystemTableColumnsPopulate(t *testing.T) {
	// Use the system table columns schema itself as the test subject
	tblDesc, err := getSystemTableColumnsDesc()
	require.NoError(t, err)

	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, chTech)
	require.NoError(t, err)

	allocator := memory.DefaultAllocator
	entity := common.NewInEntitySystemTableColumns(allocator, 64)

	err = common.PopulateSchemaTable(entity, ir, tblDesc.DictionaryEntry.Name, tblDesc.DictionaryEntry.Comment)
	require.NoError(t, err)

	var records []arrow.RecordBatch
	records, err = entity.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)

	// The system table columns schema has columns across plain values + tagged value sections
	// Verify we got a reasonable number of entities (physical columns)
	numRows := records[0].NumRows()
	t.Logf("populated %d physical column entities from system-table-columns schema", numRows)
	require.Greater(t, numRows, int64(0))
}

func TestSystemTableColumnsDmlGeneration(t *testing.T) {
	tblDesc, err := getSystemTableColumnsDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

	var sourceCode []byte
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("common", naming.MustBeValidStylableName("system_table_columns"), tblDesc, common.SystemTableColumnsTableRowConfig, namingStyle)
	require.NoError(t, err)

	writeFileSystemTableColumns("../common/lw_system_table_columns_dml.out.go", unsafeperf.UnsafeBytesToString(sourceCode), t)
}
