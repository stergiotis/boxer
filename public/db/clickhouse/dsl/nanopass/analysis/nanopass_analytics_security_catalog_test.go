//go:build integration

package analysis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
)

// catalog asks clickhouse-local for one column of names.
func catalog(t *testing.T, query string) (names []string) {
	t.Helper()
	if _, ok := extbin.ClickHouseLocal.Resolve(); !ok {
		t.Skip("no clickhouse binary to read the server's catalog from")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := extbin.ClickHouseLocal.Output(ctx, extbin.Opts{}, "--query", query+" FORMAT TSV")
	require.NoError(t, err)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	require.NotEmpty(t, names, query)
	return
}

// ADR-0132 §SD5: every table function the server lists is classified, so an
// upgrade that adds one fails here instead of passing as "unknown".
func TestSecurityVocabularyCoversTheServersTableFunctions(t *testing.T) {
	var unclassified []string
	for _, name := range catalog(t, "SELECT name FROM system.table_functions ORDER BY name") {
		if _, known := lookupTableFunction(name); !known {
			unclassified = append(unclassified, name)
		}
	}
	assert.Empty(t, unclassified, "classify these in tableFunctionSpellings")
}

// reachMarkers are the phrases with which the server describes a function
// that reaches beyond the query's data or keeps state. A tripwire, not a
// proof: a reaching function described otherwise passes it.
const reachMarkers = `(?i)(LLM provider|AI provider|embedding provider|embedding endpoint|reads a file|zookeeper|keeper|external (catboost )?model|named collection|remote server|sends the)`

// reviewedScalars match reachMarkers and do not reach beyond the query's own
// data, each with the reason.
var reviewedScalars = map[string]string{
	"globalIn":                 "ships a set to the shards of the query's own distributed table",
	"globalInIgnoreSet":        "as globalIn",
	"globalNotIn":              "as globalIn",
	"globalNotInIgnoreSet":     "as globalIn",
	"globalNotNullIn":          "as globalIn",
	"globalNotNullInIgnoreSet": "as globalIn",
	"globalNullIn":             "as globalIn",
	"globalNullInIgnoreSet":    "as globalIn",
	"hostName":                 "names the server that evaluates it",
	"zookeeperSessionUptime":   "reports the age of the server's Keeper session",
}

// A scalar the server describes as reaching out is on one of the lists, or
// reviewed; a new one fails here at the upgrade that brings it.
func TestSecurityVocabularyCoversReachingScalars(t *testing.T) {
	var unreviewed []string
	for _, name := range catalog(t, "SELECT name FROM system.functions WHERE origin = 'System' AND alias_to = '' AND match(description, '"+reachMarkers+"') ORDER BY name") {
		lname := strings.ToLower(name)
		_, egress := egressScalarFunctions[lname]
		_, changes := stateChangingScalarFunctions[lname]
		_, reviewed := reviewedScalars[name]
		if !egress && !changes && !reviewed {
			unreviewed = append(unreviewed, name)
		}
	}
	assert.Empty(t, unreviewed, "classify these in egressScalarSpellings or stateChangingScalarSpellings, or review them")
}
