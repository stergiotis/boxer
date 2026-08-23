package readback

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
)

// udfTruthTestSQL is the truth-table that exercises every helper UDF; it
// emits one row per failed check (empty output ⇒ all pass).
//
//go:embed lw_readback_udfs_truth_test.sql
var udfTruthTestSQL string

// runClickHouseLocal runs script through `clickhouse-local` and returns its
// stdout, skipping the test when the binary is unavailable. clickhouse-local
// executes CREATE FUNCTION and the trailing SELECT in one session, so it can
// run the UDF DDL and the truth-table together without a server.
func runClickHouseLocal(t *testing.T, script string) string {
	t.Helper()
	cmd, err := extbin.ClickHouseLocal.Command(t.Context(), extbin.Opts{}, "--multiquery", "--output-format", "TSV")
	if err != nil {
		t.Skipf("clickhouse not on PATH, skipping (install ClickHouse to run UDF tests): %v", err)
	}
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("clickhouse local failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return stdout.String()
}

// TestHelperUDFs_TruthTable creates the helper UDFs and runs the truth-table
// against clickhouse-local. The fixtures cover scalar value-by-tag, aliasing,
// empty/missing edges, the begin/end/card round-trip, membership-set reads,
// and level-2 array unflatten + list-by-tag (incl. an empty array attribute
// and membership-card decoupled from value-length).
func TestHelperUDFs_TruthTable(t *testing.T) {
	out := runClickHouseLocal(t, HelperUDFsSQL()+"\n"+udfTruthTestSQL)
	if failed := strings.TrimSpace(out); failed != "" {
		t.Fatalf("UDF truth-table checks failed:\n%s", failed)
	}
}

// TestValueCountMatchesTheUdfPack pins the one place a rendered expression
// restates what a helper UDF does. lwextract.ValueCount cannot call
// LW_LIST_BY_TAG_EQUAL / LW_RAGGED_PARENT_IDS, because its term lands in the
// Filter a generated Scan embeds where no helper pack is installed
// (ADR-0100 S2) — so it locates the owning attribute with built-ins and reads
// the length lane there. That is a restatement, and a restatement drifts
// unless something compares the two. This runs both forms over the same
// fixtures, including the shapes the built-in form has to get right on its
// own: an attribute carrying several memberships, one carrying none, and an
// absent membership (where the search would otherwise answer attribute 1).
func TestValueCountMatchesTheUdfPack(t *testing.T) {
	lanes := lwextract.Lanes{Value: "valFlat", Ident: "tags", Card: "card", Length: "lens"}
	udf, err := lwextract.Value(lwextract.Request{Lanes: lanes, Shape: lwextract.ShapeList, Membership: "t"})
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	builtin, err := lwextract.ValueCount(lwextract.Request{Lanes: lanes, Shape: lwextract.ShapeList, Membership: "t"})
	if err != nil {
		t.Fatalf("ValueCount: %v", err)
	}
	if strings.Contains(builtin, "LW_") {
		t.Fatalf("the value count must stay inside the built-ins-only budget:\n%s", builtin)
	}

	// Per fixture: the per-attribute membership cardinality, the per-attribute
	// element count, the flattened membership tags, and the tag to look for.
	fixtures := []string{
		"[1, 1, 1] AS card, [1, 2, 1] AS lens, [7, 8, 9] AS tags, 8 AS t", // one membership each
		"[2, 1] AS card, [3, 1] AS lens, [7, 8, 9] AS tags, 8 AS t",       // an attribute carrying two
		"[0, 2, 1] AS card, [0, 2, 5] AS lens, [7, 8, 9] AS tags, 8 AS t", // an attribute carrying none
		"[2, 1] AS card, [3, 1] AS lens, [7, 8, 9] AS tags, 42 AS t",      // absent
		"[1, 1, 1] AS card, [0, 4, 1] AS lens, [7, 8, 9] AS tags, 7 AS t", // an empty list first
		"[1, 3] AS card, [2, 6] AS lens, [7, 8, 9, 10] AS tags, 10 AS t",  // last membership of the last attribute
	}
	var sb strings.Builder
	sb.WriteString(HelperUDFsSQL())
	for _, f := range fixtures {
		sb.WriteString("\nSELECT length(" + udf + ") AS viaUdf, " + builtin + " AS viaBuiltin FROM (SELECT " + f +
			", arrayFlatten(arrayMap((n) -> range(n), lens)) AS valFlat);")
	}
	out := strings.TrimSpace(runClickHouseLocal(t, sb.String()))
	rows := strings.Split(out, "\n")
	if len(rows) != len(fixtures) {
		t.Fatalf("want %d rows, got:\n%s", len(fixtures), out)
	}
	for i, row := range rows {
		cols := strings.Split(strings.TrimSpace(row), "\t")
		if len(cols) != 2 || cols[0] != cols[1] {
			t.Errorf("fixture %d (%s): UDF form and built-in form disagree: %q", i, fixtures[i], row)
		}
	}
}

// TestHelperUDFs_SQLShape is a server-free guard on the provisioning DDL:
// the co/ragged pack (ADR-0162) is layered underneath, the expected family
// functions are present, LEEWAY_UNFLATTEN stays retired (level-2 unflatten
// is the pack's LW_RAGGED_NEST), and the inherited BEGIN_INCL bug (referencing
// an undefined LW_LU_VAL_IDX_TO_MEMB_IDX_END) stays fixed.
func TestHelperUDFs_SQLShape(t *testing.T) {
	sql := HelperUDFsSQL()
	for _, fn := range []string{
		"LW_RAGGED_NEST",
		"LW_RAGGED_PARENT_IDS",
		"LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL",
		"LW_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL",
		"LW_LU_VAL_BY_MEMB_IDX",
		"LW_LU_ATTR_BY_TAG",
		"LW_LU_MEMBS_OF_VAL_IDX",
		"LW_VALUE_BY_TAG_EQUAL",
		"LW_LIST_BY_TAG_EQUAL",
	} {
		if !strings.Contains(sql, "FUNCTION "+fn+" ") {
			t.Errorf("HelperUDFsSQL missing CREATE FUNCTION %s", fn)
		}
	}
	// The undefined-_END regression: BEGIN_INCL must derive from arrayCumSum,
	// never call a LW_LU_VAL_IDX_TO_MEMB_IDX_END that no statement defines.
	if strings.Contains(sql, "VAL_IDX_TO_MEMB_IDX_END(") {
		t.Errorf("HelperUDFsSQL references undefined LW_LU_VAL_IDX_TO_MEMB_IDX_END (the inherited bug)")
	}
}
