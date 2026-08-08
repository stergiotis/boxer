package mdedit

import (
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// outlineSplitFrac is the outline's share of the window, taken from the
	// preview rather than from the source — the source pane's width is what
	// the writer is working in.
	outlineSplitFrac = float32(0.18)

	// outlineMinWindowPx is the width below which the outline hides itself. A
	// fixed share of a narrow window is a column too thin to read a heading
	// in, and it would take that width from the two panes that are doing the
	// work. Hiding is not a failure mode here; it is the honest answer to
	// "there is no room".
	outlineMinWindowPx = float32(760)

	// outlineIndentPx is the per-level indent. Headings nest up to six deep,
	// so the step is small enough that level six is still readable.
	outlineIndentPx = float32(10)
)

// outlineVisible reports whether the outline column renders this frame: the
// reader has it switched on AND the window is wide enough to give it a share
// without starving the panes beside it.
func (inst *App) outlineVisible() (yes bool) {
	if !inst.showOutline {
		return false
	}
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW >= outlineMinWindowPx
}

// outlineWidth is the outline column's width for this frame, on the same
// derive-every-frame basis as the source split and for the same reason: a
// retained panel width clamps destructively and never recovers.
func (inst *App) outlineWidth() (px float32) {
	winW := inst.winW
	if winW <= 0 {
		winW = windowFallbackWidthPx
	}
	return winW * outlineSplitFrac
}

// renderOutline draws the heading list. A click scrolls the preview to that
// section.
//
// It owns its scroll area for the same reason the other panes do — the
// horizontal axis has to be scrollable so a long heading cannot push its width
// back out into the window's minimum (ADR-0178 §Decision 6).
func (inst *App) renderOutline() {
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
		if inst.doc == nil {
			return
		}
		headings := inst.doc.Headings()
		if len(headings) == 0 {
			c.Label("No headings yet.").Send()
			return
		}
		// The slug is the id, not the index: a heading keeps its identity when
		// one above it is inserted or removed, so the row the reader is
		// pointing at does not change under them mid-edit. Duplicate slugs
		// (two sections with the same title) would collide, so the ordinal
		// disambiguates — an id collision reads as a click that does nothing.
		seen := make(map[string]int, len(headings))
		for _, h := range headings {
			if h.Slug == "" {
				continue
			}
			ord := seen[h.Slug]
			seen[h.Slug] = ord + 1
			inst.renderOutlineRow(h, ord)
		}
	}
}

func (inst *App) renderOutlineRow(h markdown.HeadingInfo, ord int) {
	key := h.Slug + "#" + itoa(ord)
	for range c.IdScope(inst.ids.PrepareStr("ol-" + key)) {
		for range c.Horizontal().KeepIter() {
			if h.Level > 1 {
				c.AddSpace(float32(h.Level-1) * outlineIndentPx)
			}
			label := h.Text
			if label == "" {
				label = h.Slug
			}
			if c.SelectableLabel(inst.ids.PrepareStr("row"), inst.caretSlug == h.Slug, label).
				SendResp().HasPrimaryClicked() {
				// Set the scroll target WITHOUT touching caretSlug. The caret
				// has not moved, so leaving its baseline alone is what stops
				// the next frame from dragging the preview straight back to
				// the section the caret is in.
				inst.pendingScroll = h.Slug
			}
		}
	}
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

// outlineSummary is the one-line label the toggle carries, so the button says
// what turning it on would show.
func outlineSummary(headings []markdown.HeadingInfo) (s string) {
	n := 0
	for _, h := range headings {
		if h.Slug != "" {
			n++
		}
	}
	var b strings.Builder
	b.WriteString("Outline (")
	b.WriteString(itoa(n))
	b.WriteString(")")
	return b.String()
}
