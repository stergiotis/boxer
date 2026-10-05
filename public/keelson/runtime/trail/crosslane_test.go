package trail_test

import (
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Over clickhouse-local: a model call, the agent action its reply asked for
// and the query run that action caused are three rows written by three
// lanes — two through the recorder, one hand-written by the query-run
// capture — and they join by key alone (ADR-0277, Consequences): call to
// action through Cause, action to query run through Delegation, each to the
// run through Origin. No predicate names a time.
//
// The query-run row was written by code that composes no trail component;
// it reads back as Origin and Delegation because it carries their slots.
func TestRowsOfThreeLanesJoinByKey(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}
	const (
		run, chat, play = "run-7", "apps/chat", "apps/play"
		conversation    = "chat-1"
		turn            = "turn-1"
		modelCall       = "llm-1"
		task            = "task-ab"
		call            = "task-ab-3"
	)
	rec := trail.NewRecorder(exec, run, zerolog.Nop())
	defer rec.Close()
	at := time.Unix(1700000000, 0).UTC()
	conv := option.Some(trail.Conversation{Conversation: conversation, Turn: option.Some(turn), Round: option.Some(uint32(0))})
	deleg := option.Some(trail.Delegation{Task: task, Epoch: 1, Call: option.Some(call)})

	// The model call, from the chat window.
	require.NoError(t, rec.LlmCall(at, trail.Context{Origin: rec.OriginOf(chat, 4), Conversation: conv},
		trail.LlmCall{CallId: modelCall, Purpose: "chat/turn", ToolCalls: 1}))
	// The action its reply asked for, in the play window 9.
	require.NoError(t, rec.AgentAction(at.Add(time.Second), trail.Context{Origin: rec.OriginOf(chat, 4), Conversation: conv, Delegation: deleg},
		option.Some(trail.Cause{ModelCall: modelCall, ToolCall: option.Some("call_0")}),
		trail.AgentAction{Key: trail.ToolKey(modelCall, 0), Instance: 9, App: play, Operation: "run", Decision: "final", Phase: "completed"}))
	require.NoError(t, rec.Flush(ctx))

	// The query run that action caused, as the capture writes it from the
	// stamp play put on the query.
	stamp := `{"run_id":"` + run + `","app":"` + play + `","instance":9,"lane":"main","task":"` + task + `","task_epoch":1,"task_call":"` + call + `"}`
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 1)
	require.NoError(t, queryrunfacts.BuildEntities(ent, []queryrunfacts.Row{{
		Type: "QueryFinish", EventUs: at.Add(2 * time.Second).UnixMicro(), QueryId: "q-1", Query: "SELECT 1", QueryKind: "Select", LogComment: stamp}}))
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	require.NoError(t, exec.InsertArrow(ctx, trail.TrailTableName, records))
	for _, r := range records {
		r.Release()
	}

	scan := func(pick func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error]) []*trail.TrailEntity {
		ents, serr := rec.Scan(pick)
		require.NoError(t, serr)
		return ents
	}
	// Origin: one predicate finds the run's rows of every lane.
	origins := scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanOrigin(ctx, recordstore.ScanOpts{})
	})
	require.Len(t, origins, 3, "the call, the action and the hand-written query run")
	windows := map[uint64]string{}
	for _, e := range origins {
		assert.Equal(t, run, e.Origin.Val.Run)
		windows[e.Origin.Val.Instance] = e.Origin.Val.App
	}
	assert.Equal(t, map[uint64]string{4: chat, 9: play}, windows)

	// Delegation: the action and what it caused, on (task, call).
	delegated := scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanDelegation(ctx, recordstore.ScanOpts{})
	})
	require.Len(t, delegated, 2)
	var action, caused *trail.TrailEntity
	for _, e := range delegated {
		assert.Equal(t, task, e.Delegation.Val.Task)
		assert.Equal(t, call, e.Delegation.Val.Call.Val)
		if e.AgentAction.Has {
			action = e
		} else {
			caused = e
		}
	}
	require.NotNil(t, action)
	require.NotNil(t, caused, "the query run carries the Delegation slots")
	assert.Equal(t, action.AgentAction.Val.Instance, caused.Origin.Val.Instance, "the run happened in the window the action addressed")
	assert.Empty(t, caused.Archetype()[2:], "no trail domain component: the row is the capture's")

	// Cause: the action names the model call, whose row names the conversation.
	require.True(t, action.Cause.Has)
	calls := scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanLlmCall(ctx, recordstore.ScanOpts{})
	})
	require.Len(t, calls, 1)
	assert.Equal(t, calls[0].LlmCall.Val.CallId, action.Cause.Val.ModelCall)
	assert.Equal(t, conversation, calls[0].Conversation.Val.Conversation)
	assert.Equal(t, turn, action.Conversation.Val.Turn.Val)
}
