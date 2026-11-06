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
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// A rests_on may name the result id the query answered with, which the model
// reads, and one that names nothing lists what can be cited.
func TestCiteTakesAResultIdAndSaysWhatIsCitable(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	id := strconv.Itoa(noteResultId)
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"count","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"count_words","args":{}}`),
		toolCall("k1", "cite", `{"assertions":[
			{"id":"a1","rests_on":["`+id+`"],"says":"one word","form":"holds","statement":"SELECT words = 1 FROM note"},
			{"id":"a2","rests_on":["result:`+id+`"],"says":"one word","form":"holds","statement":"SELECT words = 1 FROM note"},
			{"id":"a3","rests_on":["5"],"says":"one word","form":"holds","statement":"SELECT words = 1 FROM note"}]}`),
		{Content: "One word [a1].", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	var out citeResult
	require.NoError(t, json.Unmarshal([]byte(toolReplies(res.messages)["k1"]), &out))
	require.Len(t, out.Assertions, 3)
	assert.Equal(t, verdictHeld, out.Assertions[0].Outcome, out.Assertions[0].Reason)
	assert.Equal(t, verdictHeld, out.Assertions[1].Outcome, out.Assertions[1].Reason)
	assert.Equal(t, verdictUnchecked, out.Assertions[2].Outcome)
	assert.Contains(t, out.Assertions[2].Reason, "you can cite: "+id+" (count_words in window 100)")
}

// The first read of a window that holds sealed data is held with what it
// costs; repeating the call reads it.
func TestTheFirstReadOfASealedWindowIsHeld(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"count","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"count_words","args":{}}`),
		toolCall("c4", "call_operation", `{"window":100,"operation":"count_words","args":{}}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRig(t, bus, model, false)
	host.sealed = map[uint64]bool{100: true}
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	assert.Contains(t, replies["c3"], `"phase":"held"`)
	assert.Contains(t, replies["c3"], "cannot be exported")
	assert.Contains(t, replies["c4"], "completed", "the model chose to read it")
}

func TestTheExportRefusalNamesWhatConfinedTheConversation(t *testing.T) {
	assert.NotContains(t, exportRefusal(""), "since")
	assert.Contains(t, exportRefusal("sample_rows in window 2 (tool call c9)"), "since sample_rows in window 2 (tool call c9)")
}

// supersededEntries is a conversation's journal in the shape the edits read:
// a state read three times, a result read twice and a window tree, each
// result n bytes.
func supersededEntries(n int) (es []agentrun.Entry) {
	body := strings.Repeat("x", n)
	var ordinal uint32
	add := func(e agentrun.Entry) {
		ordinal++
		e.Ordinal = ordinal
		es = append(es, e)
	}
	call := func(key string, name string, arguments string) {
		add(agentrun.Entry{Kind: agentrun.KindReply, Key: "r" + key})
		add(agentrun.Entry{Kind: agentrun.KindIntent, Key: key, Name: name, Arguments: arguments, Effect: agentrun.EffectNone})
		add(agentrun.Entry{Kind: agentrun.KindOutcome, Key: key, Name: "id-" + key, Role: projection.RoleTool, Body: body})
	}
	state := `{"window":2,"operation":"get_state","args":{},"title":"%s"}`
	call("s1", "call_operation", strings.Replace(state, "%s", "first", 1))
	call("t1", "read_window_tree", `{"windows":[2]}`)
	call("d1", "call_operation", `{"window":2,"operation":"sample_rows","args":{}}`)
	call("s2", "call_operation", strings.Replace(state, "%s", "second", 1))
	call("d2", "call_operation", `{"window":2,"operation":"sample_rows","args":{}}`)
	call("s3", "call_operation", strings.Replace(state, "%s", "third", 1))
	return
}

// A state read a later identical one replaced and a window tree past its step
// are edited; a read of a result is kept, since an earlier result's figures
// are not out of date, and so is the latest state read.
func TestSupersededResultsAreEditedInBatches(t *testing.T) {
	coord := &coordinator{}
	p := ProfileT{SupersededReads: []string{"get_state"}, SupersedeBatchBytes: 4 << 10, Compact: projection.Constants{ElideMinBytes: 512}}
	entries := supersededEntries(2 << 10)
	edits, freed := coord.supersededEdits(entries, p, "t")
	require.Len(t, edits, 3, "two state reads and the tree")
	assert.Positive(t, freed)
	byOrdinal := make(map[uint32]string, len(entries))
	for _, e := range entries {
		byOrdinal[e.Ordinal] = e.Key
	}
	var keys []string
	for _, e := range edits {
		assert.Equal(t, agentrun.KindEdit, e.Kind)
		assert.Equal(t, formSupersede, e.Form)
		assert.Contains(t, e.Body, "recall(")
		keys = append(keys, byOrdinal[e.Target])
	}
	assert.ElementsMatch(t, []string{"s1", "t1", "s2"}, keys)

	// Under the batch size nothing is written: an edit costs the cached prefix.
	edits, _ = coord.supersededEdits(supersededEntries(1<<10), p, "t")
	assert.Empty(t, edits)
	// What was edited is not edited again.
	for i, e := range entries {
		if e.Ordinal == 3 {
			entries = append(entries, agentrun.Entry{Kind: agentrun.KindEdit, Target: entries[i].Ordinal, Ordinal: 99})
		}
	}
	p.SupersedeBatchBytes = 1 << 10
	edits, _ = coord.supersededEdits(entries, p, "t")
	assert.Len(t, edits, 2)
}

func TestRoundsThePersonGrantedExtendTheTurn(t *testing.T) {
	coord := &coordinator{}
	_, hi := roundRange()
	assert.Equal(t, 10, coord.extendRounds(10), "none granted")
	coord.grantRounds(6)
	assert.Equal(t, 16, coord.extendRounds(10))
	assert.Equal(t, 16, coord.extendRounds(16), "taken once")
	coord.grantRounds(hi * 2)
	assert.Equal(t, hi, coord.extendRounds(10), "within the host's most")
}

func TestAllowOfNamesTheSetting(t *testing.T) {
	assert.Equal(t, allowOutside, allowOf("outside"))
	assert.Equal(t, allowRead, allowOf("read"))
	assert.Equal(t, allowRun, allowOf("run"))
	assert.Equal(t, allowRun, allowOf("nonsense"), "the seed's own default")
}

// A run answers with the result it produced, so a claim may rest on it as on
// a read of that result; a command that only changes the window may not.
func TestCiteRestsOnARunsResultButNotOnAWrite(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	id := strconv.Itoa(noteResultId)
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"count","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"recount","args":{}}`),
		toolCall("c4", "call_operation", `{"window":100,"operation":"set_note","args":{"Text":"one"}}`),
		toolCall("k1", "cite", `{"assertions":[
			{"id":"a1","rests_on":["`+id+`"],"says":"one word","form":"holds","statement":"SELECT words = 1 FROM note"},
			{"id":"a2","rests_on":["c4"],"says":"one word","form":"holds","statement":"SELECT words = 1 FROM note"}]}`),
		{Content: "One word [a1].", FinishReason: "stop"},
	}}
	ops := noteOpsSet()
	appops.Command(ops, app.OperationSpec{Name: "recount", Version: 1, Summary: "count the note's words again, as a new result",
		Effect: app.OperationEffectRun, Reads: []string{"text"}, Agents: true, Untrusted: true},
		func(n *note, call app.OperationCall, in appops.None) (wordCount, error) {
			return wordCount{ResultId: noteResultId, Words: len(strings.Fields(n.text))}, nil
		})
	_, coord, cli, req, ctx := coordRigWith(t, bus, model, false, ops, nil)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	var out citeResult
	require.NoError(t, json.Unmarshal([]byte(toolReplies(res.messages)["k1"]), &out))
	require.Len(t, out.Assertions, 2)
	assert.Equal(t, verdictHeld, out.Assertions[0].Outcome, out.Assertions[0].Reason)
	assert.Equal(t, verdictUnchecked, out.Assertions[1].Outcome)
	assert.Contains(t, out.Assertions[1].Reason, "not a read of a result")
}
