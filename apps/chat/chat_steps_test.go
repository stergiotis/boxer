package chat

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The tool loop records each model call and each tool call as it happens,
// and a landed turn's tool entries keep their steps to inspect.
func TestTheStepsRecordEveryStep(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}],"title":"Asking"}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","text":"tidied"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)

	kinds := make([]stepKindE, 0, len(res.steps))
	for _, s := range res.steps {
		kinds = append(kinds, s.kind)
		assert.True(t, s.done, "a landed turn has no running step")
	}
	assert.Equal(t, []stepKindE{stepModel, stepTool, stepModel, stepTool, stepModel, stepTool, stepModel, stepTool, stepModel}, kinds)
	first := res.steps[1]
	assert.Equal(t, "request_access", first.name)
	assert.Equal(t, "Asking", first.title)
	assert.Contains(t, first.args, "\n  \"plan\": \"tidy the note\"", "arguments are indented for reading")
	assert.Equal(t, res.activity[0], first.activity)
	assert.Contains(t, res.steps[5].result, `"text":"first"`, "a step keeps what the model read back")
	assert.True(t, res.steps[7].refused, "a refused call is marked")
	assert.Equal(t, 1, res.steps[0].tools)

	conv := newConversation()
	conv.begin("tidy it", 0, false)
	conv.landTurn(req, res, nil, time.Now().UnixMilli())
	var tools []entry
	for _, e := range conv.entries {
		if e.speaker == speakerTool {
			tools = append(tools, e)
		}
	}
	require.Len(t, tools, 4)
	for _, e := range tools {
		require.Len(t, e.steps, 2, "the model call that asked, then the call itself")
		assert.Equal(t, stepModel, e.steps[0].kind)
		assert.Equal(t, stepTool, e.steps[1].kind)
		assert.Equal(t, e.text, e.steps[1].activity)
	}
}

// A compaction before a round's first tool call travels with it, as the
// model call does; one with no tool call after it stays in the running
// steps alone.
func TestStepsOfToolsCarriesACompactionWithTheNextTool(t *testing.T) {
	steps := []turnStep{{kind: stepCompact, name: "elided"}, {kind: stepModel, round: 0}, {kind: stepTool, name: "a"}, {kind: stepTool, name: "b"}}
	got := stepsOfTools(steps)
	require.Len(t, got, 2)
	assert.Equal(t, []stepKindE{stepCompact, stepModel, stepTool}, kindsOf(got[0]))
	assert.Equal(t, []stepKindE{stepTool}, kindsOf(got[1]))
	assert.Contains(t, stepLine(&steps[0]), "compaction · elided")
}

func kindsOf(steps []turnStep) (kinds []stepKindE) {
	for _, s := range steps {
		kinds = append(kinds, s.kind)
	}
	return
}

func TestStepsOfToolsPairsARoundsFirstToolWithItsModelCall(t *testing.T) {
	steps := []turnStep{{kind: stepModel, round: 0}, {kind: stepTool, name: "a"}, {kind: stepTool, name: "b"}, {kind: stepModel, round: 1}}
	got := stepsOfTools(steps)
	require.Len(t, got, 2)
	assert.Len(t, got[0], 2)
	assert.Len(t, got[1], 1, "a second tool of the round carries only itself")
	assert.Equal(t, "b", got[1][0].name)
}

func TestClipKeepsWholeRunes(t *testing.T) {
	s := strings.Repeat("ä", 10)
	got := clip(s, 5)
	assert.True(t, strings.HasPrefix(got, "ää\n… "), got)
	assert.Contains(t, got, "16 bytes more")
	tail := clipTail(s, 5)
	assert.True(t, strings.HasSuffix(tail, "\nää"), tail)
	assert.Equal(t, "short", clip("short", 5))
}

func TestNilStepsRecordNothing(t *testing.T) {
	var tr *turnSteps
	tr.reset()
	i := tr.begin(turnStep{})
	tr.finish(i, func(s *turnStep) { s.name = "x" })
	steps, _, changed := tr.snapshot(0)
	assert.Nil(t, steps)
	assert.False(t, changed)
}

// The person's limit on rounds bounds the turn: its last round offers no
// tools, and a limit outside the configured range is brought into it.
func TestAChatsRoundLimitBoundsTheTurn(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	var replies []openaichat.CompletionResponse
	for i := range 10 {
		replies = append(replies, toolCall("l"+strconv.Itoa(i), "list_windows", `{}`))
	}
	model := &scriptedModel{replies: replies}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	coord.setRounds(5)
	_, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	model.mu.Lock()
	defer model.mu.Unlock()
	require.Len(t, model.seen, 5)
	assert.Equal(t, "none", model.seen[4].ToolChoice)

	lo, hi := roundRange()
	coord.setRounds(1)
	assert.Equal(t, lo, coord.roundLimit())
	coord.setRounds(100000)
	assert.Equal(t, hi, coord.roundLimit())
}

// A tool call waiting on the person says so while it waits, and keeps how
// long it waited once it is done.
func TestATurnStepWaitsOnThePerson(t *testing.T) {
	tr := &turnSteps{}
	tr.begin(turnStep{kind: stepModel})
	i := tr.begin(turnStep{kind: stepTool, name: "call_operation"})
	tr.personWait(true)
	steps, _, _ := tr.snapshot(0)
	assert.True(t, steps[i].waitingOnPerson())
	assert.False(t, steps[0].waitingOnPerson(), "a model call never waits on the person")
	assert.Contains(t, stepLine(&steps[i]), "waiting for you")
	time.Sleep(5 * time.Millisecond)
	tr.personWait(false)
	steps, _, _ = tr.snapshot(0)
	assert.False(t, steps[i].waitingOnPerson())
	assert.GreaterOrEqual(t, steps[i].waited, 5*time.Millisecond)
	tr.personWait(true)
	tr.finish(i, func(*turnStep) {})
	steps, _, _ = tr.snapshot(0)
	assert.False(t, steps[i].waitingOnPerson(), "a finished step waits no more")
	assert.NotContains(t, stepLine(&steps[i]), "waited", "a wait under a second is no decision")
	steps[i].waited = 3 * time.Second
	assert.Contains(t, stepLine(&steps[i]), "waited 3.0 s for you")
}

// A step with nothing to show has nothing to copy.
func TestAStepWithoutDetailHasNone(t *testing.T) {
	assert.False(t, (&turnStep{kind: stepModel, tools: 1}).hasDetail())
	assert.True(t, (&turnStep{kind: stepModel, reasoning: "x"}).hasDetail())
}

// streamingRig is the rig's client with a stream in front of it: each
// call hands on its pieces as deltas and, while it does, records the
// running step as the window would see it.
type streamingRig struct {
	cli    *llm.Client
	coord  *coordinator
	pieces []llm.Delta
	seen   []turnStep
}

func (inst *streamingRig) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return inst.cli.Complete(ctx, req)
}

func (inst *streamingRig) CompleteStreaming(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (llm.Response, error) {
	for _, d := range inst.pieces {
		onDelta(d)
		steps, _, _ := inst.coord.steps.snapshot(0)
		inst.seen = append(inst.seen, steps[len(steps)-1])
	}
	return inst.cli.Complete(ctx, req)
}

// A streaming completer's deltas grow the running model step — its line
// says what the model is doing, and the waiting bubble has the answer so
// far — and the landed step is the reply's (ADR-0286 §SD4).
func TestAStreamedAnswerGrowsTheRunningStep(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{{Content: "the reply", FinishReason: "stop"}}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	rig := &streamingRig{cli: cli, coord: coord, pieces: []llm.Delta{{Reasoning: "hmm"}, {Content: "the "}, {Content: "rep"}}}
	res, err := runTurn(ctx, rig, coord, req, nil)
	require.NoError(t, err)

	require.Len(t, rig.seen, 3)
	assert.Contains(t, stepLine(&rig.seen[0]), "· reasoning")
	assert.Empty(t, liveAnswer(rig.seen[:1]))
	assert.Contains(t, stepLine(&rig.seen[2]), "· writing the answer")
	assert.Equal(t, "the rep", liveAnswer(rig.seen[2:]))
	assert.Equal(t, "hmm", rig.seen[2].reasoning)

	require.Len(t, res.steps, 1)
	assert.True(t, res.steps[0].done)
	assert.Equal(t, "the reply", res.steps[0].content, "the landed step is the reply's")
	assert.Empty(t, liveAnswer(res.steps))
}

// A tool call the model begins is named on the running step's line.
func TestARunningStepNamesTheToolsItAsksFor(t *testing.T) {
	var tr turnSteps
	i := tr.begin(turnStep{kind: stepModel})
	tr.grow(i, llm.Delta{Tools: []string{"open_window"}})
	tr.grow(i, llm.Delta{Tools: []string{"call_operation"}})
	steps, _, _ := tr.snapshot(0)
	assert.Contains(t, stepLine(&steps[0]), "· asking for open_window, call_operation")
	tr.finish(i, func(s *turnStep) { s.tools = 2 })
	tr.grow(i, llm.Delta{Content: "late"})
	steps, _, _ = tr.snapshot(0)
	assert.Empty(t, steps[0].content, "nothing grows a finished step")
}

// A stopped turn's late delta or finish misses the next turn's steps.
func TestAStaleStepHandleMissesTheNextTurn(t *testing.T) {
	var tr turnSteps
	old := tr.begin(turnStep{kind: stepModel})
	tr.reset()
	i := tr.begin(turnStep{kind: stepModel})
	tr.grow(old, llm.Delta{Content: "stale"})
	tr.finish(old, func(s *turnStep) { s.content = "stale" })
	steps, _, _ := tr.snapshot(0)
	require.Len(t, steps, 1)
	assert.Empty(t, steps[0].content)
	assert.False(t, steps[0].done)
	tr.grow(i, llm.Delta{Content: "new"})
	steps, _, _ = tr.snapshot(0)
	assert.Equal(t, "new", steps[0].content)
}

// streamingFakeModel is fakeModel over the stream, counting the calls
// that came streamed.
type streamingFakeModel struct {
	fakeModel
	streamed atomic.Int32
}

func (f *streamingFakeModel) CompleteStream(ctx context.Context, req openaichat.CompletionRequest, onDelta func(openaichat.Delta)) (resp openaichat.CompletionResponse, err error) {
	f.streamed.Add(1)
	resp, err = f.Complete(ctx, req)
	onDelta(openaichat.Delta{Content: resp.Content})
	return
}

// Under Ask first the approved retry is streamed as the first call was
// (ADR-0286 §SD4); the refused call never reached the model.
func TestAnApprovedRetryIsStreamed(t *testing.T) {
	model := &streamingFakeModel{}
	inst, _ := appOverRemote(t, model, agentrun.NewMemStore())
	amberNote(t, inst)
	require.True(t, inst.startTurn("q1"))
	var o *openAsk
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil }, 5*time.Second, 5*time.Millisecond)
	o.form.single[0], o.form.chosen[0][0], o.form.notes[0][0] = 0, true, "the fact is the customer's own"
	o.submit()
	drainUntil(t, inst)
	require.Len(t, inst.conv.entries, 2, "%+v", inst.conv.entries)
	assert.Equal(t, "a1", inst.conv.entries[1].text)
	assert.EqualValues(t, 1, model.streamed.Load(), "the retry came streamed")
	model.mu.Lock()
	defer model.mu.Unlock()
	assert.Equal(t, 1, model.calls)
}
