package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
)

// fakeModel answers "a<n>" to the n-th call and records what it saw.
type fakeModel struct {
	calls int
	seen  []openaichat.CompletionRequest
}

func (f *fakeModel) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.calls++
	f.seen = append(f.seen, req)
	return openaichat.CompletionResponse{Content: "a" + string(rune('0'+f.calls)), FinishReason: "stop", InputTokens: 10, OutputTokens: 5}, nil
}
func (f *fakeModel) Close() (err error) { return }

// host is the llm service on an in-process bus and a client holding the
// app's manifest caps.
func host(t *testing.T, cfg llm.Config) (cli *llm.Client, svc *llm.Service, fm *fakeModel) {
	t.Helper()
	fm = &fakeModel{}
	cfg.Endpoint, cfg.Model, cfg.Client = "http://127.0.0.1:1234/v1", "m", fm
	bus := inprocbus.NewInst(zerolog.Nop())
	var err error
	svc, err = llm.NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli = llm.NewClient(bus.NewClient(ManifestId, manifest.Caps))
	cli.Timeout = 5 * time.Second
	return
}

// keeping is a conversation whose first send asked to keep it.
func keeping() (conv *conversation) {
	conv = newConversation()
	conv.keep = true
	return
}

// turn runs one turn the way the app does: request, complete, land.
func turn(t *testing.T, cli *llm.Client, conv *conversation, text string) (res llm.Response) {
	t.Helper()
	req := conv.request(text)
	conv.begin(text, time.Now().UnixMilli(), false)
	res, err := cli.Complete(context.Background(), req)
	if err != nil {
		conv.land(req, nil, err, time.Now().UnixMilli())
		return
	}
	conv.land(req, &res, nil, time.Now().UnixMilli())
	return
}

// ADR-0265 §SD3: the second turn names the first reply as parent and
// resends it verbatim; the manifest's grants are enough for both subjects.
func TestSecondTurnContinuesTheFirst(t *testing.T) {
	cli, svc, fm := host(t, llm.Config{Retain: llm.RetainRing})
	conv := keeping()
	r1 := turn(t, cli, conv, "q1")
	require.NotEmpty(t, r1.CallId)
	turn(t, cli, conv, "q2")

	require.Len(t, fm.seen, 2)
	second := fm.seen[1].Messages
	require.Len(t, second, 3)
	assert.Equal(t, openaichat.ChatRoleAssistant, second[1].Role)
	assert.Equal(t, "a1", second[1].Content, "the reply resent exactly as it came back")

	calls := svc.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, conv.id, calls[0].Conversation)
	assert.Empty(t, calls[0].ParentCallId, "the first turn has no parent")
	assert.Equal(t, calls[0].CallId, calls[1].ParentCallId)
	assert.Equal(t, "ring, not durable", strings.TrimPrefix(conv.notKept, "this host's BOXER_LLM_RETAIN is "))
	assert.EqualValues(t, 15, conv.lastIn+conv.lastOut)
}

// A failed turn stays in the transcript and is never resent or a parent.
func TestFailedTurnIsNotAParent(t *testing.T) {
	conv := keeping()
	req := conv.request("q1")
	conv.begin("q1", 1, false)
	conv.land(req, &llm.Response{Content: "a1", CallId: "call-1"}, nil, 2)

	req = conv.request("q2")
	conv.begin("q2", 3, false)
	conv.land(req, nil, errors.New("boom"), 4)
	require.Len(t, conv.entries, 3)
	assert.True(t, conv.entries[2].failed)
	assert.Equal(t, "boom", conv.entries[2].reason)

	next := conv.request("q3")
	assert.Equal(t, "call-1", next.ParentCallId, "continues from the last reply")
	require.Len(t, next.Messages, 3, "q1, a1, q3 — the failed q2 is not resent")
	assert.Equal(t, "q3", next.Messages[2].Content)
	assert.Equal(t, "cancelled", failureReason(context.Canceled))
}

// Keep off sends on llm.complete: the call carries no conversation.
func TestKeepOffIsNotRetained(t *testing.T) {
	cli, svc, _ := host(t, llm.Config{Retain: llm.RetainDurable})
	conv := newConversation()
	res := turn(t, cli, conv, "q1")
	assert.Equal(t, llm.RetentionNotAsked, res.Retention)
	require.Len(t, svc.Calls(), 1)
	assert.Empty(t, svc.Calls()[0].Conversation)
	assert.Empty(t, conv.notKept)
}

// New conversations get fresh ids.
func TestNewConversationMintsAFreshId(t *testing.T) {
	a, b := newConversation(), newConversation()
	assert.NotEqual(t, a.id, b.id)
	assert.True(t, strings.HasPrefix(a.id, "chat-"))
}

// The transcript model validates, marks a failure on its own bubble and
// shows the waiting bubble last; ordinal i is entry i.
func TestTranscriptModel(t *testing.T) {
	conv := keeping()
	conv.entries = []entry{
		{speaker: speakerUser, text: "q1", atMs: 10},
		{speaker: speakerModel, text: "a1", atMs: 5},
		{speaker: speakerUser, text: "q2", atMs: 20, failed: true, reason: "boom", edited: true},
	}
	m, kinds := transcriptModel(conv, true, 30)
	require.NoError(t, m.Validate(), "times are kept ascending")
	require.Equal(t, 4, m.Len())
	assert.EqualValues(t, 1, m.Sender[1])
	assert.Equal(t, chatview.StatusFailed, m.Status[2])
	assert.NotZero(t, m.Flags[2]&chatview.FlagEdited)
	assert.Equal(t, 2, kinds[2].entry)
	assert.True(t, kinds[3].pending)
	before := m.Body[2]
	conv.entries[2].failed = false
	m, _ = transcriptModel(conv, false, 30)
	assert.NotEqual(t, before, m.Body[2], "a bubble whose actions change is measured again")
}

// Regenerate and Edit take the last answered turn back: the next request
// is the one that turn sent, from the same parent — a branch.
func TestTheLastTurnCanBeTakenBack(t *testing.T) {
	cli, _, _ := host(t, llm.Config{Retain: llm.RetainRing})
	conv := keeping()
	r1 := turn(t, cli, conv, "q1")
	want := conv.request("q2")
	turn(t, cli, conv, "q2")
	require.Len(t, conv.entries, 4)
	require.True(t, conv.canRewind())

	text, undo, ok := conv.rewind()
	require.True(t, ok)
	assert.Equal(t, "q2", text)
	assert.Len(t, conv.entries, 2)
	assert.False(t, conv.canRewind(), "one turn back, not two")
	again := conv.request(text)
	assert.Equal(t, r1.CallId, again.ParentCallId)
	assert.Equal(t, want.Messages, again.Messages)

	require.True(t, conv.restore(undo))
	assert.Len(t, conv.entries, 4)
	assert.True(t, conv.canRewind())
	assert.Equal(t, "a2", conv.entries[3].text)

	_, undo, _ = conv.rewind()
	turn(t, cli, conv, "q2 edited")
	assert.False(t, conv.restore(undo), "a turn sent since leaves nothing to put back")
}

// A failed turn is taken back from the transcript alone; it is no
// answered turn to rewind.
func TestAFailedTurnIsDroppedNotRewound(t *testing.T) {
	conv := keeping()
	req := conv.request("q1")
	conv.begin("q1", 1, false)
	conv.land(req, &llm.Response{Content: "a1", CallId: "call-1"}, nil, 2)
	req = conv.request("q2")
	conv.begin("q2", 3, false)
	conv.land(req, nil, &llm.CallError{Kind: "server", Reason: "HTTP 503: overloaded", CallId: "call-2"}, 4)
	assert.False(t, conv.canRewind(), "a failure after a turn leaves it in place")
	f := conv.entries[2].fail
	assert.Equal(t, "server", f.kind)
	assert.Equal(t, "call-2", f.callId)
	assert.Contains(t, f.detail, "HTTP 503")

	text, ok := conv.dropFailed()
	require.True(t, ok)
	assert.Equal(t, "q2", text)
	assert.Len(t, conv.entries, 2)
	_, ok = conv.dropFailed()
	assert.False(t, ok, "the answered turn is not dropped")
}

// Retry sends the failed turn again; Edit puts an answered one in the
// composer and marks the resend edited.
func TestRetryAndEditOnTheApp(t *testing.T) {
	inst := appOn(t, &fakeModel{})
	inst.conv.begin("q0", 1, false)
	inst.conv.land(inst.conv.request("q0"), nil, errors.New("boom"), 2)
	inst.retry()
	drainUntil(t, inst)
	require.Len(t, inst.conv.entries, 2)
	assert.False(t, inst.conv.entries[0].failed)
	assert.Equal(t, "q0", inst.conv.entries[0].text)

	inst.edit()
	assert.Equal(t, "q0", inst.draft)
	require.NotNil(t, inst.editing)
	assert.Empty(t, inst.conv.entries)
	require.True(t, inst.startTurn("q0 edited"))
	drainUntil(t, inst)
	assert.True(t, inst.conv.entries[0].edited)
	assert.Nil(t, inst.editing)

	require.True(t, inst.startTurn("q1"))
	inst.regenerate()
	assert.Contains(t, inst.note.text, "a turn is running", "an action during a turn says why it did nothing")
	drainUntil(t, inst)
}

// A failure's call opens in play on its row.
func TestCallRecordSql(t *testing.T) {
	assert.Equal(t, "SELECT *\nFROM keelson('llm_calls')\nWHERE call_id = 'run-1\\'x'", callRecordSql("run-1'x"))
}

// Over clickhouse-local with the host's ceiling durable: the second turn
// keeps only its new messages — what breaks if the app appends the reply
// differently from how it came back (ADR-0265 verification plan).
func TestKeptTurnsStoreOnlyWhatIsNew(t *testing.T) {
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
	cli, svc, _ := host(t, llm.Config{Retain: llm.RetainDurable, Exec: exec})
	conv := keeping()
	r1 := turn(t, cli, conv, "q1")
	require.Equal(t, llm.RetentionKept, r1.Retention, r1.RetentionReason)
	r2 := turn(t, cli, conv, "q2")
	require.Equal(t, llm.RetentionKept, r2.Retention, r2.RetentionReason)
	assert.Empty(t, conv.notKept)

	calls := svc.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, 0, calls[0].RetainedFrom)
	assert.Equal(t, 2, calls[1].RetainedFrom, "q1 and a1 were kept by the first turn")
}

// appOn is the app mounted on a host whose model is fm, without a frame:
// startTurn and drain touch no UI.
func appOn(t *testing.T, fm openaichat.ClientI) (inst *App) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: fm, Retain: llm.RetainRing})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	inst = newApp()
	inst.cli = llm.NewClient(bus.NewClient(ManifestId, manifest.Caps))
	inst.cli.Timeout = 5 * time.Second
	return
}

// drainUntil drains until the turn in flight has landed.
func drainUntil(t *testing.T, inst *App) {
	t.Helper()
	require.Eventually(t, func() bool { inst.drain(); return inst.pending == nil }, 5*time.Second, 5*time.Millisecond)
}

// blockingModel answers only when its context ends.
type blockingModel struct{}

func (blockingModel) Complete(ctx context.Context, _ openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	<-ctx.Done()
	return openaichat.CompletionResponse{}, ctx.Err()
}
func (blockingModel) Close() (err error) { return }

// Cancel lands the turn as failed and frees the composer: bgjob resets a
// cancelled run to idle, with neither a result nor an error.
func TestCancelLandsAndFreesTheComposer(t *testing.T) {
	inst := appOn(t, blockingModel{})
	require.True(t, inst.startTurn("q1"))
	assert.False(t, inst.startTurn("q2"), "one turn at a time")
	inst.turn.Cancel()
	drainUntil(t, inst)
	require.Len(t, inst.conv.entries, 1)
	assert.True(t, inst.conv.entries[0].failed)
	assert.Equal(t, "cancelled", inst.conv.entries[0].reason)
	assert.Empty(t, inst.conv.history, "a cancelled turn is not resent")
}

// The first send takes the Keep toggle; later changes do not reach the
// started conversation. New conversation drops a turn in flight.
func TestKeepIsFixedAtTheFirstSendAndNewConversationStartsOver(t *testing.T) {
	inst := appOn(t, &fakeModel{})
	inst.keep = false
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	inst.keep = true
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	assert.False(t, inst.conv.keep)
	label, _ := keepBadge(inst.conv)
	assert.Equal(t, "not kept", label)

	inst = appOn(t, blockingModel{})
	old := inst.conv
	require.True(t, inst.startTurn("q1"))
	inst.newConversation()
	assert.NotSame(t, old, inst.conv)
	assert.Nil(t, inst.pending)
	assert.True(t, inst.startTurn("q2"), "the invalidated run no longer blocks a new one")
	assert.False(t, inst.startTurn("   "), "a blank draft is not sent")
}

// The badge says what the host did.
func TestKeepBadge(t *testing.T) {
	conv := keeping()
	label, _ := keepBadge(conv)
	assert.Equal(t, "keep asked", label, "no verdict yet")
	conv.kept = true
	label, _ = keepBadge(conv)
	assert.Equal(t, "kept", label)
	conv.notKept = "ring"
	label, _ = keepBadge(conv)
	assert.Equal(t, "not kept", label, "a decline wins over an earlier keep")
}

func TestAnOutOfCreditFailureSaysSo(t *testing.T) {
	err := fmt.Errorf("llm: HTTP 402: This request requires more credits: %w", openaichat.ErrPaymentRequired)
	assert.Equal(t, "the model provider is out of credit or quota for this account (HTTP 402: This request requires more credits)", failureReason(err))
	err = fmt.Errorf("llm: openaichat: non-2xx response: HTTP 503: overloaded: %w", openaichat.ErrServer)
	assert.Equal(t, "the model provider answered HTTP 503: overloaded", failureReason(err))
}
