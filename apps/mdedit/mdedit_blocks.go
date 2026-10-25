package mdedit

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdblocks"
)

// The Blocks view mode (ADR-0334 §SD8): the document edited as a list of
// blocks over the same source string the other modes edit. The widget owns
// the caret, the focused block and the document's undo; this file is the
// seam between it and the app's one buffer.
//
// Three rules keep the two in step, and each has a direction. A source that
// moved under the widget — a restored document, a replace-all, an applied
// proposal, a reload from disk — goes IN through SetSource, which resets the
// widget's editing state and history (a document-level undo cannot span a
// rewrite it did not make). The widget's own edits come OUT through Source
// after its Render, and raise rebindSrc so the source TextEdit, which is not
// rendering in this mode and still holds its cached copy, drops it on the
// frame it next renders. And the caret flows both ways: the widget's byte
// offset feeds the app's cursor so the outline follows it, and an outline
// click forwards the pending caret to FocusAt.

// ensureBlocks builds the widget on first use. The id stack is the app's,
// pre-salted per window by the host (ADR-0026 §SD9).
func (inst *App) ensureBlocks() {
	if inst.blocks != nil {
		return
	}
	inst.blocks = mdblocks.New(inst.ids, "blocks", mdblocks.Opts{HintText: hintEmpty})
}

// renderBlocks is the central region in Blocks mode. Owns its scroll area,
// as the source pane does, so the widget's pane probe sees the panel.
func (inst *App) renderBlocks() {
	inst.ensureBlocks()
	if inst.blocks.Source() != inst.src {
		inst.blocks.SetSource(inst.src)
	}
	if inst.pendingCaretOk {
		// A jump from the outline or from find: the widget opens the block
		// holding the offset. Focus is the requester's call, as it is for the
		// source pane; the widget asks for it either way, since an unfocused
		// block editor shows no caret to jump to.
		inst.blocks.FocusAt(inst.pendingCaret.Start)
		inst.pendingCaretOk = false
	}
	var ev mdblocks.Events
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
		ev = inst.blocks.Render()
	}
	if ev.Changed {
		inst.src = inst.blocks.Source()
		inst.rebindSrc = true
	}
	if ev.Caret >= 0 {
		ch := mdline.ByteToChar(inst.src, ev.Caret)
		inst.cursor = c.PackCursorRange(ch, ch)
	}
	if ev.CopyRequest != "" {
		inst.copyToClipboard(ev.CopyRequest, false)
	}
}
