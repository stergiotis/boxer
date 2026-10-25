package mdoutline

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

const (
	// IndentPx is the per-level indent, narrower than the tree widget's own
	// default of 14. Headings nest up to six deep and the column is usually
	// a slice of a window rather than a pane in its own right, so the step
	// has to be small enough that level six still has room for a word.
	IndentPx = float32(10)

	// CountWidthPx is the trailing column that carries a collapsed
	// section's hidden-heading count.
	//
	// It is a column rather than a chip after the label. A truncating label
	// in a horizontal row takes the whole width it is offered, so anything
	// emitted after it is pushed out of the cell, which for an outline,
	// where long headings are the common case, would mean the count
	// disappears on the rows that have most to hide. A column of its own is
	// reserved before the label is laid out, so the count is always there
	// and always in the same place.
	CountWidthPx = float32(30)

	// ScrollbarPx is held back from the measured pane so the outline column
	// stops short of the table's vertical scrollbar instead of running under
	// it. A deliberate over-estimate.
	ScrollbarPx = float32(16)

	// MinColWidthPx floors the outline column, so a pane measurement that
	// has not landed yet cannot collapse it to nothing.
	MinColWidthPx = float32(70)

	// FallbackPaneH stands in for the pane height on the first frame, before
	// the probe reports. Without some height the table falls back to the
	// auto-fit heuristic, capped at ETABLE_AUTOFIT_CAP_PX, and a document of
	// any length overruns it.
	FallbackPaneH = float32(360)

	defaultScopeKey = "outline"
	defaultEmpty    = "No headings yet."
)

// Hover help for the outline-wide controls.
const (
	tipExpandAll   = "Open every section."
	tipCollapseAll = "Close every section, leaving the top-level headings. Collapsing is per section and lasts as long as the window."
)

// The bar's buttons, built once: identical retained bytes across frames
// intern to one blob.
var (
	atomsExpand   = c.Atoms().Text(icons.PhArrowsOutSimple).Keep()
	atomsCollapse = c.Atoms().Text(icons.PhArrowsInSimple).Keep()
)

// The count chip's colours, from the design-system roles (ADR-0031).
// Secondary text on no background of its own: the row block behind it
// already carries the fill, and a chip with its own would fight it.
var (
	countFg = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	countBg = color.Transparent
)

// View is one outline on screen: the hierarchy, the tree widget's own
// expansion and selection, and what the outline last revealed. Retain it
// across frames; its zero value is ready.
type View struct {
	Model Model
	// State is the tree widget's expansion, selection and cursor, filed
	// under the hierarchy's keys so a collapse survives a rebuild.
	State tree.State
	// doc is the parse the hierarchy was built from; a different pointer
	// means a rebuild.
	doc *markdown.Doc
	// revealed is the section the outline last scrolled itself to, so a
	// caret that stays put re-issues nothing.
	revealed string
	// paneW/H are what the pane probe last reported, held across frames.
	paneW float32
	paneH float32
}

// Input is one frame's render request.
type Input struct {
	// Ids is the host's widget id stack. The tree opens its own scope under
	// it keyed by ScopeKey; the bar's buttons and the pane probe derive
	// from ScopeKey too, so two outlines under one host differ by it alone.
	Ids *c.WidgetIdStack
	// ScopeKey names this outline within the host's id space; empty uses
	// "outline".
	ScopeKey string
	// Doc is the parsed document; the hierarchy is rebuilt when it is not
	// the one it was built from. A nil Doc shows the empty label.
	Doc *markdown.Doc
	// CurrentSlug is the section the reader is in, which the selection is
	// derived from each frame and which the outline reveals when it changes.
	// Empty is "above the first heading", and selects nothing.
	CurrentSlug string
	// MaxHeight caps the table; 0 probes the pane the outline renders in,
	// one frame late, with [FallbackPaneH] until the probe answers.
	MaxHeight float32
	// Controls, when set, draws the host's own buttons after the expand and
	// collapse pair, inside the same row.
	Controls func()
	// Empty is the label shown when the document has no headings; empty
	// uses "No headings yet.".
	Empty string
}

// Result is what one Render reports.
type Result struct {
	// Clicked is the node whose row was clicked this frame, -1 for none.
	// Expansion needs nothing: the widget has already applied it.
	Clicked int32
	// Node is the clicked node's metadata, meaningful when Clicked >= 0.
	Node Node
}

// Sync rebuilds the hierarchy when the parse behind it has moved, and binds
// the widget's State to the result either way.
//
// The gate is the Doc pointer: a host installs a fresh one whenever the
// source changes and leaves it alone otherwise, so pointer identity is
// exactly "the headings are the ones we already built from". The bind is
// outside the gate and comes first, because everything downstream writes to
// the State by node index, and an index means nothing until the State knows
// which parse it belongs to.
func (v *View) Sync(doc *markdown.Doc) {
	if v.doc != doc {
		v.doc = doc
		var headings []markdown.HeadingInfo
		if doc != nil {
			headings = doc.Headings()
		}
		v.Model.Build(headings)
	}
	v.State.Bind(v.Model.Tree())
}

// Select sets up the widget's state for the frame: the expansion default,
// and the selection derived from the current section.
//
// Expansion is not pushed. The hierarchy carries a key column, so the widget
// files a collapse under slug#ord and it survives a rebuild. Sections
// default to open, so the zero value is a fully expanded outline.
//
// Selection is derived rather than remembered: the selected row is whichever
// node the reader is in, never something the tree holds on to, so the
// outline can never disagree with the host about where the reader is.
func (v *View) Select(currentSlug string) {
	// Bound to this frame's hierarchy before anything is written, or the
	// selection is filed under whatever key a previous parse gave that
	// index, and on the first frame under no key at all.
	v.State.Bind(v.Model.Tree())
	v.State.SetDefaultExpanded(true)
	sel := v.Model.NodeBySlug(currentSlug)
	if sel < 0 {
		v.State.ClearSelection()
		return
	}
	v.State.SelectOnly(sel)
}

// Reveal brings the current section into view when it has changed since
// the last reveal: it opens whatever is closed above it and scrolls its row
// to the middle. Reports whether it asked for one.
//
// Only on a change, because asking every frame would pin the outline
// against the reader's own scrolling. The consequence is that collapsing
// the section the reader is already in leaves it collapsed, which is the
// right answer: it was a deliberate gesture, and nothing has happened since
// to override it. Opening the ancestors is left to the widget.
func (v *View) Reveal(currentSlug string) (asked bool) {
	if currentSlug == "" || currentSlug == v.revealed {
		return false
	}
	v.revealed = currentSlug
	node := v.Model.NodeBySlug(currentSlug)
	if node < 0 {
		return false
	}
	v.State.Reveal(node)
	return true
}

// MarkRevealed records slug as already revealed, so the reader arriving in
// a section they just clicked does not scroll the outline under a pointer
// that was already on target.
func (v *View) MarkRevealed(slug string) { v.revealed = slug }

// CollapseAll closes every section that has one. Leaves are skipped:
// collapsing one does nothing and would only leave records in the State.
//
// Deliberately a loop over what is there rather than [tree.State.CollapseAll],
// which would also flip the default closed: a heading written after this
// would then arrive folded, and the outline's promise is that new content
// shows.
func (v *View) CollapseAll() {
	for i := range v.Model.nodes {
		if v.Model.nodes[i].Descendants > 0 {
			v.State.SetExpanded(int32(i), false)
		}
	}
}

// ExpandAll opens every section.
func (v *View) ExpandAll() { v.State.ExpandAll() }

// ColWidth is the width of the tree's own outline column, which is the
// measured pane less the scrollbar and the count column beside it.
//
// It is derived from the pane rather than fixed, and the column is declared
// non-resizable: the table leaves a non-resizable column's declared width
// alone, so this is what lets the column track the pane as the window is
// dragged. A fixed slab would leave the rest of the pane dead beside it.
func (v *View) ColWidth() (px float32) {
	px = v.paneW - ScrollbarPx - CountWidthPx
	if px < MinColWidthPx {
		px = MinColWidthPx
	}
	return
}

// SetPaneWidth records a pane measurement taken by the host, for a host
// that probes the pane itself.
func (v *View) SetPaneWidth(px float32) { v.paneW = px }

// Render draws the heading tree: the control row, then the tree. A click is
// reported; a disclosure control folds the section away.
//
// There is no ScrollArea here. The tree renders through a table that brings
// its own scroll and culls the rows outside it; wrapping it in one would
// give the pane two scrollbars and hand the table an unbounded parent.
func (v *View) Render(in Input) (res Result) {
	res.Clicked = -1
	scope := in.ScopeKey
	if scope == "" {
		scope = defaultScopeKey
	}
	v.Sync(in.Doc)
	if v.Model.Len() == 0 {
		empty := in.Empty
		if empty == "" {
			empty = defaultEmpty
		}
		c.Label(empty).Send()
		return
	}
	v.renderControls(in, scope)

	// Probed after the control row, so what is measured is the space left
	// for the tree. Held across frames: a frame in which the probe does not
	// answer reuses the last known size instead of collapsing to the
	// fallback.
	if w, h, ok := c.CapturePaneSize(in.Ids.PrepareStr(scope + "-pane").Derive()); ok && w > 0 {
		v.paneW, v.paneH = w, h
	}
	maxH := in.MaxHeight
	if maxH <= 0 {
		maxH = v.paneH
	}
	if maxH <= 0 {
		maxH = FallbackPaneH
	}

	// Reveal before select: opening the current section's ancestors is a
	// write to the same map select reads, and the widget's flatten has to
	// see the result.
	v.Reveal(in.CurrentSlug)
	v.Select(in.CurrentSlug)

	tr := tree.Render(tree.Input{
		Ids:      in.Ids,
		ScopeKey: scope,
		Tree:     v.Model.Tree(),
		State:    &v.State,
		Outline: tree.Column{
			Width: v.ColWidth(),
			Cell:  v.headingCell,
		},
		Columns: []tree.Column{
			{Width: CountWidthPx, Cell: v.countCell},
		},
		Indent:    IndentPx,
		MaxHeight: maxH,
	})
	// A result describes the previous frame's geometry, and a reparse
	// between the two can have shortened the outline; an out-of-range node
	// is dropped.
	if tr.Clicked >= 0 && int(tr.Clicked) < v.Model.Len() {
		res.Clicked = tr.Clicked
		res.Node = v.Model.Node(tr.Clicked)
	}
	return
}

// renderControls draws the expand and collapse pair, then the host's own
// controls. The pair exists because collapsing does: reopening a document
// folded down to its top level is one click per section otherwise.
func (v *View) renderControls(in Input, scope string) {
	for range c.Horizontal().KeepIter() {
		expand, collapse := false, false
		for range c.HoverText(tipExpandAll).KeepIter() {
			expand = c.Button(in.Ids.PrepareStr(scope+"-expand"), atomsExpand).
				Small().SendResp().HasPrimaryClicked()
		}
		for range c.HoverText(tipCollapseAll).KeepIter() {
			collapse = c.Button(in.Ids.PrepareStr(scope+"-collapse"), atomsCollapse).
				Small().SendResp().HasPrimaryClicked()
		}
		switch {
		case expand:
			v.ExpandAll()
		case collapse:
			v.CollapseAll()
		}
		if in.Controls != nil {
			in.Controls()
		}
	}
}

// headingCell draws a row's heading text.
//
// It truncates rather than wraps, since the row is one line high, and
// carries the full heading as a tooltip, which is where a long one is read.
// Selectable(false) is load-bearing: a selectable label senses
// click-and-drag and is registered after the row's own sense region, so it
// would sit over it and swallow every click on its rect.
func (v *View) headingCell(r tree.Row) {
	label := v.Model.Label(r.Node)
	for range c.HoverText(label).KeepIter() {
		c.Label(label).Selectable(false).Truncate().Send()
	}
}

// countCell draws how many headings a closed section is hiding, and nothing
// otherwise: an open section's contents are on screen to be counted.
func (v *View) countCell(r tree.Row) {
	n := v.Model.Node(r.Node)
	if n.Descendants == 0 || r.Expanded {
		return
	}
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(countFg, countBg, itoa(n.Descendants)).Small().End().
		Keep()).Selectable(false).Send()
}
