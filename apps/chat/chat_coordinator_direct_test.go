package chat

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// directOutcome reads a call's answer, delimited or not.
func directOutcome(t *testing.T, content string) (co callOutcome) {
	t.Helper()
	text, _, _ := unwrapUntrusted(content)
	require.NoError(t, json.Unmarshal([]byte(text), &co), content)
	return
}

// A direct tool is in the tool list before any task or window exists, and
// the list is the same in every round. Its first call asks for access and
// opens a window of the app; the next reuses that window, and writes there
// as call_operation would after a read.
func TestADirectToolOpensWhatItNeedsOnItsWay(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("d1", "notes_get_note", `{"reason":"read the note"}`),
		toolCall("d2", "notes_set_note", `{"text":"direct","reason":"tidy the note"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, noteOpsSet("get_note", "set_note"), nil)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "done", res.final.Content)

	host.mu.Lock()
	assert.Len(t, host.engines, 1, "one window was opened and then reused")
	assert.Equal(t, "direct", host.notes[100].text, "the write went through the dispatcher to the window")
	host.mu.Unlock()
	task, _, _ := coord.state()
	assert.NotEmpty(t, task, "the call started a task")

	replies := toolReplies(res.messages)
	first := directOutcome(t, replies["d1"])
	assert.Equal(t, "completed", first.Phase)
	assert.Equal(t, uint64(100), first.Window, "the answer names the window")
	assert.True(t, first.Opened)
	assert.Contains(t, first.Access, "granted for task "+task)
	second := directOutcome(t, replies["d2"])
	assert.Equal(t, uint64(100), second.Window)
	assert.False(t, second.Opened, "the second call reused the window")
	assert.Empty(t, second.Access)
	assert.Contains(t, string(first.Result), `"first"`, "the read ran in the window it opened")
	assert.Contains(t, res.activity[0], "opened Notes as window 100")

	model.mu.Lock()
	defer model.mu.Unlock()
	require.Len(t, model.seen, 3)
	names := toolNames(model.seen[0].Tools)
	assert.Contains(t, names, "notes_get_note", "offered with no task and no window")
	assert.Contains(t, names, "notes_set_note")
	for i := range model.seen {
		assert.Equal(t, names, toolNames(model.seen[i].Tools), "round %d offers the same tools", i)
	}
	for _, tl := range model.seen[0].Tools {
		if tl.Name == "notes_set_note" {
			params := string(tl.Parameters)
			for _, p := range []string{`"text"`, `"reason"`, `"window"`, `"title"`} {
				assert.Contains(t, params, p)
			}
			assert.True(t, strings.HasPrefix(tl.Description, "Notes: replace the note."), tl.Description)
		}
	}
}

// A direct call runs in the window it names, else in the window the last
// direct call into the app used; a window of another app or of no task is
// refused.
func TestADirectCallRunsInTheWindowItNames(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"two notes","open":[{"app":"notes","count":2}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("o2", "open_window", `{"app":"notes"}`),
		toolCall("g1", "notes_get_note", `{"window":101}`),
		toolCall("g2", "notes_get_note", `{}`),
		toolCall("g3", "notes_get_note", `{"window":7}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, noteOpsSet("get_note"), nil)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	host.mu.Lock()
	assert.Len(t, host.engines, 2, "the direct calls opened no window")
	host.mu.Unlock()
	replies := toolReplies(res.messages)
	assert.Equal(t, uint64(101), directOutcome(t, replies["g1"]).Window)
	assert.Equal(t, uint64(101), directOutcome(t, replies["g2"]).Window, "the window the last direct call used")
	assert.Contains(t, replies["g3"], "window 7 is not a window of Notes in your task")
}

// A direct call reuses a window the model opened itself, and asks for
// nothing.
func TestADirectCallUsesAWindowTheTaskHolds(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("g1", "notes_get_note", `{}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, noteOpsSet("get_note"), nil)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	host.mu.Lock()
	assert.Len(t, host.engines, 1)
	host.mu.Unlock()
	co := directOutcome(t, toolReplies(res.messages)["g1"])
	assert.Equal(t, uint64(100), co.Window)
	assert.False(t, co.Opened)
	assert.Empty(t, co.Access)

	// The call's window is what a citation or a supersede reads it by.
	coord.mu.Lock()
	require.Len(t, coord.directWindows, 1)
	var key string
	for k := range coord.directWindows {
		key = k
	}
	coord.mu.Unlock()
	window, op, ok := coord.windowOf(agentrun.Entry{Name: "notes_get_note", Key: key})
	require.True(t, ok)
	assert.Equal(t, uint64(100), window)
	assert.Equal(t, "get_note", op)
}

// When the access a direct call asks for is not granted, the call is
// refused with the request's refusal, opens nothing and calls nothing; the
// same call made again is answered without asking again.
func TestADirectCallIsRefusedWhenAccessIsNot(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	host, coord, _, _, ctx := coordRigWith(t, bus, &scriptedModel{}, false, noteOpsSet("set_note"), nil)
	c := agent.Unlimited()
	c.Launch = false
	coord.setOptions(c, false)
	coord.tools(ctx)
	call := openaichat.ToolCall{Id: "d1", Name: "notes_set_note", Arguments: `{"text":"x","reason":"tidy"}`}
	content, activity := coord.exec(ctx, toolOrigin{}, call)
	assert.True(t, strings.HasPrefix(content, "the person did not grant access"), content)
	assert.Contains(t, content, "do not let the model open windows")
	assert.Contains(t, activity, "access not granted")
	again, _ := coord.exec(ctx, toolOrigin{}, call)
	assert.Contains(t, again, "same call that was just refused")
	host.mu.Lock()
	defer host.mu.Unlock()
	assert.Empty(t, host.engines, "no window was opened")
	assert.Empty(t, host.submitted, "no operation was called")
}

// With direct tools off, none is offered and a call to one is no tool.
func TestDirectToolsOff(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	_, coord, _, _, ctx := coordRigWith(t, bus, &scriptedModel{}, false, noteOpsSet("get_note"), nil)
	coord.mu.Lock()
	coord.direct = false
	coord.mu.Unlock()
	assert.NotContains(t, toolNames(coord.tools(ctx)), "notes_get_note")
	content, _ := coord.exec(ctx, toolOrigin{}, openaichat.ToolCall{Id: "d1", Name: "notes_get_note", Arguments: `{}`})
	assert.Equal(t, "error: no tool notes_get_note", content)
}

// The apps that say when to use them come first in the apps note, with
// their advice and their direct tools; describe_app carries both.
func TestTheAppsNoteGivesAdviceAndToolsFirst(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	calc := app.Manifest{Id: "github.com/x/apps/zcalc", Display: "Calc", Summary: "compute over a note",
		WhenToUse: "Use for sums the note cannot do by itself.",
		Surface:   app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]}, Operations: noteOpsSet("get_note").Catalog()}
	idle := app.Manifest{Id: "github.com/x/apps/aidle", Display: "Idle", Summary: "open without operations",
		WhenToUse: "Use for anything at all.", Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]}}
	_, coord, _, _, ctx := coordRigCfg(t, bus, &scriptedModel{}, false, noteOps, nil, nil, calc, idle)
	note := coord.appsNote(ctx)
	assert.Contains(t, note, "- github.com/x/apps/aidle — Idle: open without operations (no operations)\n", "an app that cannot be operated gives no advice")
	assert.NotContains(t, note, "Use for anything at all.")
	calcLine := "- github.com/x/apps/zcalc — Calc: compute over a note (tool zcalc_get_note). Use for sums the note cannot do by itself."
	notesLine := "- " + string(notesId) + " — Notes: keep a note (help)"
	require.Contains(t, note, calcLine)
	require.Contains(t, note, notesLine)
	assert.Less(t, strings.Index(note, calcLine), strings.Index(note, notesLine), "an app with advice comes first")
	assert.Less(t, strings.Index(note, calcLine), strings.Index(note, "aidle"), "before one whose advice is not given")

	got, _ := coord.describeApp(ctx, agent.Asked{}, map[string]any{"app": "zcalc"})
	assert.Contains(t, got, `"when_to_use":"Use for sums the note cannot do by itself."`)
	assert.Contains(t, got, `"tool":"zcalc_get_note"`)
}

// The direct tools are bounded, in the order the host lists the apps.
func TestDirectToolsAreBounded(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	var more []app.Manifest
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		more = append(more, app.Manifest{Id: app.AppIdT("github.com/x/apps/" + id), Display: id, Summary: "keep another note",
			Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]}, Operations: noteOpsSet("get_note", "set_note").Catalog()})
	}
	_, coord, _, _, ctx := coordRigCfg(t, bus, &scriptedModel{}, false, noteOps, nil, nil, more...)
	ops := coord.directOps(ctx)
	require.Len(t, ops, maxDirectTools)
	assert.Equal(t, "a1_get_note", ops[0].name)
	assert.Equal(t, "a4_set_note", ops[maxDirectTools-1].name)
	assert.Equal(t, "document", coord.effectOf(ctx, "a1_set_note", `{}`), "a direct tool's effect is its operation's")
	assert.Equal(t, agentrun.EffectNone, coord.effectOf(ctx, "a1_get_note", `{}`))
}

// Unattended, the host decides a direct call's access within the ceiling,
// and the answer carries the note on it.
func TestADirectCallUnattended(t *testing.T) {
	if !agent.Unattended {
		t.Skip("needs -tags boxer_unattended")
	}
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("d1", "notes_get_note", `{"reason":"read the note"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigCfg(t, bus, model, false, noteOpsSet("get_note"), nil, func(cfg *agent.Config) {
		cfg.TestGrants, cfg.Unattended, cfg.Coordinators = false, true, []string{string(ManifestId)}
	})
	coord.setOptions(agent.Unlimited(), false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	co := directOutcome(t, toolReplies(res.messages)["d1"])
	assert.Equal(t, "completed", co.Phase, toolReplies(res.messages)["d1"])
	assert.True(t, co.Opened)
	assert.Contains(t, co.Access, unattendedNote)
	coord.mu.Lock()
	assert.True(t, coord.grant.Unattended)
	coord.mu.Unlock()
	host.mu.Lock()
	assert.Len(t, host.engines, 1)
	host.mu.Unlock()
}

func TestDirectToolNames(t *testing.T) {
	assert.Equal(t, "play_query", directToolName("github.com/stergiotis/boxer/apps/play", "query"))
	assert.Equal(t, "wolfram_evaluate", directToolName("x/wolfram", "evaluate"))
	assert.Equal(t, "my_app_run", directToolName("x/my.app", "run"))
	assert.Empty(t, directToolName("x/w12", "run"), "the shape of a window's operation tool")
	assert.Len(t, directToolName("x/"+strings.Repeat("a", 70), "run"), 64)
}

func TestPickWindowAndLaunchNeedsAccess(t *testing.T) {
	windows := []agent.Instance{
		{Instance: 1, App: "other", Load: "ready"},
		{Instance: 2, App: "a", Load: "failed"},
		{Instance: 3, App: "a", Load: "opening"},
		{Instance: 4, App: "a", Load: "ready"},
		{Instance: 5, App: "a", Load: "ready"},
	}
	w, ok := pickWindow(windows, "a", 0)
	require.True(t, ok)
	assert.Equal(t, uint64(4), w.Instance, "the first ready one")
	w, _ = pickWindow(windows, "a", 5)
	assert.Equal(t, uint64(5), w.Instance, "the last one used")
	w, _ = pickWindow(windows[:3], "a", 2)
	assert.Equal(t, uint64(3), w.Instance, "a failed one is not picked; one still opening is")
	_, ok = pickWindow(windows, "none", 0)
	assert.False(t, ok)

	assert.True(t, launchNeedsAccess("the grant does not let the task open windows of x"))
	assert.True(t, launchNeedsAccess("the task opened every window of x its grant allows"))
	assert.False(t, launchNeedsAccess("the chat's settings do not let the model open windows"))
}

// A task that may not open the app is widened: the call asks for the app
// under the same task, then opens a window of it.
func TestADirectCallWidensATaskWithoutTheApp(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	calc := app.Manifest{Id: "github.com/x/apps/calc", Display: "Calc", Summary: "compute over a note",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]}, Operations: noteOpsSet("get_note").Catalog()}
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("c1", "calc_get_note", `{"reason":"sum it"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigCfg(t, bus, model, false, noteOps, nil, nil, calc)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	co := directOutcome(t, toolReplies(res.messages)["c1"])
	assert.Equal(t, "completed", co.Phase, toolReplies(res.messages)["c1"])
	assert.Equal(t, uint64(101), co.Window)
	assert.True(t, co.Opened)
	assert.NotEmpty(t, co.Access, "the task was widened to the app")
	assert.Len(t, coord.heldTasks(), 1, "under the same task")
	host.mu.Lock()
	assert.Len(t, host.engines, 2)
	host.mu.Unlock()
}

// A direct write into the window the call itself opened needs no read
// before it: the task knows the window as it opened.
func TestADirectWriteIntoTheWindowItOpened(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("d1", "notes_set_note", `{"text":"direct","reason":"write the note"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, noteOpsSet("set_note"), nil)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	co := directOutcome(t, toolReplies(res.messages)["d1"])
	assert.Equal(t, "rendered", co.Phase, co.Reason)
	assert.True(t, co.Opened)
	host.mu.Lock()
	defer host.mu.Unlock()
	assert.Equal(t, "direct", host.notes[100].text)
}

// A direct tool's description carries the operation's leading Follows
// lines, in order, up to the bound; a line past it is left out with every
// later one.
func TestADirectToolDescriptionCarriesFollows(t *testing.T) {
	long := strings.Repeat("x", maxDirectFollowsRunes)
	d := directOpOf("calc_evaluate", agent.AppOperations{App: "calc", Display: "Calc", WhenToUse: "Use for sums."},
		agent.Operation{Name: "evaluate", Summary: "evaluate an expression", Follows: []string{"a cell stops at its first error", " ", "prefer the built-ins", long, "never reached"}})
	desc := d.tool.Description
	assert.True(t, strings.HasPrefix(desc, "Calc: evaluate an expression. Use for sums. Call it without request_access"), desc)
	assert.True(t, strings.HasSuffix(desc, "\n- a cell stops at its first error\n- prefer the built-ins"), desc)
	assert.NotContains(t, desc, "never reached")
	assert.Equal(t, []string{"a", "b"}, followsWithin([]string{"a", "b", "cd"}, 3))
}
