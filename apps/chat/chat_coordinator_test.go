package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
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
func (inst *noteHost) OpsAttach(k uint64, a bool) bool     { inst.eng(k).SetAttached(a); return true }
func (inst *noteHost) OpsCapture(k uint64) (string, error) { return "", nil }
func (inst *noteHost) OpsCaptureStatus(string) (opwire.CaptureStatus, bool) {
	return opwire.CaptureStatus{}, false
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

// scriptedModel answers with one scripted reply per call.
type scriptedModel struct {
	mu      sync.Mutex
	replies []openaichat.CompletionResponse
	seen    []openaichat.CompletionRequest
}

func (inst *scriptedModel) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.seen = append(inst.seen, req)
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
		toolCall("c3", "call", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("c4", "call", `{"window":100,"operation":"set_note","args":{"text":"tidied"},"reason":"tidy it"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: model})
	require.NoError(t, err)
	t.Cleanup(svc.Close)

	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(app.Manifest{Id: notesId, Display: "Notes", Summary: "keep a note",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.AllTopics[0]}, Operations: noteOps.Catalog()},
		func() (app.AppI, error) { return nil, nil }))
	host := &noteHost{engines: map[uint64]*opengine.Engine{}, notes: map[uint64]*note{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	asvc, err := agent.NewService(bus, zerolog.Nop(), agent.Config{Registry: reg, Host: host, TestGrants: true})
	require.NoError(t, err)
	t.Cleanup(asvc.Close)

	chatBus := bus.NewClient(ManifestId, manifest.Caps)
	cli := llm.NewClient(chatBus)
	cli.Timeout = 10 * time.Second
	conv := newConversation()
	coord := newCoordinator(agent.NewClient(chatBus), conv.id)
	req := conv.request("please tidy my note")
	req.Messages = append([]openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: coordinatorPrompt}}, req.Messages...)

	res, err := runTurn(ctx, cli, coord, req)
	require.NoError(t, err)
	assert.Equal(t, "done", res.final.Content)
	assert.Equal(t, "tidied", host.notes[100].text, "the write went through the host to the window")
	require.Len(t, res.activity, 4)
	assert.Contains(t, res.activity[1], "opened notes as window 100")
	assert.Contains(t, res.activity[3], "set_note in window 100")

	var readBack string
	for _, m := range res.messages {
		if m.Role == openaichat.ChatRoleTool && m.ToolCallId == "c3" {
			readBack = m.Content
		}
	}
	assert.True(t, strings.HasPrefix(readBack, untrustedOpen), "an untrusted result reaches the model delimited: %s", readBack)
	assert.Contains(t, readBack, `"text":"first"`)
	task, tainted, _ := coord.state()
	assert.NotEmpty(t, task)
	assert.True(t, tainted)
	model.mu.Lock()
	assert.Len(t, model.seen[0].Tools, 6, "every call offers the fixed tools")
	model.mu.Unlock()
}

// A turn that called tools and stopped without an answer keeps its calls
// in the transcript and marks the person's message unanswered; history
// stays as it was, since an unanswered turn is not resent (ADR-0265 §SD3).
func TestAStoppedTurnKeepsItsCallsShown(t *testing.T) {
	conv := newConversation()
	req := conv.request("go")
	conv.begin("go", 1)
	conv.landTurn(req, &turnResult{activity: []string{"get_note in window 100 · completed"}, stopped: "the model kept calling tools past 24 rounds"}, nil, 2)
	require.Len(t, conv.entries, 2)
	assert.True(t, conv.entries[0].failed)
	assert.Contains(t, conv.entries[0].reason, "24 rounds")
	assert.Equal(t, speakerTool, conv.entries[1].speaker)
	assert.Empty(t, conv.history)
}
