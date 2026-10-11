package mdedit

// The proposal pane: a replacement an agent offers through propose_edit
// (mdedit_ops.go), shown for the person's verdict before anything changes.
// mdedit holds no model grant (ADR-0318): the agentic harness reads the
// document through get_document, does the model work on its own account,
// and hands the result back here.
//
// The flow is preview-then-apply, never rewrite-on-arrival: a whole-buffer
// rebind is invisible to the editor's own undo (ADR-0178 M3's standing
// caveat), so a change the reader has not looked at must not be one. The
// proposal lands in a bottom pane rendered as markdown beside the live
// preview, and Apply splices it over exactly the span it names — refusing
// when the buffer has moved since, because a splice computed against one
// buffer must not land on another. Discard and Copy are the other two
// verdicts.

import (
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// proposalPaneFrac is the pane's share of the window height, with a
	// fallback for the frames before the window probe reports. Derived every
	// frame, the app's standing rule for pane sizes (see sourceSplitFrac).
	proposalPaneFrac       = float32(0.32)
	proposalPaneFallbackPx = float32(260)
	proposalPaneMinPx      = float32(160)
)

const (
	tipProposalApply = "Replace the text the proposal names with it. Refused if the buffer changed since it was proposed. A rebind from outside the editor — not an edit its undo describes."

	tipProposalDiscard = "Drop the proposal. The buffer is untouched."

	tipProposalCopy = "Copy the proposed text to the clipboard, leaving the buffer and its dirty marker alone."
)

var (
	atomsProposalApply   = c.Atoms().Text("Apply").Keep()
	atomsProposalDiscard = c.Atoms().Text("Discard").Keep()
	atomsProposalCopy    = c.Atoms().Text("Copy").Keep()
)

// The verdicts get_proposal reports for the last proposal.
const (
	outcomeApplied    = "applied"
	outcomeDiscarded  = "discarded"
	outcomeSuperseded = "superseded by a later proposal"
)

// proposal is one replacement awaiting the person's verdict.
type proposal struct {
	title   string
	content string
	// writer and reason are who proposed it and why, as the call named
	// them (app.OperationCall).
	writer string
	reason string
}

// proposalState is everything the pane owns, one field on App (the
// findState shape).
type proposalState struct {
	// pending is the proposal awaiting a verdict; nil while none.
	pending *proposal

	// The span it replaces, and the buffer it was computed against — what
	// Apply splices into, and the staleness check.
	reqSrc   string
	reqStart int
	reqStop  int

	// resDoc is the parsed preview of pending.content, keyed by text (the
	// doc/docSrc shape).
	resDoc    *markdown.Doc
	resDocSrc string

	// outcome is what became of the last proposal, for get_proposal.
	outcome string
}

// proposalPaneOpen reports whether the pane renders this frame.
func (inst *App) proposalPaneOpen() (yes bool) {
	yes = inst.prop.pending != nil
	return
}

// proposalPaneHeight derives the pane's height from the measured window.
func (inst *App) proposalPaneHeight() (px float32) {
	if inst.winH <= 0 {
		return proposalPaneFallbackPx
	}
	px = inst.winH * proposalPaneFrac
	if px < proposalPaneMinPx {
		px = proposalPaneMinPx
	}
	return
}

// selectionSpan is the selection as byte offsets — in a mode where the
// editor (and so the selection) is visible — and false when there is none.
func (inst *App) selectionSpan() (start, stop int, ok bool) {
	if !inst.editorVisible() {
		return 0, 0, false
	}
	a, b := c.UnpackCursorRange(inst.cursor)
	if a == b {
		return 0, 0, false
	}
	if a > b {
		a, b = b, a
	}
	return charToByte(inst.src, a), charToByte(inst.src, b), true
}

// offerProposal opens the pane on a replacement of src[start:stop]. The
// caller has checked the span against the buffer. A proposal still waiting
// is superseded: the pane shows one at a time.
func (inst *App) offerProposal(p proposal, start, stop int) {
	x := &inst.prop
	if x.pending != nil {
		x.outcome = outcomeSuperseded
	}
	x.pending = &p
	x.reqSrc, x.reqStart, x.reqStop = inst.src, start, stop
	x.resDoc, x.resDocSrc = nil, ""
	inst.status = "proposed: " + p.title
}

// ensureProposalDoc parses the proposal for preview, text-keyed like the main
// preview's doc/docSrc gate. Render goroutine only (markdown.Parse lowers
// through FFI).
func (inst *App) ensureProposalDoc() {
	x := &inst.prop
	if x.pending == nil {
		return
	}
	if x.resDoc != nil && x.resDocSrc == x.pending.content {
		return
	}
	x.resDoc = markdown.Parse([]byte(x.pending.content))
	x.resDocSrc = x.pending.content
}

// renderProposalPane draws the verdict pane: header (what is proposed, by
// whom, the verdict buttons), then the proposal rendered as markdown —
// beside the live preview, which is the comparison Apply is judged on.
// Callers own the enclosing panel.
func (inst *App) renderProposalPane() {
	x := &inst.prop
	if x.pending == nil {
		return
	}

	for range c.HorizontalTop().KeepIter() {
		c.Label(x.pending.title).Selectable(false).Send()
		c.Label(inst.proposalSummaryLine()).Selectable(false).Send()

		apply, discard, copyRes := false, false, false
		for range c.HoverText(tipProposalApply).KeepIter() {
			apply = c.Button(inst.ids.PrepareStr("proposal-apply"), atomsProposalApply).
				SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipProposalCopy).KeepIter() {
			copyRes = c.Button(inst.ids.PrepareStr("proposal-copy"), atomsProposalCopy).
				SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipProposalDiscard).KeepIter() {
			discard = c.Button(inst.ids.PrepareStr("proposal-discard"), atomsProposalDiscard).
				SendResp().HasPrimaryClicked()
		}
		switch {
		case apply:
			inst.applyProposal()
		case discard:
			inst.discardProposal()
		case copyRes && !inst.exportInFlight():
			inst.copyToClipboard(x.pending.content, false)
		}
	}
	if x.pending == nil {
		return
	}

	inst.ensureProposalDoc()
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
		// Its own scope key: the main preview renders the same widget kinds
		// in the same window.
		markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "proposal-preview", Doc: x.resDoc})
	}
}

// proposalSummaryLine is the pane header's provenance readout: who proposed
// it, what it replaces, and the reason the call gave.
func (inst *App) proposalSummaryLine() (s string) {
	x := &inst.prop
	var b strings.Builder
	b.WriteString("from ")
	b.WriteString(x.pending.writer)
	b.WriteString(" · ")
	switch {
	case x.reqStart == 0 && x.reqStop == len(x.reqSrc):
		b.WriteString("replaces the document")
	case x.reqStart == x.reqStop:
		b.WriteString("inserts at byte ")
		b.WriteString(itoa(x.reqStart))
	default:
		b.WriteString("replaces ")
		b.WriteString(itoa(x.reqStop - x.reqStart))
		b.WriteString(" bytes")
	}
	if x.pending.reason != "" {
		b.WriteString(" · ")
		b.WriteString(x.pending.reason)
	}
	return b.String()
}

// spliceResult replaces src[start:stop] with content — Apply's one pure step.
func spliceResult(src string, start, stop int, content string) (out string) {
	out = src[:start] + content + src[stop:]
	return
}

// applyProposal splices the proposal over the span it names. The staleness
// check is the simplest sound rule: any edit since it was proposed refuses
// the apply, because a splice computed against one buffer must not land on
// another — re-anchoring a moved span is a judgment this gesture should not
// make silently.
func (inst *App) applyProposal() {
	x := &inst.prop
	if x.pending == nil {
		return
	}
	if inst.src != x.reqSrc {
		inst.status = "buffer changed since the proposal — copy or discard instead"
		return
	}
	title := x.pending.title
	if inst.rebindBuffer(spliceResult(inst.src, x.reqStart, x.reqStop, x.pending.content)) {
		inst.status = "applied: " + title
	}
	inst.closeProposal(outcomeApplied)
}

// discardProposal closes the pane and drops the proposal.
func (inst *App) discardProposal() {
	inst.closeProposal(outcomeDiscarded)
}

// closeProposal drops the proposal and its request snapshot, recording what
// became of it.
func (inst *App) closeProposal(outcome string) {
	x := &inst.prop
	x.pending = nil
	x.resDoc, x.resDocSrc = nil, ""
	x.reqSrc, x.reqStart, x.reqStop = "", 0, 0
	x.outcome = outcome
}
