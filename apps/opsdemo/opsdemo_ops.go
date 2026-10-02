package opsdemo

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The catalog (ADR-0269 §SD2): two resources and four operations.

const (
	resNote  = "note"
	resLevel = "level"

	opGetState = "get_state"
	opSetNote  = "set_note"
	opSetLevel = "set_level"
	// opClearNote is also the Clear button's gesture.
	opClearNote = "clear_note"
	// opCopyNote writes outside the app, to the clipboard: consequential,
	// so the person confirms it every time an agent asks.
	opCopyNote = "copy_note"
)

// snap is what queries read: a copy taken after the command stage.
type snap struct {
	note  string
	level float64
}

// State is get_state's result.
type State struct {
	Note  string  `desc:"the note text"`
	Level float64 `desc:"the level, 0 to 100"`
}

// SetNoteArgs is set_note's argument.
type SetNoteArgs struct {
	Text string `desc:"the new note text"`
}

// SetLevelArgs is set_level's argument.
type SetLevelArgs struct {
	Level float64 `desc:"the new level, 0 to 100"`
}

var ops = func() (s *appops.Set[*App, snap]) {
	s = appops.NewSet(func(inst *App) snap { return snap{note: inst.note, level: inst.level} })
	s.Resource(resNote, "the note text", func(inst *App) any { return inst.note })
	s.Resource(resLevel, "the level slider", func(inst *App) any { return inst.level })
	s.Restorable(resNote, func(inst *App, v any) bool {
		text, ok := v.(string)
		if ok {
			inst.note = text
		}
		return ok
	})
	s.Restorable(resLevel, func(inst *App, v any) bool {
		level, ok := v.(float64)
		if ok {
			inst.level = level
		}
		return ok
	})
	s.Editing(resNote, func(inst *App) bool { return appops.WidgetEditing(inst.noteH) })
	s.Editing(resLevel, func(inst *App) bool { return appops.WidgetEditing(inst.levelH) })

	appops.Query(s, app.OperationSpec{Name: opGetState, Version: 1, Summary: "read the note and the level",
		Reads: []string{resNote, resLevel}, Agents: true, Untrusted: true},
		func(sn snap, in appops.None) (State, error) { return State{Note: sn.note, Level: sn.level}, nil })
	appops.Command(s, app.OperationSpec{Name: opSetNote, Version: 1, Summary: "replace the note text",
		Effect: app.OperationEffectDocument, Writes: []string{resNote}, Agents: true, Gesture: "typing in the note"},
		func(inst *App, call app.OperationCall, in SetNoteArgs) (appops.None, error) {
			inst.note = in.Text
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSetLevel, Version: 1, Summary: "set the level, 0 to 100",
		Effect: app.OperationEffectDocument, Writes: []string{resLevel}, Agents: true, Gesture: "dragging the level slider"},
		func(inst *App, call app.OperationCall, in SetLevelArgs) (appops.None, error) {
			if in.Level < 0 || in.Level > 100 {
				return appops.None{}, app.RefuseOperation("the level is between 0 and 100")
			}
			inst.level = in.Level
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opCopyNote, Version: 1, Summary: "copy the note to the clipboard",
		Effect: app.OperationEffectConsequential, Reads: []string{resNote}, Agents: true, Gesture: "the Copy button"},
		func(inst *App, call app.OperationCall, in appops.None) (appops.None, error) {
			inst.copyNote()
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opClearNote, Version: 1, Summary: "clear the note",
		Effect: app.OperationEffectDocument, Writes: []string{resNote}, Agents: true, Gesture: "the Clear button"},
		func(inst *App, call app.OperationCall, in appops.None) (appops.None, error) {
			inst.note = ""
			return appops.None{}, nil
		})
	return
}()
