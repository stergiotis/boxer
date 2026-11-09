// Package editexternally draws the gesture that hands a window's text to
// the host's editor app and brings each save back (externaledit): one
// button while idle, and while the text is handed over the edit's status,
// a way to stop, and the choice when a save arrives after the person edited
// the text here.
package editexternally

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Input is one frame of the gesture.
type Input struct {
	// Ids is the host's widget id stack and ScopeKey names this gesture
	// within it; empty ScopeKey uses "editexternally".
	Ids      *c.WidgetIdStack
	ScopeKey string
	Editing  *externaledit.Editing
	// Bus is the window's bus; without one nothing is drawn.
	Bus app.BusI
	// Name is the file name the editor shows; its extension picks the
	// editor's syntax.
	Name string
	// Text is the text as the window holds it now.
	Text string
	// Disabled draws the button disabled, as when the text is not the
	// person's to change.
	Disabled bool
	// Small draws small buttons, for a row beside an input.
	Small bool
}

// Render draws the gesture in the caller's current row and answers the
// text to put in place of Input.Text when a save is taken; a caller whose
// text is bound to an input overrides the binding with it. It draws nothing
// where the host has no single editor app or the window no bus.
func Render(in Input) (text string, replace bool) {
	if in.Editing == nil || in.Bus == nil || in.Ids == nil {
		return
	}
	editor, ok := externaledit.Editor()
	if !ok {
		return
	}
	scope := in.ScopeKey
	if scope == "" {
		scope = "editexternally"
	}
	ed := in.Editing
	text, replace = ed.Poll(in.Text)
	current := in.Text
	if replace {
		current = text
	}
	if ed.Busy() {
		c.RequestRepaintAfter(0.5)
	}
	button := func(key string, label string) bool {
		b := c.Button(in.Ids.PrepareStr(key), c.Atoms().Text(label).Keep())
		if in.Small {
			b = b.Small()
		}
		return b.SendResp().HasPrimaryClicked()
	}
	for range c.IdScope(in.Ids.PrepareStr(scope)) {
		if !ed.Active() {
			for range c.EnabledUi(!in.Disabled).KeepIter() {
				if button("start", "Edit in "+editor) {
					ed.Start(in.Bus, in.Name, current)
				}
			}
		} else if button("stop", "Stop editing in "+editor) {
			ed.End()
		}
		if ed.Conflict() {
			if button("take", "Take "+editor+"'s save") {
				text, replace = ed.Take()
			}
			if button("keep", "Keep mine") {
				ed.Keep(current)
			}
		}
		if line := StatusLine(ed); line != "" {
			for rt := range c.RichTextLabel(line) {
				rt.Small().Weak()
			}
		}
	}
	return
}

// StatusLine is the gesture's status text: where the edit stands, the
// latest save and who could have typed it, and a waiting save.
func StatusLine(ed *externaledit.Editing) string {
	line := ed.Status()
	if last := ed.Last(); last != nil && ed.Active() {
		who := "session " + last.Session
		if last.Session == "" {
			who = "an unrecorded session"
		}
		if last.Agent {
			who += ", where an agent could type"
		}
		line = "save " + strconv.FormatUint(last.Seq, 10) + " from " + who
	}
	if ed.Conflict() {
		line = "a save waits: the text was edited here meanwhile"
	}
	return line
}
