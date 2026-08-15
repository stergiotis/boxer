package recordstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckTableRef(t *testing.T) {
	for _, ok := range []string{"facts", "boxer.facts", "scratch_db.persist_state", "_x.T1"} {
		assert.NoError(t, CheckTableRef(ok), ok)
	}
	for _, bad := range []string{"", "a.b.c", "boxer.", ".facts", "1abc", "my table", "x;DROP", "`quoted`", "a-b"} {
		assert.Error(t, CheckTableRef(bad), "%q must be refused", bad)
	}
}

const body = "\t\"id:id:s:4::0:\" String CODEC(ZSTD(3))\n" +
	") ENGINE = MergeTree()\nORDER BY (\"id:id:s:4::0:\")\n" +
	"SETTINGS allow_suspicious_low_cardinality_types=1"

const qualifiedDDL = "CREATE DATABASE IF NOT EXISTS boxer;\n\n" +
	"CREATE TABLE IF NOT EXISTS boxer.persiststate (\n" + body + "\n"

const bareDDL = "CREATE TABLE IF NOT EXISTS persiststate (\n" + body + "\n"

// TestProvisioningStatements_Baked is the no-override path every generated
// EnsureTable now takes: the script becomes one statement per Exec, the
// prelude without its terminator, the CREATE TABLE without a trailing
// newline.
func TestProvisioningStatements_Baked(t *testing.T) {
	stmts, err := ProvisioningStatements(qualifiedDDL, "boxer.persiststate", "")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"CREATE DATABASE IF NOT EXISTS boxer",
		"CREATE TABLE IF NOT EXISTS boxer.persiststate (\n" + body,
	}, stmts)

	stmts, err = ProvisioningStatements(bareDDL, "persiststate", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"CREATE TABLE IF NOT EXISTS persiststate (\n" + body}, stmts)
}

// TestProvisioningStatements_Retarget covers the four qualification
// combinations of baked × target: the database statement follows the
// target, the header is renamed, the body is untouched.
func TestProvisioningStatements_Retarget(t *testing.T) {
	stmts, err := ProvisioningStatements(qualifiedDDL, "boxer.persiststate", "scratch.persiststate")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"CREATE DATABASE IF NOT EXISTS scratch",
		"CREATE TABLE IF NOT EXISTS scratch.persiststate (\n" + body,
	}, stmts, "qualified → qualified")

	stmts, err = ProvisioningStatements(qualifiedDDL, "boxer.persiststate", "ps_test")
	require.NoError(t, err)
	assert.Equal(t, []string{"CREATE TABLE IF NOT EXISTS ps_test (\n" + body}, stmts,
		"qualified → bare: no database statement, the executor's database is the binding")

	stmts, err = ProvisioningStatements(bareDDL, "persiststate", "scratch.t")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"CREATE DATABASE IF NOT EXISTS scratch",
		"CREATE TABLE IF NOT EXISTS scratch.t (\n" + body,
	}, stmts, "bare → qualified")

	stmts, err = ProvisioningStatements(bareDDL, "persiststate", "t2")
	require.NoError(t, err)
	assert.Equal(t, []string{"CREATE TABLE IF NOT EXISTS t2 (\n" + body}, stmts, "bare → bare")
}

// TestProvisioningStatements_OtherVerbs covers the two other create modes
// the composer can emit.
func TestProvisioningStatements_OtherVerbs(t *testing.T) {
	for _, verb := range []string{"CREATE TABLE ", "CREATE OR REPLACE TABLE "} {
		stmts, err := ProvisioningStatements(verb+"t (\n) ENGINE = Log\n", "t", "u")
		require.NoError(t, err, verb)
		assert.Equal(t, []string{verb + "u (\n) ENGINE = Log"}, stmts, verb)
	}
}

// TestProvisioningStatements_HeaderOnly is the safety property: a table
// whose bare name recurs inside the column block (here "id") is re-pointed
// at the header only.
func TestProvisioningStatements_HeaderOnly(t *testing.T) {
	ddl := "CREATE TABLE IF NOT EXISTS id (\n\t\"id:id:u64:47::0:\" UInt64\n) ENGINE = MergeTree()\nORDER BY (\"id:id:u64:47::0:\")\n"
	stmts, err := ProvisioningStatements(ddl, "id", "other")
	require.NoError(t, err)
	assert.Equal(t, []string{"CREATE TABLE IF NOT EXISTS other (\n\t\"id:id:u64:47::0:\" UInt64\n) ENGINE = MergeTree()\nORDER BY (\"id:id:u64:47::0:\")"}, stmts)
}

// TestProvisioningStatements_Refuses pins the refuse-rather-than-guess
// posture.
func TestProvisioningStatements_Refuses(t *testing.T) {
	_, err := ProvisioningStatements(qualifiedDDL, "boxer.persiststate", "bad name")
	assert.Error(t, err, "malformed target")
	_, err = ProvisioningStatements(bareDDL, "boxer.persiststate", "")
	assert.Error(t, err, "qualified baked reference without a prelude")
	_, err = ProvisioningStatements(qualifiedDDL, "boxer.other", "")
	assert.Error(t, err, "prelude present but the header names another table")
	_, err = ProvisioningStatements("ATTACH TABLE t (\n)", "t", "")
	assert.Error(t, err, "unknown header verb")
	_, err = ProvisioningStatements(bareDDL, "", "")
	assert.Error(t, err, "empty baked reference")
}
