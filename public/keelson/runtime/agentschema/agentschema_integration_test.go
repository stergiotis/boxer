//go:build integration

package agentschema

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Under clickhouse local: the worker-target views scope the journal to the
// pinned run and conversation, agent_history carries no body, and the
// withheld section's laws L11–L13 and the definer-side L18 return what
// they must.
func TestTheWorkerViewsUnderClickhouseLocal(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	for _, stmt := range []string{
		"CREATE TABLE agent_runs (id String, conversation String, owner_app String) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE agent_run_entries (run String, ordinal UInt64, at_ms Int64, kind String, key String, role String, body String, calls String, body_digest String, calls_digest String, name String, book String, form String, target UInt64, `from` UInt64, effect String, turn String, label_level String, label_tier String, label_exposure Array(String), label_origin Array(String), writer_run String, writer_turn String, writer_ordinal UInt64, prev UInt64, digest String, key_id String, parent UInt64, attachments String) ENGINE = MergeTree ORDER BY (run, ordinal)",
		"INSERT INTO agent_runs VALUES ('r1', 'c1', 'chat'), ('r2', 'c1', 'chat'), ('r3', 'c2', 'other')",
		"INSERT INTO agent_run_entries (run, ordinal, kind, body, digest) VALUES ('r1', 1, 'user', 'hello', 'd1'), ('r1', 2, 'reply', 'hi', 'd2'), ('r2', 1, 'user', 'other run', 'd3'), ('r3', 1, 'user', 'other conversation', 'd4')",
		"CREATE TABLE scratch_notes (run String, ord UInt64, note String, contact String, secret String, memberships Array(String)) ENGINE = MergeTree ORDER BY ord",
		"INSERT INTO scratch_notes VALUES ('r1', 1, 'a', 'alice@x', 's', ['personal', 'public']), ('r1', 2, 'b', 'bob', 's', ['public']), ('r2', 3, 'c', 'carol', 's', ['credential'])",
	} {
		require.NoError(t, exec.Exec(ctx, stmt), stmt)
	}
	w := withholding()
	w.Over = "scratch_notes"
	scopes := Scopes{ScopeSetting: "r1", ScopeConversation: "c1", ScopeApp: "chat"}
	for _, r := range []Relation{MyRun(), MyConversation(), AgentHistory(), w} {
		view, verr := WorkerView(r, scopes)
		require.NoError(t, verr)
		require.NoError(t, exec.Exec(ctx, view), view)
	}
	count := func(sql string) (n int64) {
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			require.NoError(t, qerr, sql)
			n += rec.NumRows()
		}
		return
	}
	assert.EqualValues(t, 2, count("SELECT * FROM my_run"), "the pinned run's rows alone")
	assert.EqualValues(t, 1, count("SELECT * FROM my_conversation"), "the conversation's earlier run; the pinned run is my_run's")
	assert.EqualValues(t, 0, count("SELECT * FROM my_conversation WHERE run = 'r1'"))
	assert.EqualValues(t, 3, count("SELECT * FROM agent_history"), "the app's runs")
	assert.EqualValues(t, 0, count("SELECT * FROM my_run WHERE run != 'r1'"))

	// agent_history carries sizes and digests, no body.
	for rec, qerr := range exec.QueryArrow(ctx, "SELECT body_bytes, body_digest FROM agent_history WHERE run = 'r1' AND ordinal = 1") {
		require.NoError(t, qerr)
		require.EqualValues(t, 1, rec.NumRows())
		assert.NotEqual(t, "hello", rec.Column(1).ValueStr(0))
		assert.Equal(t, "5", rec.Column(0).ValueStr(0))
	}
	assert.EqualValues(t, 0, count("SELECT * FROM agent_history WHERE 0 = 1"))

	// Withheld, not absent: the contact of a row carrying a withheld
	// membership is blanked, the membership moves to the withheld section,
	// and the laws return no rows.
	for rec, qerr := range exec.QueryArrow(ctx, "SELECT ord, contact, memberships, withheld_memberships, withheld_policy FROM notes ORDER BY ord") {
		require.NoError(t, qerr)
		require.EqualValues(t, 2, rec.NumRows())
		assert.Equal(t, "", rec.Column(1).ValueStr(0), "the withheld row's contact is blank")
		assert.Equal(t, "bob", rec.Column(1).ValueStr(1))
		assert.Equal(t, `["public"]`, rec.Column(2).ValueStr(0))
		assert.Equal(t, `["personal"]`, rec.Column(3).ValueStr(0))
		assert.Equal(t, `["withhold:notes"]`, rec.Column(4).ValueStr(0))
	}
	l11, err := L11(w, scopes)
	require.NoError(t, err)
	l12, err := L12(w, scopes)
	require.NoError(t, err)
	l13, err := L13(w, scopes, "ord")
	require.NoError(t, err)
	assert.EqualValues(t, 0, count(l11), "L11")
	assert.EqualValues(t, 0, count(l12), "L12")
	assert.EqualValues(t, 0, count(l13), "L13")
	agent, definer, err := L18(MyRun(), scopes, "body")
	require.NoError(t, err)
	assert.Equal(t, rowText(t, exec, agent), rowText(t, exec, definer), "L18: the view and the definer's filtered base agree")
}

func rowText(t *testing.T, exec *chexec.LocalExecutor, sql string) (s string) {
	t.Helper()
	for rec, err := range exec.QueryArrow(context.Background(), sql) {
		require.NoError(t, err, sql)
		for c := 0; c < int(rec.NumCols()); c++ {
			s += rec.Column(c).ValueStr(0) + "|"
		}
	}
	return
}
