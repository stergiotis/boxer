// Package chat is a chat with the host's model (ADR-0265): one conversation
// per window, held in memory for the window's session, each turn sent as
// one llm.retain.complete — or llm.complete when the user turned keeping
// off — and nothing read back. Past conversations are read in play over
// SQL on the llmMessage kind (ADR-0264 §SD6).
//
// The turn loop lives in chat_conversation.go and touches no UI; this file owns
// the app's lifecycle and the two background jobs, chat_render.go the frame.
package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
)

// App is one chat window.
type App struct {
	ids *c.WidgetIdStack
	cli *llm.Client

	// describe asks the host once whether it offers a model.
	describe bgjob.Runner[llm.Description]
	model    llm.Description
	answered bool

	// turn runs one completion at a time; pending is what it was started
	// with. New conversation invalidates the run, so a late answer never
	// lands on the conversation that replaced the one that asked.
	turn    bgjob.Runner[turnResult]
	pending *pendingTurn

	// apps lets the model work in windows the person shares (ADR-0269):
	// the turn becomes a tool loop over runtime.agent. coord is the
	// conversation's side of it, renewed with the conversation.
	apps     bool
	agentCli *agent.Client
	coord    *coordinator

	conv *conversation
	// keep is the Keep toggle; a conversation takes it at its first send.
	keep bool
	// draft is the composer's text, bound to the text input; hlJob colours
	// it as markdown, rebuilt only when hlSrc no longer equals it.
	draft string
	hlSrc string
	hlJob typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	hlOk  bool
	view  chatview.State
	// focused is whether this window is the shell's active one, which
	// gates the process-wide Ctrl+Enter chord (play's claimRunChord).
	focused bool
	// draftId is the composer's widget id: the Escape it captures while a
	// turn runs cancels the turn.
	draftId uint64
	// pastNoted hides the "past conversations are in play" line once read.
	pastNoted bool

	// editing is the turn Edit took back, while it can be put back;
	// editedNext marks the next send as an edit. action runs a copy or an
	// open, and note is the status line its outcome lands on.
	editing    *rewound
	editedNext bool
	action     bgjob.Runner[string]
	note       status
	// later holds the actions clicked while the transcript draws, run
	// once it is drawn.
	later []func()

	// advanced shows the Statistics panel (AdvancedSeed); stats are the
	// window's records, across its conversations, and showStats whether
	// the panel is open. handover publishes them and opens play.
	advanced     bool
	showStats    bool
	stats        chatStats
	bus          app.BusI
	pubs         statsPublishers
	handover     bgjob.Runner[string]
	handoverNote string
	bandKeys     [3]ecdf.BandJobKey
}

// pendingTurn is a turn in flight.
type pendingTurn struct {
	req     llm.Request
	started time.Time
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), keep: true, draft: DraftSeed.Get(), conv: newConversation(), apps: AppsSeed.Get(),
		advanced: AdvancedSeed.Get(), pubs: newStatsPublishers()}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's id stack and bus and asks, off the frame, whether
// a model is offered.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	bus := ctx.Bus()
	if bus == nil {
		inst.model, inst.answered = llm.Description{Reason: "this host gives the app no bus"}, true
		return
	}
	cli := llm.NewClient(bus)
	inst.cli, inst.bus = cli, bus
	for i := range inst.bandKeys {
		inst.bandKeys[i] = ecdf.BandJobKey(inst.ids.ProbeSeq("chat-stats-band-" + strconv.Itoa(i)))
	}
	inst.agentCli = agent.NewClient(bus)
	inst.coord = newCoordinator(inst.agentCli, inst.conv.id)
	inst.describe.Start(nil, bgjob.Spec{Kind: "chat-llm-describe", Title: "model"},
		func(ctx context.Context) (d *llm.Description, err error) {
			got, err := cli.Describe(ctx)
			if err != nil {
				return
			}
			d = &got
			return
		})
	return
}

// Unmount cancels a turn in flight; the client tells the host to stop the
// call (llm.cancel, ADR-0254).
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.turn.Cancel()
	inst.describe.Cancel()
	inst.handover.Cancel()
	inst.action.Cancel()
	for _, k := range inst.bandKeys {
		ecdf.CancelBandJob(k)
	}
	return
}

// Frame renders one frame.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.focused = true
	if f, ok := ctx.(app.WindowFocusI); ok {
		inst.focused = f.WindowFocused()
	}
	inst.drain()
	inst.render()
	return
}

// drain lands the background jobs' results on the render thread.
func (inst *App) drain() {
	inst.drainHandover()
	inst.drainAction()
	if d, _, ok := inst.describe.TakeResult(); ok {
		inst.model, inst.answered = *d, true
	} else if snap := inst.describe.Snapshot(); snap.State == bgjob.StateFailed {
		inst.describe.Invalidate()
		reason := "the host did not answer llm.describe"
		if snap.Err != nil {
			reason += ": " + snap.Err.Error()
		}
		inst.model, inst.answered = llm.Description{Reason: reason}, true
	}
	p := inst.pending
	if p == nil {
		return
	}
	now := time.Now().UnixMilli()
	// A landed turn is read from its start: the question at the top of the
	// view, then the answer — a long answer is not entered at its end.
	asked := int32(inst.conv.lastUser())
	if res, _, ok := inst.turn.TakeResult(); ok {
		inst.stats.addTurn(inst.conv.id, p.started, now, res, nil)
		inst.conv.landTurn(p.req, res, nil, now)
		inst.pending = nil
		inst.view.ScrollToStart(asked)
		return
	}
	defer func() {
		if inst.pending == nil {
			inst.view.ScrollToStart(asked)
		}
	}()
	switch snap := inst.turn.Snapshot(); snap.State {
	case bgjob.StateFailed:
		inst.turn.Invalidate()
		err := snap.Err
		if err == nil {
			err = errors.New("the turn failed")
		}
		inst.stats.addTurn(inst.conv.id, p.started, now, nil, err)
		inst.conv.land(p.req, nil, err, now)
		inst.pending = nil
	case bgjob.StateIdle:
		// A cancelled run resets to idle without a result or an error
		// (bgjob's contract), so idle with a turn pending is the cancel.
		inst.stats.addTurn(inst.conv.id, p.started, now, nil, context.Canceled)
		inst.conv.land(p.req, nil, context.Canceled, now)
		inst.pending = nil
	}
}

// send starts a turn for the draft and clears the composer.
func (inst *App) send() {
	if inst.startTurn(inst.draft) {
		inst.draft = ""
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
	}
}

// startTurn sends text as the next turn, unless one is in flight. The
// request is built here, on the render thread; the job touches only its
// copy. The first send fixes the conversation's Keep.
func (inst *App) startTurn(text string) (started bool) {
	if inst.cli == nil || inst.pending != nil || strings.TrimSpace(text) == "" {
		return false
	}
	conv := inst.conv
	if !conv.started {
		conv.keep, conv.apps = inst.keep, inst.apps
	}
	req := conv.request(text)
	cli := inst.cli
	var coord *coordinator
	if conv.apps && inst.coord != nil {
		coord = inst.coord
		if req.Messages[0].Role != openaichat.ChatRoleSystem {
			req.Messages = append([]openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: coordinatorPrompt}}, req.Messages...)
		}
	}
	ok := inst.turn.StartReporting(nil, bgjob.Spec{Kind: "chat-turn", Title: "answer"},
		func(ctx context.Context, report bgjob.Reporter) (res *turnResult, err error) {
			if coord != nil {
				return runTurn(ctx, cli, coord, req, func(round int, doing string) {
					note := "round " + strconv.Itoa(round+1) + " of " + strconv.Itoa(maxRounds)
					if doing != "" {
						note += " · " + doing
					}
					report(uint64(round+1), maxRounds, note)
				})
			}
			got, err := cli.Complete(ctx, req)
			if err != nil {
				return
			}
			res = &turnResult{final: got, calls: []callStat{callStatOf(0, got)}, messages: append(append([]openaichat.Message(nil), req.Messages...),
				openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: got.Content, ToolCalls: got.ToolCalls})}
			return
		})
	if !ok {
		return false
	}
	conv.begin(text, time.Now().UnixMilli(), inst.editedNext)
	inst.pending = &pendingTurn{req: req, started: time.Now()}
	inst.editing, inst.editedNext = nil, false
	// The waiting bubble is at the end: follow it.
	inst.view.SetFollow(true)
	return true
}

// newConversation stops a turn in flight — Invalidate cancels it and drops
// its answer — and starts over.
func (inst *App) newConversation() {
	inst.turn.Invalidate()
	inst.pending = nil
	if inst.coord != nil {
		if h := inst.coord.handle(); h != "" {
			// A task belongs to one conversation (ADR-0269 §SD6).
			cli := inst.agentCli
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), agent.DefaultTimeout)
				defer cancel()
				_ = cli.Stop(ctx, h)
			}()
		}
	}
	inst.conv = newConversation()
	inst.editing, inst.editedNext = nil, false
	if inst.agentCli != nil {
		inst.coord = newCoordinator(inst.agentCli, inst.conv.id)
	}
	inst.view = chatview.State{}
}
