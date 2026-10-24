package mdedit

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The line gestures themselves are mdline's and tested there; these pin the
// app's side of the wiring — what reaches the widget as a snippet and what
// becomes a rebind with a caret request.
func TestApplyEnterContinuesThroughInsertAndExitsThroughRebind(t *testing.T) {
	inst := &App{src: "- one\n- two"}
	inst.cursor = c.PackCursorRange(11, 11)
	inst.applyEnter(11, false)
	if inst.pendingInsert != "\n- " || inst.rebindSrc {
		t.Fatalf("continue: insert %q rebind %v", inst.pendingInsert, inst.rebindSrc)
	}
	inst = &App{src: "- one\n- "}
	inst.applyEnter(8, false)
	if !inst.rebindSrc || inst.src != "- one\n" || !inst.pendingCaretOk || inst.pendingCaret.Start != 6 || !inst.pendingCaret.Focus {
		t.Fatalf("exit: src %q rebind %v caret %+v", inst.src, inst.rebindSrc, inst.pendingCaret)
	}
}

func TestApplyTabIndentsThroughRebind(t *testing.T) {
	inst := &App{src: "- one"}
	inst.applyTab(5, false)
	if !inst.rebindSrc || inst.src != "\t- one" || inst.pendingCaret.Start != 6 {
		t.Fatalf("indent: src %q caret %+v", inst.src, inst.pendingCaret)
	}
	inst = &App{src: "prose"}
	inst.applyTab(5, false)
	if inst.pendingInsert != "\t" || inst.rebindSrc {
		t.Fatalf("tab: insert %q rebind %v", inst.pendingInsert, inst.rebindSrc)
	}
}
