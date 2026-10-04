package llm

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// serveRetaining is serve with an app that also holds RetainCaps.
func serveRetaining(t *testing.T, bus *inprocbus.Inst, cfg Config, id app.AppIdT) (cli *Client, svc *Service) {
	t.Helper()
	var err error
	svc, err = NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli = retainingClient(bus, id)
	return
}

func retainingClient(bus *inprocbus.Inst, id app.AppIdT) (cli *Client) {
	caps := append(ClientCaps("test: ask"), RetainCaps("test: keep")...)
	cli = NewClient(bus.NewClient(id, caps))
	cli.Timeout = 5 * time.Second
	return
}

func user(s string) openaichat.Message {
	return openaichat.Message{Role: openaichat.ChatRoleUser, Content: s}
}

// ADR-0264 §SD1: the retained subject is outside every llm.* grant, so an
// app that declared only ClientCaps is refused it by the bus.
func TestRetainNeedsItsOwnGrant(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a"}}
	cli, _, _ := serve(t, localCfg(p))
	_, err := cli.Complete(context.Background(), Request{Retain: true, Conversation: "c1", Messages: []openaichat.Message{user("hi")}})
	require.Error(t, err)
	assert.Empty(t, p.seen.Messages, "nothing reached the provider")
}

// Below durable a retained request is served and its reply says it was
// not kept, and why; ring still keeps the text on the in-process record.
func TestRetainBelowDurableIsServedNotKept(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    func(Config) Config
		reason string
	}{
		{"off", func(c Config) Config { return c }, "is off"},
		{"ring", func(c Config) Config { c.Retain = RetainRing; return c }, "is ring"},
		{"durable without a backend", func(c Config) Config { c.Retain = RetainDurable; return c }, "no durable backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a", FinishReason: "stop"}}
			cli, svc := serveRetaining(t, inprocbus.NewInst(zerolog.Nop()), tc.cfg(localCfg(p)), appId)
			res, err := cli.Complete(context.Background(), Request{Retain: true, Conversation: "c1", Messages: []openaichat.Message{user("hi")}})
			require.NoError(t, err)
			assert.Equal(t, "a", res.Content)
			assert.NotEmpty(t, res.CallId)
			assert.Equal(t, RetentionNotKept, res.Retention)
			assert.Contains(t, res.RetentionReason, tc.reason)
			calls := svc.Calls()
			require.Len(t, calls, 1)
			assert.Equal(t, "c1", calls[0].Conversation)
			assert.False(t, calls[0].Kept)
			assert.Equal(t, tc.name != "off", calls[0].Prompt != "", "ring and above keep the text on the record")
		})
	}
}

// A plain completion is not asked about, and a retained one must name its
// conversation.
func TestRetentionVerdictShapes(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a"}}
	cli, _ := serveRetaining(t, inprocbus.NewInst(zerolog.Nop()), localCfg(p), appId)
	res, err := cli.Complete(context.Background(), Request{Messages: []openaichat.Message{user("hi")}})
	require.NoError(t, err)
	assert.Equal(t, RetentionNotAsked, res.Retention)
	_, err = cli.Complete(context.Background(), Request{Retain: true, Messages: []openaichat.Message{user("hi")}})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, refused.Reason, "conversation")
}

// §SD3: a continuing turn keeps what follows its parent's conversation,
// with a declared omission taken out; anything else keeps it all.
func TestContinuationFollowsTheParent(t *testing.T) {
	first := []openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: "s"}, user("q1"), {Role: openaichat.ChatRoleAssistant, Content: "a1"}}
	parent := option.Some(seen{conversation: "c", hashes: messageHashes(first)})

	next := append(append([]openaichat.Message(nil), first...), user("q2"))
	covered, ok := continuation(parent, "c", messageHashes(next), 0, 0)
	assert.True(t, ok)
	assert.Equal(t, 3, covered)
	_, ok = continuation(parent, "other", messageHashes(next), 0, 0)
	assert.False(t, ok, "another conversation")
	_, ok = continuation(option.None[seen](), "c", messageHashes(next), 0, 0)
	assert.False(t, ok, "unknown parent")

	rewritten := append([]openaichat.Message(nil), next...)
	rewritten[1] = user("q1, edited")
	_, ok = continuation(parent, "c", messageHashes(rewritten), 0, 0)
	assert.False(t, ok, "a rewritten prefix keeps it all")
	_, ok = continuation(parent, "c", messageHashes(first[:2]), 0, 0)
	assert.False(t, ok, "shorter than the parent")

	// The window: the system message kept, q1 and a1 left out, declared.
	window := []openaichat.Message{first[0], user("q2")}
	covered, ok = continuation(parent, "c", messageHashes(window), 1, 3)
	assert.True(t, ok)
	assert.Equal(t, 1, covered, "only the system message is the parent's")
	_, ok = continuation(parent, "c", messageHashes(window), 0, 0)
	assert.False(t, ok, "the same window undeclared is a rewrite")
	_, ok = continuation(parent, "c", messageHashes(window), 1, 2)
	assert.False(t, ok, "a declaration that does not match")
	_, ok = continuation(parent, "c", messageHashes(window), 1, 9)
	assert.False(t, ok, "a range past the conversation")

	withCall := append([]openaichat.Message(nil), first...)
	withCall[2].ToolCalls = []openaichat.ToolCall{{Id: "t", Name: "n", Arguments: "{}"}}
	assert.NotEqual(t, messageHashes(first)[2], messageHashes(withCall)[2], "tool calls are part of a message")
}

// plan places a window's new messages after the parent's whole
// conversation, and the logical conversation grows by what is new.
func TestPlanContinuesTheLogicalConversation(t *testing.T) {
	first := []openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: "s"}, user("q1"), {Role: openaichat.ChatRoleAssistant, Content: "a1"}}
	parent := option.Some(seen{conversation: "c", hashes: messageHashes(first)})
	tr := turn{conversation: "c", messages: []openaichat.Message{first[0], user("q2")}, omitFrom: 1, omitTo: 3,
		reply: option.Some(openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: "a2"})}
	tr.plan(parent)
	assert.Equal(t, 3, tr.from)
	assert.Equal(t, 1, tr.covered, "the system message is the parent's")
	logical := tr.logical()
	assert.Len(t, logical, 5, "s, q1, a1 from the parent; q2, a2 new")
	assert.Equal(t, messageHashes(first), logical[:3])

	whole := turn{conversation: "c", messages: first[:2]}
	whole.plan(option.None[seen]())
	assert.Zero(t, whole.from)
	assert.Zero(t, whole.covered, "no parent seen: the whole request is new")
}

// The audit row never carries text; the body does, and the last row alone
// the reasoning. Images keep their digest, not their bytes.
func TestMessageRows(t *testing.T) {
	rec := CallRecord{CallId: "llm-1", At: time.Unix(1700000000, 0), Sender: appId, Sensitivity: queryengine.SensitivityConfined}
	ms := []openaichat.Message{
		{Role: openaichat.ChatRoleUser, Content: "q", Images: []openaichat.Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}}},
		{Role: openaichat.ChatRoleAssistant, Content: "a", ToolCalls: []openaichat.ToolCall{{Id: "t1", Name: "n", Arguments: `{"x":1}`}}},
		{Role: openaichat.ChatRoleTool, Content: "42", ToolCallId: "t1"},
	}
	rows := messageRows(rec, ms[:2], 0, "because")
	require.Len(t, rows, 2)
	assert.Equal(t, "user", rows[0].audit.Role)
	assert.EqualValues(t, 1, rows[0].audit.Bytes)
	assert.Equal(t, trail.ContentDigest("q"), rows[0].audit.Digest)
	assert.Empty(t, rows[0].body.Reasoning)
	require.Len(t, rows[0].audit.Images, 1)
	assert.True(t, strings.HasPrefix(rows[0].audit.Images[0], "image/png ") && strings.HasSuffix(rows[0].audit.Images[0], " 3"))
	assert.EqualValues(t, 1, rows[1].audit.Ordinal)
	assert.Equal(t, "because", rows[1].body.Reasoning)
	assert.Equal(t, []string{"t1"}, rows[1].audit.ToolCallIds, "the audit follows a tool call without the text")
	assert.Equal(t, []string{"n"}, rows[1].audit.ToolNames)
	assert.Equal(t, []string{`{"id":"t1","name":"n","arguments":"{\"x\":1}"}`}, rows[1].body.ToolCalls)
	assert.Equal(t, "confined", rows[1].audit.Sensitivity, "the call's label on every row")
	tool := messageRows(rec, ms[2:], 7, "")
	assert.EqualValues(t, 7, tool[0].audit.Ordinal, "ordinals continue from first")
	assert.Equal(t, "t1", tool[0].audit.ToolCallId.Val)
}

// What a call belongs to survives its row and context components.
func TestCallRowRoundTrip(t *testing.T) {
	rec := CallRecord{CallId: "llm-x-2", At: time.Unix(1700000000, 0).UTC(), Sender: appId, SenderInstance: 7,
		Conversation: "c", Turn: "t1", Round: 2, ParentCallId: "llm-x-1", Task: "task-1", TaskEpoch: 3, TaskCall: "task-1-9",
		ProviderId: "chatcmpl-1", ReportedModel: "m-2026", ToolsDigest: "d", MaxTokens: 512,
		Durable: true, RetainAsked: true, Kept: true, MessagesFrom: 3, HistoryHash: "ab", OmitFrom: 1, OmitTo: 3}
	assert.Equal(t, rec, RecordOf(entityOf(rec, true)))

	bare := entityOf(CallRecord{CallId: "c"}, false)
	assert.False(t, bare.Conversation.Has || bare.Delegation.Has)
	row := bare.LlmCall.Val
	assert.False(t, row.Parent.Has || row.ProviderId.Has || row.OmitTo.Has)
	assert.Equal(t, "not-asked", row.Retention)
	assert.Equal(t, "not-kept", RowOf(CallRecord{CallId: "c", RetainAsked: true}, false).Retention)
}

// Over clickhouse-local, without Keep: the call and each of its messages
// land as audit rows before the provider is asked, with the conversation,
// turn and round the request named, and no text anywhere.
func TestTrailIsWrittenAheadWithoutKeep(t *testing.T) {
	exec := localFacts(t)
	ctx := context.Background()
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a1", FinishReason: "stop", Id: "chatcmpl-9", Model: "m-reported"}}
	cfg := localCfg(p)
	cfg.Trail = trail.NewRecorder(exec, "run-test", zerolog.Nop())
	t.Cleanup(cfg.Trail.Close)
	cli, svc, _ := serve(t, cfg)
	var ahead []*trail.TrailEntity
	p.during = func() { ahead = scanMessages(t, svc) }

	res, err := cli.Complete(ctx, Request{Purpose: "test/ask", Conversation: "c1", Turn: "t1", Round: 1,
		Messages: []openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: "s"}, user("q1")},
		Tools:    []openaichat.Tool{{Name: "n", Description: "d"}}})
	require.NoError(t, err)
	assert.Equal(t, RetentionNotAsked, res.Retention)
	require.Len(t, ahead, 2, "the request's messages were durable while the provider answered")

	ms := scanMessages(t, svc)
	require.Len(t, ms, 3, "system, user, reply")
	for _, m := range ms {
		assert.False(t, m.LlmMessageBody.Has, "no text without Keep")
		require.True(t, m.Origin.Has)
		assert.Equal(t, "run-test", m.Origin.Val.Run)
		assert.Equal(t, string(appId), m.Origin.Val.App)
		require.True(t, m.Conversation.Has)
		assert.Equal(t, "t1", m.Conversation.Val.Turn.Val)
	}
	calls, err := svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	c := calls[0]
	assert.Equal(t, "c1", c.Conversation)
	assert.Equal(t, "t1", c.Turn)
	assert.Equal(t, 1, c.Round)
	assert.Equal(t, "chatcmpl-9", c.ProviderId)
	assert.Equal(t, "m-reported", c.ReportedModel)
	assert.NotEmpty(t, c.ToolsDigest)
	assert.False(t, c.RetainAsked || c.Kept)
	ring := svc.Calls()
	require.Len(t, ring, 1)
	assert.True(t, ring[0].Durable)
}

// BOXER_TRAIL_REQUIRED: a call whose trail row cannot be written does not
// leave the machine; without it the call proceeds and says it is not durable.
func TestTrailRequiredRefusesWithoutABackend(t *testing.T) {
	for _, required := range []bool{false, true} {
		trail.RequiredEnv.SetForTest(t, strconv.FormatBool(required))
		p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a", FinishReason: "stop"}}
		cfg := localCfg(p)
		cfg.Trail = trail.NewRecorder(nil, "run-test", zerolog.Nop())
		cli, svc, _ := serve(t, cfg)
		p.seen = openaichat.CompletionRequest{}
		_, err := cli.Complete(context.Background(), Request{Purpose: "test/ask", Messages: []openaichat.Message{user("hi")}})
		var refused *RefusedError
		if required {
			require.True(t, errors.As(err, &refused), "%v", err)
			assert.Contains(t, refused.Reason, "BOXER_TRAIL_REQUIRED")
			assert.Empty(t, p.seen.Messages, "the provider was not asked")
		} else {
			require.NoError(t, err)
			assert.False(t, svc.Calls()[0].Durable)
		}
		cfg.Trail.Close()
	}
}

// Over clickhouse-local: two turns and a branch land as llmMessage rows,
// each turn keeping only what is new; then the SD6 ditch removes the text
// — one app's, then all — while every llmCall row stays.
func TestRetainedConversationLandsAndDitches(t *testing.T) {
	exec := localFacts(t)
	ctx := context.Background()

	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a1", Reasoning: "r1", FinishReason: "stop"}}
	cfg := localCfg(p)
	cfg.Trail, cfg.Retain = trail.NewRecorder(exec, "run-test", zerolog.Nop()), RetainDurable
	t.Cleanup(cfg.Trail.Close)
	bus := inprocbus.NewInst(zerolog.Nop())
	cli, svc := serveRetaining(t, bus, cfg, appId)

	sys := openaichat.Message{Role: openaichat.ChatRoleSystem, Content: "s"}
	hist := []openaichat.Message{sys, user("q1")}
	r1, err := cli.Complete(ctx, Request{Retain: true, Conversation: "c1", Sensitivity: queryengine.SensitivityConfined, Messages: hist})
	require.NoError(t, err)
	require.Equal(t, RetentionKept, r1.Retention, r1.RetentionReason)

	p.resp = openaichat.CompletionResponse{Content: "a2", FinishReason: "stop"}
	hist = append(hist, openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: r1.Content}, user("q2"))
	r2, err := cli.Complete(ctx, Request{Retain: true, Conversation: "c1", ParentCallId: r1.CallId, Messages: hist})
	require.NoError(t, err)
	require.Equal(t, RetentionKept, r2.Retention)

	// A sliding window: the system message kept, q1 and a1 left out and
	// declared; then the same window one turn later, now also leaving out
	// q2 and a2. Each keeps only its new user message and reply, at
	// ordinals that continue the conversation.
	p.resp = openaichat.CompletionResponse{Content: "a3", FinishReason: "stop"}
	win := []openaichat.Message{sys, user("q2"), {Role: openaichat.ChatRoleAssistant, Content: "a2"}, user("q3")}
	r4, err := cli.Complete(ctx, Request{Retain: true, Conversation: "c1", ParentCallId: r2.CallId, OmitFrom: 1, OmitTo: 3, Messages: win})
	require.NoError(t, err)
	require.Equal(t, RetentionKept, r4.Retention)
	p.resp = openaichat.CompletionResponse{Content: "a4", FinishReason: "stop"}
	win = []openaichat.Message{sys, user("q3"), {Role: openaichat.ChatRoleAssistant, Content: "a3"}, user("q4")}
	r5, err := cli.Complete(ctx, Request{Retain: true, Conversation: "c1", ParentCallId: r4.CallId, OmitFrom: 1, OmitTo: 5, Messages: win})
	require.NoError(t, err)
	require.Equal(t, RetentionKept, r5.Retention)

	branch := append([]openaichat.Message(nil), hist...)
	branch[1] = user("q1, edited")
	r3, err := cli.Complete(ctx, Request{Retain: true, Conversation: "c1", ParentCallId: r1.CallId, Messages: branch})
	require.NoError(t, err)
	require.Equal(t, RetentionKept, r3.Retention)

	other := retainingClient(bus, "test.llm.other")
	_, err = other.Complete(ctx, Request{Retain: true, Conversation: "o1", Messages: []openaichat.Message{user("hello")}})
	require.NoError(t, err)

	type kept struct {
		trail.LlmMessage
		body trail.LlmMessageBody
		app  string
	}
	messages := func() (byCall map[string][]kept, n int, withText int) {
		byCall = map[string][]kept{}
		for _, ent := range scanMessages(t, svc) {
			m := kept{LlmMessage: ent.LlmMessage.Val, body: ent.LlmMessageBody.Val, app: ent.Origin.Val.App}
			byCall[m.CallId] = append(byCall[m.CallId], m)
			n++
			if ent.LlmMessageBody.Has {
				withText++
			}
		}
		for _, ms := range byCall {
			// The scan orders by (ts, id); a call's rows share ts, so order
			// them by their place in the conversation.
			slices.SortFunc(ms, func(a, b kept) int { return int(a.Ordinal) - int(b.Ordinal) })
		}
		return
	}
	byCall, n, withText := messages()
	assert.Equal(t, 16, n, "fourteen of this app's, two of the other's")
	assert.Equal(t, 16, withText, "every kept message carries its text on the audit row")
	require.Len(t, byCall[r1.CallId], 3, "system, user, reply")
	assert.Equal(t, "r1", byCall[r1.CallId][2].body.Reasoning)
	assert.Equal(t, "confined", byCall[r1.CallId][0].Sensitivity)
	require.Len(t, byCall[r2.CallId], 2, "only what is new: q2 and its reply")
	assert.EqualValues(t, 3, byCall[r2.CallId][0].Ordinal)
	assert.Equal(t, "q2", byCall[r2.CallId][0].body.Content)
	assert.Len(t, byCall[r3.CallId], 5, "a rewritten prefix writes the whole history")
	require.Len(t, byCall[r4.CallId], 2, "a declared window writes only what is new")
	assert.EqualValues(t, 5, byCall[r4.CallId][0].Ordinal, "after s, q1, a1, q2, a2")
	assert.Equal(t, "q3", byCall[r4.CallId][0].body.Content)
	require.Len(t, byCall[r5.CallId], 2, "the window slid and still only the new is written")
	assert.EqualValues(t, 7, byCall[r5.CallId][0].Ordinal)
	assert.Equal(t, "a4", byCall[r5.CallId][1].body.Content)

	calls, err := svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, calls, 6)
	for _, c := range calls {
		if c.CallId == r2.CallId {
			assert.Equal(t, r1.CallId, c.ParentCallId)
			assert.Equal(t, 3, c.MessagesFrom)
			assert.True(t, c.Kept)
		}
		if c.CallId == r3.CallId {
			assert.Equal(t, 0, c.MessagesFrom, "marked: a parent, yet written from 0")
		}
		if c.CallId == r5.CallId {
			assert.Equal(t, 1, c.OmitFrom)
			assert.Equal(t, 5, c.OmitTo)
			assert.Equal(t, 7, c.MessagesFrom)
		}
	}

	// The ditch empties the text and leaves the audit: every message row
	// stays, with its size, digest and place.
	require.NoError(t, exec.Exec(ctx, trail.DitchBodiesSQL("", "test.llm.other")))
	_, n, withText = messages()
	assert.Equal(t, 16, n, "no row is removed")
	assert.Equal(t, 14, withText, "the other app's text is gone, this app's stays")
	require.NoError(t, exec.Exec(ctx, trail.DitchBodiesSQL("", "")))
	byCall, n, withText = messages()
	assert.Equal(t, 16, n)
	assert.Zero(t, withText, "no text is left")
	assert.EqualValues(t, 2, byCall[r2.CallId][0].Bytes, "the audit survives the ditch")
	assert.Equal(t, trail.ContentDigest("q2"), byCall[r2.CallId][0].Digest)
	calls, err = svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 100)
	require.NoError(t, err)
	assert.Len(t, calls, 6, "the counts survive the ditch")
}

// localFacts is a clickhouse-local executor holding an empty boxer.facts;
// the test is skipped where there is no clickhouse.
func localFacts(t *testing.T) (exec recordstore.ExecutorI) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(context.Background(), stmt))
		}
	}
	return
}

// scanMessages are the message rows on the trail, each with every component
// its row carries.
func scanMessages(t *testing.T, svc *Service) (ents []*trail.TrailEntity) {
	t.Helper()
	ents, err := svc.cfg.Trail.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanLlmMessage(context.Background(), recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	return
}
