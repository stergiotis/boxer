package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// A tiny participant: one note.
type note struct{ text string }

type noteSnap struct{ text string }

type setNoteArgs struct{ Text string }
type noteResult struct{ Text string }

const notesId app.AppIdT = "github.com/x/apps/notes"

var noteOps = func() *appops.Set[*note, noteSnap] {
	s := appops.NewSet(func(n *note) noteSnap { return noteSnap{text: n.text} })
	s.Resource("text", "the note", func(n *note) any { return n.text })
	appops.Query(s, app.OperationSpec{Name: "get_note", Version: 1, Summary: "read the note", Reads: []string{"text"}, Agents: true, Untrusted: true},
		func(sn noteSnap, in appops.None) (noteResult, error) { return noteResult{Text: sn.text}, nil })
	appops.Command(s, app.OperationSpec{Name: "set_note", Version: 1, Summary: "replace the note", Effect: app.OperationEffectDocument,
		Writes: []string{"text"}, Agents: true},
		func(n *note, call app.OperationCall, in setNoteArgs) (appops.None, error) {
			n.text = in.Text
			return appops.None{}, nil
		})
	return s
}()

// noteHost is a window host with note windows; a goroutine of its own
// stands in for the render goroutine and runs their frames.
type noteHost struct {
	mu      sync.Mutex
	engines map[uint64]*opengine.Engine
	notes   map[uint64]*note
}

func (inst *noteHost) eng(k uint64) *opengine.Engine {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.engines[k]
}
func (inst *noteHost) OpsInstances() (out []opwire.InstanceInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for k := range inst.engines {
		out = append(out, opwire.InstanceInfo{App: notesId, Alias: notesId.SubjectAlias(), Key: k, Title: "Notes", Ops: true})
	}
	return
}
func (inst *noteHost) OpsRenderGoroutine() uint64 { return 0 }
func (inst *noteHost) OpsStatus(k uint64, id string) (opwire.Outcome, bool) {
	return inst.eng(k).Status(id)
}
func (inst *noteHost) OpsCancel(k uint64, id string) (opwire.Outcome, bool) {
	return inst.eng(k).Cancel(id)
}
func (inst *noteHost) OpsExpire(k uint64, ids []string, reason string) {
	inst.eng(k).Expire(ids, reason)
}
func (inst *noteHost) OpsAttach(k uint64, a bool) bool { inst.eng(k).SetAttached(a); return true }
func (inst *noteHost) RenderSvg(k uint64, recheck func() bool) (string, error) {
	return "", nil
}
func (inst *noteHost) RenderPixels(k []uint64, recheck func() bool) (string, error) {
	return "", nil
}
func (inst *noteHost) RenderTree(k []uint64, recheck func() bool) (string, error) {
	return "", nil
}
func (inst *noteHost) OpsArrange(string, []uint64) error                         { return nil }
func (inst *noteHost) OpsRaise(uint64) error                                     { return nil }
func (inst *noteHost) OpsPlace(uint64, float32, float32, float32, float32) error { return nil }
func (inst *noteHost) SourceStatus(string) (capture.SourceResult, bool) {
	return capture.SourceResult{}, false
}
func (inst *noteHost) OpsRevisions(k uint64) (map[string]uint64, bool) {
	r, _ := inst.eng(k).SnapshotRevisions()
	return r, true
}
func (inst *noteHost) OpsUndo(k uint64, id string) bool { return inst.eng(k).Undo(id) }
func (inst *noteHost) OpsUndoStatus(k uint64, id string) (string, bool) {
	return inst.eng(k).UndoStatus(id)
}
func (inst *noteHost) OpsLogSince(k uint64, seq uint64) ([]opengine.LogEntry, uint64, bool) {
	e := inst.eng(k)
	return e.LogSince(seq), e.LogSeq(), true
}
func (inst *noteHost) OpsOpen(appId app.AppIdT, kind string, cfg []byte) (uint64, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	key := uint64(100 + len(inst.engines))
	n := &note{text: "first"}
	inst.engines[key], inst.notes[key] = opengine.New(noteOps.Catalog(), noteOps.Bind(n)), n
	return key, nil
}

func (inst *noteHost) frames(ctx context.Context) {
	for ctx.Err() == nil {
		inst.mu.Lock()
		engs := make([]*opengine.Engine, 0, len(inst.engines))
		for _, e := range inst.engines {
			engs = append(engs, e)
		}
		inst.mu.Unlock()
		for _, e := range engs {
			e.BeginFrame()
			e.ApplyQueued()
			e.TakeSnapshot()
			e.EndFrame()
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// scriptedModel answers with one scripted reply per call; past failAfter
// calls, when set, it fails them as a cancelled turn would.
type scriptedModel struct {
	mu        sync.Mutex
	replies   []openaichat.CompletionResponse
	seen      []openaichat.CompletionRequest
	failAfter int
}

func (inst *scriptedModel) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.seen = append(inst.seen, req)
	if inst.failAfter > 0 && len(inst.seen) > inst.failAfter {
		return openaichat.CompletionResponse{}, context.Canceled
	}
	if len(inst.replies) == 0 {
		return openaichat.CompletionResponse{Content: "nothing more", FinishReason: "stop"}, nil
	}
	r := inst.replies[0]
	inst.replies = inst.replies[1:]
	return r, nil
}
func (inst *scriptedModel) Close() (err error) { return }

func toolCall(id string, name string, args string) openaichat.CompletionResponse {
	return openaichat.CompletionResponse{FinishReason: "tool_calls",
		ToolCalls: []openaichat.ToolCall{{Id: id, Name: name, Arguments: args}}}
}

// ADR-0269 M5: one turn of the coordinator asks for access, opens a window
// it may open, reads it — untrusted, so delimited — and writes to it, every
// call through the host's dispatcher.
func TestTheCoordinatorsToolLoop(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","text":"tidied"}`),
		toolCall("s2", "call_operation", `{"window":100,"operation":"set_note","reason":"tidy it"}`),
		toolCall("c4", "call_operation", `{"window":100,"operation":"set_note","args":{"text":"tidied"},"reason":"tidy it"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRig(t, bus, model, false)

	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "done", res.final.Content)
	assert.Equal(t, "tidied", host.notes[100].text, "the write went through the host to the window")
	require.Len(t, res.activity, 6)
	assert.Len(t, res.calls, 7, "every model call of the turn is recorded, the answering one included")
	assert.Contains(t, res.activity[1], "opened notes as window 100")
	assert.Contains(t, res.activity[5], "set_note in window 100")

	var readBack string
	replies := map[string]string{}
	for _, m := range res.messages {
		if m.Role == openaichat.ChatRoleTool {
			replies[m.ToolCallId] = m.Content
		}
	}
	readBack = replies["c3"]
	assert.Contains(t, replies["s1"], "put text under args", "a key beside window and operation is named, not dropped")
	assert.Contains(t, replies["s2"], "you sent no args", "a refusal of missing args says where they go")
	assert.True(t, strings.HasPrefix(readBack, untrustedOpen), "an untrusted result reaches the model delimited: %s", readBack)
	assert.Contains(t, readBack, `"text":"first"`)
	task, tainted, _ := coord.state()
	assert.NotEmpty(t, task)
	assert.True(t, tainted)
	model.mu.Lock()
	assert.Len(t, model.seen[0].Tools, len((&coordinator{}).fixedTools()), "every call offers the fixed tools")
	model.mu.Unlock()
}

// A turn that called tools and stopped without an answer keeps its calls
// in the transcript and marks the person's message unanswered; history
// stays as it was, since an unanswered turn is not resent (ADR-0265 §SD3).
func TestAStoppedTurnKeepsItsCallsShown(t *testing.T) {
	conv := newConversation()
	req := conv.request("go")
	conv.begin("go", 1, false)
	conv.landTurn(req, &turnResult{activity: []string{"get_note in window 100 · completed"}, stopped: "the model kept calling tools past 24 rounds"}, nil, 2)
	require.Len(t, conv.entries, 2)
	assert.True(t, conv.entries[0].failed)
	assert.Contains(t, conv.entries[0].reason, "24 rounds")
	assert.Equal(t, speakerTool, conv.entries[1].speaker)
	assert.Empty(t, conv.history)
}

// coordRig wires a coordinator to a note host through the dispatcher under
// test grants, with model answering; opTools turns operation tools on.
func coordRig(t *testing.T, bus *inprocbus.Inst, model *scriptedModel, opTools bool) (host *noteHost, coord *coordinator, cli *llm.Client, req llm.Request, ctx context.Context) {
	t.Helper()
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: model})
	require.NoError(t, err)
	t.Cleanup(svc.Close)

	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(app.Manifest{Id: notesId, Display: "Notes", Summary: "keep a note",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.AllTopics[0]}, Operations: noteOps.Catalog(),
		Help: fstest.MapFS{"overview.md": {Data: []byte("# Notes\n\nA note holds one text.\n\n## Tidying\n\nTidy a note by replacing its text with set_note.\n")}}},
		func() (app.AppI, error) { return nil, nil }))
	host = &noteHost{engines: map[uint64]*opengine.Engine{}, notes: map[uint64]*note{}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go host.frames(ctx)
	hostClient := bus.NewClient(opwire.HostOpsAppId, []app.SubjectFilter{
		{Pattern: opwire.Pattern, Direction: app.CapDirectionSub},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	_, err = hostClient.Subscribe(opwire.Pattern, func(msg *app.Msg) {
		_, key, op, _ := opwire.ParseSubject(msg.Subject)
		req, _ := buscodec.Decode[opwire.CallRequest](msg.Payload)
		_ = buscodec.Reply(hostClient.Publish, msg.Reply, opwire.CallReply{V: opwire.WireVersion, Outcome: host.eng(key).Submit(op, req)})
	})
	require.NoError(t, err)
	asvc, err := agent.NewService(bus, zerolog.Nop(), agent.Config{Registry: reg, Host: host, TestGrants: true, Pace: time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(asvc.Close)

	chatBus := bus.NewClient(ManifestId, manifest.Caps)
	cli = llm.NewClient(chatBus)
	cli.Timeout = 10 * time.Second
	conv := newConversation()
	coord = newCoordinator(agent.NewClient(chatBus), nil, conv.id)
	req = conv.request("please tidy my note")
	req.Messages = append([]openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: coordinatorPrompt}}, req.Messages...)
	coord.opTools = opTools
	coord.offer(true, false)
	return
}

func toolReplies(msgs []openaichat.Message) (replies map[string]string) {
	replies = map[string]string{}
	for _, m := range msgs {
		if m.Role == openaichat.ChatRoleTool {
			replies[m.ToolCallId] = m.Content
		}
	}
	return
}

// A refusal says what to do next; the same refused call is not made again
// until something else went through; describe_app gives schemas as objects.
func TestRefusalsSayWhatToDoNextAndARepeatIsNotMade(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note"}`),
		toolCall("r2", "request_access", `{"plan":"tidy the note"}`),
		toolCall("r3", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","args":{}}`),
		toolCall("s2", "call_operation", `{"window":100,"operation":"set_note","args":{}}`),
		toolCall("d1", "describe_app", `{"app":"notes","operation":"set_note"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)

	assert.Contains(t, replies["r1"], `next: {"tool":"request_access"`, "a grant with nothing to open says to name the app")
	assert.Contains(t, replies["r2"], "same call that was just refused")
	assert.Contains(t, replies["r3"], "access granted")
	assert.Contains(t, replies["r3"], "the task may make ", "the answer states the grant's bounds")
	var s1 callOutcome
	require.NoError(t, json.Unmarshal([]byte(replies["s1"]), &s1))
	assert.Equal(t, "refused", s1.Phase)
	require.NotNil(t, s1.Next)
	assert.Equal(t, "call_operation", s1.Next.Tool)
	assert.Contains(t, string(s1.Next.ArgsSchema), `"properties"`, "a schema refusal carries the schema")
	assert.Contains(t, replies["s2"], "same call that was just refused")
	assert.Contains(t, replies["d1"], `"args_schema":{`, "schemas are JSON objects, not text")
}

// A refused call is not made again within its turn, and is made again in
// the next one: the person's message is a change, and a request that
// expired while they were away reaches their dialog again.
func TestARefusalIsForgottenWhenTheNextTurnStarts(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	_, coord, _, _, ctx := coordRig(t, bus, &scriptedModel{}, false)
	call := openaichat.ToolCall{Id: "r", Name: "request_access", Arguments: `{"plan":"tidy the note"}`}
	first, _ := coord.exec(ctx, toolOrigin{}, call)
	require.Contains(t, first, "next:", "a grant with nothing to open is refused")
	again, _ := coord.exec(ctx, toolOrigin{}, call)
	assert.Contains(t, again, "same call that was just refused")
	coord.forgetRefusals()
	next, _ := coord.exec(ctx, toolOrigin{}, call)
	assert.NotContains(t, next, "same call that was just refused")
}

// With operation tools on, a window's operations are typed tools of their
// own, and calling one goes through the dispatcher like call_operation.
func TestOperationToolsCallAWindowsOperationDirectly(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("g1", "w100_get_note", `{}`),
		toolCall("t1", "w100_set_note", `{"text":"typed","reason":"tidy it"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	host, coord, cli, req, ctx := coordRig(t, bus, model, true)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "done", res.final.Content)
	host.mu.Lock()
	assert.Equal(t, "typed", host.notes[100].text)
	host.mu.Unlock()

	model.mu.Lock()
	defer model.mu.Unlock()
	assert.Len(t, model.seen[0].Tools, len((&coordinator{}).fixedTools()), "no window, no operation tools")
	var names []string
	for _, tl := range model.seen[len(model.seen)-1].Tools {
		names = append(names, tl.Name)
	}
	assert.Contains(t, names, "w100_set_note")
	assert.Contains(t, names, "call_operation", "the fixed tools stay")
}

func TestARemedyOfDestinationsAsksForThem(t *testing.T) {
	coord := newCoordinator(nil, nil, "c")
	n := coord.nextFor(2, "run", &agent.Remedy{Destinations: []string{"clickhouse:localhost:8123"}})
	require.NotNil(t, n)
	assert.Equal(t, "request_access", n.Tool)
	assert.Equal(t, []string{"clickhouse:localhost:8123"}, n.Args["destinations"])
	assert.Contains(t, n.Then, "run in window 2 again")
	assert.Nil(t, coord.nextFor(2, "run", nil))
}

// The apps' help reaches the model: describe_app marks an app that has
// some, and read_help searches it, lists it and reads a section.
func TestReadHelpGivesTheModelTheAppsDocumentation(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("d1", "describe_app", `{}`),
		toolCall("h1", "read_help", `{"search":"tidy"}`),
		toolCall("h2", "read_help", `{"app":"notes"}`),
		toolCall("h3", "read_help", `{"app":"notes","doc":"overview","section":"tidying"}`),
		toolCall("h4", "read_help", `{"app":"notes","doc":"nope"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)

	assert.Contains(t, replies["d1"], `"help":true`)
	assert.Contains(t, replies["h1"], `"section":"tidying"`)
	assert.Contains(t, replies["h2"], `"doc":"overview"`)
	assert.Contains(t, replies["h3"], "replacing its text with set_note")
	assert.False(t, strings.HasPrefix(replies["h3"], untrustedOpen), "an app's own help is not untrusted content")
	assert.Contains(t, replies["h4"], "no help document")
	_, tainted, _ := coord.state()
	assert.False(t, tainted, "reading help taints nothing")
}

// A model that would call tools forever is asked, on the last round, to
// answer without them; the turn ends with that answer, and the note asking
// for it stays out of the history.
func TestTheLastRoundAsksForAnAnswer(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	var replies []openaichat.CompletionResponse
	for i := 0; i < defaultRounds-1; i++ {
		replies = append(replies, toolCall("l"+strconv.Itoa(i), "list_windows", `{}`))
	}
	replies = append(replies, openaichat.CompletionResponse{Content: "Here is what I found.", FinishReason: "stop"})
	model := &scriptedModel{replies: replies}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	var rounds []int
	res, err := runTurn(ctx, cli, coord, req, func(round int, doing string) {
		if len(rounds) == 0 || rounds[len(rounds)-1] != round {
			rounds = append(rounds, round)
		}
	})
	require.NoError(t, err)
	assert.Empty(t, res.stopped)
	assert.Equal(t, "Here is what I found.", res.final.Content)
	assert.Len(t, rounds, defaultRounds, "every round is reported")

	model.mu.Lock()
	defer model.mu.Unlock()
	require.Len(t, model.seen, defaultRounds)
	last := model.seen[defaultRounds-1]
	assert.Equal(t, "none", last.ToolChoice)
	assert.Equal(t, lastRoundNote, last.Messages[len(last.Messages)-1].Content)
	assert.Empty(t, model.seen[0].ToolChoice, "earlier rounds leave the choice to the model")
	for _, m := range res.messages {
		assert.NotEqual(t, lastRoundNote, m.Content, "the note is not resent next turn")
	}
}

// A call's title is the person's: shown while it runs and in the
// transcript, never dispatched, and no part of what makes a call the same.
func TestATitleIsShownAndNotDispatched(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}],"title":"Asking for the notes"}`),
		toolCall("o1", "open_window", `{"app":"notes","title":"Opening notes"}`),
		toolCall("g1", "call_operation", `{"window":100,"operation":"get_note","args":{},"title":"Reading the note\nnow"}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","args":{},"title":"Writing it"}`),
		toolCall("s2", "call_operation", `{"window":100,"operation":"set_note","args":{},"title":"Writing it, again"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	var doing []string
	res, err := runTurn(ctx, cli, coord, req, func(round int, d string) {
		if d != "" {
			doing = append(doing, d)
		}
	})
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	assert.Contains(t, res.activity[2], "Reading the note now · get_note in window 100", "one line, before the call's own line")
	assert.NotContains(t, replies["g1"], "error", "the title is no stray key")
	assert.Contains(t, replies["s2"], "same call that was just refused", "a new title does not make a new call")
	assert.Equal(t, []string{"Asking for the notes", "Opening notes", "Reading the note now", "Writing it", "Writing it, again"}, doing)

	model.mu.Lock()
	defer model.mu.Unlock()
	for _, tl := range model.seen[0].Tools {
		assert.Contains(t, string(tl.Parameters), `"title"`, tl.Name)
	}
}

func TestAnOperationsOwnTitleArgumentStaysItsOwn(t *testing.T) {
	_, titled := withReason(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`)
	assert.False(t, titled)
	v, titled := withReason(`{"type":"object","properties":{"text":{"type":"string"}}}`)
	assert.True(t, titled)
	assert.Contains(t, string(v), `"reason"`)
	assert.Equal(t, strings.Repeat("x", maxTitleRunes-1)+"…", cleanTitle(strings.Repeat("x", 100)))
}

// A task that is gone does not strand the conversation: the call says so,
// the grant is forgotten, and the next request_access starts a new task.
func TestAnEndedTaskIsReplacedByTheNextRequest(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		{Content: "asked", FinishReason: "stop"},
		toolCall("g1", "list_windows", `{}`),
		toolCall("r2", "request_access", `{"plan":"tidy the note again","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("g2", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	_, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	first, _, _ := coord.state()
	require.NotEmpty(t, first)
	coord.mu.Lock()
	coord.grant.Handle = "no-such-handle" // the task is gone
	coord.mu.Unlock()

	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	assert.Contains(t, replies["g1"], "request_access starts a new one")
	assert.Contains(t, replies["r2"], "access granted")
	assert.NotContains(t, replies["g2"], "denied")
	second, _, _ := coord.state()
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second, "a new task")
}

// A model names the apps to open as it likes — a bare string, the display
// name — and the window opens; the grant's reply says what open_window may
// open. An item naming no app is refused by name, and nothing is asked.
func TestOpenTakesTheShapesModelsWrite(t *testing.T) {
	for _, open := range []string{`["notes"]`, `[{"app":"Notes"}]`, `"notes"`} {
		bus := inprocbus.NewInst(zerolog.Nop())
		model := &scriptedModel{replies: []openaichat.CompletionResponse{
			toolCall("c1", "request_access", `{"plan":"p","open":`+open+`}`),
			toolCall("c2", "open_window", `{"app":"notes"}`),
			{Content: "done", FinishReason: "stop"},
		}}
		_, coord, cli, req, ctx := coordRig(t, bus, model, false)
		res, err := runTurn(ctx, cli, coord, req, nil)
		require.NoError(t, err)
		r := toolReplies(res.messages)
		assert.Contains(t, r["c1"], "open_window may now open:", open)
		assert.Contains(t, r["c2"], `"window":100`, open)
	}

	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"p","open":[{"name":"notes"}]}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Contains(t, toolReplies(res.messages)["c1"], "this item names no app")
	task, _, _ := coord.state()
	assert.Empty(t, task, "nothing was asked")
}

// A refused open_window says what to do: ask for the app when the grant
// does not name it, look it up when no app answers to the name.
func TestARefusedOpenSaysWhatToDoNext(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"p","open":["notes"]}`),
		toolCall("c2", "open_window", `{"app":"notepad"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	r := toolReplies(res.messages)
	assert.Contains(t, r["c2"], `no app named "notepad"`)
	assert.Contains(t, r["c2"], `next: {"tool":"describe_app"`)
	assert.Contains(t, openNext("notes", "the grant does not let the task open windows of x"), `"tool":"request_access"`)
}

// arrange_windows needs the desktop: refused with the request_access that
// asks for it, and completed once that is granted (ADR-0276 §SD4).
func TestArrangeWindowsAsksForTheDesktop(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the desktop","open":["notes"]}`),
		toolCall("a1", "arrange_windows", `{"command":"tile"}`),
		toolCall("r2", "request_access", `{"plan":"tidy the desktop","desktop":true}`),
		toolCall("a2", "arrange_windows", `{"command":"columns","windows":[100]}`),
		toolCall("x1", "raise_window", `{"window":7}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	assert.Contains(t, replies["a1"], "input_required")
	assert.Contains(t, replies["a1"], `"desktop":true`, "the refusal names the request that lifts it")
	assert.Contains(t, replies["r2"], "access granted")
	assert.Contains(t, replies["a2"], "completed")
	assert.Contains(t, replies["x1"], "does not cover", "a window outside the task is not raised")
}

// The settings are a ceiling the host enforces (ADR-0280): a model asking
// for more than they allow is refused before anyone is asked, and is told
// what they allow on the turn they change.
func TestTheSettingsCeilingBindsTheModel(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		{Content: "I may only read.", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	read := permissions{allow: allowRead}
	coord.setOptions(read.ceiling(true), false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)

	assert.Contains(t, toolReplies(res.messages)["r1"], "open windows", "the host refuses the launch the settings do not allow")
	assert.Empty(t, coord.handle(), "no task was started")
	var told string
	for _, m := range res.messages {
		if m.Role == openaichat.ChatRoleSystem && strings.Contains(m.Content, "settings let you at most") {
			told = m.Content
		}
	}
	assert.Contains(t, told, "at most: read")
	assert.Empty(t, coord.ceilingNote(), "told once, until the settings move")
	coord.setOptions(defaultPermissions().ceiling(true), false)
	assert.Contains(t, coord.ceilingNote(), "at most: run")
}

// The settings map onto the ladder: without Apps the model only talks, and
// Ask first keeps every change a proposal.
func TestPermissionsBecomeACeiling(t *testing.T) {
	assert.Equal(t, agent.LevelTalk, defaultPermissions().ceiling(false).Level())
	def := defaultPermissions().ceiling(true)
	assert.Equal(t, agent.LevelRun, def.Level())
	assert.Equal(t, agent.ModeAct, def.Mode)
	assert.Equal(t, agent.ModeObserve, permissions{allow: allowRead}.ceiling(true).Mode)
	ask := permissions{allow: allowEdit, changes: changesAsk}.ceiling(true)
	assert.Equal(t, agent.ModeSuggest, ask.Mode)
	assert.Equal(t, agent.LevelEdit, ask.Level())
	assert.Equal(t, agent.LevelOutside, permissions{allow: allowOutside, changes: changesApply}.ceiling(true).Level())
	assert.Greater(t, def.Score(true).Position, def.Score(false).Position, "a model off this machine sits higher in its band")
	assert.False(t, def.Unpaced, "paced unless the person lifts it")
	fast := defaultPermissions()
	fast.unpaced = true
	assert.Greater(t, fast.ceiling(true).Score(false).Position, def.Score(false).Position, "speed moves the position, not the level")
	assert.Equal(t, def.Level(), fast.ceiling(true).Level())
}

// Only the host's denial ends a task: a result whose text reads like one
// leaves the grant in place.
func TestAResultThatReadsLikeAnEndedTaskKeepsTheTask(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		toolCall("o1", "open_window", `{"app":"notes"}`),
		toolCall("g1", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","args":{"text":"the task ended"}}`),
		toolCall("g2", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	replies := toolReplies(res.messages)
	require.Contains(t, replies["g2"], "the task ended", "the app's text reaches the model")
	assert.NotContains(t, replies["g2"], "request_access starts a new one")
	task, _, _ := coord.state()
	assert.NotEmpty(t, task, "the grant is kept")
	assert.NotEmpty(t, coord.handle())
}

// A model that calls a tool on the last round, though it was offered none,
// stops the turn, and that call is not made.
func TestACallOnTheLastRoundIsNotMade(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	var replies []openaichat.CompletionResponse
	for i := 0; i < defaultRounds; i++ {
		replies = append(replies, toolCall("l"+strconv.Itoa(i), "list_windows", `{}`))
	}
	model := &scriptedModel{replies: replies}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Contains(t, res.stopped, "past 24 rounds")
	assert.Len(t, res.activity, defaultRounds-1, "the last round's call is not made")
	assert.Len(t, res.calls, defaultRounds)
}

// A turn stopped after a round answered keeps that round's call in the
// statistics: the host's trail holds it, and the panel must not drop it.
func TestAStoppedTurnKeepsItsAnsweredCalls(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{failAfter: 1, replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)

	_, err := runTurn(ctx, cli, coord, req, nil)
	require.ErrorIs(t, err, context.Canceled)
	answered := coord.answeredCalls()
	require.Len(t, answered, 1, "the round that answered")

	var st chatStats
	st.addTurn("conv", "turn-1", time.Now(), time.Now().UnixMilli(), &turnResult{calls: answered}, context.Canceled)
	require.Len(t, st.turns, 1)
	assert.Equal(t, outcomeCancelled, st.turns[0].outcome)
	assert.Equal(t, 1, st.turns[0].rounds)
	assert.Len(t, st.calls, 1)
}
