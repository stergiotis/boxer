package chat

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// turnStateE is where the conversation's turn stands (ADR-0265 §SD8): what a
// running turn is waiting on, or how the last one ended. Before it was named,
// the answer was spread over the pending turn, the job's state, the edit in
// progress and the question form, and "busy" looked the same whether the
// model was thinking or a dialog was waiting on the person.
//
// The window never drives the machine: each frame observes the state
// (observeTurn) and mirrors it, as play does for its query result and
// jackstay for its plan. The rules below draw the graph the state chip opens
// and name what causes each edge.
type turnStateE uint8

const (
	turnIdle      turnStateE = iota // no message was sent in this conversation
	turnModel                       // a model call is in flight
	turnTool                        // a tool the model called is running
	turnPerson                      // the turn waits on the person: a request for access, a question, a confirmation
	turnAnswered                    // the last turn was answered
	turnStopped                     // the last turn called tools and ended without an answer
	turnFailed                      // the last turn's model call failed or was refused
	turnCancelled                   // the person cancelled the last turn
	turnEditing                     // the last turn is taken back, its message in the composer
)

var allTurnStates = []turnStateE{
	turnIdle, turnModel, turnTool, turnPerson, turnAnswered, turnStopped, turnFailed, turnCancelled, turnEditing,
}

func (inst turnStateE) String() (s string) {
	switch inst {
	case turnIdle:
		return "idle"
	case turnModel:
		return "model"
	case turnTool:
		return "tool"
	case turnPerson:
		return "your turn"
	case turnAnswered:
		return "answered"
	case turnStopped:
		return "stopped"
	case turnFailed:
		return "failed"
	case turnCancelled:
		return "cancelled"
	case turnEditing:
		return "editing"
	}
	return "?"
}

// running says a turn is in flight in this state.
func (inst turnStateE) running() (yes bool) {
	return inst == turnModel || inst == turnTool || inst == turnPerson
}

// stageE is what a running tool loop is doing, as the coordinator reports
// it from its goroutine.
type stageE uint8

const (
	stageModel stageE = iota
	stageTool
	stagePerson
)

// observeTurn derives the turn's state from this frame, with no memory of
// its own: a turn in flight outranks an edit, an edit what the transcript
// holds. A running tool loop says which of the model, a tool and the person
// it waits on; a plain turn waits on the model.
func (inst *App) observeTurn() (s turnStateE) {
	switch {
	case inst.pending != nil:
		if inst.coord == nil {
			return turnModel
		}
		switch inst.coord.stageNow() {
		case stageTool:
			return turnTool
		case stagePerson:
			return turnPerson
		}
		return turnModel
	case inst.editing != nil:
		return turnEditing
	}
	return inst.conv.lastOutcome()
}

// lastOutcome is how the conversation's last turn ended, read off the
// transcript: the last message the person sent and what follows it.
func (inst *conversation) lastOutcome() (s turnStateE) {
	i := inst.lastUser()
	if i < 0 {
		return turnIdle
	}
	e := &inst.entries[i]
	switch {
	case e.stopped:
		return turnStopped
	case e.failed && e.fail.kind == "cancelled":
		return turnCancelled
	case e.failed:
		return turnFailed
	}
	return turnAnswered
}

// mirrorTurn moves the machine to the state this frame observed. A frame
// samples the turn, so a tool call that starts and ends between two frames
// shows as an edge that skips it; that is a declared path taken unwatched
// and logs at debug. An observation no declared path reaches contradicts the
// model and logs a warning: the kind of state nobody named.
func (inst *App) mirrorTurn() {
	m := inst.turnMachine
	cur, obs := m.Current(), inst.observeTurn()
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := inst.log.Warn()
	msg := "chat: the turn moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = inst.log.Debug()
		msg = "chat: the turn skipped states no frame sampled (mirrored)"
	} else {
		inst.turnOffGraph++
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// newTurnMachine declares the turn's lifecycle. A turn starts at a send and
// alternates between the model, its tools and the person until it ends
// answered, stopped, failed or cancelled. From an ended turn the person sends
// the next, takes the last one back to edit it, or — after a failure — puts
// its message back, which leaves the conversation where the turn before it
// ended. New conversation returns to idle from anywhere.
func newTurnMachine() (m *fsmview.Machine[turnStateE]) {
	m = fsmview.NewMachine(turnIdle, 64, fsmview.MachineOptions[turnStateE]{
		Label:      func(s turnStateE) string { return s.String() },
		StateOrder: allTurnStates,
		StateColor: turnColor,
	})
	ended := []turnStateE{turnAnswered, turnStopped, turnFailed, turnCancelled}
	m.AddRule(turnIdle, turnModel).
		AddRule(turnModel, turnTool, turnPerson, turnAnswered, turnStopped, turnFailed, turnCancelled, turnIdle).
		AddRule(turnTool, turnModel, turnPerson, turnAnswered, turnStopped, turnFailed, turnCancelled, turnIdle).
		AddRule(turnPerson, turnTool, turnModel, turnAnswered, turnStopped, turnFailed, turnCancelled, turnIdle).
		AddRule(turnEditing, turnModel, turnAnswered, turnIdle).
		EdgeLabel(turnIdle, turnModel, "Send").
		EdgeLabel(turnModel, turnTool, "the model calls a tool").
		EdgeLabel(turnModel, turnAnswered, "answer").
		EdgeLabel(turnModel, turnFailed, "refused or failed").
		EdgeLabel(turnModel, turnStopped, "failed after tool calls, or out of rounds").
		EdgeLabel(turnTool, turnModel, "the tool returns").
		EdgeLabel(turnTool, turnPerson, "asks you").
		EdgeLabel(turnPerson, turnTool, "you decide").
		EdgeLabel(turnEditing, turnModel, "Send").
		EdgeLabel(turnEditing, turnAnswered, "Cancel edit")
	for _, s := range []turnStateE{turnModel, turnTool, turnPerson} {
		m.EdgeLabel(s, turnCancelled, "Cancel").EdgeLabel(s, turnIdle, "New conversation")
	}
	for _, s := range ended {
		// The next send; New conversation; and, for a turn that got no
		// answer, its message put back, which shows the turn before it.
		m.AddRule(s, turnModel, turnIdle).
			EdgeLabel(s, turnModel, "Send").
			EdgeLabel(s, turnIdle, "New conversation")
		if s != turnAnswered {
			for _, back := range ended {
				if back != s {
					m.AddRule(s, back)
				}
			}
		}
	}
	m.AddRule(turnAnswered, turnEditing).EdgeLabel(turnAnswered, turnEditing, "Edit")
	m.EdgeLabel(turnEditing, turnIdle, "New conversation")
	return
}

// turnColor tints the graph's nodes: the running states in the accent, the
// one that waits on the person as a warning, the outcomes by how they ended.
func turnColor(s turnStateE, _ bool) styletokens.RGBA8 {
	switch s {
	case turnModel, turnTool:
		return styletokens.AccentDefault
	case turnPerson, turnStopped, turnEditing:
		return styletokens.WarningDefault
	case turnAnswered:
		return styletokens.SuccessDefault
	case turnFailed:
		return styletokens.ErrorDefault
	case turnCancelled:
		return styletokens.NeutralDefault
	}
	return styletokens.NeutralSubtle
}

// turnTone is turnColor for the chip's badge.
func turnTone(s turnStateE) badge.ToneE {
	switch s {
	case turnModel, turnTool:
		return badge.TonePrimary
	case turnPerson, turnStopped, turnEditing:
		return badge.ToneWarning
	case turnAnswered:
		return badge.ToneSuccess
	case turnFailed:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// newTurnChip builds the state chip; it opens the inspector with the graph
// and the history of the turn's steps.
func (inst *App) newTurnChip() (v *fsmview.View[turnStateE]) {
	return fsmview.New(inst.ids, "turn-state", inst.turnMachine, fsmview.Options[turnStateE]{
		Title:      "Turn",
		Tethered:   true,
		BadgeTone:  turnTone,
		AutoAnchor: true,
	})
}

// renderTurnState draws the state chip. It carries no line beside it: the
// state's name says what the turn waits on, and the bar's row is full.
func (inst *App) renderTurnState() {
	if inst.turnChip == nil {
		return
	}
	inst.turnChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.chat.turn.state",
		SourceApp: string(ManifestId),
	}
	inst.turnChip.Render()
}

// The window's operations catalog (ADR-0269): the turn's state, as the chip
// draws it — turn_state and turn_machine, the operations every app's mounted
// machine offers. Nothing here sends a message or changes a setting: an
// agent sharing this window reads where its turn stands and no more.

// opsSnap is empty: the mounted machine is read on the render goroutine and
// no query reads a snapshot.
type opsSnap struct{}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet(func(inst *App) opsSnap { return opsSnap{} })
	fsmops.Mount(s, "turn", "where the conversation's turn stands: what a running turn waits on, or how the last one ended", func(inst *App) opfsm.SourceI {
		if inst.turnMachine == nil {
			return nil
		}
		return inst.turnMachine
	}, fsmops.Options{History: 16})
	return
}()

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
