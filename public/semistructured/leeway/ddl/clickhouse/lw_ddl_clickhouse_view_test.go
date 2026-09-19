package clickhouse_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stretchr/testify/require"
)

// passThrough fills every column from a same-named source column: what a
// view over a table of the same shape does.
func passThrough(col clickhouse.ViewColumn) (clickhouse.ViewExpr, error) {
	return clickhouse.ViewExpr{SQL: `"` + col.Physical + `"`, Exact: true}, nil
}

// TestComposeCreateView pins the view half of the seam: the view declares
// the table's physical columns — same names, same order — and casts each
// caller expression to the type the table declares.
func TestComposeCreateView(t *testing.T) {
	ir, conv := composeFixture(t)
	table, err := clickhouse.ComposeCreateTable("tclause", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.TableOptions{Engine: "MergeTree()"})
	require.NoError(t, err)

	var seen []clickhouse.ViewColumn
	sql, err := clickhouse.ComposeCreateView("tclause_v", ir, common.TableRowConfigMultiAttributesPerRow, conv, clickhouse.ViewOptions{
		Mode:    clickhouse.CreateModeOrReplace,
		Clauses: "SQL SECURITY DEFINER",
		From:    "src WHERE keep",
	}, func(col clickhouse.ViewColumn) (clickhouse.ViewExpr, error) {
		seen = append(seen, col)
		if col.PlainItem == common.PlainItemTypeEntityId {
			return clickhouse.ViewExpr{SQL: "src_id", Exact: true}, nil
		}
		return clickhouse.ViewExpr{SQL: "e" + col.Type[:1]}, nil
	})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(sql, "CREATE OR REPLACE VIEW tclause_v SQL SECURITY DEFINER AS\nSELECT\n"), sql)
	require.True(t, strings.HasSuffix(sql, "\nFROM src WHERE keep"), sql)
	require.NotContains(t, sql, "SETTINGS")

	// Every column of the table, in the table's order, under its name and
	// its declared type.
	require.NotEmpty(t, seen)
	at := 0
	for _, col := range seen {
		decl := `"` + col.Physical + `" ` + col.Type
		i := strings.Index(table[at:], decl)
		require.GreaterOrEqual(t, i, 0, "the table does not declare %s after offset %d:\n%s", decl, at, table)
		at += i + len(decl)
		require.Contains(t, sql, ` AS "`+col.Physical+`"`)
	}
	require.Equal(t, strings.Count(table, "\n\t\""), len(seen), "one expression per declared column")

	// Coordinates: the plains carry their lane, the section's columns their
	// role; the id passes through uncast, the rest are cast.
	require.Equal(t, common.PlainItemTypeEntityId, seen[0].PlainItem)
	require.EqualValues(t, "id", seen[0].Name)
	require.Contains(t, sql, "\n\tsrc_id AS \"id:")
	var value clickhouse.ViewColumn
	for _, col := range seen {
		if col.Section == "symbol" && col.Role == common.ColumnRoleValue {
			value = col
		}
	}
	require.EqualValues(t, "value", value.Name)
	require.Equal(t, "Array(String)", value.Type)
	require.Contains(t, sql, "CAST(eA AS Array(String)) AS \""+value.Physical+"\"")
}

func TestComposeCreateViewRejects(t *testing.T) {
	ir, conv := composeFixture(t)
	rc := common.TableRowConfigMultiAttributesPerRow

	_, err := clickhouse.ComposeCreateView("v", ir, rc, conv, clickhouse.ViewOptions{}, passThrough)
	require.ErrorContains(t, err, "From is required")

	_, err = clickhouse.ComposeCreateView("v", ir, rc, conv, clickhouse.ViewOptions{From: "src"}, nil)
	require.ErrorContains(t, err, "exprFor is required")

	_, err = clickhouse.ComposeCreateView("v", ir, rc, conv, clickhouse.ViewOptions{From: "src", Mode: 99}, passThrough)
	require.ErrorContains(t, err, "unknown create mode")

	// A column left without an expression is the drift this exists to stop.
	_, err = clickhouse.ComposeCreateView("v", ir, rc, conv, clickhouse.ViewOptions{From: "src"}, func(col clickhouse.ViewColumn) (clickhouse.ViewExpr, error) {
		if col.Section != "" {
			return clickhouse.ViewExpr{}, nil
		}
		return passThrough(col)
	})
	require.ErrorContains(t, err, "has no expression")

	boom := errors.New("boom")
	_, err = clickhouse.ComposeCreateView("v", ir, rc, conv, clickhouse.ViewOptions{From: "src"}, func(clickhouse.ViewColumn) (clickhouse.ViewExpr, error) {
		return clickhouse.ViewExpr{}, boom
	})
	require.ErrorIs(t, err, boom)
}

// TestComposeCreateViewMatchesTable runs both statements through
// clickhouse-local: a cast view over a table of the same shape must
// DESCRIBE to the table's own names and types, LowCardinality lanes
// included.
func TestComposeCreateViewMatchesTable(t *testing.T) {
	bin, err := clickhouse.GetClickHouseBinaryPath()
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ir, conv := composeFixture(t)
	rc := common.TableRowConfigMultiAttributesPerRow
	table, err := clickhouse.ComposeCreateTable("tclause", ir, rc, conv, clickhouse.TableOptions{
		Engine:   "MergeTree()",
		OrderBy:  []clickhouse.ColumnRef{{Plain: "id"}},
		Settings: []string{"allow_suspicious_low_cardinality_types=1"},
	})
	require.NoError(t, err)
	require.Contains(t, table, "LowCardinality(UInt", "the fixture must exercise a non-string LowCardinality lane")
	lowCard := []string{"allow_suspicious_low_cardinality_types=1"}
	view, err := clickhouse.ComposeCreateView("tclause_v", ir, rc, conv, clickhouse.ViewOptions{From: "tclause", Settings: lowCard},
		func(col clickhouse.ViewColumn) (clickhouse.ViewExpr, error) {
			return clickhouse.ViewExpr{SQL: `"` + col.Physical + `"`}, nil
		})
	require.NoError(t, err)

	describe := func(name string) string {
		script := table + ";\n" + view + ";\nSELECT count() FROM tclause_v FORMAT Null;\nSELECT name, type FROM system.columns WHERE table = '" + name + "' ORDER BY position FORMAT TSV;\n"
		path := filepath.Join(t.TempDir(), "q.sql")
		require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
		// No session setting: the view's own SETTINGS clause has to carry
		// the cast to the LowCardinality membership lane.
		cmd := exec.Command(bin, "local", "--queries-file", path)
		out, cerr := cmd.CombinedOutput()
		require.NoError(t, cerr, "%s\n%s", script, out)
		return string(out)
	}
	want := describe("tclause")
	require.NotEmpty(t, strings.TrimSpace(want))
	require.Equal(t, want, describe("tclause_v"))
}
