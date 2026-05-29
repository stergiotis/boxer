//go:build llm_generated_opus47

package ddl_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
)

func TestComposeCreateTableSql_EmitsHeaderAndFooter(t *testing.T) {
	sql, err := ddl.ComposeCreateTableSql("MergeTree() PARTITION BY toYYYYMM(ts) ORDER BY (id)")
	require.NoError(t, err)
	assert.Contains(t, sql, "CREATE DATABASE IF NOT EXISTS runtime;")
	assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS runtime.facts")
	assert.Contains(t, sql, "MergeTree()")
	assert.Contains(t, sql, "SETTINGS allow_suspicious_low_cardinality_types=1;")
}

func TestComposeCreateTableSql_EmptyEngineRejected(t *testing.T) {
	_, err := ddl.ComposeCreateTableSql("")
	require.Error(t, err)
}

func TestComposeCreateTableSql_MentionsPlainValueColumns(t *testing.T) {
	sql, err := ddl.ComposeCreateTableSql("MergeTree() ORDER BY (id)")
	require.NoError(t, err)
	// Physical column names are name-encoded by the convention, but the
	// plain-value names appear somewhere in the encoded form. Case-insensitive
	// substring check is forgiving against the exact convention separator.
	lower := strings.ToLower(sql)
	for _, name := range []string{"id", "naturalkey", "ts", "expiresat"} {
		assert.Contains(t, lower, name, "expected column name fragment %q in DDL", name)
	}
}

func TestComposeCreateTableSql_MentionsAllSectionTypes(t *testing.T) {
	sql, err := ddl.ComposeCreateTableSql("MergeTree() ORDER BY (id)")
	require.NoError(t, err)
	lower := strings.ToLower(sql)
	// Per ADR-0026 §SD6: data sections span text/string/symbol/blob, u8-u64,
	// i8-i64, f32/f64, time, bool, u32Range; plus the foreignKey relation
	// section.
	for _, sec := range []string{
		"text", "string", "symbol", "blob",
		"u8", "u16", "u32", "u64",
		"i8", "i16", "i32", "i64",
		"f32", "f64",
		"time", "bool",
		"foreignkey",
	} {
		assert.Contains(t, lower, sec, "expected section %q in DDL", sec)
	}
}
