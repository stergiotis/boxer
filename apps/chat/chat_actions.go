package chat

// What the person can do with a message (ADR-0265 §SD4): copy it, send a
// failed turn again, answer the last turn again, take the last turn back
// to edit it, and open a code block's SQL — or a failed call's record — in
// play. Copy and open go over the bus off the render thread; their outcome
// is the status line, so a failure says what it was rather than vanishing.

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// noteFor is how long a status line that reports success stays; one that
// reports a failure stays until dismissed or replaced.
const noteFor = 6 * time.Second

// status is the line above the composer: what the last action did.
type status struct {
	text   string
	failed bool
	at     time.Time
}

// setNote shows text on the status line.
func (inst *App) setNote(text string, failed bool) {
	inst.note = status{text: text, failed: failed, at: time.Now()}
}

// noteShown is the status line as of now, "" once a success has aged out.
func (inst *App) noteShown() (s status, ok bool) {
	s = inst.note
	if s.text == "" || (!s.failed && time.Since(s.at) > noteFor) {
		return status{}, false
	}
	return s, true
}

// canAct says a turn can start: a model to ask, none in flight.
func (inst *App) canAct() bool { return inst.cli != nil && inst.pending == nil }

// busyNote is the status line for an action asked for while a turn runs.
func (inst *App) busyNote() {
	inst.setNote("a turn is running: wait for its answer, or Cancel it", true)
}

// setDraft puts text in the composer. A draft already there is kept below
// it, so taking a turn back never loses what was being written.
func (inst *App) setDraft(text string) {
	if inst.draft != "" && inst.draft != text {
		text += "\n\n" + inst.draft
	}
	inst.draft = text
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
}

// retry sends the last turn again when it failed.
func (inst *App) retry() {
	if !inst.canAct() {
		inst.busyNote()
		return
	}
	if text, ok := inst.conv.dropFailed(); ok && !inst.startTurn(text) {
		inst.setDraft(text)
	}
}

// regenerate answers the last turn again: the turn is taken back and its
// message sent anew, from the same parent.
func (inst *App) regenerate() {
	if !inst.canAct() {
		inst.busyNote()
		return
	}
	if text, _, ok := inst.conv.rewind(); ok && !inst.startTurn(text) {
		inst.setDraft(text)
	}
}

// edit takes the last turn back and puts its message in the composer; the
// next send is marked edited. An answered turn can be put back with
// Cancel edit until then.
func (inst *App) edit() {
	if !inst.canAct() {
		inst.busyNote()
		return
	}
	if text, ok := inst.conv.dropFailed(); ok {
		inst.setDraft(text)
		inst.editedNext = true
		return
	}
	if text, undo, ok := inst.conv.rewind(); ok {
		inst.setDraft(text)
		inst.editing, inst.editedNext = undo, true
	}
}

// cancelEdit puts back the turn edit took, and clears the composer.
func (inst *App) cancelEdit() {
	if inst.conv.restore(inst.editing) {
		inst.draft = ""
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draft)
	}
	inst.editing, inst.editedNext = nil, false
}

// copyText puts text on the clipboard; what names it on the status line.
func (inst *App) copyText(what string, text string) {
	bus := inst.bus
	inst.runAction("chat-copy", "copy "+what, func(ctx context.Context) (note string, err error) {
		if _, err = bus.Request(clipboardbroker.SubjectWrite, []byte(text)); err != nil {
			err = eh.Errorf("copy %s: %w", what, err)
			return
		}
		return "copied " + what, nil
	})
}

// openSqlInPlay opens a play window with sql in its editor, not run: the
// SQL is the model's, and the person runs it.
func (inst *App) openSqlInPlay(sql string) {
	inst.openPlay("the SQL", launchcfg.PlayLaunch{Sql: sql})
}

// openCallInPlay opens play on a failed call's row in the host's call
// record.
func (inst *App) openCallInPlay(callId string) {
	inst.openPlay("the call record", launchcfg.PlayLaunch{Sql: callRecordSql(callId), AutoRun: true, Endpoint: launchcfg.EndpointIntrospection})
}

// callRecordSql reads one call's row.
func callRecordSql(callId string) (sql string) {
	return "SELECT *\nFROM keelson('llm_calls')\nWHERE call_id = " + marshalling.EscapeString(callId)
}

func (inst *App) openPlay(what string, l launchcfg.PlayLaunch) {
	bus := inst.bus
	inst.runAction("chat-open-play", "open "+what+" in play", func(ctx context.Context) (note string, err error) {
		cfg, err := buscodec.Encode(l)
		if err != nil {
			err = eh.Errorf("encode the play launch config: %w", err)
			return
		}
		if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
			err = eh.Errorf("open play: %w", err)
			return
		}
		return "opened " + what + " in play", nil
	})
}

// runAction runs one copy or open off the render thread; drainAction
// lands its outcome on the status line.
func (inst *App) runAction(kind string, title string, f func(ctx context.Context) (string, error)) {
	if inst.bus == nil {
		inst.setNote(title+": this host gives the app no bus", true)
		return
	}
	ok := inst.action.Start(nil, bgjob.Spec{Kind: kind, Title: title}, func(ctx context.Context) (note *string, err error) {
		n, err := f(ctx)
		if err != nil {
			return
		}
		note = &n
		return
	})
	if !ok {
		inst.setNote(title+": the previous copy or open is still running", true)
	}
}

func (inst *App) drainAction() {
	if note, _, ok := inst.action.TakeResult(); ok {
		inst.setNote(*note, false)
	} else if snap := inst.action.Snapshot(); snap.State == bgjob.StateFailed {
		inst.action.Invalidate()
		text := "the action failed"
		if snap.Err != nil {
			text = snap.Err.Error()
		}
		inst.setNote(text, true)
	}
}
