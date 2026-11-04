package llm

import (
	"context"
	"encoding/json/jsontext"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// journaled opens a run in the sender's name with one request entry for req
// and names the entry on it, as a harness does.
func journaled(t *testing.T, store agentrun.StoreI, req Request) (Request, *agentrun.Writer) {
	t.Helper()
	ctx := context.Background()
	w, err := agentrun.Open(ctx, store, agentrun.Run{Conversation: "c", OwnerApp: string(appId), Coordinator: "w"})
	require.NoError(t, err)
	digest, err := req.ContentDigest()
	require.NoError(t, err)
	written, err := w.Append(ctx, agentrun.Entry{Kind: agentrun.KindRequest, Key: "t#0", Name: digest, Turn: "t"})
	require.NoError(t, err)
	req.Run, req.Journal = w.Run().Id, JournalKey(w.Run().Id, written[0].Ordinal)
	return req, w
}

func aRequest(text string) Request {
	return Request{Conversation: "c", Turn: "t", Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: text}}}
}

func TestACallNamingItsRequestEntryGoes(t *testing.T) {
	store := agentrun.NewMemStore()
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop"}}
	cfg := localCfg(p)
	cfg.Journal = StoreJournal(store)
	cli, svc, _ := serve(t, cfg)
	req, w := journaled(t, store, aRequest("hi"))
	_, err := cli.Complete(context.Background(), req)
	require.NoError(t, err)
	calls := svc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, w.Entries()[0].EntryId, calls[0].RequestEntry, "the call's row names the entry")
}

func TestTheServiceRefusesACallTheJournalDoesNotCover(t *testing.T) {
	store := agentrun.NewMemStore()
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop"}}
	cfg := localCfg(p)
	cfg.Journal = StoreJournal(store)
	cli, svc, _ := serve(t, cfg)
	ctx := context.Background()

	refused := func(t *testing.T, req Request, why string) {
		t.Helper()
		before := len(svc.Calls())
		_, err := cli.Complete(ctx, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), why)
		calls := svc.Calls()
		require.Len(t, calls, before+1, "the refusal is on the record")
		assert.True(t, calls[len(calls)-1].Refused)
	}

	t.Run("no key", func(t *testing.T) {
		refused(t, aRequest("hi"), "names no journal entry")
	})
	t.Run("a key that is not one", func(t *testing.T) {
		req := aRequest("hi")
		req.Journal = "nonsense"
		refused(t, req, "not <run>#<ordinal>")
	})
	t.Run("no such run", func(t *testing.T) {
		req := aRequest("hi")
		req.Journal = JournalKey(987654, 1)
		refused(t, req, "no such run")
	})
	t.Run("no such entry", func(t *testing.T) {
		req, w := journaled(t, store, aRequest("hi"))
		req.Journal = JournalKey(w.Run().Id, 99)
		refused(t, req, "no such entry")
	})
	t.Run("another kind of entry", func(t *testing.T) {
		req, w := journaled(t, store, aRequest("hi"))
		written, err := w.Append(ctx, agentrun.Entry{Kind: agentrun.KindNote, Name: "n", Body: "b"})
		require.NoError(t, err)
		req.Journal = JournalKey(w.Run().Id, written[0].Ordinal)
		refused(t, req, "not a request")
	})
	t.Run("recorded one thing, sent another", func(t *testing.T) {
		req, _ := journaled(t, store, aRequest("what was recorded"))
		req.Messages = aRequest("what was sent").Messages
		refused(t, req, "content differs")
	})
	t.Run("another run than the request names", func(t *testing.T) {
		req, _ := journaled(t, store, aRequest("hi"))
		req.Run++
		refused(t, req, "another run")
	})
	t.Run("a closed run", func(t *testing.T) {
		req, w := journaled(t, store, aRequest("hi"))
		require.NoError(t, w.Close(ctx, agentrun.StateClosed))
		refused(t, req, "not open")
	})
	t.Run("another app's run", func(t *testing.T) {
		w, err := agentrun.Open(ctx, store, agentrun.Run{Conversation: "c", OwnerApp: "someone-else", Coordinator: "w"})
		require.NoError(t, err)
		req := aRequest("hi")
		digest, err := req.ContentDigest()
		require.NoError(t, err)
		written, err := w.Append(ctx, agentrun.Entry{Kind: agentrun.KindRequest, Key: "t#0", Name: digest})
		require.NoError(t, err)
		req.Journal = JournalKey(w.Run().Id, written[0].Ordinal)
		refused(t, req, "no such run")
	})
	assert.Zero(t, p.calls.Load(), "nothing left")
}

// With no checker wired every completion is refused (ADR-0357 §SD4).
func TestAServiceWithNoJournalRefuses(t *testing.T) {
	cfg := localCfg(&fakeProvider{resp: openaichat.CompletionResponse{Content: "out"}})
	cfg.Journal = nil
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient(appId, ClientCaps("test: ask")))
	cli.Timeout = 5 * time.Second
	_, err = cli.Complete(context.Background(), aRequest("hi"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no run journal")
}

// Both sides compute the digest: the harness from the request, the service
// from what the bus delivered. Every field must survive the trip.
func TestTheContentDigestSurvivesTheBus(t *testing.T) {
	temp, seed := float32(0.3), int64(7)
	req := Request{
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: "s"},
			{Role: openaichat.ChatRoleUser, Content: "u", Images: []openaichat.Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}}},
			{Role: openaichat.ChatRoleAssistant, ToolCalls: []openaichat.ToolCall{{Id: "a", Name: "f", Arguments: `{"b":1,"a":2}`}}},
			{Role: openaichat.ChatRoleTool, ToolCallId: "a", Content: "r"},
		},
		Temperature: &temp, Seed: &seed, MaxTokens: 9, Stop: []string{"x"}, EnableThinking: true, ToolChoice: "auto",
		Tools: []openaichat.Tool{
			{Name: "f", Description: "d", Parameters: jsontext.Value(`{"type":"object","properties":{"z":{},"a":{}}}`)},
			{Name: "no-args"},
		},
		ResponseFormat: openaichat.JSONObjectFormat(),
		Extra:          map[string]any{"z": 1, "a": map[string]any{"y": true, "b": "c"}, "m": []any{1, 2}},
	}
	want, err := req.ContentDigest()
	require.NoError(t, err)
	extra, err := encodeExtra(req.Extra)
	require.NoError(t, err)
	wire := wireRequest{Messages: req.Messages, Temperature: req.Temperature, MaxTokens: req.MaxTokens, Seed: req.Seed, Stop: req.Stop,
		EnableThinking: req.EnableThinking, Tools: req.Tools, ToolChoice: req.ToolChoice, ResponseFormat: req.ResponseFormat, Extra: extra,
		Approval: "1#2", Journal: "1#1", CancelKey: "routing, not content"}
	b, err := buscodec.Encode(wire)
	require.NoError(t, err)
	got, err := buscodec.Decode[wireRequest](b)
	require.NoError(t, err)
	d, err := got.content().digest()
	require.NoError(t, err)
	assert.Equal(t, want, d)

	for i := 0; i < 20; i++ {
		again, derr := req.ContentDigest()
		require.NoError(t, derr)
		require.Equal(t, want, again, "a map's order does not move it")
	}
	req.Messages[1].Content = "changed"
	moved, err := req.ContentDigest()
	require.NoError(t, err)
	assert.NotEqual(t, want, moved)
}

// The digest leaves the approval out: an approved resend presents the same
// entry (ADR-0357 §SD2).
func TestAnApprovalDoesNotMoveTheDigest(t *testing.T) {
	a := aRequest("hi")
	b := a
	b.Approval = "1#9"
	da, err := a.ContentDigest()
	require.NoError(t, err)
	db, err := b.ContentDigest()
	require.NoError(t, err)
	assert.Equal(t, da, db)
}
