package chat

import (
	"encoding/json/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// ADR-0340 §SD1–§SD5: the model reads a result, asserts four claims
// about it through cite — one holds, one fails, one rests on no call, one
// names no table — and writes a reply referring to them; each assertion
// is a verdict entry keyed by the cite call, the reply's references
// resolve (L36), the verdicts that ran rest on the query (L37), and the
// figure the reply states is accounted for (L38).
func TestACitedReplyCarriesItsVerdicts(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"count the words","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"count_words","args":{}}`),
		toolCall("k1", "cite", `{"assertions":[
			{"id":"a1","rests_on":["c3"],"says":"the note has one word","form":"value","statement":"SELECT words FROM note","expect":"1"},
			{"id":"a2","rests_on":["c3"],"says":"the note has two words","form":"holds","statement":"SELECT words = 2 FROM note"},
			{"id":"a3","rests_on":["c9"],"says":"something","form":"holds","statement":"SELECT words = 1 FROM note"},
			{"id":"a4","rests_on":["c3"],"says":"one is one","form":"holds","statement":"SELECT 1"}]}`),
		{Content: "The note has 1 word [a1], not two [a2]; see also [a5].", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)

	replies := toolReplies(res.messages)
	var out citeResult
	require.NoError(t, json.Unmarshal([]byte(replies["k1"]), &out), replies["k1"])
	require.Len(t, out.Assertions, 4)
	assert.Equal(t, assertionOutcome{Id: "a1", Outcome: verdictHeld, Observed: "1"}, out.Assertions[0])
	assert.Equal(t, verdictFailed, out.Assertions[1].Outcome)
	assert.Equal(t, "false", out.Assertions[1].Observed)
	assert.Equal(t, verdictUnchecked, out.Assertions[2].Outcome)
	assert.Contains(t, out.Assertions[2].Reason, "c9")
	assert.Equal(t, verdictRefused, out.Assertions[3].Outcome)
	assert.Contains(t, out.Assertions[3].Reason, "names no table")
	assert.Contains(t, res.activity[3], "cite · 1 held, 1 failed, 1 refused, 1 unchecked")

	entries := coord.journalNow().Entries()
	verdicts := projection.Verdicts(entries, req.Turn)
	require.Len(t, verdicts, 4)
	v := verdicts[0]
	assert.Equal(t, agentrun.KindVerdict, v.Kind)
	assert.Equal(t, "a1", v.Name)
	assert.Equal(t, verdictFormCheck, v.Form)
	assert.Equal(t, "1", v.Body, "the observed value is the entry's body")
	assert.Contains(t, v.Arguments, `"says":"the note has one word"`, "the assertion is the entry's arguments")
	assert.Equal(t, verdictHeld, v.Verdict.Outcome)
	assert.Equal(t, uint64(noteResultId), v.Verdict.Result)
	assert.Equal(t, "clickhouse:note:9000", v.Verdict.Endpoint)
	assert.NotEmpty(t, v.Verdict.Digest)
	assert.NotEmpty(t, v.Verdict.Source)
	require.Len(t, v.Verdict.RestsOn, 1)
	assert.Equal(t, agentrun.LevelSystem, v.Label.Level, "a verdict is deterministic system data")
	var intentKey string
	for _, e := range entries {
		if e.Kind == agentrun.KindOutcome && e.Name == "c3" {
			intentKey = e.Key
		}
	}
	assert.Equal(t, intentKey, v.Verdict.RestsOn[0], "rests_on carries the coordinator's key of the cited call")
	assert.Equal(t, verdictUnchecked, verdicts[2].Verdict.Outcome)
	assert.Empty(t, verdicts[2].Verdict.RestsOn)

	assert.Equal(t, []string{"L36: reply " + ordinalOfReply(entries) + " refers to [a5], which no verdict of its turn names before it"}, projection.CheckCitations(entries))
	assert.Empty(t, projection.Uncited(entries), "the 1 the reply states is the observed value")

	rows := agentrun.VerdictRows(entries)
	require.Len(t, rows, 4)
	assert.Equal(t, "the note has one word", rows[0].Says)
	assert.Equal(t, "SELECT words FROM note", rows[0].Statement)
	assert.NotZero(t, rows[0].Reply, "the reply that refers to a1 is found")
	assert.Zero(t, rows[2].Reply, "nothing refers to a3")
}

func ordinalOfReply(entries []agentrun.Entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == agentrun.KindReply {
			return strconv.FormatUint(uint64(entries[i].Ordinal), 10)
		}
	}
	return ""
}

// The cite call is bounded: more assertions than the budget, and more
// calls than the turn allows, are refused with the bound named; a refused
// call still counts, since its intent was journaled.
func TestCiteIsBounded(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	many := strings.Repeat(`{"id":"a1","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"},`, maxCiteAssertions)
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"count","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"count_words","args":{}}`),
		toolCall("k1", "cite", `{"assertions":[`+strings.TrimSuffix(many, ",")+`,{"id":"a9","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"}]}`),
		toolCall("k2", "cite", `{"assertions":[{"id":"b1","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"},{"id":"b1","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"},{"id":"B2","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"}]}`),
		toolCall("k3", "cite", `{"assertions":[{"id":"b1","rests_on":["c3"],"says":"s","form":"holds","statement":"SELECT words = 1 FROM note"}]}`),
		{Content: "done [b1]", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	assert.Contains(t, replies["k1"], "at most 8 assertions")
	var out citeResult
	require.NoError(t, json.Unmarshal([]byte(replies["k2"]), &out))
	assert.Equal(t, verdictHeld, out.Assertions[0].Outcome)
	assert.Contains(t, out.Assertions[1].Reason, "earlier assertion")
	assert.Contains(t, out.Assertions[2].Reason, "letters then digits")
	assert.Contains(t, replies["k3"], "at most 2 cite calls", "a refused cite call spent one of the turn's two")
	assert.Empty(t, projection.CheckCitations(coord.journalNow().Entries()))
}

func TestValuesHoldAtTheirDisplayedPrecision(t *testing.T) {
	assert.True(t, holdsAtPrecision("12 %", "12.4"))
	assert.False(t, holdsAtPrecision("12 %", "12.6"))
	assert.True(t, holdsAtPrecision("12.4 %", "12.41"))
	assert.False(t, holdsAtPrecision("12.4 %", "12.46"))
	assert.True(t, holdsAtPrecision("1,234.5", "1234.49"))
	assert.True(t, holdsAtPrecision("-3", "-2.6"))
	assert.False(t, holdsAtPrecision("3", "three"))
	assert.True(t, holdsAtPrecision("March", " march "))
	assert.False(t, holdsAtPrecision("March", "April"))
	assert.False(t, holdsAtPrecision("2024-03-01", "2024-12-31"), "a date is not one figure: its text must match")
	assert.True(t, holdsAtPrecision("2024-03-01", "2024-03-01"))
	assert.False(t, holdsAtPrecision("Q3 1,234", "3"), "a quarter is not one figure")
	assert.True(t, holdsAtPrecision("€ 1,234.50", "1234.5"))
	assert.True(t, holdsAtPrecision("-3 items", "-2.6"))
	assert.False(t, holdsAtPrecision("3", "3 4"), "an observed cell that is not one figure does not hold")
}

// The readers (ADR-0340 §SD6): a reply's references carry their marks and
// its citations follow it; a copy of text citing a failed claim is held
// once.
func TestACitedReplyShowsItsMarks(t *testing.T) {
	e := entry{speaker: speakerModel, text: "One [a1], two [a2], three [a3]. See `[a1]` and [a1](url).", cites: []citationMark{
		{id: "a1", outcome: verdictHeld, says: "one", observed: "1"},
		{id: "a2", outcome: verdictFailed, says: "two", observed: "1", reason: "expected 2, observed 1"},
	}}
	shown := e.shown()
	assert.True(t, strings.HasPrefix(shown, "One [a1 ✓], two [a2 ✗], three [a3 ?]. See `[a1]` and [a1](url)."), shown)
	marks := e.cites
	e.setCites(nil)
	assert.True(t, strings.HasPrefix(e.shown(), "One [a1 ?]"), "setCites forgets what was shown")
	e.setCites(marks)
	assert.Contains(t, shown, "- [a1] ✓ held · one · observed 1\n")
	assert.Contains(t, shown, "- [a2] ✗ failed · two · observed 1 · expected 2, observed 1\n")
	assert.Contains(t, shown, "- [a3] ? no verdict of this turn names it")
	assert.Equal(t, "plain", (&entry{speaker: speakerModel, text: "plain"}).shown())
	assert.Equal(t, "it cites a claim that did not hold ([a2]) and refers to no verdict for [a3]", citationHold(e.text, e.cites))
	assert.Empty(t, citationHold("One [a1].", e.cites))
	conv := newConversation()
	conv.entries = append(conv.entries, e)
	a := &App{conv: conv}
	assert.True(t, a.heldByCitations("copy it", e.text), "held once")
	assert.False(t, a.heldByCitations("copy it", e.text), "sent on the second gesture")
	assert.True(t, a.heldByCitations("copy it", e.text), "and held again after")
}
