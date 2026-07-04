package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateDeviceStore emits the whole device store package — DML, the
// composed CREATE TABLE, RA classes, the component codecs and the store
// glue — through the recordstore generator (ADR-0100 SD6). Run it to
// (re)generate:
//
//	go test -tags "$(cat tags)" -run TestGenerateDeviceStore ./public/storage/recordstore/example/
func TestGenerateDeviceStore(t *testing.T) {
	manip, err := GetDeviceSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "example",
		StoreName:   "Device",
		TableName:   "device",
		Table:       td,
		RowConfig:   TableRowConfig,
		ComponentPaths: []string{
			"./identity_dto.go",
			"./battery_dto.go",
			"./tagged_dto.go",
			"./located_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/storage/recordstore/example",
		// One data-skipping index through the ADR-0102 seam: the symbol
		// section's LowCardRef membership column is what the baked Scan
		// Filter's has()/hasAll() presence conjuncts prune on.
		DDL: &clickhouse.TableOptions{
			Indexes: []clickhouse.IndexSpec{{
				Ref:         clickhouse.ColumnRef{Section: "symbol", Role: common.ColumnRoleLowCardRef},
				Type:        "bloom_filter",
				Granularity: 4,
			}},
		},
	}.Generate())
}
