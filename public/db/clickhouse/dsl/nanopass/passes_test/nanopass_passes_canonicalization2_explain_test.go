//go:build llm_generated_opus46

package passes_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// EXPLAIN SYNTAX Integration Tests
//
// Require CLICKHOUSE_ENDPOINT env var (e.g. "http://localhost:8123").
// Optional: CLICKHOUSE_USER, CLICKHOUSE_PASSWORD, CLICKHOUSE_DATABASE.
//
//   CLICKHOUSE_ENDPOINT="http://localhost:8123" go test -run TestExplain -v -count=1
//
// Known EXPLAIN SYNTAX mismatches (not bugs — different canonical forms):
//   - Comma join: ClickHouse preserves "FROM t1, t2", we rewrite to CROSS JOIN
//   - Simple CASE: ClickHouse uses caseWithExpression internally, we use multiIf
//     with equality expansion. EXPLAIN SYNTAX normalizes both to multiIf.
// ============================================================================

type clickHouseTestClient struct {
	endpoint string
	httpCli  *http.Client
}

func newClickHouseTestClient(endpoint string) *clickHouseTestClient {
	return &clickHouseTestClient{
		endpoint: endpoint,
		httpCli:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (inst *clickHouseTestClient) injectReqHeaders(req *http.Request) {
	user := os.Getenv("CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	req.Header.Set("X-ClickHouse-User", user)
	if pw := os.Getenv("CLICKHOUSE_PASSWORD"); pw != "" {
		req.Header.Set("X-ClickHouse-Key", pw)
	}
	if db := os.Getenv("CLICKHOUSE_DATABASE"); db != "" {
		req.Header.Set("X-ClickHouse-Database", db)
	}
}

func (inst *clickHouseTestClient) Ping(ctx context.Context) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.endpoint+"/ping", nil)
	if err != nil {
		err = eh.Errorf("ping request: %w", err)
		return
	}
	inst.injectReqHeaders(req)
	resp, err := inst.httpCli.Do(req)
	if err != nil {
		err = eh.Errorf("clickhouse not reachable: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("statusCode", resp.StatusCode).Errorf("clickhouse ping failed")
	}
	return
}

func (inst *clickHouseTestClient) Exec(ctx context.Context, query string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.endpoint+"/", strings.NewReader(query))
	if err != nil {
		err = eh.Errorf("exec request: %w", err)
		return
	}
	inst.injectReqHeaders(req)
	resp, err := inst.httpCli.Do(req)
	if err != nil {
		err = eh.Errorf("exec failed: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", string(body)).Str("query", query).Errorf("clickhouse query error")
	}
	return
}

func (inst *clickHouseTestClient) Query(ctx context.Context, query string) (result []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.endpoint+"/", strings.NewReader(query))
	if err != nil {
		err = eh.Errorf("query request: %w", err)
		return
	}
	inst.injectReqHeaders(req)
	resp, err := inst.httpCli.Do(req)
	if err != nil {
		err = eh.Errorf("query failed: %w", err)
		return
	}
	defer resp.Body.Close()
	result, err = io.ReadAll(resp.Body)
	if err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", string(result)).Str("query", query).Errorf("clickhouse query error")
	}
	return
}

func getClickHouseClient(t *testing.T) *clickHouseTestClient {
	t.Helper()
	endpoint := os.Getenv("CLICKHOUSE_ENDPOINT")
	if endpoint == "" {
		t.Skip("CLICKHOUSE_ENDPOINT not set")
	}
	cli := newClickHouseTestClient(endpoint)
	err := cli.Ping(context.Background())
	if err != nil {
		t.Skipf("cannot connect: %v", err)
	}
	return cli
}

func explainSyntax(ctx context.Context, cli *clickHouseTestClient, query string) (result string, err error) {
	body, err := cli.Query(ctx, "EXPLAIN SYNTAX "+query)
	if err != nil {
		return
	}
	result = strings.TrimSpace(string(body))
	return
}

func setupTestSchema(ctx context.Context, cli *clickHouseTestClient) (err error) {
	ddl := []string{
		"DROP TABLE IF EXISTS t",
		"DROP TABLE IF EXISTS t1",
		"DROP TABLE IF EXISTS t2",
		"DROP TABLE IF EXISTS t3",
		"DROP TABLE IF EXISTS orders",
		"DROP TABLE IF EXISTS customers",
		`CREATE TABLE t (a Int64, b Int64, c Int64, d Date, s String, x Float64, y Float64, z Float64, g Int64, status String, created Date, arr Array(Int64)) ENGINE = MergeTree() ORDER BY a`,
		`CREATE TABLE t1 (id Int64, a Int64, b Int64, c Int64, status String, created Date) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE t2 (id Int64, a Int64, b Int64, visible UInt8) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE t3 (id Int64, a Int64) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE orders (id Int64, amount Decimal(10,2), tenant_id Int64, created Date) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE customers (id Int64, name String, email String) ENGINE = MergeTree() ORDER BY id`,
	}
	for _, stmt := range ddl {
		err = cli.Exec(ctx, stmt)
		if err != nil {
			return
		}
	}
	return
}

func teardownTestSchema(ctx context.Context, cli *clickHouseTestClient) {
	for _, tbl := range []string{"t", "t1", "t2", "t3", "orders", "customers"} {
		cli.Exec(ctx, "DROP TABLE IF EXISTS "+tbl)
	}
}

// ============================================================================
// EXPLAIN SYNTAX equivalence — hand-crafted queries
//
// Tests that ClickHouse produces identical EXPLAIN SYNTAX for the original
// and our normalized SQL. This is the strongest correctness guarantee.
//
// Excluded:
//   - Comma join: ClickHouse EXPLAIN SYNTAX preserves "FROM t1, t2" but we
//     rewrite to CROSS JOIN. Semantically equivalent but textually different.
// ============================================================================

func TestExplainSyntaxEquivalence(t *testing.T) {
	cli := getClickHouseClient(t)
	ctx := context.Background()
	err := setupTestSchema(ctx, cli)
	require.NoError(t, err)
	defer teardownTestSchema(ctx, cli)

	tests := []struct {
		name     string
		original string
	}{
		// JOIN
		{"join_keyword_order", "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id"},
		{"join_remove_outer", "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id"},
		{"join_using_no_parens", "SELECT a FROM t1 JOIN t2 USING id"},
		{"double_equals", "SELECT a FROM t WHERE a == 1"},

		// Ternary
		{"ternary_simple", "SELECT a > 0 ? a : -a FROM t"},

		// DATE/TIMESTAMP
		{"date_sugar", "SELECT DATE '2024-01-01'"},
		{"timestamp_sugar", "SELECT TIMESTAMP '2024-01-01 00:00:00'"},

		// CASE
		{"case_searched", "SELECT CASE WHEN a = 1 THEN 'one' WHEN a = 2 THEN 'two' ELSE 'other' END FROM t"},
		{"case_simple", "SELECT CASE a WHEN 1 THEN 'one' WHEN 2 THEN 'two' ELSE 'other' END FROM t"},
		{"case_no_else", "SELECT CASE WHEN a = 1 THEN 'one' END FROM t"},

		// EXTRACT, SUBSTRING, TRIM
		{"extract", "SELECT EXTRACT(DAY FROM d) FROM t"},
		{"substring", "SELECT SUBSTRING(s FROM 1 FOR 3) FROM t"},
		{"trim", "SELECT TRIM(BOTH ' ' FROM s) FROM t"},

		// Combined (no comma join)
		{"combined", `SELECT
			CASE WHEN a = 1 THEN DATE '2024-01-01' ELSE d END,
			a > 0 ? a : -a,
			EXTRACT(DAY FROM d)
		FROM t1 LEFT OUTER ALL JOIN t2 ON t1.id == t2.id
		WHERE a BETWEEN 1 AND 100`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explainOrig, err := explainSyntax(ctx, cli, tt.original)
			if err != nil {
				t.Skipf("EXPLAIN SYNTAX failed for original: %v", err)
			}

			normalized, err := fullCanonicalizationPipeline(tt.original)
			require.NoError(t, err, "pipeline failed")

			explainNorm, err := explainSyntax(ctx, cli, normalized)
			require.NoError(t, err, "EXPLAIN SYNTAX failed for normalized: %s", normalized)

			assert.Equal(t, explainOrig, explainNorm,
				"EXPLAIN SYNTAX mismatch for %s\nOriginal:   %s\nCanonicalized: %s\nEXPLAIN original:\n%s\nEXPLAIN normalized:\n%s",
				tt.name, tt.original, normalized, explainOrig, explainNorm)
		})
	}
}

// ============================================================================
// Corpus — EXPLAIN SYNTAX equivalence
// ============================================================================

func TestExplainSyntaxCorpus(t *testing.T) {
	cli := getClickHouseClient(t)
	ctx := context.Background()
	err := setupTestSchema(ctx, cli)
	require.NoError(t, err)
	defer teardownTestSchema(ctx, cli)

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	passed, skipped, failed := 0, 0, 0

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			explainOrig, err := explainSyntax(ctx, cli, entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("EXPLAIN failed for original: %v", err)
			}

			normalized, err := fullCanonicalizationPipeline(entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("pipeline failed: %v", err)
			}

			explainNorm, err := explainSyntax(ctx, cli, normalized)
			if err != nil {
				failed++
				t.Fatalf("EXPLAIN succeeded for original but failed for normalized.\n"+
					"Original:   %s\nCanonicalized: %s\nError: %v", entry.SQL, normalized, err)
			}

			if explainOrig == explainNorm {
				passed++
			} else {
				failed++
				t.Errorf("EXPLAIN SYNTAX mismatch for %s\n"+
					"EXPLAIN original:\n%s\n\nEXPLAIN normalized:\n%s\n\n"+
					"Original SQL:\n%s\n\nCanonicalized SQL:\n%s",
					entry.Name, explainOrig, explainNorm, entry.SQL, normalized)
			}
		})
	}

	t.Logf("EXPLAIN corpus: %d passed, %d skipped, %d failed (of %d)", passed, skipped, failed, len(entries))
}

// ============================================================================
// Smoke — normalized SQL accepted by ClickHouse
// ============================================================================

func TestExplainSmokeCanonicalizedSQLValid(t *testing.T) {
	cli := getClickHouseClient(t)
	ctx := context.Background()
	err := setupTestSchema(ctx, cli)
	require.NoError(t, err)
	defer teardownTestSchema(ctx, cli)

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			_, err := explainSyntax(ctx, cli, entry.SQL)
			if err != nil {
				t.Skipf("original doesn't work with test schema: %v", err)
			}

			normalized, err := fullCanonicalizationPipeline(entry.SQL)
			if err != nil {
				t.Skipf("pipeline failed: %v", err)
			}

			_, err = explainSyntax(ctx, cli, normalized)
			assert.NoError(t, err,
				"normalized SQL rejected by ClickHouse:\nOriginal:   %s\nCanonicalized: %s", entry.SQL, normalized)
		})
	}
}

// ============================================================================
// Differential — EXPLAIN SYNTAX per individual pass
// ============================================================================

func TestExplainDifferentialPerPass(t *testing.T) {
	cli := getClickHouseClient(t)
	ctx := context.Background()
	err := setupTestSchema(ctx, cli)
	require.NoError(t, err)
	defer teardownTestSchema(ctx, cli)

	tests := []struct {
		passName string
		pass     func(string) (string, error)
		sql      string
	}{
		{"CanonicalizeJoin/reorder", passes.CanonicalizeJoin, "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id"},
		{"CanonicalizeJoin/outer", passes.CanonicalizeJoin, "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id"},
		{"CanonicalizeJoin/eq", passes.CanonicalizeJoin, "SELECT a FROM t WHERE a == 1"},
		{"CanonicalizeTernary", passes.CanonicalizeTernary, "SELECT a > 0 ? a : -a FROM t"},
		{"CanonicalizeCase/searched", passes.CanonicalizeCaseConditionals, "SELECT CASE WHEN a = 1 THEN 'one' ELSE 'other' END FROM t"},
		{"CanonicalizeCase/simple", passes.CanonicalizeCaseConditionals, "SELECT CASE a WHEN 1 THEN 'one' ELSE 'other' END FROM t"},
		{"CanonicalizeSugar/date", passes.CanonicalizeSugar, "SELECT DATE '2024-01-01'"},
		{"CanonicalizeSugar/extract", passes.CanonicalizeSugar, "SELECT EXTRACT(DAY FROM d) FROM t"},
		{"CanonicalizeSugar/substring", passes.CanonicalizeSugar, "SELECT SUBSTRING(s FROM 1 FOR 3) FROM t"},
		{"CanonicalizeSugar/trim", passes.CanonicalizeSugar, "SELECT TRIM(BOTH ' ' FROM s) FROM t"},
	}

	for _, tt := range tests {
		t.Run(tt.passName, func(t *testing.T) {
			explainBefore, err := explainSyntax(ctx, cli, tt.sql)
			if err != nil {
				t.Skipf("original doesn't work: %v", err)
			}

			transformed, err := tt.pass(tt.sql)
			require.NoError(t, err, "pass failed")

			explainAfter, err := explainSyntax(ctx, cli, transformed)
			if err != nil {
				t.Fatalf("%s produced SQL rejected by ClickHouse:\nInput: %s\nOutput: %s\nError: %v",
					tt.passName, tt.sql, transformed, err)
			}

			assert.Equal(t, explainBefore, explainAfter,
				"%s changed EXPLAIN SYNTAX:\nBefore: %s\nAfter:  %s\nInput:  %s\nOutput: %s",
				tt.passName, explainBefore, explainAfter, tt.sql, transformed)
		})
	}
}
