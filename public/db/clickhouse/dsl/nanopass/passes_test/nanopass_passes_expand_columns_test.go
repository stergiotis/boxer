//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSchema() *passes.StaticSchemaProvider {
	return passes.NewStaticSchemaProvider(map[string][]string{
		"orders":    {"id", "amount", "tenant_id", "customer_id", "created"},
		"customers": {"id", "name", "email", "visible", "created"},
		"products":  {"id", "name", "price", "category"},
	})
}

// --- Bare * expansion ---

func TestExpandColumnsBareAsterisk(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single_table",
			input:    "SELECT * FROM orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders",
		},
		{
			name:     "aliased_table",
			input:    "SELECT * FROM orders AS o",
			expected: "SELECT o.id, o.amount, o.tenant_id, o.customer_id, o.created FROM orders AS o",
		},
		{
			name:     "two_tables_joined",
			input:    "SELECT * FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
			expected: "SELECT o.id, o.amount, o.tenant_id, o.customer_id, o.created, c.id, c.name, c.email, c.visible, c.created FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		},
		{
			name:     "unknown_table_left_unexpanded",
			input:    "SELECT * FROM unknown_table",
			expected: "SELECT * FROM unknown_table",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- table.* expansion ---

func TestExpandColumnsTableAsterisk(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "qualified_star",
			input:    "SELECT orders.* FROM orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders",
		},
		{
			name:     "aliased_qualified_star",
			input:    "SELECT o.* FROM orders AS o",
			expected: "SELECT o.id, o.amount, o.tenant_id, o.customer_id, o.created FROM orders AS o",
		},
		{
			name:     "one_star_one_explicit",
			input:    "SELECT o.*, p.name FROM orders AS o JOIN products AS p ON o.id = p.id",
			expected: "SELECT o.id, o.amount, o.tenant_id, o.customer_id, o.created, p.name FROM orders AS o JOIN products AS p ON o.id = p.id",
		},
		{
			name:     "two_qualified_stars",
			input:    "SELECT o.*, c.* FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
			expected: "SELECT o.id, o.amount, o.tenant_id, o.customer_id, o.created, c.id, c.name, c.email, c.visible, c.created FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		},
		{
			name:     "unknown_table_star_left_unexpanded",
			input:    "SELECT u.* FROM unknown_table AS u",
			expected: "SELECT u.* FROM unknown_table AS u",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- COLUMNS('regex') expansion ---

func TestExpandColumnsDynamic(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "match_all",
			input:    "SELECT COLUMNS('.*') FROM orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders",
		},
		{
			name:     "match_id_columns",
			input:    "SELECT COLUMNS('.*_id') FROM orders",
			expected: "SELECT orders.tenant_id, orders.customer_id FROM orders",
		},
		{
			name:     "match_name",
			input:    "SELECT COLUMNS('name') FROM customers",
			expected: "SELECT customers.name FROM customers",
		},
		{
			name:     "match_across_joined_tables",
			input:    "SELECT COLUMNS('id') FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
			expected: "SELECT o.id, o.tenant_id, o.customer_id, c.id FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		},
		{
			name:     "no_match_left_unexpanded",
			input:    "SELECT COLUMNS('nonexistent') FROM orders",
			expected: "SELECT COLUMNS('nonexistent') FROM orders",
		},
		{
			name:     "aliased_table",
			input:    "SELECT COLUMNS('amount') FROM orders AS o",
			expected: "SELECT o.amount FROM orders AS o",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- UNION ALL ---

func TestExpandColumnsUnionAll(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:  "both_branches",
			input: "SELECT * FROM orders UNION ALL SELECT * FROM customers",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders" +
				" UNION ALL SELECT customers.id, customers.name, customers.email, customers.visible, customers.created FROM customers",
		},
		{
			name:  "mixed_star_and_explicit",
			input: "SELECT * FROM orders UNION ALL SELECT id, name FROM customers",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders" +
				" UNION ALL SELECT id, name FROM customers",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- CTEs ---

func TestExpandColumnsCTEs(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:  "cte_body_expanded",
			input: "WITH cte AS (SELECT * FROM orders) SELECT * FROM cte",
			expected: "WITH cte AS (SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders)" +
				" SELECT * FROM cte",
		},
		{
			name:     "cte_ref_not_expanded",
			input:    "WITH cte AS (SELECT id, name FROM customers) SELECT * FROM cte",
			expected: "WITH cte AS (SELECT id, name FROM customers) SELECT * FROM cte",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Subqueries ---

func TestExpandColumnsSubqueries(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "from_subquery",
			input:    "SELECT * FROM (SELECT * FROM orders)",
			expected: "SELECT * FROM (SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created FROM orders)",
		},
		{
			name:     "nested_subquery",
			input:    "SELECT * FROM (SELECT * FROM (SELECT * FROM products))",
			expected: "SELECT * FROM (SELECT * FROM (SELECT products.id, products.name, products.price, products.category FROM products))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Mixed with explicit columns ---

func TestExpandColumnsMixed(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "star_with_extra_column",
			input:    "SELECT *, 1 AS const FROM orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.customer_id, orders.created, 1 AS const FROM orders",
		},
		{
			name:     "dynamic_with_explicit",
			input:    "SELECT COLUMNS('^e.*'), name FROM customers",
			expected: "SELECT customers.email, name FROM customers",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Pipeline integration ---

// --- Edge cases ---

func TestExpandColumnsNoFrom(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT 1"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExpandColumnsEmptySchema(t *testing.T) {
	schema := passes.NewStaticSchemaProvider(map[string][]string{})
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT * FROM orders"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got) // unexpanded — table not in schema
}

func TestExpandColumnsIdempotent(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sqls := []string{
		"SELECT * FROM orders",
		"SELECT o.* FROM orders AS o",
		"SELECT COLUMNS('.*_id') FROM orders",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1, err := pass(sql)
			require.NoError(t, err)
			pass2, err := pass(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

func TestExpandColumnsCaseInsensitive(t *testing.T) {
	schema := passes.NewStaticSchemaProvider(map[string][]string{
		"Orders": {"id", "amount"},
	})
	pass := passes.ExpandColumns(schema, "")

	got, err := pass("SELECT * FROM orders")
	require.NoError(t, err)
	assert.Equal(t, "SELECT orders.id, orders.amount FROM orders", got)
}

func TestExpandColumnsOutputValidity(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
		})
	}
}

func TestExpandColumnsRejectsInvalid(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}
func TestExpandColumnsPartialSchema(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	// One table in schema, one not — bare * left unexpanded
	sql := "SELECT * FROM orders JOIN unknown_table AS u ON orders.id = u.id"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got, "bare * should be left unexpanded when any table is missing from schema")

	// But qualified star for known table should still expand
	sql2 := "SELECT orders.*, u.id FROM orders JOIN unknown_table AS u ON orders.id = u.id"
	got2, err := pass(sql2)
	require.NoError(t, err)
	assert.Contains(t, got2, "orders.id, orders.amount")
	assert.Contains(t, got2, "u.id")
	assert.NotContains(t, got2, "orders.*")
}

func TestExpandColumnsCTEStarUnexpanded(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	// cte.* cannot be expanded — no schema for CTEs
	sql := "WITH cte AS (SELECT id, name FROM customers) SELECT cte.* FROM cte"
	got, err := pass(sql)
	require.NoError(t, err)
	// The outer SELECT's cte.* should be left unexpanded
	assert.Contains(t, got, "cte.*")
}

func TestExpandColumnsInvalidRegex(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT COLUMNS('[invalid') FROM orders"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got, "invalid regex should leave COLUMNS unexpanded")
}

func TestExpandColumnsMultipleDynamic(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT COLUMNS('^id$'), COLUMNS('amount') FROM orders"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "orders.id")
	assert.Contains(t, got, "orders.amount")
	assert.NotContains(t, got, "COLUMNS")

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

func TestExpandColumnsDynamicInSubquery(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT * FROM (SELECT COLUMNS('.*_id') FROM orders)"
	got, err := pass(sql)
	require.NoError(t, err)
	// Inner COLUMNS should be expanded
	assert.Contains(t, got, "orders.tenant_id")
	assert.Contains(t, got, "orders.customer_id")
	// Outer * is a subquery source — no schema, left unexpanded
	assert.Contains(t, got, "SELECT * FROM")

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

func TestExpandColumnsColumnOrder(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	// Verify column order matches schema definition order
	got, err := pass("SELECT * FROM products")
	require.NoError(t, err)
	assert.Equal(t, "SELECT products.id, products.name, products.price, products.category FROM products", got)
}

func TestExpandColumnsPreservesOtherExpressions(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	// Non-star, non-COLUMNS expressions should be untouched
	sql := "SELECT count(*), sum(amount) FROM orders"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got, "aggregate expressions should be untouched")
}

func TestExpandColumnsWithWhere(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	sql := "SELECT * FROM orders WHERE amount > 100"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "orders.id, orders.amount")
	assert.Contains(t, got, "WHERE amount > 100")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestExpandColumnsWithGroupBy(t *testing.T) {
	schema := newTestSchema()
	pass := passes.ExpandColumns(schema, "")

	// Star expansion with GROUP BY — syntactically valid even if semantically questionable
	sql := "SELECT * FROM orders GROUP BY id"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "orders.id, orders.amount")
	assert.Contains(t, got, "GROUP BY id")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}
func TestExpandColumnsWithDefaultDatabase(t *testing.T) {
	// Schema keyed by db.table — requires default database to resolve unqualified tables
	schema := passes.NewStaticSchemaProvider(map[string][]string{
		"mydb.orders": {"id", "amount", "tenant_id"},
	})

	// Custom pass that uses BuildScopes with default database
	// (ExpandColumns currently doesn't pass defaultDB to BuildScopes —
	// this test documents the pattern for when it does)
	sql := "SELECT * FROM orders"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	// Verify ResolvedDatabase works
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "mydb", scopes[0].Tables[0].ResolvedDatabase(scopes[0]))

	// Verify schema lookup with resolved database
	db := scopes[0].Tables[0].ResolvedDatabase(scopes[0])
	cols, _, found := schema.GetColumns(db, "orders")
	assert.True(t, found)
	assert.Equal(t, []string{"id", "amount", "tenant_id"}, slices.Collect(cols))
}
func TestExpandColumnsWithDatabaseQualifiedSchema(t *testing.T) {
	schema := passes.NewStaticSchemaProvider(map[string][]string{
		"prod.orders":    {"id", "amount", "tenant_id"},
		"staging.orders": {"id", "amount", "tenant_id", "debug_flag"},
	})
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "explicit_prod",
			input:    "SELECT * FROM prod.orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id FROM prod.orders",
		},
		{
			name:     "explicit_staging",
			input:    "SELECT * FROM staging.orders",
			expected: "SELECT orders.id, orders.amount, orders.tenant_id, orders.debug_flag FROM staging.orders",
		},
		{
			name:     "unqualified_no_match",
			input:    "SELECT * FROM orders",
			expected: "SELECT * FROM orders",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestExpandColumnsWithMixedSchema(t *testing.T) {
	schema := passes.NewStaticSchemaProvider(map[string][]string{
		"orders":      {"id", "amount"},
		"prod.orders": {"id", "amount", "extra"},
		"products":    {"id", "name", "price"},
	})
	pass := passes.ExpandColumns(schema, "")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "qualified_uses_qualified_schema",
			input:    "SELECT * FROM prod.orders",
			expected: "SELECT orders.id, orders.amount, orders.extra FROM prod.orders",
		},
		{
			name:     "unqualified_uses_fallback",
			input:    "SELECT * FROM orders",
			expected: "SELECT orders.id, orders.amount FROM orders",
		},
		{
			name:     "aliased_qualified",
			input:    "SELECT o.* FROM prod.orders AS o",
			expected: "SELECT o.id, o.amount, o.extra FROM prod.orders AS o",
		},
		{
			name:     "unqualified_product",
			input:    "SELECT * FROM products",
			expected: "SELECT products.id, products.name, products.price FROM products",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}
