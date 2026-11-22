package schema

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/schemaplan"
)

func TestContractsNameEveryStoredTableOnce(t *testing.T) {
	cs, err := Contracts()
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, c := range cs {
		require.NotEmpty(t, c.Columns, c.Table)
		require.False(t, seen[c.Table], "listed twice: %s", c.Table)
		seen[c.Table] = true
	}
	assert.True(t, seen["boxer.persiststate"])
	assert.GreaterOrEqual(t, len(cs), 10)
}

func TestPrintWritesRunnableSQL(t *testing.T) {
	var b bytes.Buffer
	Print(&b, []schemaplan.Plan{
		{Verdict: recordstore.SchemaVerdict{Table: "db.t", Verdict: recordstore.SchemaVerdictMigratable, Missing: []string{"c"}}, Statements: []string{"ALTER TABLE `db`.`t` ADD COLUMN IF NOT EXISTS `c` UInt8 AFTER `b`"}},
		{Verdict: recordstore.SchemaVerdict{Table: "db.u", Verdict: recordstore.SchemaVerdictIncompatible}, Refused: []string{"column x is UInt16 on the table, UInt8 generated"}},
	})
	assert.Equal(t, "-- db.t: migratable — 1 column(s) missing (c); `boxer schema plan` lists the statements\n"+
		"ALTER TABLE `db`.`t` ADD COLUMN IF NOT EXISTS `c` UInt8 AFTER `b`;\n"+
		"-- db.u: incompatible; see doc/migration/\n"+
		"--   refused: column x is UInt16 on the table, UInt8 generated\n", b.String())
}

func TestTablesGiveEveryStoredTableAnOwner(t *testing.T) {
	ts, err := Tables()
	require.NoError(t, err)
	owners := map[string]int{}
	for _, st := range ts {
		owners[st.Owner]++
	}
	for _, o := range []string{"facts", "persist", "agentrun", "watchbill", "lading"} {
		assert.Positive(t, owners[o], o)
	}
}

func TestSelectTables(t *testing.T) {
	all := []recordstore.StoredTable{
		{Owner: "a", Contract: recordstore.TableContract{Table: "db.one"}},
		{Owner: "a", Contract: recordstore.TableContract{Table: "db.two"}},
		{Owner: "b", Contract: recordstore.TableContract{Table: "db.three"}},
	}
	names := func(ts []recordstore.StoredTable) (out []string) {
		for _, st := range ts {
			out = append(out, st.Contract.Table)
		}
		return
	}
	ts, err := selectTables(all, nil, nil)
	require.NoError(t, err)
	assert.Len(t, ts, 3)
	ts, err = selectTables(all, nil, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"db.one", "db.two"}, names(ts))
	ts, err = selectTables(all, []string{"db.two", "db.three"}, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"db.two"}, names(ts))
	_, err = selectTables(all, nil, []string{"c"})
	assert.ErrorContains(t, err, "owner")
	_, err = selectTables(all, []string{"db.none"}, nil)
	assert.ErrorContains(t, err, "name")
	_, err = selectTables(all, []string{"db.three"}, []string{"a"})
	assert.ErrorContains(t, err, "no stored table")
}

func TestTheWorstOutcomeDecidesTheExitCode(t *testing.T) {
	ok := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.ok", Verdict: recordstore.SchemaVerdictOk}}
	pending := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.new", Verdict: recordstore.SchemaVerdictAbsent}, Creates: []string{"CREATE TABLE IF NOT EXISTS db.new (a UInt8) ENGINE = MergeTree ORDER BY a"}}
	blocked := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.bad", Verdict: recordstore.SchemaVerdictIncompatible}, Refused: []string{"r"}}
	unchecked := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.gone", Verdict: recordstore.SchemaVerdictUnchecked}, Refused: []string{"r"}}

	assert.Zero(t, ExitCode([]schemaplan.Plan{ok}))
	assert.Equal(t, ExitPending, ExitCode([]schemaplan.Plan{ok, pending}))
	assert.Equal(t, ExitBlocked, ExitCode([]schemaplan.Plan{pending, blocked}))
	assert.Equal(t, ExitUnchecked, ExitCode([]schemaplan.Plan{blocked, unchecked, pending}))
	assert.NoError(t, exitFor([]schemaplan.Plan{ok}))
	assert.ErrorContains(t, exitFor([]schemaplan.Plan{ok, pending, blocked}), "db.new (pending), db.bad (blocked)")

	r := NewReport([]schemaplan.Plan{ok, pending}, []Applied{{Table: "db.ok", Altered: 1}})
	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.JSONEq(t, `{"schema":"boxer.schema-report/1","outcome":"pending","applied":[{"table":"db.ok","altered":1}],"tables":[
		{"outcome":"in-sync","verdict":{"kind":"","table":"db.ok","verdict":"ok","age":"unstamped","dedupWindow":false}},
		{"outcome":"pending","verdict":{"kind":"","table":"db.new","verdict":"absent","age":"unstamped","dedupWindow":false},"creates":["CREATE TABLE IF NOT EXISTS db.new (a UInt8) ENGINE = MergeTree ORDER BY a"]}]}`, string(b))

	var out bytes.Buffer
	Print(&out, []schemaplan.Plan{pending})
	assert.Equal(t, "-- db.new: absent\nCREATE TABLE IF NOT EXISTS db.new (a UInt8) ENGINE = MergeTree ORDER BY a;\n", out.String())
}

// `check --create --alter` answers whether an apply of those kinds would end
// in line: a pending table is accepted when every kind of statement it
// holds is named, a blocked or unreadable one never.
func TestCheckAcceptsWhatTheNamedKindsWouldBringInLine(t *testing.T) {
	ok := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.ok", Verdict: recordstore.SchemaVerdictOk}}
	absent := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.new", Verdict: recordstore.SchemaVerdictAbsent}, Creates: []string{"CREATE"}}
	older := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.old", Verdict: recordstore.SchemaVerdictMigratable}, Statements: []string{"ALTER"}}
	blocked := schemaplan.Plan{Verdict: recordstore.SchemaVerdict{Table: "db.bad", Verdict: recordstore.SchemaVerdictIncompatible}, Refused: []string{"r"}}
	all := []schemaplan.Plan{ok, absent, older, blocked}
	names := func(ps []schemaplan.Plan) (out []string) {
		for _, p := range ps {
			out = append(out, p.Verdict.Table)
		}
		return
	}
	assert.Equal(t, []string{"db.new", "db.old", "db.bad"}, names(Unaccepted(all, false, false)))
	assert.Equal(t, []string{"db.old", "db.bad"}, names(Unaccepted(all, true, false)))
	assert.Equal(t, []string{"db.new", "db.bad"}, names(Unaccepted(all, false, true)))
	assert.Equal(t, []string{"db.bad"}, names(Unaccepted(all, true, true)))
	assert.Zero(t, ExitCode(Unaccepted([]schemaplan.Plan{ok, absent}, true, false)))
	assert.Equal(t, ExitPending, ExitCode(Unaccepted([]schemaplan.Plan{ok, absent, older}, true, false)))
}
