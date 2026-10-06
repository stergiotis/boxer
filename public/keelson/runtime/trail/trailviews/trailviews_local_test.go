package trailviews_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Over clickhouse-local: one short agent episode is written through the
// trail's own Recorder — a grant, two model calls with their messages, an
// action that is accepted and rendered and one that is refused — and the
// views are created and read back. The assertions are what a reader of
// each view should be able to tell without decoding a single lane.
func TestViewsReadBackAnEpisodeOverLocal(t *testing.T) {
	dir := t.TempDir()
	exec, err := chexec.NewLocalExecutor(dir, nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	// The whole surface, decode views included: leeway.columns is how the
	// test asks the server whether the views are leeway tables.
	stmts := append([]string{}, lwsqlsurface.AllStatements("")...)
	for stmt := range strings.SplitSeq(setup, ";") {
		stmts = append(stmts, stmt)
	}
	for _, stmt := range stmts {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}

	writeEpisode(t, exec)

	views, err := trailviews.Compose("", "")
	require.NoError(t, err)
	byName := map[string]trailviews.View{}
	for _, v := range views {
		require.NoError(t, exec.Exec(ctx, v.Sql), v.Name)
		byName[v.Name] = v
	}

	query := func(sql string) (rows []map[string]any) {
		cmd, cerr := extbin.ClickHouseLocal.Command(ctx, extbin.Opts{}, "--path", dir, "--output_format_json_quote_64bit_integers=0", "--query", sql+" FORMAT JSONEachRow")
		require.NoError(t, cerr)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		dec := json.NewDecoder(&out)
		for dec.More() {
			row := map[string]any{}
			require.NoError(t, dec.Decode(&row))
			rows = append(rows, row)
		}
		return rows
	}
	// read selects columns of a view by logical name — the physical name
	// aliased back to it — and appends the rest of the statement.
	read := func(view string, names []string, rest string) []map[string]any {
		phys := map[string]string{}
		for _, c := range byName[view].Columns {
			phys[c.Name] = c.Physical
		}
		items := make([]string, 0, len(names))
		for _, n := range names {
			p, ok := phys[n]
			require.True(t, ok, "%s has no column %s", view, n)
			items = append(items, `"`+p+`" AS "`+n+`"`)
		}
		sql := "SELECT " + strings.Join(items, ", ") + " FROM boxer." + view
		for _, n := range names {
			rest = strings.ReplaceAll(rest, "{"+n+"}", `"`+phys[n]+`"`)
		}
		return query(sql + " " + rest)
	}

	// The server's own decode agrees: every view column is a leeway plain
	// column of the declared canonical type, holding values of the
	// ClickHouse type that type stands for.
	comments := query("SELECT name, comment FROM system.tables WHERE database = 'boxer' AND (name LIKE 'dm\\_%' OR name LIKE 'agg\\_%') ORDER BY name")
	require.Len(t, comments, len(trailviews.AllViewNames()))
	for _, r := range comments {
		require.Equal(t, string(trailviews.Stamp()), r["comment"], r["name"])
	}
	for _, v := range views {
		decoded := query("SELECT name, type, layout, canonical_type FROM leeway.columns WHERE database = 'boxer' AND table = '" + v.Name + "' ORDER BY position")
		require.Len(t, decoded, len(v.Columns), v.Name)
		for i, c := range v.Columns {
			r := decoded[i]
			require.Equal(t, c.Physical, r["name"], v.Name)
			require.Equal(t, "plain", r["layout"], "%s.%s", v.Name, c.Name)
			if !c.Backbone {
				require.Equal(t, c.CanonicalType, r["canonical_type"], "%s.%s", v.Name, c.Name)
				require.Equal(t, c.ClickHouse, r["type"], "%s.%s", v.Name, c.Name)
			}
		}
	}

	calls := read(trailviews.ViewModelCalls, []string{"call-id", "conversation", "turn", "purpose", "input-tokens", "finish-reason"}, "ORDER BY {call-id}")
	require.Len(t, calls, 2)
	require.Equal(t, "call-1", calls[0]["call-id"])
	require.Equal(t, "conv-1", calls[0]["conversation"])
	require.Equal(t, "turn-1", calls[0]["turn"])
	require.Equal(t, "chat/turn", calls[0]["purpose"])
	require.EqualValues(t, 10, calls[0]["input-tokens"])
	require.Equal(t, "tool_calls", calls[0]["finish-reason"])

	msgs := read(trailviews.ViewModelMessages, []string{"ordinal", "role", "content", "tool-names"}, "ORDER BY {ordinal}")
	require.Len(t, msgs, 3)
	require.Equal(t, "please write a note", msgs[0]["content"])
	require.Equal(t, []any{"call_operation"}, msgs[1]["tool-names"])
	require.Equal(t, "", msgs[1]["content"], "a message whose body was not kept reads empty")

	actions := read(trailviews.ViewAgentActions, []string{"task", "cause-model-call", "decision", "phase", "call-title"}, "ORDER BY {decision}, {phase}")
	require.Len(t, actions, 3, "dispatch and final rows of the first call, the dispatch row of the refused one")
	require.Equal(t, "task-1", actions[0]["task"])
	require.Equal(t, "call-1", actions[0]["cause-model-call"])
	require.Equal(t, []any{"Writing the note"}, actions[0]["call-title"])

	outcomes := read(trailviews.ViewActionOutcomes, []string{"action-key", "outcome", "done", "not-done", "reason", "call-title", "phases", "facts-ids"}, "ORDER BY {action-key}")
	require.Len(t, outcomes, 2, "one row per action, not per trail row")
	require.Equal(t, "rendered", outcomes[0]["outcome"])
	require.Equal(t, true, outcomes[0]["done"])
	require.Equal(t, []any{"accepted", "rendered"}, outcomes[0]["phases"])
	require.Equal(t, []any{"Writing the note"}, outcomes[0]["call-title"])
	require.Len(t, outcomes[0]["facts-ids"], 2, "the lineage names both trail rows of the action")
	require.Equal(t, "refused", outcomes[1]["outcome"])
	require.Equal(t, true, outcomes[1]["not-done"])
	require.Equal(t, []any{"above the ceiling"}, outcomes[1]["reason"])
	require.Len(t, outcomes[1]["facts-ids"], 1)

	timeline := read(trailviews.ViewTimeline, []string{"kind", "status", "headline", "ts", "seq"}, "ORDER BY {ts}, {seq}")
	kinds := make([]string, 0, len(timeline))
	for _, r := range timeline {
		kinds = append(kinds, r["kind"].(string))
	}
	require.Equal(t, []string{
		"agentGrant",
		"llmCall", "llmMessage", "llmMessage",
		"agentAction", "agentAction", "agentAction",
		"llmCall", "llmMessage",
		"agentGrant",
	}, kinds)
	require.Equal(t, "grant approved by host: write a note", timeline[0]["headline"])
	require.Equal(t, "user message #0: please write a note", timeline[2]["headline"])
	require.Contains(t, timeline[4]["headline"], "set_note on apps/opsdemo#2 [document] dispatch -> accepted: Writing the note")

	// A data mart is 1:1 with facts rows: the timeline holds every trail
	// row of the episode exactly once, under the fact's own id.
	ids := query(`SELECT count() AS n, uniqExact("` + byName[trailviews.ViewTimeline].Columns[0].Physical + `") AS u FROM boxer.` + trailviews.ViewTimeline)
	require.EqualValues(t, 10, ids[0]["n"])
	require.EqualValues(t, 10, ids[0]["u"])

	tasks := read(trailviews.ViewTasks, []string{"plan", "grant-events", "ended", "actions", "actions-done", "actions-not-done", "not-done-reasons", "facts-ids"}, "")
	require.Len(t, tasks, 1)
	task := tasks[0]
	require.Equal(t, "write a note", task["plan"])
	require.Equal(t, []any{"approved", "ended"}, task["grant-events"])
	require.Equal(t, true, task["ended"])
	require.EqualValues(t, 2, task["actions"])
	require.EqualValues(t, 1, task["actions-done"])
	require.EqualValues(t, 1, task["actions-not-done"])
	require.Equal(t, []any{"set_note: refused (above the ceiling)"}, task["not-done-reasons"])
	require.Len(t, task["facts-ids"], 5, "two grant events and three action rows")

	convs := read(trailviews.ViewConversations, []string{"turns", "model-call-count", "tokens-in", "message-count", "messages-kept", "actions", "tasks", "first-question", "last-answer", "facts-ids"}, "")
	require.Len(t, convs, 1)
	conv := convs[0]
	require.EqualValues(t, 1, conv["turns"])
	require.EqualValues(t, 2, conv["model-call-count"])
	require.EqualValues(t, 14, conv["tokens-in"])
	require.EqualValues(t, 3, conv["message-count"])
	require.EqualValues(t, 2, conv["messages-kept"])
	require.EqualValues(t, 2, conv["actions"])
	require.Equal(t, []any{"task-1"}, conv["tasks"])
	require.Equal(t, "please write a note", conv["first-question"])
	require.Equal(t, "Done.", conv["last-answer"])
	require.Len(t, conv["facts-ids"], 10, "every trail row of the conversation")
}

func writeEpisode(t *testing.T, exec *chexec.LocalExecutor) {
	rec := trail.NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()

	at := time.Unix(1700000000, 0).UTC()
	step := func() time.Time { at = at.Add(100 * time.Millisecond); return at }
	conv := func(turn string, round uint32) option.Option[trail.Conversation] {
		return option.Some(trail.Conversation{Conversation: "conv-1", Turn: option.Some(turn), Round: option.Some(round)})
	}
	task := option.Some(trail.Delegation{Task: "task-1", Epoch: 1})
	chat := trail.Context{Origin: rec.OriginOf("apps/chat", 1), Conversation: conv("turn-1", 0)}
	delegated := chat
	delegated.Delegation = task

	require.NoError(t, rec.AgentGrant(step(), delegated, option.None[trail.Cause](), trail.AgentGrant{
		Event: trail.GrantEventApproved, Plan: "write a note", PlanDigest: "pd-1", DecidedBy: "host", CallsBudget: 200,
	}))

	callAt := step()
	require.NoError(t, rec.LlmCall(callAt, chat, trail.LlmCall{
		CallId: "call-1", Purpose: "chat/turn", Sensitivity: "ordinary", Model: "m", EndpointHost: "localhost",
		Messages: 2, InputTokens: 10, OutputTokens: 1, ToolCalls: 1, FinishReason: "tool_calls", Retention: "kept",
	}))
	require.NoError(t, rec.LlmMessage(callAt, chat, trail.LlmMessage{CallId: "call-1", Ordinal: 0, Role: "user", Sensitivity: "ordinary", Bytes: 19},
		option.Some(trail.LlmMessageBody{Content: "please write a note"})))
	require.NoError(t, rec.LlmMessage(callAt, chat, trail.LlmMessage{CallId: "call-1", Ordinal: 1, Role: "assistant", Sensitivity: "ordinary", ToolNames: []string{"call_operation"}},
		option.None[trail.LlmMessageBody]()))

	cause := func(index uint32) option.Option[trail.Cause] {
		return option.Some(trail.Cause{ModelCall: "call-1", ToolIndex: index})
	}
	action := func(key string, decision string, phase string, title []string, reason []string) trail.AgentAction {
		return trail.AgentAction{Key: key, Instance: 2, App: "apps/opsdemo", Operation: "set_note", Effect: "document",
			Decision: decision, Phase: phase, CallTitle: title, Reason: reason, BudgetLeft: 199}
	}
	require.NoError(t, rec.AgentAction(step(), delegated, cause(0), action("call-1#0", "dispatch", "accepted", []string{"Writing the note"}, nil)))
	require.NoError(t, rec.AgentAction(step(), delegated, cause(0), action("call-1#0", "final", "rendered", nil, nil)))
	require.NoError(t, rec.AgentAction(step(), delegated, cause(1), action("call-1#1", "dispatch", "refused", nil, []string{"above the ceiling"})))

	answerAt := step()
	answer := trail.Context{Origin: chat.Origin, Conversation: conv("turn-1", 1)}
	require.NoError(t, rec.LlmCall(answerAt, answer, trail.LlmCall{
		CallId: "call-2", Parent: option.Some("call-1"), Purpose: "chat/turn", Sensitivity: "ordinary", Model: "m",
		EndpointHost: "localhost", Messages: 1, InputTokens: 4, OutputTokens: 1, FinishReason: "stop", Retention: "kept",
	}))
	require.NoError(t, rec.LlmMessage(answerAt, answer, trail.LlmMessage{CallId: "call-2", Ordinal: 2, Role: "assistant", Sensitivity: "ordinary", Bytes: 5},
		option.Some(trail.LlmMessageBody{Content: "Done."})))

	require.NoError(t, rec.AgentGrant(step(), delegated, option.None[trail.Cause](), trail.AgentGrant{
		Event: trail.GrantEventEnded, Plan: "write a note", PlanDigest: "pd-1", DecidedBy: "host",
	}))
	require.NoError(t, rec.Flush(context.Background()))
}
