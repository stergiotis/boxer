package mdedit

// mdedit_outline.go is the heading outline: the mdoutline widget (the
// hierarchy, the fold, the selection that follows the caret) hosted in the
// side panel (mdedit_side.go), plus what a click means here, which is a caret
// move and a preview scroll, and the numbering pair, which is this app's own.

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
)

// Hover help for the numbering pair, stated at the button rather than in a
// block of prose.
const (
	tipOutlineRenumber = "Insert or refresh numeric section prefixes on every heading (\"2.1 Title\"), numbered by this outline's own nesting. Rewrites heading text from outside the editor — not an edit its undo describes — and heading slugs and anchors move with it. Clear numbers is the way back."

	tipOutlineStripNumbers = "Remove the numeric section prefixes Renumber inserts. A heading title that itself starts with a dotted number (\"3.14 constants\") loses it too."
)

// The numbering buttons, built once — identical retained bytes across frames
// intern to one blob.
var (
	atomsOutlineRenumber     = c.Atoms().Text(icons.PhListNumbers).Keep()
	atomsOutlineStripNumbers = c.Atoms().Text(icons.PhEraser).Keep()
)

// ---------------------------------------------------------------------------
// The frame
// ---------------------------------------------------------------------------

// renderOutline draws the heading tree. A click scrolls the preview to that
// section and takes the caret with it; a disclosure control folds the section
// away. The widget probes its own pane, so there is no ScrollArea here.
func (inst *App) renderOutline() {
	res := inst.outline.Render(mdoutline.Input{
		Ids:         inst.ids,
		ScopeKey:    "outline",
		Doc:         inst.doc,
		CurrentSlug: inst.caretSlug,
		Controls:    inst.renderNumberingControls,
	})
	inst.applyOutline(res)
}

// renderNumberingControls draws the numbering pair beside the widget's
// expand and collapse buttons. The numbers it writes are this pane's own
// nesting made visible — with the accepted cost that the command is
// unreachable while the outline is hidden.
//
// Numbering rewrites the buffer, so the click is stashed rather than
// applied: this pane renders after the source pane, and a rebind here would
// miss the frame's databinding override (mdedit_number.go). Only when the
// editor renders — the gesture edits, and read mode suppresses edits.
func (inst *App) renderNumberingControls() {
	if !inst.editorVisible() {
		return
	}
	renumber, strip := false, false
	for range c.HoverText(tipOutlineRenumber).KeepIter() {
		renumber = c.Button(inst.ids.PrepareStr("ol-renumber"), atomsOutlineRenumber).
			Small().SendResp().HasPrimaryClicked()
	}
	for range c.HoverText(tipOutlineStripNumbers).KeepIter() {
		strip = c.Button(inst.ids.PrepareStr("ol-numbers-clear"), atomsOutlineStripNumbers).
			Small().SendResp().HasPrimaryClicked()
	}
	switch {
	case renumber:
		inst.numberAction = numberActRenumber
	case strip:
		inst.numberAction = numberActStrip
	}
}

// applyOutline turns a frame's outline interaction into an app change: a
// click becomes a navigation. Expansion needs nothing — the widget has
// already applied it to the state that owns it.
func (inst *App) applyOutline(res mdoutline.Result) {
	if res.Clicked < 0 {
		return
	}
	nd := res.Node

	// Set the scroll target WITHOUT touching caretSlug. The caret has not moved
	// YET — the request below moves it, and the caret's own baseline updates
	// when the editor reports back. Moving it here instead would make the next
	// frame read the caret's real section as changed and drag the preview
	// straight back off the heading just clicked.
	inst.pendingScroll = nd.Slug

	// The reveal is marked done for the section the caret is about to land in.
	// Without this the caret's arrival next frame reads as a change and scrolls
	// the outline to centre a row the reader just clicked — which is to say,
	// the list jumps under a pointer that was already on target.
	inst.outline.MarkRevealed(nd.Slug)

	// Take the writer to the heading too, not just the reader. The caret goes
	// to the LINE start so it sits before the `#`, which is where "go to this
	// heading" means, and with focus because the gesture means "take me there".
	off := lineStart(inst.src, nd.Off)
	inst.requestCaret(off, off, true)
}

// outlineSummary is the one-line label the toggle carries, so the button says
// what turning it on would show.
func outlineSummary(headings []markdown.HeadingInfo) (s string) {
	var b strings.Builder
	b.WriteString("Outline (")
	b.WriteString(itoa(mdoutline.Count(headings)))
	b.WriteString(")")
	return b.String()
}

// itoa is strconv.Itoa for small non-negative ordinals, kept local so the id
// path allocates nothing per row.
func itoa(n int) (s string) {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
