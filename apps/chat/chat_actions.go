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
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
	text, _, ok := inst.conv.rewind()
	if !ok {
		inst.rewindRefused()
		return
	}
	if !inst.startTurn(text) {
		inst.setDraft(text)
	}
}

// rewindRefused says why a turn that could be taken back was not: the
// journal refused to drop it, so the model would still see what the
// transcript no longer shows.
func (inst *App) rewindRefused() {
	if inst.conv.canRewind() {
		inst.setNote("the turn was not taken back: the journal refused to drop it", true)
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
	text, undo, ok := inst.conv.rewind()
	if !ok {
		inst.rewindRefused()
		return
	}
	inst.setDraft(text)
	inst.editing, inst.editedNext = undo, true
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
	bus, coord := inst.bus, inst.coord
	// What a chat copies is model-touched unless it is the conversation's
	// own id: the run's label with the model's integrity, so the broker
	// can apply the clipboard policy and the journal records the copy
	// (ADR-0317 §SD1, §SD3).
	label := agentrun.Label{Level: agentrun.LevelOwnModel}
	if what == "the conversation's id" {
		label = agentrun.Label{Level: agentrun.LevelSystem}
	}
	if coord != nil {
		if w := coord.journalNow(); w != nil {
			label = label.Join(w.Run().Label)
		}
	}
	inst.runAction("chat-copy", "copy "+what, func(ctx context.Context) (note string, err error) {
		payload := clipboardbroker.Payload{Text: text, Label: label, Source: string(ManifestId) + ":" + what}
		// Journaled before it is made (ADR-0308 §SD2): the record names
		// the copy by its source and label; the broker's handle, when it
		// gives one, is in the broker's own record.
		if coord != nil {
			if err = coord.copyEntry(ctx, clipboardbroker.CopyRecord{Digest: agentrun.ContentDigest(text), Label: label, Level: uint8(label.Level), Tier: uint8(label.Tier), Source: payload.Source}); err != nil {
				err = eb.Build().Str("what", what).Errorf("copy: %w", err)
				return
			}
		}
		rec, err := clipboardbroker.Write(bus, payload)
		if err != nil {
			err = eb.Build().Str("what", what).Errorf("copy: %w", err)
			return
		}
		note = "copied " + what
		if rec.Handle != "" {
			note += " by reference (" + rec.Handle + "): paste it inside boxer"
		}
		return note, nil
	}, nil)
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
	}, nil)
}

// runAction runs one copy, open or delete off the render thread;
// drainAction lands its outcome on the status line, and runs after, when
// given, once the action landed well. It reports whether the action
// started: a refused one installs nothing.
func (inst *App) runAction(kind string, title string, f func(ctx context.Context) (string, error), after func()) (started bool) {
	if inst.bus == nil {
		inst.setNote(title+": this host gives the app no bus", true)
		return false
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
		return false
	}
	inst.afterAction = after
	return true
}

func (inst *App) drainAction() {
	if note, _, ok := inst.action.TakeResult(); ok {
		inst.setNote(*note, false)
		inst.deleting = ""
		if after := inst.afterAction; after != nil {
			inst.afterAction = nil
			after()
		}
	} else if snap := inst.action.Snapshot(); snap.State == bgjob.StateFailed {
		inst.action.Invalidate()
		// What was to follow a success does not follow a failure.
		inst.afterAction, inst.deleting = nil, ""
		text := "the action failed"
		if snap.Err != nil {
			text = snap.Err.Error()
		}
		inst.setNote(text, true)
	}
}
