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
	"github.com/stergiotis/boxer/public/identity/callident"
	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
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
	dir, exec, _ := localSurface(t)

	writeEpisode(t, exec)

	views, byName := installViews(t, exec)
	query, read := localReaders(t, dir, byName)

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

	admitted := read(trailviews.ViewModelCalls, []string{"admission", "queued-ms", "route", "tier", "approval"}, "ORDER BY {approval} DESC")
	require.Len(t, admitted, 2)
	require.Equal(t, "queued", admitted[0]["admission"])
	require.EqualValues(t, 40, admitted[0]["queued-ms"])
	require.Equal(t, "local", admitted[0]["route"])
	require.EqualValues(t, 2, admitted[0]["tier"])
	require.EqualValues(t, 77, admitted[0]["approval"], "the journal's approval entry, joinable (L33)")
	require.EqualValues(t, 0, admitted[1]["approval"], "no approval reads zero")

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
		Admission: option.Some("queued"), QueuedMs: option.Some(uint64(40)), Route: option.Some("local"), Tier: 2,
		Approval: option.Some(uint64(77)),
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

// localSurface is clickhouse-local with the read surface, decode views
// included, and the facts table, or a skip.
func localSurface(t *testing.T) (dir string, exec *chexec.LocalExecutor, ctx context.Context) {
	t.Helper()
	dir = t.TempDir()
	exec, err := chexec.NewLocalExecutor(dir, nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx = context.Background()
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
	return
}

// installViews creates the functions and then every view.
func installViews(t *testing.T, exec *chexec.LocalExecutor) (views []trailviews.View, byName map[string]trailviews.View) {
	t.Helper()
	for _, stmt := range trailviews.FunctionStatements() {
		require.NoError(t, exec.Exec(context.Background(), stmt))
	}
	views, err := trailviews.Compose("", "")
	require.NoError(t, err)
	byName = map[string]trailviews.View{}
	for _, v := range views {
		require.NoError(t, exec.Exec(context.Background(), v.Sql), v.Name)
		byName[v.Name] = v
	}
	return
}

// localReaders are query, which runs a statement and decodes its rows,
// and read, which selects columns of a view by logical name — the physical
// name aliased back to it — and appends the rest of the statement.
func localReaders(t *testing.T, dir string, byName map[string]trailviews.View) (query func(sql string) []map[string]any, read func(view string, names []string, rest string) []map[string]any) {
	query = func(sql string) (rows []map[string]any) {
		cmd, cerr := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{}, "--path", dir, "--output_format_json_quote_64bit_integers=0", "--query", sql+" FORMAT JSONEachRow")
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
	read = func(view string, names []string, rest string) []map[string]any {
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
	return
}

// Over clickhouse-local: bus requests recorded through the trail's bus
// sink read back from the one audit-event mart beside every other audit
// event — the result as the outcome, the subject a reference, the
// measurements attributes — and the timeline carries each as one
// auditEvent row whose headline names domain, action and outcome.
func TestBusRequestsReadBackOverLocal(t *testing.T) {
	dir, exec, ctx := localSurface(t)
	rec := trail.NewRecorder(exec, "run-1", zerolog.Nop())
	at := time.Unix(1700000000, 0).UTC()
	sink := trail.AsAuditSink(rec)
	sink.Record(busaudit.AuditRecord{AppId: "apps/play", InstanceKey: 3, Subject: "runtime.persist.play.x.get", Result: busaudit.AuditResultOk,
		LatencyMs: 12, RequestSizeB: 40, Ts: at})
	sink.Record(busaudit.AuditRecord{AppId: "apps/play", InstanceKey: 3, Subject: "runtime.persist.play.x.set", Result: busaudit.AuditResultTimeout,
		LatencyMs: 30000, RequestSizeB: 7, Ts: at.Add(time.Second)})
	require.NoError(t, rec.Event(ctx, at.Add(2*time.Second), trail.Context{Origin: rec.OriginOf("apps/dmdm", 1)}, trail.AuditEvent{
		Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Subject: 7, Retention: "disclosure"}))
	require.NoError(t, rec.Flush(ctx))
	rec.Close()

	_, byName := installViews(t, exec)
	query, read := localReaders(t, dir, byName)

	events := read(trailviews.ViewAuditEvents, []string{"app", "instance", "run", "domain", "action", "outcome", "ref-types", "ref-values", "attr-keys", "attr-values"}, "ORDER BY {domain}, {outcome}")
	require.Len(t, events, 3, "the bus's requests are in the one audit-event mart")
	bus := events[:2]
	require.Equal(t, "apps/play", bus[0]["app"])
	require.EqualValues(t, 3, bus[0]["instance"])
	require.Equal(t, "run-1", bus[0]["run"])
	require.Equal(t, busaudit.BusDomain, bus[0]["domain"])
	require.Equal(t, busaudit.BusActionRequest, bus[0]["action"])
	require.Equal(t, "ok", bus[0]["outcome"])
	require.Equal(t, []any{busaudit.BusRefSubject}, bus[0]["ref-types"])
	require.Equal(t, []any{"runtime.persist.play.x.get"}, bus[0]["ref-values"])
	require.Equal(t, []any{busaudit.BusAttrLatencyMs, busaudit.BusAttrRequestBytes}, bus[0]["attr-keys"], "a measurement the writer left out is absent")
	require.Equal(t, []any{"12", "40"}, bus[0]["attr-values"])
	require.Equal(t, "timeout", bus[1]["outcome"], "the bus's result is the outcome")
	require.Equal(t, "dmdm", events[2]["domain"])

	timeline := read(trailviews.ViewTimeline, []string{"kind", "status", "headline", "ts"}, "ORDER BY {ts}")
	require.Len(t, timeline, 3)
	for _, r := range timeline {
		require.Equal(t, "auditEvent", r["kind"])
	}
	require.Equal(t, "ok", timeline[0]["status"])
	require.Equal(t, "bus request -> ok [subject:runtime.persist.play.x.get]", timeline[0]["headline"])
	require.Equal(t, "bus request -> timeout [subject:runtime.persist.play.x.set]", timeline[1]["headline"])
	ids := query(`SELECT uniqExact("` + byName[trailviews.ViewTimeline].Columns[0].Physical + `") AS u FROM boxer.` + trailviews.ViewTimeline)
	require.EqualValues(t, 3, ids[0]["u"])
}

// Over clickhouse-local: the functions on literal arrays — a key present
// once and twice, a key absent, a reference type repeated — and the
// version function reporting the package's revision.
func TestFunctionsOverLocal(t *testing.T) {
	dir, exec, _ := localSurface(t)
	_, byName := installViews(t, exec)
	query, _ := localReaders(t, dir, byName)

	rows := query(`SELECT
  TRAIL_ATTR(['state', 'from', 'state'], ['on', 'off', 'later'], 'from') AS attr,
  TRAIL_ATTR(['state', 'from', 'state'], ['on', 'off', 'later'], 'state') AS attr_first,
  TRAIL_ATTR(['state'], ['on'], 'reason') AS attr_absent,
  TRAIL_ATTR(CAST([], 'Array(String)'), CAST([], 'Array(String)'), 'reason') AS attr_empty,
  TRAIL_REF(['rule', 'subject', 'rule'], ['r1', 's9', 'r2'], 'rule') AS ref,
  TRAIL_REF(['rule', 'subject', 'rule'], ['r1', 's9', 'r2'], 'subject') AS ref_one,
  TRAIL_REF(['rule'], ['r1'], 'peer') AS ref_absent,
  TRAIL_REFS(['rule', 'subject', 'rule'], ['r1', 's9', 'r2'], 'rule') AS refs,
  TRAIL_REFS(['rule'], ['r1'], 'peer') AS refs_absent,
  TRAIL_VIEWS_VERSION() AS version`)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "off", r["attr"])
	require.Equal(t, "on", r["attr_first"], "a repeated key reads its first value")
	require.Equal(t, "", r["attr_absent"])
	require.Equal(t, "", r["attr_empty"])
	require.Equal(t, "r1", r["ref"], "the first reference of the type")
	require.Equal(t, "s9", r["ref_one"])
	require.Equal(t, "", r["ref_absent"])
	require.Equal(t, []any{"r1", "r2"}, r["refs"], "every reference of the type, in order")
	require.Equal(t, []any{}, r["refs_absent"])
	require.EqualValues(t, trailviews.ViewsVersion, r["version"])
}

// Over clickhouse-local: two entities with three state-changing events
// each, beside an event with no reference, read back through the entity
// column and the entities aggregate; and a review mark written through the
// recorder under a person's principal reads back from the reviews mart,
// naming the row it marks.
func TestEntitiesAndReviewsOverLocal(t *testing.T) {
	dir, exec, ctx := localSurface(t)
	rec := trail.NewRecorder(exec, "run-1", zerolog.Nop())
	at := time.Unix(1700000000, 0).UTC()
	c := trail.Context{Origin: rec.OriginOf("apps/rules", 1)}
	ev := func(action string, state string, refs ...string) trail.AuditEvent {
		e := trail.AuditEvent{Domain: "rules", Action: action, Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Retention: "ops",
			AttrKeys: []string{"state"}, AttrValues: []string{state}}
		for i := 0; i+1 < len(refs); i += 2 {
			e.RefTypes = append(e.RefTypes, refs[i])
			e.RefValues = append(e.RefValues, refs[i+1])
		}
		return e
	}
	write := func(offset time.Duration, e trail.AuditEvent) {
		require.NoError(t, rec.Event(ctx, at.Add(offset), c, e))
	}
	// Interleaved, and r2's later references are context, not its entity.
	write(1*time.Second, ev("created", "draft", "rule", "r1"))
	write(2*time.Second, ev("created", "draft", "rule", "r2", "subject", "s9"))
	write(3*time.Second, ev("updated", "active", "rule", "r1"))
	write(4*time.Second, ev("updated", "active", "rule", "r2", "subject", "s9"))
	write(5*time.Second, ev("updated", "retired", "rule", "r1"))
	write(6*time.Second, ev("deleted", "gone", "rule", "r2"))
	write(7*time.Second, ev("swept", "idle"))
	require.NoError(t, rec.Flush(ctx))

	_, byName := installViews(t, exec)
	query, read := localReaders(t, dir, byName)

	events := read(trailviews.ViewAuditEvents, []string{"id", "ts", "action", "entity"}, "ORDER BY {ts}")
	require.Len(t, events, 7)
	entities := make([]string, 0, len(events))
	for _, e := range events {
		entities = append(entities, e["entity"].(string))
	}
	require.Equal(t, []string{"rule:r1", "rule:r2", "rule:r1", "rule:r2", "rule:r1", "rule:r2", ""}, entities,
		"the first reference as type:value, empty when there is none")
	// Ids exceed a float's mantissa, so they are compared as text.
	asText := func(view string, column string, rest string) (out []string) {
		phys := map[string]string{}
		for _, c := range byName[view].Columns {
			phys[c.Name] = c.Physical
		}
		for n, p := range phys {
			rest = strings.ReplaceAll(rest, "{"+n+"}", `"`+p+`"`)
		}
		for _, r := range query(`SELECT toString("` + phys[column] + `") AS v FROM boxer.` + view + " " + rest) {
			out = append(out, r["v"].(string))
		}
		return out
	}
	ids := asText(trailviews.ViewAuditEvents, "id", "ORDER BY {ts}")
	require.Len(t, ids, 7)

	aggs := read(trailviews.ViewAuditEntities, []string{"domain", "entity", "first-ts", "last-ts", "events", "last-action", "last-outcome", "last-attr-keys", "last-attr-values", "facts-ids"}, "ORDER BY {entity}")
	require.Len(t, aggs, 2, "one row per entity; the event without one is in none")
	r1, r2 := aggs[0], aggs[1]
	require.Equal(t, "rules", r1["domain"])
	require.Equal(t, "rule:r1", r1["entity"])
	require.EqualValues(t, 3, r1["events"])
	require.Equal(t, "updated", r1["last-action"])
	require.Equal(t, "ok", r1["last-outcome"])
	require.Equal(t, []any{"state"}, r1["last-attr-keys"])
	require.Equal(t, []any{"retired"}, r1["last-attr-values"], "the state after the entity's latest event")
	require.Less(t, r1["first-ts"].(string), r1["last-ts"].(string))
	lineage := asText(trailviews.ViewAuditEntities, "facts-ids", "ORDER BY {entity}")
	require.Len(t, lineage, 2)
	require.ElementsMatch(t, []string{ids[0], ids[2], ids[4]}, strings.Split(strings.Trim(lineage[0], "[]"), ","), "the lineage names the entity's three rows")
	require.Equal(t, "rule:r2", r2["entity"])
	require.EqualValues(t, 3, r2["events"])
	require.Equal(t, "deleted", r2["last-action"])
	require.Equal(t, []any{"gone"}, r2["last-attr-values"])
	require.ElementsMatch(t, []string{ids[1], ids[3], ids[5]}, strings.Split(strings.Trim(lineage[1], "[]"), ","))

	// A person flags the swept event, clearing nothing; then marks it
	// reviewed, clearing a rule.
	marked := ids[6]
	person := callident.WithClaims(ctx, callident.Claims{Principal: "person-1"})
	mark := func(action string, attrs ...string) trail.AuditEvent {
		e := trail.AuditEvent{Domain: trail.ReviewDomain, Action: action, Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByEnv, Retention: "trail",
			RefTypes: []string{trail.ReviewRefTrailRow}, RefValues: []string{marked}}
		for i := 0; i+1 < len(attrs); i += 2 {
			e.AttrKeys = append(e.AttrKeys, attrs[i])
			e.AttrValues = append(e.AttrValues, attrs[i+1])
		}
		return e
	}
	require.NoError(t, rec.Event(person, at.Add(8*time.Second), c, mark(trail.ReviewActionFlagged, trail.ReviewAttrReason, "unexpected sweep")))
	require.NoError(t, rec.Event(person, at.Add(9*time.Second), c, mark(trail.ReviewActionReviewed, trail.ReviewAttrReason, "scheduled", trail.ReviewAttrRule, "new-action")))
	require.NoError(t, rec.Flush(ctx))
	rec.Close()

	reviews := read(trailviews.ViewReviews, []string{"id", "ts", "app", "run", "trail-row", "mark", "reason", "rule", "principal", "principal-by"}, "ORDER BY {ts}")
	require.Len(t, reviews, 2, "one row per mark: the other domains' rows are not marks")
	require.Equal(t, []string{marked, marked}, asText(trailviews.ViewReviews, "trail-row", "ORDER BY {ts}"), "the marked row's facts id")
	require.Equal(t, trail.ReviewActionFlagged, reviews[0]["mark"])
	require.Equal(t, "unexpected sweep", reviews[0]["reason"])
	require.Equal(t, "", reviews[0]["rule"], "a mark that clears no rule names none")
	require.Equal(t, "person-1", reviews[0]["principal"])
	require.Equal(t, trail.PrincipalByEnv, reviews[0]["principal-by"])
	require.Equal(t, "apps/rules", reviews[0]["app"])
	require.Equal(t, "run-1", reviews[0]["run"])
	require.Equal(t, trail.ReviewActionReviewed, reviews[1]["mark"])
	require.Equal(t, "new-action", reviews[1]["rule"])

	// 1:1 with facts rows: each mark is one audit event, under its own id.
	markIds := asText(trailviews.ViewReviews, "id", "ORDER BY {ts}")
	allIds := asText(trailviews.ViewAuditEvents, "id", "ORDER BY {ts}")
	require.Len(t, allIds, 9)
	require.Equal(t, allIds[7:], markIds)
}
