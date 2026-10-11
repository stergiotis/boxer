package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/tokenmeter"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// sizedModel answers with answerBytes of text, reports the prompt tokens
// the meter would, and answers the summariser with a fixed summary, or
// fails it when failSummary is set.
type sizedModel struct {
	mu          sync.Mutex
	answerBytes int
	failSummary bool
	seen        []openaichat.CompletionRequest
	summaries   int
}

var errSummariser = errors.New("provider: HTTP 503 upstream unavailable")

func (f *sizedModel) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(req.Messages[0].Content, "Write a title") {
		return openaichat.CompletionResponse{Content: "a title", FinishReason: "stop"}, nil
	}
	f.seen = append(f.seen, req)
	in := int32(tokenmeter.Estimate(req.Messages))
	last := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(last, "Summarise everything before") {
		f.summaries++
		if f.failSummary {
			return openaichat.CompletionResponse{}, errSummariser
		}
		return openaichat.CompletionResponse{Content: "SUMMARY of the earlier turns", FinishReason: "stop", InputTokens: in, OutputTokens: 5}, nil
	}
	n := len(f.seen)
	return openaichat.CompletionResponse{Content: "answer " + string(rune('0'+n)) + " " + strings.Repeat("a", f.answerBytes), FinishReason: "stop", InputTokens: in, OutputTokens: 5}, nil
}
func (f *sizedModel) Close() (err error) { return }

// compactContext is the route context the compaction tests run under: the
// profile's floor, so the trigger is 70% of 16384 tokens.
const compactContext = 16384

// Past the trigger a turn is compacted before its request: the plan cuts
// at a turn start outside the kept turns, the summariser is asked on the
// turn's route with the projected prefix, the compact entry carries the
// summary with the person's words verbatim, the next request starts from
// the summary rendered as model-written in the recalled band, and the
// laws hold (ADR-0316 §SD1, §SD3, §SD4; ADR-0311 L17).
func TestATurnOverTheTriggerIsSummarised(t *testing.T) {
	model := &sizedModel{answerBytes: 16000}
	inst := appOver(t, model, agentrun.NewMemStore())
	inst.coord.setRoute("", true, compactContext)
	for _, q := range []string{"q1", "q2", "q3"} {
		require.True(t, inst.startTurn(q))
		drainUntil(t, inst)
	}
	require.Len(t, model.seen, 3, "no compaction yet")
	require.True(t, inst.startTurn("q4"))
	drainUntil(t, inst)
	require.Len(t, model.seen, 5, "the summariser's call, then the turn's")
	summariser, turn := model.seen[3], model.seen[4]
	assert.Contains(t, summariser.Messages[len(summariser.Messages)-1].Content, "Summarise everything before the person's message that begins: \"q2\"", "the earliest cut whose summary fits")
	assert.Empty(t, summariser.Tools)
	prefix := model.seen[2].Messages
	require.Len(t, summariser.Messages, len(prefix)+3, "the cached prefix, the third answer, q4 and the instruction")
	assert.Equal(t, prefix, summariser.Messages[:len(prefix)], "the summariser extends the projected prefix")
	assert.Equal(t, "q4", summariser.Messages[len(prefix)+1].Content)
	w := inst.coord.journalNow()
	require.Equal(t, openaichat.ChatRoleSystem, turn.Messages[0].Role)
	assert.True(t, strings.HasPrefix(turn.Messages[0].Content, `<recalled source="self" run="`+w.Run().Id+`"`), "the summary is model output re-entering (L17): %s", turn.Messages[0].Content)
	assert.Contains(t, turn.Messages[0].Content, "SUMMARY of the earlier turns")
	assert.Contains(t, turn.Messages[0].Content, "The person said:\nq1")
	_, banded, _ := strings.Cut(turn.Messages[0].Content, ">\n")
	assert.NotContains(t, banded, "q2", "the kept turns are not summarised")
	assert.NotContains(t, turn.Messages[0].Content, projection.PersonSummarisedMark)
	assert.Equal(t, "q2", turn.Messages[1].Content, "the kept turns follow the summary")
	assert.Equal(t, "q4", turn.Messages[len(turn.Messages)-1].Content)
	for _, m := range turn.Messages[1:] {
		assert.NotContains(t, m.Content, "answer 1", "the first turn's answer is gone")
	}
	var names []string
	for _, tool := range turn.Tools {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "recall", "recall is offered once compacted")

	entries := w.Entries()
	var compacts []agentrun.Entry
	var intents, requests, outcomes int
	for _, e := range entries {
		if e.Kind == agentrun.KindCompact {
			compacts = append(compacts, e)
			continue
		}
		if !strings.HasPrefix(e.Key, "compact#") {
			continue
		}
		switch e.Kind {
		case agentrun.KindIntent:
			intents++
			assert.Equal(t, "summarise", e.Name)
			assert.Equal(t, agentrun.LevelOwnModel, e.Label.Level, "the summariser's call carries the request's label (L3)")
		case agentrun.KindRequest:
			requests++
			assert.Contains(t, e.Body, `"digest"`)
		case agentrun.KindOutcome:
			outcomes++
			assert.Contains(t, e.Body, `"call_id"`)
			assert.Equal(t, agentrun.LevelOwnModel, e.Label.Level)
		}
	}
	for _, e := range entries {
		if e.Kind == agentrun.KindIgnorable {
			assert.NotEqual(t, compactNoteName, e.Name, "nothing stood in the plan's way")
		}
	}
	require.Len(t, compacts, 1)
	c := compacts[0]
	assert.Equal(t, projection.FormSummary, c.Form)
	assert.EqualValues(t, 0, c.From)
	assert.Equal(t, agentrun.KindUser, entries[c.Target-1].Kind)
	assert.Equal(t, "q2", entries[c.Target-1].Body)
	assert.Equal(t, agentrun.LevelOwnModel, c.Label.Level, "a summary is model output")
	assert.Equal(t, w.Run().Id, c.WriterRun)
	assert.Equal(t, w.Run().Id, c.Run)
	assert.NotEmpty(t, c.Key)
	assert.Equal(t, 1, intents)
	assert.Equal(t, 1, requests, "the summariser's call has a request entry of its own")
	assert.Equal(t, 1, outcomes)
	assert.Empty(t, projection.CheckCompaction(entries))
	assert.Empty(t, projection.CheckBound(entries, sectionOrder(), inst.coord.compactConstants(0), lastReported))
	broken, err := agentrun.Verify(w.Run().Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
}

// A summariser that fails leaves no compaction behind (law L24 is not
// faked with an elision the plan judged too small): the intent and the
// failed outcome are journaled, the turn records that its request went
// out over the trigger, the request goes out as projected, and the next
// turn asks the summariser again.
func TestAFailedSummariserLeavesTheRequestOverTheTrigger(t *testing.T) {
	model := &sizedModel{answerBytes: 16000, failSummary: true}
	inst := appOver(t, model, agentrun.NewMemStore())
	inst.coord.setRoute("", true, compactContext)
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		require.True(t, inst.startTurn(q))
		drainUntil(t, inst)
	}
	require.Len(t, model.seen, 5, "the summariser's failed call, then the turn's")
	assert.Equal(t, 1, model.summaries)
	turn := model.seen[4]
	assert.Equal(t, "q1", turn.Messages[0].Content, "nothing was compacted")
	assert.True(t, strings.HasPrefix(inst.conv.entries[len(inst.conv.entries)-1].text, "answer "), "the turn was answered")
	w := inst.coord.journalNow()
	var compacts, failed, intents int
	for _, e := range w.Entries() {
		switch {
		case e.Kind == agentrun.KindCompact:
			compacts++
		case e.Kind == agentrun.KindIntent && e.Name == "summarise":
			intents++
		case e.Kind == agentrun.KindOutcome && strings.HasPrefix(e.Body, "error:"):
			assert.Contains(t, e.Body, errSummariser.Error())
		case e.Kind == agentrun.KindIgnorable && e.Name == compactNoteName:
			failed++
			assert.Equal(t, compactNoteFailed, e.Form)
			assert.Contains(t, e.Body, "over the trigger")
		}
	}
	assert.Zero(t, compacts)
	assert.Equal(t, 1, intents)
	assert.Equal(t, 1, failed)
	require.True(t, inst.startTurn("q5"))
	drainUntil(t, inst)
	assert.Equal(t, 2, model.summaries, "the next turn asks again")
}

// A span whose weight is tool results is elided without a model: the
// results become stubs naming their call key, and recall brings one back
// under the label the journal holds it at (ADR-0316 §SD2). A turn no
// cut can bring under the trigger goes out as it is and says so in the
// journal.
func TestAHeavyResultIsElidedAndRecalled(t *testing.T) {
	model := &sizedModel{answerBytes: 10}
	inst := appOver(t, model, agentrun.NewMemStore())
	inst.coord.setRoute("", true, compactContext)
	// A turn the host ran before: a call whose result is most of the
	// history, and confidential.
	w, err := inst.coord.openJournal(context.Background(), false)
	require.NoError(t, err)
	big := strings.Repeat("<row/>", 8000)
	_, err = w.Append(context.Background(),
		agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "q0", Turn: "t0", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "t0#0", Role: projection.RoleAssistant, Calls: projection.CallsBody([]openaichat.ToolCall{{Id: "c0", Name: "read_window_tree", Arguments: "{}"}}), Turn: "t0", Label: agentrun.Label{Level: agentrun.LevelOwnModel}},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "t0#0#0", Name: "c0", Effect: agentrun.EffectNone, Turn: "t0"},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "t0#0#0", Name: "c0", Role: projection.RoleTool, Body: big, Turn: "t0", Label: agentrun.Label{Level: agentrun.LevelExternal, Tier: agentrun.TierAmber}},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "t0#1", Role: projection.RoleAssistant, Body: "answer 0", Turn: "t0", Label: agentrun.Label{Level: agentrun.LevelOwnModel}},
		agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t0", Body: "answered"})
	require.NoError(t, err)
	var resultOrdinal uint32
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindOutcome {
			resultOrdinal = e.Ordinal
		}
	}
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	var refused []agentrun.Entry
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindIgnorable && e.Name == compactNoteName {
			refused = append(refused, e)
		}
	}
	require.Len(t, refused, 1, "the first turn had no cut outside the kept turns")
	assert.Equal(t, compactNoteRefused, refused[0].Form)
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	require.Len(t, model.seen, 2, "no summariser call")
	req := model.seen[1]
	require.Equal(t, openaichat.ChatRoleSystem, req.Messages[0].Role)
	stub := projection.Stub(projection.Message{Ordinal: resultOrdinal, ToolCallId: "c0", Key: "t0#0#0", Content: big})
	assert.Contains(t, req.Messages[0].Content, stub)
	assert.Contains(t, req.Messages[0].Content, "The person said:\nq0")
	for _, m := range req.Messages {
		assert.NotContains(t, m.Content, strings.Repeat("<row/>", 10))
	}
	entries := w.Entries()
	var compacts []agentrun.Entry
	for _, e := range entries {
		if e.Kind == agentrun.KindCompact {
			compacts = append(compacts, e)
		}
	}
	require.Len(t, compacts, 1)
	assert.Equal(t, projection.FormElide, compacts[0].Form)
	assert.Equal(t, agentrun.LevelOwnModel, compacts[0].Label.Level, "the elision's integrity is its inputs' least")
	assert.Empty(t, compacts[0].WriterRun, "no model wrote it")
	assert.Empty(t, projection.CheckCompaction(entries))

	spec := lookupTool("recall")
	require.NotNil(t, spec)
	assert.False(t, inst.coord.lastUntrusted)
	content, activity := inst.coord.recall(spec, map[string]any{"ordinal": float64(resultOrdinal)})
	assert.Equal(t, big, content)
	assert.Equal(t, "recalled the result at ordinal "+itoa(resultOrdinal), activity)
	assert.True(t, inst.coord.lastUntrusted, "the result re-enters under its label")
	writer, _ := inst.coord.takeWriter()
	assert.Equal(t, agentrun.TierAmber, writer.label.Tier, "and its tier")
	content, _ = inst.coord.recall(spec, map[string]any{"ordinal": float64(resultOrdinal - 1)})
	assert.True(t, strings.HasPrefix(content, "error:"))
}

// A recall over the budget is cut on a rune boundary.
func TestARecallCutsOnARuneBoundary(t *testing.T) {
	inst := appOver(t, &sizedModel{}, agentrun.NewMemStore())
	w, err := inst.coord.openJournal(context.Background(), false)
	require.NoError(t, err)
	body := "a" + strings.Repeat("é", recallBudget)
	written, err := w.Append(context.Background(), agentrun.Entry{Kind: agentrun.KindOutcome, Key: "k", Name: "c", Role: projection.RoleTool, Body: body, Turn: "t0", Label: harnessLabel()})
	require.NoError(t, err)
	content, _ := inst.coord.recall(lookupTool("recall"), map[string]any{"ordinal": float64(written[0].Ordinal)})
	head, _, _ := strings.Cut(content, "\n… cut at")
	assert.True(t, strings.HasSuffix(head, "é"), "cut between runes")
	assert.Len(t, head, recallBudget-1)
	assert.Equal(t, "abc", cutRunes("abc", 3))
	assert.Equal(t, "", cutRunes("é", 1))
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

// The profile's constants pass the thrash check, and a profile whose
// summary, tail and margin reach the trigger does not validate. A route
// whose context is below the floor is checked again with the head as
// projected: when the constants would thrash under its trigger,
// compaction is off for the conversation.
func TestTheProfileDoesNotThrash(t *testing.T) {
	p := defaultProfile()
	require.NoError(t, p.Validate())
	p.Compact.TailTokens = p.CompactContextFloor
	assert.Error(t, p.Validate())

	inst := appOver(t, &sizedModel{}, agentrun.NewMemStore())
	_, err := inst.coord.openJournal(context.Background(), false)
	require.NoError(t, err)
	inst.coord.setRoute("", true, compactContext)
	assert.Positive(t, inst.coord.compactConstants(2000).TriggerTokens, "at the floor the profile's check stands")
	inst.coord.setRoute("", true, 4096)
	assert.Zero(t, inst.coord.compactConstants(500).TriggerTokens, "a 4k context cannot hold the summary, the tail and a round")
	inst.coord.setRoute("", true, 0)
	assert.Zero(t, inst.coord.compactConstants(0).TriggerTokens, "an unknown context never compacts")
}
