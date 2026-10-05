package agent

import (
	"context"
	"iter"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// trailOnLocal is a trail recorder over a fresh boxer.facts on
// clickhouse-local; the test is skipped where there is no clickhouse.
func trailOnLocal(t *testing.T) (rec *trail.Recorder) {
	t.Helper()
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
	rec = trail.NewRecorder(exec, "run-test", zerolog.Nop())
	t.Cleanup(rec.Close)
	return
}

// Over clickhouse-local: every decision and every final phase lands as an
// agentAction row on the trail (ADR-0269 §SD9), flushed off the call's
// path and at shutdown, each with the context components that join it to
// the rest (ADR-0277 §SD1); and the grant leaves its events.
func TestActionRecordLandsOnTheTrail(t *testing.T) {
	ctx := context.Background()
	rec := trailOnLocal(t)
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.Trail = true, rec })
	require.True(t, r.svc.Durable())
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "edit the doc", Conversation: "chat-1",
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)
	r.call(g, "q", "get_text", "{}")
	// A model's tool call: keyed by the coordinator, with the model call
	// that asked for it and the provider's own id beside the key.
	key := trail.ToolKey("llm-1", 0)
	_, err = r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: r.docKey, Operation: "set_text", Args: `{"text":"agent"}`,
		Key: key, Turn: "turn-1", ModelCall: "llm-1", ToolCall: "call_0", ToolIndex: 0})
	require.NoError(t, err)
	r.host.frame(7)
	r.host.frame(7)
	_, err = r.cli.Status(ctx, g.Handle, key, 0)
	require.NoError(t, err)
	require.NoError(t, r.cli.Stop(ctx, g.Handle))
	want := len(r.svc.Actions())
	r.svc.Close() // flushes what is buffered

	rows, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentAction(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	assert.Len(t, rows, want)
	var finals, caused int
	for _, ent := range rows {
		row := ent.AgentAction.Val
		assert.Equal(t, "agentAction", row.Kind)
		assert.True(t, row.Test)
		require.True(t, ent.Origin.Has)
		assert.Equal(t, "run-test", ent.Origin.Val.Run, "the run is on every row")
		assert.NotEmpty(t, ent.Origin.Val.App, "the coordinator is the row's origin")
		require.True(t, ent.Delegation.Has)
		assert.Equal(t, g.Task, ent.Delegation.Val.Task)
		require.True(t, ent.Conversation.Has)
		assert.Equal(t, "chat-1", ent.Conversation.Val.Conversation)
		if row.Decision == "final" {
			finals++
			assert.True(t, ent.Delegation.Val.Call.Has, "a routed call names the dispatcher's call id")
		}
		if row.Key == key {
			caused++
			require.True(t, ent.Cause.Has)
			assert.Equal(t, "llm-1", ent.Cause.Val.ModelCall)
			assert.Equal(t, "call_0", ent.Cause.Val.ToolCall.Val)
			assert.Equal(t, "turn-1", ent.Conversation.Val.Turn.Val)
		} else {
			assert.False(t, ent.Cause.Has)
		}
	}
	assert.Equal(t, 2, finals, "the query and the command each reached a final phase")
	assert.Equal(t, 2, caused, "the tool call's dispatch row and its final row")

	grants, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentGrant(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	var events []string
	for _, ent := range grants {
		row := ent.AgentGrant.Val
		events = append(events, row.Event)
		assert.Equal(t, "edit the doc", row.Plan)
		assert.Equal(t, trail.ContentDigest("edit the doc"), row.PlanDigest)
		assert.Equal(t, "chat-1", ent.Conversation.Val.Conversation)
		assert.Equal(t, g.Task, ent.Delegation.Val.Task)
		if row.Event == trail.GrantEventApproved {
			assert.Equal(t, "host", row.DecidedBy, "a test grant stands in for the person")
			assert.Len(t, row.Entries, 1)
		}
	}
	assert.ElementsMatch(t, []string{trail.GrantEventApproved, trail.GrantEventEnded}, events)
}

// A repeated key returns the first outcome, so a coordinator keys a tool
// call by the model call and its index: two replies that carry the same
// provider id are still two calls (ADR-0277 §SD6).
func TestToolKeysDifferAcrossReplies(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	ctx := context.Background()
	call := func(modelCall string) Outcome {
		out, err := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: r.docKey, Operation: "get_text", Args: "{}",
			Key: trail.ToolKey(modelCall, 0), ModelCall: modelCall, ToolCall: "call_0"})
		require.NoError(t, err)
		return out
	}
	call("llm-1")
	call("llm-2")
	var keys []string
	for _, a := range r.svc.Actions() {
		if a.Decision == "dispatch" {
			keys = append(keys, a.Key)
			assert.Equal(t, "call_0", a.ToolCallId)
		}
	}
	assert.Equal(t, []string{"llm-1#0", "llm-2#0"}, keys, "the same provider id, two dispatches")
}

// Over clickhouse-local: what the model asked for outside a window — a
// describe, a help read, a grant — names the model call that asked; a
// call's title and reason ride its dispatch row; the person's change
// pauses the task once and the next turn resumes it; a stop from the
// coordinator's button is the person's (ADR-0277 §SD1).
func TestTheTrailNamesWhatAskedAndWhoDecided(t *testing.T) {
	ctx := context.Background()
	rec := trailOnLocal(t)
	r := newRigWith(t, func(cfg *Config) { cfg.Coordinators, cfg.Trail = []string{"test.coordinator"}, rec })
	ask := func(i int) Asked {
		return Asked{Key: trail.ToolKey("llm-1", i), Conversation: "chat-1", Turn: "turn-1", ModelCall: "llm-1",
			ToolCall: "call_" + strconv.Itoa(i), ToolIndex: uint32(i)}
	}
	_, err := r.cli.Describe(ctx, DescribeRequest{Asked: ask(0)})
	require.NoError(t, err)
	_, err = r.cli.Describe(ctx, DescribeRequest{})
	require.NoError(t, err, "the coordinator's own read")
	_, err = r.cli.Help(ctx, HelpRequest{App: string(docAppId), Asked: ask(1)})
	require.NoError(t, err)
	got := make(chan Grant, 1)
	go func() {
		g, gerr := r.cli.Request(ctx, GrantRequest{Plan: "edit the doc", Conversation: "chat-1",
			Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}, Asked: ask(2)})
		assert.NoError(t, gerr)
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	a := ask(3)
	_, err = r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: r.docKey, Operation: "get_text", Args: "{}", Key: a.Key,
		Title: "Reading\nthe doc", Reason: "to see what is there", Turn: a.Turn, ModelCall: a.ModelCall, ToolCall: a.ToolCall, ToolIndex: a.ToolIndex})
	require.NoError(t, err)
	r.host.person(r.docKey, func(d *doc) { d.text = "the person's" })
	r.host.frame(r.docKey)
	r.host.person(r.docKey, func(d *doc) { d.text = "the person's again" })
	r.host.frame(r.docKey)
	_, err = r.cli.TurnAsked(ctx, g.Handle, Asked{Conversation: "chat-1", Turn: "turn-2"})
	require.NoError(t, err)
	require.NoError(t, r.cli.StopWith(ctx, g.Handle, StopRequest{ByPerson: true, Reason: "the person stopped it in the chat"}))
	r.svc.Close()

	actions, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentAction(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	byOp := map[string][]*trail.TrailEntity{}
	for _, ent := range actions {
		byOp[ent.AgentAction.Val.Operation] = append(byOp[ent.AgentAction.Val.Operation], ent)
	}
	for i, op := range []string{"describe", "help"} {
		require.Len(t, byOp[op], 1, "%s: one row, and none for the coordinator's own read", op)
		ent := byOp[op][0]
		assert.Equal(t, "final", ent.AgentAction.Val.Decision)
		assert.Equal(t, "completed", ent.AgentAction.Val.Phase)
		require.True(t, ent.Cause.Has)
		assert.Equal(t, "llm-1", ent.Cause.Val.ModelCall)
		assert.Equal(t, uint32(i), ent.Cause.Val.ToolIndex)
		require.True(t, ent.Conversation.Has)
		assert.Equal(t, "chat-1", ent.Conversation.Val.Conversation)
		assert.Equal(t, "turn-1", ent.Conversation.Val.Turn.Val)
		assert.False(t, ent.Delegation.Has, "no task held it")
	}
	for _, ent := range byOp["get_text"] {
		row := ent.AgentAction.Val
		if row.Decision == "dispatch" {
			assert.Equal(t, []string{"Reading the doc"}, row.CallTitle, "one line")
			assert.Equal(t, []string{"to see what is there"}, row.CallReason)
		} else {
			assert.Empty(t, row.CallTitle, "the final row does not repeat it")
		}
	}

	grants, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentGrant(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	byEvent := map[string][]*trail.TrailEntity{}
	for _, ent := range grants {
		byEvent[ent.AgentGrant.Val.Event] = append(byEvent[ent.AgentGrant.Val.Event], ent)
	}
	require.Len(t, byEvent[trail.GrantEventRequested], 1)
	req := byEvent[trail.GrantEventRequested][0]
	require.True(t, req.Cause.Has, "the request names the model call that asked")
	assert.Equal(t, uint32(2), req.Cause.Val.ToolIndex)
	assert.Equal(t, "turn-1", req.Conversation.Val.Turn.Val)
	require.Len(t, byEvent[trail.GrantEventPaused], 1, "two changes, one pause")
	paused := byEvent[trail.GrantEventPaused][0].AgentGrant.Val
	assert.Equal(t, "person", paused.DecidedBy)
	assert.Contains(t, paused.Reason[0], "window "+strconv.FormatUint(r.docKey, 10))
	require.Len(t, byEvent[trail.GrantEventResumed], 1)
	resumed := byEvent[trail.GrantEventResumed][0]
	assert.Equal(t, "coordinator", resumed.AgentGrant.Val.DecidedBy)
	assert.Equal(t, "turn-2", resumed.Conversation.Val.Turn.Val)
	require.Len(t, byEvent[trail.GrantEventEnded], 1)
	ended := byEvent[trail.GrantEventEnded][0].AgentGrant.Val
	assert.Equal(t, "person", ended.DecidedBy)
	assert.Equal(t, []string{"the person stopped it in the chat"}, ended.Reason)
}

// Over clickhouse-local: a capture leaves one agentCapture row (ADR-0281
// §SD6) — a permitted one with its decision, obligations and digests, a
// denied one with the reason — joined to the task like an action.
func TestTheCaptureRecordLandsOnTheTrail(t *testing.T) {
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
	rec := trail.NewRecorder(exec, "run-test", zerolog.Nop())
	defer rec.Close()
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.Trail = true, rec })
	g := r.grant(ModeObserve)
	out, err := r.cli.CaptureWith(ctx, CaptureRequest{Handle: g.Handle, Instances: []uint64{7}, Format: CaptureFormatPng, Key: "png"})
	require.NoError(t, err)
	require.Equal(t, "completed", out.Phase, out.Reason)
	out, err = r.cli.CaptureWith(ctx, CaptureRequest{Handle: g.Handle, Instances: []uint64{7}, Format: "gif", Key: "gif"})
	require.NoError(t, err)
	require.Equal(t, "refused", out.Phase)
	r.svc.Close()

	rows, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentCapture(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byFormat := map[string]trail.AgentCapture{}
	for _, ent := range rows {
		byFormat[ent.AgentCapture.Val.Format] = ent.AgentCapture.Val
		require.True(t, ent.Delegation.Has)
		assert.Equal(t, g.Task, ent.Delegation.Val.Task)
	}
	png := byFormat["png"]
	assert.Equal(t, "permit", png.Decision)
	assert.Equal(t, "grant", png.Policy)
	assert.Equal(t, []string{"scope@1"}, png.Obligations)
	assert.Equal(t, []uint64{7}, png.Windows)
	assert.Equal(t, "completed", png.Phase)
	assert.Len(t, png.Digest, 64)
	assert.Positive(t, png.Bytes)
	gif := byFormat["gif"]
	assert.Equal(t, "deny", gif.Decision)
	assert.Equal(t, "refused", gif.Phase)
	require.Len(t, gif.Reason, 1)
	assert.Contains(t, gif.Reason[0], "svg or png")
}
