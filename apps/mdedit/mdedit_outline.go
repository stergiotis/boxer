package mdedit

// mdedit_outline.go is the heading outline. It renders through the native tree
// widget (ADR-0176) rather than the flat SelectableLabel list M2 shipped, which
// buys three things the flat list could not have: headings nest, a section can
// be collapsed, and only the rows on screen build widgets.
//
// The file is in two halves, the split ADR-0176's own adopters use. Everything
// above renderOutline is pure — it turns the parsed headings into the columnar
// shape tree.Render takes and is testable without a binding in sight — and
// everything below it is the frame.

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
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

	// outlineIndentPx is the per-level indent, narrower than the tree widget's
	// own default of 14. Headings nest up to six deep and the column is a
	// slice of the window rather than a pane in its own right, so the step has
	// to be small enough that level six still has room for a word.
	outlineIndentPx = float32(10)

	// outlineCountWidthPx is the trailing column that carries a collapsed
	// section's hidden-heading count.
	//
	// It is a column rather than a chip after the label, and that is the one
	// layout decision here worth stating. A truncating label in a horizontal
	// row takes the whole width it is offered, so anything emitted after it is
	// pushed out of the cell — which for an outline, where long headings are
	// the common case and not the exception, would mean the count disappears
	// exactly on the rows that have most to hide. A column of its own is
	// reserved before the label is laid out, so the count is always there and
	// always in the same place.
	outlineCountWidthPx = float32(30)

	// outlineScrollbarPx is held back from the measured pane so the outline
	// column stops short of the table's vertical scrollbar instead of running
	// under it. A deliberate over-estimate, like the editor's.
	outlineScrollbarPx = float32(16)

	// outlineMinColWidthPx floors the outline column, so a pane measurement
	// that has not landed yet cannot collapse it to nothing.
	outlineMinColWidthPx = float32(70)

	// outlineFallbackPaneH stands in for the pane height on the first frame,
	// before the probe reports. Without SOME height the table falls back to
	// egui_table's auto-fit heuristic, capped at ETABLE_AUTOFIT_CAP_PX, and a
	// document of any length overruns it.
	outlineFallbackPaneH = float32(360)
)

// Hover help for the two outline-wide controls, stated at the button rather
// than in a block of prose.
const (
	tipOutlineExpandAll   = "Open every section."
	tipOutlineCollapseAll = "Close every section, leaving the top-level headings. Collapsing is per section and lasts as long as the window."
)

// The outline's own bar buttons, built once — identical retained bytes across
// frames intern to one blob.
var (
	atomsOutlineExpand   = c.Atoms().Text(icons.PhArrowsOutSimple).Keep()
	atomsOutlineCollapse = c.Atoms().Text(icons.PhArrowsInSimple).Keep()
)

// The count chip's colours, from the design-system roles rather than literals
// (ADR-0031). Secondary text on no background of its own: the row block behind
// it already carries the fill, and a chip with its own would fight it.
var (
	outlineCountFg = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	outlineCountBg = color.Transparent
)

// ---------------------------------------------------------------------------
// The hierarchy — pure
// ---------------------------------------------------------------------------

// outlineNode is the per-node metadata carried beside the columnar tree, one
// entry per node and indexed the same way. It holds the three things the tree
// widget has no way to know: what this row is filed under across rebuilds,
// where in the buffer it points, and how much it hides when closed.
type outlineNode struct {
	// slug is the anchor the preview scrolls to and the caret's section is
	// reported as. Not unique on its own — see key.
	slug string
	// off is the heading's byte offset into the buffer, where a click sends
	// the caret.
	off int
	// descendants is how many headings sit under this one, at any depth. Shown
	// on a collapsed row, so closing a section says what it took away.
	descendants int
}

// outlineModel is the outline's hierarchy: the columnar input tree.Render
// takes, plus the metadata above. Retained on the App and refilled in place —
// the slices are rebuilt on every reparse, so reallocating them would be one
// allocation per edit for nothing.
type outlineModel struct {
	labels  []string
	parents []int32
	nodes   []outlineNode
	// keys identify the nodes across rebuilds, and are what the tree widget
	// files a collapse under. The outline is rebuilt from a fresh parse on
	// every edit that changes the text, so a node INDEX — the only identity a
	// columnar input has on its own — means a different heading one keystroke
	// to the next, and inserting a section would hand its collapse state to
	// whichever heading slid into its slot.
	//
	// The slug is the stable part; the ordinal disambiguates two sections that
	// happen to share a title.
	keys []string
	// seen counts slugs during a build, so the ordinal in a key is stable for
	// a given document. Retained and cleared rather than allocated per build.
	seen map[string]int
}

// build turns a document's headings into the hierarchy.
//
// Nesting comes from the heading levels and nothing else, using the rule a
// reader already applies: a heading belongs to the nearest heading above it
// with a smaller level. That handles the two shapes a hand-written document
// actually takes and a strict level-equals-depth reading does not — a level
// skipped (`#` straight to `###`, which nests under the `#`), and a document
// that never uses level 1 at all (its `##`s are roots). A forest is fine here;
// tree.Tree allows several roots, and inventing a virtual document root would
// state a containment the markdown does not.
//
// Headings with no slug are dropped, as they were in the flat list: there is
// nothing to scroll the preview to.
func (m *outlineModel) build(headings []markdown.HeadingInfo) {
	labels := m.labels[:0]
	parents := m.parents[:0]
	nodes := m.nodes[:0]
	keys := m.keys[:0]
	if m.seen == nil {
		m.seen = make(map[string]int, 32)
	}
	clear(m.seen)

	// The open ancestors, innermost last. Bounded by the six heading levels in
	// practice, but nothing enforces that, so it is a slice.
	type ancestor struct {
		level uint8
		node  int32
	}
	stack := make([]ancestor, 0, 8)

	for _, h := range headings {
		if h.Slug == "" {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		parent := int32(-1)
		if len(stack) > 0 {
			parent = stack[len(stack)-1].node
		}
		label := h.Text
		if label == "" {
			label = h.Slug
		}
		ord := m.seen[h.Slug]
		m.seen[h.Slug] = ord + 1

		node := int32(len(labels))
		labels = append(labels, label)
		parents = append(parents, parent)
		keys = append(keys, h.Slug+"#"+itoa(ord))
		nodes = append(nodes, outlineNode{
			slug: h.Slug,
			off:  h.ByteOffset,
		})
		stack = append(stack, ancestor{level: h.Level, node: node})
	}

	// Descendant counts, accumulated backwards. A parent always precedes its
	// children here — headings arrive in document order and the stack only
	// ever points at one already emitted — so one reverse pass is enough and
	// no traversal is needed.
	for i := len(nodes) - 1; i >= 0; i-- {
		if p := parents[i]; p >= 0 {
			nodes[p].descendants += nodes[i].descendants + 1
		}
	}

	m.labels, m.parents, m.nodes, m.keys = labels, parents, nodes, keys
}

// len is the node count.
func (m *outlineModel) len() int { return len(m.nodes) }

// tree is the columnar input, borrowed. Valid until the next build. The key
// column is what carries a collapse across one.
func (m *outlineModel) tree() tree.Tree {
	return tree.Tree{Labels: m.labels, Parents: m.parents, Keys: m.keys}
}

// nodeBySlug finds the first node with this slug, or -1. First rather than
// exact because a slug is what the caret's section is reported as, and two
// sections can share one — in which case the outline highlights the earlier,
// which is what the flat list did too (it highlighted both).
func (m *outlineModel) nodeBySlug(slug string) (node int32) {
	if slug == "" {
		return -1
	}
	for i := range m.nodes {
		if m.nodes[i].slug == slug {
			return int32(i)
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Visibility and geometry
// ---------------------------------------------------------------------------

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

// outlineColWidth is the width of the tree's own outline column, which is the
// measured pane less the scrollbar and the count column beside it.
//
// It is derived from the pane rather than fixed, and the column is declared
// non-resizable, for the reason schemaview's navigator records: egui_table
// leaves a non-resizable column's declared width alone, so this is what lets
// the column track the pane as the window is dragged. A fixed slab would leave
// the rest of the pane dead beside it, and the row's selection outline — which
// spans the table's columns, not the pane — would stop short of the edge.
func (inst *App) outlineColWidth() (px float32) {
	px = inst.outlinePaneW - outlineScrollbarPx - outlineCountWidthPx
	if px < outlineMinColWidthPx {
		px = outlineMinColWidthPx
	}
	return
}

// ---------------------------------------------------------------------------
// Host-owned state
// ---------------------------------------------------------------------------

// outlineCollapseAll closes every section that has one. Leaves are skipped —
// collapsing one does nothing and would only leave records in the State to
// carry around.
//
// Deliberately a loop over what is there rather than [tree.State.CollapseAll],
// which would also flip the DEFAULT closed: a heading written after this would
// then arrive folded, and the outline's promise is that new content shows.
func (inst *App) outlineCollapseAll() {
	for i := range inst.outline.nodes {
		if inst.outline.nodes[i].descendants > 0 {
			inst.outlineState.SetExpanded(int32(i), false)
		}
	}
}

// syncOutline sets up the widget's state for the frame: the expansion default,
// and the selection derived from the caret.
//
// Expansion is not pushed. The hierarchy carries a key column, so the widget
// files a collapse under slug#ord and it survives the reparse every edit
// triggers — which is what this app used to keep a parallel map for. Sections
// default to OPEN, so the zero value is the fully expanded outline the flat
// list M2 shipped showed, and collapsing is added without taking the old view
// away from anyone who never uses it.
//
// Selection is derived rather than remembered: the selected row is whichever
// node the caret is in, never something the tree holds on to. A click moves
// the caret and the selection follows from that — one source of truth for
// "where am I", rather than a highlight that can disagree with the editor.
func (inst *App) syncOutline() {
	// Bound to THIS frame's hierarchy before anything is written, or the
	// selection below is filed under whatever key the previous parse gave that
	// index — and on the first frame, under no key at all.
	inst.outlineState.Bind(inst.outline.tree())
	inst.outlineState.SetDefaultExpanded(true)
	sel := int32(-1)
	for i := range inst.outline.nodes {
		if inst.outline.nodes[i].slug == inst.caretSlug {
			sel = int32(i)
			break
		}
	}
	if sel < 0 {
		inst.outlineState.ClearSelection()
		return
	}
	inst.outlineState.SelectOnly(sel)
}

// outlineReveal brings the caret's section into view when it has changed since
// the last time the outline drew: it opens whatever is closed above it and
// scrolls its row to the middle.
//
// Only on a change, for the reason the preview scroll is also only on a change
// — asking every frame would pin the outline against the reader's own
// scrolling. The consequence is that collapsing the section the caret is
// already in leaves it collapsed, which is the right answer: it was a
// deliberate gesture, and nothing has happened since to override it.
//
// Opening the ancestors is left to the widget, which does it as the first half
// of the reveal. That used to be impossible here: syncOutline rewrote every
// node's expansion from a host map on the next frame, so the widget's own
// ExpandAncestors was undone before it was ever drawn and the ancestors had to
// be opened in the map instead. With expansion filed under the key column
// there is no map to lose it to.
//
// Reports whether it asked for one, which is also readable afterwards from
// [tree.State.PendingReveal].
func (inst *App) outlineReveal() (asked bool) {
	slug := inst.caretSlug
	if slug == "" || slug == inst.outlineRevealed {
		return false
	}
	inst.outlineRevealed = slug
	node := inst.outline.nodeBySlug(slug)
	if node < 0 {
		return false
	}
	inst.outlineState.Reveal(node)
	return true
}

// applyOutline turns a frame's tree interaction into an app change: a click
// becomes a navigation. Expansion needs nothing — the widget has already
// applied it to the state that owns it.
func (inst *App) applyOutline(res tree.Result) {
	n := res.Clicked
	if n < 0 || int(n) >= inst.outline.len() {
		return
	}
	nd := inst.outline.nodes[n]

	// Set the scroll target WITHOUT touching caretSlug. The caret has not moved
	// YET — the request below moves it, and the caret's own baseline updates
	// when the editor reports back. Moving it here instead would make the next
	// frame read the caret's real section as changed and drag the preview
	// straight back off the heading just clicked.
	inst.pendingScroll = nd.slug

	// The reveal is marked done for the section the caret is about to land in.
	// Without this the caret's arrival next frame reads as a change and scrolls
	// the outline to centre a row the reader just clicked — which is to say,
	// the list jumps under a pointer that was already on target.
	inst.outlineRevealed = nd.slug

	// Take the writer to the heading too, not just the reader. The caret goes
	// to the LINE start so it sits before the `#`, which is where "go to this
	// heading" means, and with focus because the gesture means "take me there".
	off := lineStart(inst.src, nd.off)
	inst.requestCaret(off, off, true)
}

// ---------------------------------------------------------------------------
// The frame
// ---------------------------------------------------------------------------

// renderOutline draws the heading tree. A click scrolls the preview to that
// section and takes the caret with it; a disclosure control folds the section
// away.
//
// There is no ScrollArea here any more. The tree renders through an etable,
// which brings its own scroll and culls the rows outside it; wrapping it in one
// would give the pane two scrollbars and hand the table an unbounded parent,
// which is the case its auto-fit cap exists for. That also retires the reason
// the old list needed Hscroll — a long heading now truncates in a column of
// declared width instead of pushing its own width back out into the window's
// minimum, and is read from the row's tooltip.
func (inst *App) renderOutline() {
	inst.buildOutline()
	if inst.outline.len() == 0 {
		c.Label("No headings yet.").Send()
		return
	}
	inst.renderOutlineControls()

	// Probed AFTER the control row, so what is measured is the space left for
	// the tree rather than the whole panel. Held across frames: a frame in
	// which the probe does not answer reuses the last known size instead of
	// collapsing to the fallback.
	if w, h, ok := c.CapturePaneSize(inst.paneProbeSeq("outline-pane")); ok && w > 0 {
		inst.outlinePaneW, inst.outlinePaneH = w, h
	}
	paneH := inst.outlinePaneH
	if paneH <= 0 {
		paneH = outlineFallbackPaneH
	}

	// Reveal before sync: opening the caret's ancestors is a write to the same
	// map sync reads, and the widget's flatten has to see the result.
	inst.outlineReveal()
	inst.syncOutline()

	res := tree.Render(tree.Input{
		Ids:      inst.ids,
		ScopeKey: "outline",
		Tree:     inst.outline.tree(),
		State:    &inst.outlineState,
		Outline: tree.Column{
			Width: inst.outlineColWidth(),
			Cell:  inst.outlineHeadingCell,
		},
		Columns: []tree.Column{
			{Width: outlineCountWidthPx, Cell: inst.outlineCountCell},
		},
		Indent:    outlineIndentPx,
		MaxHeight: paneH,
	})
	inst.applyOutline(res)
}

// buildOutline rebuilds the hierarchy when the parse behind it has moved, and
// points the widget's State at the result either way.
//
// The gate is the Doc POINTER: syncDoc installs a fresh one whenever the buffer
// changes and leaves it alone otherwise, so pointer identity is exactly "the
// headings are the ones we already built from". Comparing the headings
// themselves would be the same answer for more work, and rebuilding every frame
// would put a walk and a map of every heading in the frame budget of a document
// nobody is editing.
//
// The bind is outside the gate and comes first, because everything downstream
// writes to the State by node index — the collapse-all button, the reveal, the
// selection — and an index means nothing until the State knows which parse it
// belongs to. tree.Render binds too, but it runs last.
func (inst *App) buildOutline() {
	if inst.outlineDoc != inst.doc {
		inst.outlineDoc = inst.doc
		var headings []markdown.HeadingInfo
		if inst.doc != nil {
			headings = inst.doc.Headings()
		}
		inst.outline.build(headings)
	}
	inst.outlineState.Bind(inst.outline.tree())
}

// renderOutlineControls draws the expand/collapse-all pair.
//
// They exist because collapsing does: reopening a document folded down to its
// top level is one click per section otherwise, and there is no keyboard route
// to the outline to do it with (ADR-0177's focus-scoped keys are not wired to
// this pane).
func (inst *App) renderOutlineControls() {
	for range c.Horizontal().KeepIter() {
		expand, collapse := false, false
		for range c.HoverText(tipOutlineExpandAll).KeepIter() {
			expand = c.Button(inst.ids.PrepareStr("ol-expand"), atomsOutlineExpand).
				Small().SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipOutlineCollapseAll).KeepIter() {
			collapse = c.Button(inst.ids.PrepareStr("ol-collapse"), atomsOutlineCollapse).
				Small().SendResp().HasPrimaryClicked()
		}
		switch {
		case expand:
			inst.outlineState.ExpandAll()
		case collapse:
			inst.outlineCollapseAll()
		}
	}
}

// outlineHeadingCell draws a row's heading text.
//
// It truncates rather than wraps — the row is one line high, so a wrapped label
// is a clipped one — and carries the full heading as a tooltip, which is where
// a long one is now read. Selectable(false) is load-bearing rather than tidy: a
// selectable label senses click-and-drag and is registered after the row's own
// sense region, so it would sit over it and swallow every click on its rect
// (ADR-0176 SD7).
func (inst *App) outlineHeadingCell(r tree.Row) {
	label := inst.outline.labels[r.Node]
	for range c.HoverText(label).KeepIter() {
		c.Label(label).Selectable(false).Truncate().Send()
	}
}

// outlineCountCell draws how many headings a closed section is hiding, and
// nothing at all otherwise — an open section's contents are on screen to be
// counted, and a number beside every row would be noise in a column this
// narrow.
func (inst *App) outlineCountCell(r tree.Row) {
	n := &inst.outline.nodes[r.Node]
	if n.descendants == 0 || r.Expanded {
		return
	}
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(outlineCountFg, outlineCountBg, itoa(n.descendants)).Small().End().
		Keep()).Selectable(false).Send()
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

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
