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
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
)

// App is one chat window.
type App struct {
	ids    *c.WidgetIdStack
	logger zerolog.Logger
	cli    *llm.Client

	// describe asks the host once whether it offers a model.
	describe bgjob.Runner[llm.Description]
	model    llm.Description
	answered bool

	// turn runs one completion at a time; pending is what it was started
	// with, so the answer lands on the conversation that asked.
	turn    bgjob.Runner[llm.Response]
	pending *pendingTurn

	conv *conversation
	// keepNext is the Keep toggle's value for the next conversation.
	keepNext bool
	// draft is the composer's text, bound to the text input.
	draft string
	view  chatview.State
	// focused is whether this window is the shell's active one, which
	// gates the process-wide Ctrl+Enter chord (play's claimRunChord).
	focused bool
	// pastNoted hides the "past conversations are in play" line once read.
	pastNoted bool
}

// pendingTurn is a turn in flight.
type pendingTurn struct {
	conv    *conversation
	req     llm.Request
	started time.Time
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), keepNext: true, draft: DraftSeed.Get()}
	inst.conv = newConversation(inst.keepNext)
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's id stack and bus and asks, off the frame, whether
// a model is offered.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	if bus := ctx.Bus(); bus != nil {
		inst.cli = llm.NewClient(bus)
		cli := inst.cli
		inst.describe.Start(nil, bgjob.Spec{Kind: "chat-llm-describe", Title: "model"},
			func(ctx context.Context) (d *llm.Description, err error) {
				got, err := cli.Describe(ctx)
				if err != nil {
					return
				}
				d = &got
				return
			})
	}
	return
}

// Unmount cancels a turn in flight; the client tells the host to stop the
// call (llm.cancel, ADR-0254).
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.turn.Cancel()
	inst.describe.Cancel()
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
	if d, _, ok := inst.describe.TakeResult(); ok {
		inst.model, inst.answered = *d, true
	} else if snap := inst.describe.Snapshot(); snap.State == bgjob.StateFailed {
		inst.describe.Invalidate()
		inst.model = llm.Description{Reason: "the host did not answer llm.describe: " + errText(snap.Err)}
		inst.answered = true
	}
	p := inst.pending
	if p == nil {
		return
	}
	if res, _, ok := inst.turn.TakeResult(); ok {
		p.conv.land(p.req, res, nil, time.Now().UnixMilli())
		inst.pending = nil
		return
	}
	if snap := inst.turn.Snapshot(); snap.State == bgjob.StateFailed {
		inst.turn.Invalidate()
		err := snap.Err
		if err == nil {
			err = errors.New("the turn failed")
		}
		p.conv.land(p.req, nil, err, time.Now().UnixMilli())
		inst.pending = nil
	}
}

// send starts a turn for the draft. The request is built on the render
// thread; the job touches only its copy.
func (inst *App) send() {
	text := inst.draft
	if inst.cli == nil || inst.pending != nil || isBlank(text) {
		return
	}
	conv := inst.conv
	req := conv.request(text)
	cli := inst.cli
	ok := inst.turn.StartReporting(nil, bgjob.Spec{Kind: "chat-turn", Title: "answer"},
		func(ctx context.Context, _ bgjob.Reporter) (res *llm.Response, err error) {
			got, err := cli.Complete(ctx, req)
			if err != nil {
				return
			}
			res = &got
			return
		})
	if !ok {
		return
	}
	conv.begin(text, time.Now().UnixMilli())
	inst.pending = &pendingTurn{conv: conv, req: req, started: time.Now()}
	inst.draft = ""
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
}

// newConversation abandons a turn in flight and starts over.
func (inst *App) newConversation() {
	if inst.pending != nil {
		inst.turn.Cancel()
		inst.pending = nil
	}
	inst.turn.Invalidate()
	inst.conv = newConversation(inst.keepNext)
	inst.view = chatview.State{}
}

func isBlank(s string) (yes bool) {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func errText(err error) (s string) {
	if err == nil {
		return "no reason given"
	}
	return err.Error()
}
