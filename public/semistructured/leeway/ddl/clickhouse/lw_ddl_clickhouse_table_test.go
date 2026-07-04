package clickhouse_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stretchr/testify/require"
)

// composeFixture builds a minimal table IR (id/ts plains + one symbol
// section) and the pieces ComposeCreateTable needs.
func composeFixture(t *testing.T) (*common.IntermediateTableRepresentation, common.NamingConventionI) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("tclause")
	manip.SetTableComment("table-clause seam test fixture")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
	sec := manip.TaggedValueSection("symbol").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	sec.TaggedValueColumn("value", ctabb.S)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)

	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	return ir, conv
}

// TestComposeCreateTable pins the ADR-0102 seam: the complete statement
// with every ColumnRef resolved to its quoted physical (encoded) name —
// the footgun every hand-wrapped clause string carried.
func TestComposeCreateTable(t *testing.T) {
	ir, conv := composeFixture(t)
	sql, err := clickhouse.ComposeCreateTable("tclause", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{
		Mode:   clickhouse.CreateModeIfNotExists,
		Engine: "MergeTree()",
		OrderBy: []clickhouse.ColumnRef{
			{PlainItem: common.PlainItemTypeEntityId},
			{Plain: "ts"},
		},
		Indexes: []clickhouse.IndexSpec{{
			Ref:         clickhouse.ColumnRef{Section: "symbol", Role: common.ColumnRoleLowCardRef},
			Type:        "bloom_filter",
			Granularity: 4,
		}},
		PartitionBy: "tuple()",
		Settings:    []string{"allow_suspicious_low_cardinality_types=1"},
		Tail:        "COMMENT 'seam test'",
	})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(sql, "CREATE TABLE IF NOT EXISTS tclause (\n"), sql)
	// ORDER BY resolved to quoted physical names, both selector forms.
	require.Contains(t, sql, `ORDER BY ("id:`)
	require.Contains(t, sql, `, "ts:`)
	// The index targets the section's membership identity column.
	require.Contains(t, sql, `INDEX idx_section_symbol_role_lr "tv:symbol:lr:`)
	require.Contains(t, sql, "TYPE bloom_filter GRANULARITY 4")
	// Clause order: ENGINE, PARTITION BY, ORDER BY, SETTINGS, tail.
	engineAt := strings.Index(sql, "ENGINE = MergeTree()")
	partitionAt := strings.Index(sql, "PARTITION BY tuple()")
	orderAt := strings.Index(sql, "ORDER BY (")
	settingsAt := strings.Index(sql, "SETTINGS allow_suspicious")
	tailAt := strings.Index(sql, "COMMENT 'seam test'")
	require.True(t, engineAt < partitionAt && partitionAt < orderAt && orderAt < settingsAt && settingsAt < tailAt, sql)
}

// TestComposeCreateTableRejects pins the generation-time failures: a
// reference that resolves to nothing, an ambiguous section-role
// reference shape, and a missing engine.
func TestComposeCreateTableRejects(t *testing.T) {
	ir, conv := composeFixture(t)
	rc := common.TableRowConfigMultiAttributesPerRow

	_, err := clickhouse.ComposeCreateTable("tclause", ir, rc, conv, clickhouse.TableOptions{
		Engine:  "MergeTree()",
		OrderBy: []clickhouse.ColumnRef{{Plain: "nope"}},
	})
	require.ErrorContains(t, err, "resolves to no physical column")

	_, err = clickhouse.ComposeCreateTable("tclause", ir, rc, conv, clickhouse.TableOptions{
		Engine:  "MergeTree()",
		OrderBy: []clickhouse.ColumnRef{{Section: "symbol"}},
	})
	require.ErrorContains(t, err, "exactly one of Column or Role")

	_, err = clickhouse.ComposeCreateTable("tclause", ir, rc, conv, clickhouse.TableOptions{})
	require.ErrorContains(t, err, "Engine is required")
}
