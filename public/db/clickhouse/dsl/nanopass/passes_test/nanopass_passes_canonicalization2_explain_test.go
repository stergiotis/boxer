//go:build integration

package passes_test

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-integration env vars for the ClickHouse EXPLAIN SYNTAX suite.
// Names match the ClickHouse client tooling convention (not BOXER_*-
// prefixed) because they are shared with downstream repositories and external
// tooling.
var (
	clickhouseUser = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_USER",
		Default:     "default",
		Description: "ClickHouse user for EXPLAIN SYNTAX integration tests",
		Category:    env.CategoryTestIntegration,
	})

	clickhousePassword = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_PASSWORD",
		Description: "ClickHouse password for EXPLAIN SYNTAX integration tests; omits header when empty",
		Category:    env.CategoryTestIntegration,
		Sensitive:   true,
	})

	clickhouseDatabase = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_DATABASE",
		Description: "ClickHouse database for EXPLAIN SYNTAX integration tests; omits header when empty",
		Category:    env.CategoryTestIntegration,
	})

	clickhouseEndpoint = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_ENDPOINT",
		Description: "ClickHouse HTTP endpoint; tests are skipped when empty",
		Category:    env.CategoryTestIntegration,
	})
)

// ============================================================================
// EXPLAIN SYNTAX Integration Tests
//
// Require CLICKHOUSE_ENDPOINT env var (e.g. "http://localhost:8123").
// Optional: CLICKHOUSE_USER, CLICKHOUSE_PASSWORD, CLICKHOUSE_DATABASE.
//
//   CLICKHOUSE_ENDPOINT="http://localhost:8123" go test -run TestExplain -v -count=1
//
// The canonical forms whose EXPLAIN SYNTAX differs from the original's, and
// how they are compared instead, are under assertExplainEquivalent.
// ============================================================================

type clickHouseTestClient struct {
	endpoint string
	httpCli  *http.Client
}

// newClickHouseTestClient takes the endpoint with or without a trailing slash
// (the registry's example has none, CLICKHOUSE_URL's default has one); the
// paths below are appended to it without one.
func newClickHouseTestClient(endpoint string) *clickHouseTestClient {
	return &clickHouseTestClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		httpCli:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (inst *clickHouseTestClient) injectReqHeaders(req *http.Request) {
	req.Header.Set("X-ClickHouse-User", clickhouseUser.Get())
	pw := clickhousePassword.Get()
	if pw != "" {
		req.Header.Set("X-ClickHouse-Key", pw)
	}
	db := clickhouseDatabase.Get()
	if db != "" {
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
	endpoint := clickhouseEndpoint.Get()
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

// explainQueryTree returns the analyzer's query tree after its passes, in the
// form normalizeQueryTree compares.
func explainQueryTree(ctx context.Context, cli *clickHouseTestClient, query string) (result string, err error) {
	body, err := cli.Query(ctx, "EXPLAIN QUERY TREE run_passes = 1 "+query)
	if err != nil {
		return
	}
	result = normalizeQueryTree(string(body))
	return
}

// assertExplainEquivalent checks that ClickHouse reads original and
// canonicalized as the same query, in two steps.
//
// The first is the EXPLAIN SYNTAX texts, compared as they are. Under the
// analyzer, EXPLAIN SYNTAX prints the parsed query with literals typed and
// runs none of its rewrites, so four canonical forms the pipeline writes on
// purpose never compare equal there (seen on 26.8 and 26.9 alike):
//
//   - a one-branch CASE as if(); the analyzer's optimize_multiif_to_if pass
//     makes the same if() from the original's multiIf().
//   - an array or tuple literal as an array() or tuple() call; the literal
//     prints as `_CAST(literal, 'T')`, the call as a call.
//   - an IN list `x IN (1, 2)` as `x IN array(1, 2)`
//     (CanonicalizeConstructors).
//   - a comma join as CROSS JOIN; both become the same CROSS JOIN node.
//
// With enable_analyzer = 0, EXPLAIN SYNTAX still runs the first and the last
// rewrite itself, which is why this suite once compared the texts alone.
//
// When the texts differ, the second step compares the query trees after the
// analyzer's passes, under normalizeQueryTree, which agree for all four forms,
// and the explicit aliases (`AS name`) in the two EXPLAIN SYNTAX texts, in
// order. The step forgives only what normalizeQueryTree removes; among it are
// the names of unaliased result columns, which follow the expression's
// spelling, so respelling an expression renames its column.
func assertExplainEquivalent(t *testing.T, ctx context.Context, cli *clickHouseTestClient, original string, canonicalized string) {
	t.Helper()
	equivalent, detail, err := explainEquivalent(ctx, cli, original, canonicalized)
	require.NoError(t, err, "Original:      %s\nCanonicalized: %s", original, canonicalized)
	assert.True(t, equivalent, "%s", detail)
}

// explainEquivalent is assertExplainEquivalent's comparison. detail describes
// a difference; err is a query the server refused.
func explainEquivalent(ctx context.Context, cli *clickHouseTestClient, original string, canonicalized string) (equivalent bool, detail string, err error) {
	syntaxOrig, err := explainSyntax(ctx, cli, original)
	if err != nil {
		err = eh.Errorf("EXPLAIN SYNTAX of the original: %w", err)
		return
	}
	syntaxCanon, err := explainSyntax(ctx, cli, canonicalized)
	if err != nil {
		err = eh.Errorf("EXPLAIN SYNTAX of the canonicalized SQL: %w", err)
		return
	}
	if syntaxOrig == syntaxCanon {
		equivalent = true
		return
	}
	detail = "Original:      " + original + "\nCanonicalized: " + canonicalized +
		"\nEXPLAIN SYNTAX original:\n" + syntaxOrig + "\nEXPLAIN SYNTAX canonicalized:\n" + syntaxCanon
	aliasesOrig := strings.Join(syntaxAliasRe.FindAllString(syntaxOrig, -1), ", ")
	aliasesCanon := strings.Join(syntaxAliasRe.FindAllString(syntaxCanon, -1), ", ")
	if aliasesOrig != aliasesCanon {
		detail += "\nthe explicit aliases differ: [" + aliasesOrig + "] against [" + aliasesCanon + "]"
		return
	}
	treeOrig, err := explainQueryTree(ctx, cli, original)
	if err != nil {
		err = eb.Build().Str("detail", detail).Errorf("EXPLAIN SYNTAX differs and EXPLAIN QUERY TREE of the original failed: %w", err)
		return
	}
	treeCanon, err := explainQueryTree(ctx, cli, canonicalized)
	if err != nil {
		err = eh.Errorf("EXPLAIN QUERY TREE of the canonicalized SQL: %w", err)
		return
	}
	equivalent = treeOrig == treeCanon
	if !equivalent {
		detail += "\nEXPLAIN QUERY TREE (normalized) original:\n" + treeOrig +
			"\nEXPLAIN QUERY TREE (normalized) canonicalized:\n" + treeCanon
	}
	return
}

// syntaxAliasRe finds an explicit alias in EXPLAIN SYNTAX output: AS followed
// by a bare or a backquoted name.
var syntaxAliasRe = regexp.MustCompile("\\bAS (`(?:[^`\\\\]|\\\\.)*`|[A-Za-z_][A-Za-z0-9_]*)")

var queryTreeIdRe = regexp.MustCompile(`\b(id|source_id): (\d+)`)

// normalizeQueryTree removes from an EXPLAIN QUERY TREE dump what a
// canonical respelling changes without changing the query:
//
//   - PROJECTION COLUMNS blocks: the result columns' names and types. The
//     names of unaliased expressions follow their spelling; the types stay
//     checked through each projection node's result_type.
//   - the EXPRESSION subtree under a CONSTANT: the expression a constant was
//     folded from, which is the only trace left of `array(1, 2)` against
//     `[1, 2]`.
//   - node ids, renumbered in order of appearance, since the removed
//     subtrees took ids.
//   - the right operand of an IN-family function written as a tuple whose
//     elements share one type: rewritten as the array of the same elements.
//     The pipeline writes `x IN (1, 2)` as `x IN array(1, 2)`; the set the
//     server builds is the same when the elements have one type, and the
//     rewrite is left out when they do not, so a mixed-type list still
//     compares unequal.
func normalizeQueryTree(dump string) (result string) {
	type inArgs struct {
		listIndent int // indent of the LIST under an IN-family function's ARGUMENTS
		argIndex   int
	}
	lines := strings.Split(strings.TrimRight(dump, "\n"), "\n")
	kept := make([]string, 0, len(lines))
	skipDeeperThan := -1
	var open []inArgs
	argsListAt := -1
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		body := strings.TrimLeft(line, " ")
		indent := len(line) - len(body)
		if skipDeeperThan >= 0 {
			if indent > skipDeeperThan {
				continue
			}
			skipDeeperThan = -1
		}
		switch {
		case body == "PROJECTION COLUMNS":
			skipDeeperThan = indent
			continue
		case body == "EXPRESSION" && len(kept) > 0 && strings.HasPrefix(strings.TrimLeft(kept[len(kept)-1], " "), "CONSTANT "):
			skipDeeperThan = indent
			continue
		}
		for len(open) > 0 && indent <= open[len(open)-1].listIndent {
			open = open[:len(open)-1]
		}
		if len(open) > 0 && indent == open[len(open)-1].listIndent+2 {
			top := &open[len(open)-1]
			top.argIndex++
			if top.argIndex == 2 && strings.HasPrefix(body, "CONSTANT ") {
				line = strings.Repeat(" ", indent) + inListTupleAsArray(body)
			}
		}
		if i == argsListAt {
			open = append(open, inArgs{listIndent: indent})
		}
		if strings.HasPrefix(body, "FUNCTION ") && isInFamilyFunction(body) && i+2 < len(lines) &&
			strings.TrimSpace(lines[i+1]) == "ARGUMENTS" && strings.HasPrefix(strings.TrimSpace(lines[i+2]), "LIST ") {
			argsListAt = i + 2
		}
		kept = append(kept, line)
	}
	ids := map[string]string{}
	result = queryTreeIdRe.ReplaceAllStringFunc(strings.Join(kept, "\n"), func(m string) string {
		sub := queryTreeIdRe.FindStringSubmatch(m)
		n, ok := ids[sub[2]]
		if !ok {
			n = strconv.Itoa(len(ids))
			ids[sub[2]] = n
		}
		return sub[1] + ": " + n
	})
	return
}

func isInFamilyFunction(functionLine string) bool {
	for _, name := range []string{"in", "notIn", "globalIn", "globalNotIn", "nullIn", "notNullIn", "globalNullIn", "globalNotNullIn"} {
		if strings.Contains(functionLine, ", function_name: "+name+",") {
			return true
		}
	}
	return false
}

// inListTupleAsArray rewrites `CONSTANT id: N, constant_value: Tuple_(v1, v2),
// constant_value_type: Tuple(T, T)` as the array constant
// `Array_[v1, v2]` of type `Array(T)`, when every element type is the same T.
// Any other constant is returned as it is.
func inListTupleAsArray(constantLine string) string {
	const valueKey, typeKey = "constant_value: Tuple_(", ", constant_value_type: Tuple("
	vi := strings.Index(constantLine, valueKey)
	ti := strings.LastIndex(constantLine, typeKey)
	if vi < 0 || ti < vi || !strings.HasSuffix(constantLine, ")") {
		return constantLine
	}
	value := constantLine[vi+len(valueKey) : ti]
	if !strings.HasSuffix(value, ")") {
		return constantLine
	}
	value = strings.TrimSuffix(value, ")")
	elemTypes := splitTopLevel(constantLine[ti+len(typeKey) : len(constantLine)-1])
	for _, et := range elemTypes[1:] {
		if et != elemTypes[0] {
			return constantLine
		}
	}
	return constantLine[:vi] + "constant_value: Array_[" + value + "], constant_value_type: Array(" + elemTypes[0] + ")"
}

// splitTopLevel splits a type list on the commas outside parentheses.
func splitTopLevel(s string) (parts []string) {
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return
}

func setupTestSchema(ctx context.Context, cli *clickHouseTestClient) (err error) {
	teardownTestSchema(ctx, cli)
	ddl := []string{
		`CREATE TABLE t (a Int64, b Int64, c Int64, d Date, s String, x Float64, y Float64, z Float64, g Int64, status String, created Date, arr Array(Int64)) ENGINE = MergeTree() ORDER BY a`,
		`CREATE TABLE t1 (id Int64, a Int64, b Int64, c Int64, status String, created Date) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE t2 (id Int64, a Int64, b Int64, visible UInt8) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE t3 (id Int64, a Int64) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE orders (id Int64, amount Decimal(10,2), tenant_id Int64, created Date) ENGINE = MergeTree() ORDER BY id`,
		`CREATE TABLE customers (id Int64, name String, email String) ENGINE = MergeTree() ORDER BY id`,
		// The corpus's larger queries read these. EXPLAIN SYNTAX takes a
		// query over unknown tables, EXPLAIN QUERY TREE does not.
		`CREATE TABLE order_events (event_created DateTime, event_payload String, event_type String) ENGINE = MergeTree() ORDER BY event_created`,
		`CREATE TABLE fact_sales (location_id Int64, product_name String, order_date Date, sales_amount Float64) ENGINE = MergeTree() ORDER BY location_id`,
		`CREATE TABLE dim_locations (location_id Int64, city String) ENGINE = MergeTree() ORDER BY location_id`,
		`CREATE TABLE dim_products (name String, subcategory String, from_date Date, to_date Date) ENGINE = MergeTree() ORDER BY name`,
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
	for _, tbl := range []string{"t", "t1", "t2", "t3", "orders", "customers", "order_events", "fact_sales", "dim_locations", "dim_products"} {
		cli.Exec(ctx, "DROP TABLE IF EXISTS "+tbl)
	}
}

// ============================================================================
// EXPLAIN SYNTAX equivalence — hand-crafted queries
//
// Tests that ClickHouse reads the original and the canonicalized SQL as the
// same query (assertExplainEquivalent).
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

		// Comma join
		{"comma_join", "SELECT t1.a, t2.b FROM t1, t2 WHERE t1.id = t2.id"},

		// Constructors and IN lists
		{"array_literal", "SELECT [1, 2, 3]"},
		{"tuple_literal", "SELECT (1, 'x')"},
		{"in_list", "SELECT a FROM t WHERE a IN (1, 2, 3)"},

		// Combined. The join is spelled so that both ClickHouse and the
		// grammar take it (ClickHouse rejects LEFT OUTER ALL JOIN, the
		// grammar LEFT ALL OUTER JOIN), and every column is one t1 has,
		// since the second step of assertExplainEquivalent resolves names.
		{"combined", `SELECT
			CASE WHEN t1.a = 1 THEN DATE '2024-01-01' ELSE t1.created END,
			t1.a > 0 ? t1.a : -t1.a,
			EXTRACT(DAY FROM t1.created)
		FROM t1 ALL LEFT OUTER JOIN t2 ON t1.id == t2.id
		WHERE t1.a BETWEEN 1 AND 100`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := fullCanonicalizationPipeline(tt.original)
			require.NoError(t, err, "pipeline failed")
			assertExplainEquivalent(t, ctx, cli, tt.original, normalized)
		})
	}
}

// TestExplainEquivalenceRejects holds explainEquivalent to what it must not
// forgive: each pair differs in meaning, or in an alias, next to one of the
// respellings the second step accepts.
func TestExplainEquivalenceRejects(t *testing.T) {
	cli := getClickHouseClient(t)
	ctx := context.Background()
	err := setupTestSchema(ctx, cli)
	require.NoError(t, err)
	defer teardownTestSchema(ctx, cli)

	tests := []struct {
		name string
		a, b string
	}{
		{"in_list_element", "SELECT a FROM t WHERE a IN (1, 2, 3)", "SELECT a FROM t WHERE a IN array(1, 2, 4)"},
		{"in_list_mixed_types", "SELECT a FROM t WHERE a IN (1, 2.5)", "SELECT a FROM t WHERE a IN array(1, 2.5)"},
		{"array_element", "SELECT [1, 2, 3]", "SELECT array(1, 2, 4)"},
		{"case_condition", "SELECT CASE WHEN a = 1 THEN 'one' END FROM t", "SELECT if(a = 2, 'one', NULL) FROM t"},
		{"comma_join_condition", "SELECT t1.a FROM t1, t2", "SELECT t1.a FROM t1 CROSS JOIN t2 WHERE t1.id = t2.id"},
		{"alias", "SELECT [1, 2] AS x FROM t", "SELECT array(1, 2) AS y FROM t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equivalent, _, err := explainEquivalent(ctx, cli, tt.a, tt.b)
			require.NoError(t, err)
			assert.False(t, equivalent, "%s\nagainst\n%s", tt.a, tt.b)
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
			_, err := explainSyntax(ctx, cli, entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("EXPLAIN failed for original: %v", err)
			}

			normalized, err := fullCanonicalizationPipeline(entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("pipeline failed: %v", err)
			}

			defer func() {
				if t.Failed() {
					failed++
				} else {
					passed++
				}
			}()
			assertExplainEquivalent(t, ctx, cli, entry.SQL, normalized)
		})
	}

	t.Logf("EXPLAIN corpus: %d passed, %d skipped, %d failed (of %d)", passed, skipped, failed, len(entries))
	// Skips pass, so a regression that rejected every corpus entry would pass
	// too. The ceiling is the count when it was pinned; it only goes down.
	// The one skip is 053_set_param_slots, a SET followed by a SELECT, which
	// EXPLAIN SYNTAX does not take as one statement (counted with clickhouse
	// local 26.9 over the schema setupTestSchema creates).
	const maxSkipped = 1
	if skipped > maxSkipped {
		t.Errorf("%d corpus entries skipped, above the pinned %d: a skip here is an entry the server could not EXPLAIN as written, or one the pipeline rejected", skipped, maxSkipped)
	}
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
		pass     nanopass.Pass
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
			transformed, err := tt.pass.Run(tt.sql)
			require.NoError(t, err, "pass failed")
			assertExplainEquivalent(t, ctx, cli, tt.sql, transformed)
		})
	}
}
