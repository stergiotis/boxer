package readback

import (
	_ "embed"
	"os/exec"
	"strings"
	"testing"
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
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH, skipping (install ClickHouse to run UDF tests): %v", err)
	}
	cmd := exec.Command(bin, "--multiquery", "--output-format", "TSV")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("clickhouse-local failed: %v\nstderr:\n%s", err, stderr.String())
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

// TestHelperUDFs_SQLShape is a server-free guard on the provisioning DDL:
// the co/ragged pack (ADR-0162) is layered underneath, the expected family
// functions are present, LEEWAY_UNFLATTEN stays retired (level-2 unflatten
// is the pack's raggedNest), and the inherited BEGIN_INCL bug (referencing
// an undefined LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END) stays fixed.
func TestHelperUDFs_SQLShape(t *testing.T) {
	sql := HelperUDFsSQL()
	for _, fn := range []string{
		"raggedNest",
		"leewayPackVersion",
		"LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL",
		"LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL",
		"LEEWAY_LU_MEMB_IDX_TO_VAL_IDX",
		"LEEWAY_LU_VAL_BY_MEMB_IDX",
		"LEEWAY_LU_ATTR_BY_TAG",
		"LEEWAY_LU_MEMBS_OF_VAL_IDX",
		"LEEWAY_VALUE_BY_TAG_EQUAL",
		"LEEWAY_LIST_BY_TAG_EQUAL",
	} {
		if !strings.Contains(sql, "FUNCTION "+fn+" ") {
			t.Errorf("HelperUDFsSQL missing CREATE FUNCTION %s", fn)
		}
	}
	// The undefined-_END regression: BEGIN_INCL must derive from arrayCumSum,
	// never call a LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END that no statement defines.
	if strings.Contains(sql, "VAL_IDX_TO_MEMB_IDX_END(") {
		t.Errorf("HelperUDFsSQL references undefined LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END (the inherited bug)")
	}
}
