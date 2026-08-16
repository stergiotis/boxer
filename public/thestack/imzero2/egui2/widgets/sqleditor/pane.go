package sqleditor

// The completion pane (ADR-0190 §SD8): a table showing the domain at the
// caret, with the match state rendered on the rows.
//
// It is not a popup. It takes no input focus and captures no key, so its rows
// are ordinary widgets that the headless tree driver can assert (ADR-0154) and
// that a click completes. What a popup buys — fewer keystrokes to a long name —
// is one captured key later (§SD10), and it does not need the pane to change.
//
// The pane lives beside the editor rather than inside it because an embedder
// docks it: play puts it next to the Docs pane, and an embedder without a dock
// draws it under the editor. Either way it is fed a
// [github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete.Result] and
// nothing else — it does no completion of its own.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

const (
	// PaneWholeDomainMax is how many candidates a domain may have and still be
	// shown whole, with the matching rows highlighted rather than the others
	// hidden (§SD8's "small closed domains show whole").
	//
	// The number is set by what the domains actually are. The closed
	// in-process ones are small — component kinds and their fields, the
	// introspection catalogue (33 today), the sections of a table, the aspect
	// enums, the gloss catalogue — and all of them fit; the ones that do not
	// are the server's (columns, functions, 597 time zones), which are exactly
	// the ones a reader narrows by typing. Showing a small domain whole is the
	// point: the pane is the glance that says what the argument IS, not only
	// what extends what has been typed.
	PaneWholeDomainMax = 64
	// PaneFallbackHeight is the height before the host's pane probe answers —
	// it reports one frame late and not at all on the first.
	PaneFallbackHeight float32 = 320

	paneNameColWidth   float32 = 210
	paneTypeColWidth   float32 = 150
	paneSourceColWidth float32 = 110
	paneDocColMinWidth float32 = 220
	paneScrollbarPad   float32 = 18
)

var (
	// paneFgMuted recesses the secondary columns, as the vocabulary panel does
	// with the same tokens — the two surfaces sit side by side.
	paneFgMuted = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	paneBgNone  = color.Transparent
	// paneMatchStroke outlines the row that matches. An outline rather than a
	// fill: a fill has to thread between invisible against the pane's own
	// surface tone and washing out the row's text, and an outline reads on any
	// backdrop without touching contrast.
	paneMatchStroke = color.Hex(styletokens.AccentDefault.AsHex())
	// paneExactStroke outlines the row the typed token resolves TO. The
	// success tone, because "this resolves" is what §SD9 tints the editor for
	// and the two surfaces must not say it in different colours.
	paneExactStroke = color.Hex(styletokens.SuccessDefault.AsHex())
)

// PaneInput is one frame's binding.
type PaneInput struct {
	// Ids is the host's widget id stack; the pane opens its own scope under
	// it, so two panes in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this pane within the host's id space; empty uses
	// "sqlcompletion".
	ScopeKey string
	// Result is what to show.
	Result sqlcomplete.Result
	// Heading is the line above the table saying what position this is. Empty
	// suppresses it, which is what an embedder drawing its own chrome wants.
	Heading string
	// MaxHeight caps the table's vertical extent. Feed it the pane's measured
	// height with [PaneFallbackHeight] for the first frame.
	MaxHeight float32
	// Width is the pane's measured width, used to give the doc column what is
	// left. Zero leaves every column at its floor.
	Width float32
	// OnAccept fires when a row in the match state is clicked. suffix is the
	// candidate minus what has already been typed — what
	// `TextEditFluid.InsertAtCursor` splices at the caret (§SD10). It is empty
	// when the candidate is already fully typed, and the pane still calls, so
	// an embedder can treat a click on the exact row as a confirmation.
	OnAccept func(item sqlcomplete.Item, suffix string)
	// CaretAtPartialEnd gates the click: a suffix insert is only valid when
	// the caret sits at the end of the token being completed. When false the
	// rows render as reference and no click completes.
	CaretAtPartialEnd bool
	// Typed is what has been typed at the caret, for computing the suffix.
	Typed string
}

// Pane is one completion pane's cross-frame state.
//
// Render-thread-only, like every stateful widget (ADR-0013).
type Pane struct {
	nav   tree.State
	rows  []paneRow
	built tree.Tree
}

type paneRow struct {
	item  sqlcomplete.Item
	exact bool
	match bool
}

// Render draws one frame of the pane.
func (inst *Pane) Render(in PaneInput) {
	scope := in.ScopeKey
	if scope == "" {
		scope = "sqlcompletion"
	}
	if in.Heading != "" {
		for rt := range c.RichTextLabel(in.Heading) {
			rt.Small().Weak()
		}
	}
	if len(in.Result.Items) == 0 {
		inst.renderSilence(in.Result)
		return
	}
	inst.build(in.Result)
	if len(inst.rows) == 0 {
		for rt := range c.RichTextLabel("nothing here extends " + strconv.Quote(in.Typed)) {
			rt.Small().Weak()
		}
		return
	}

	h := in.MaxHeight
	if h <= 0 {
		h = PaneFallbackHeight
	}
	docW := paneDocColMinWidth
	if w := in.Width - paneNameColWidth - paneTypeColWidth - paneSourceColWidth - paneScrollbarPad; w > docW {
		docW = w
	}

	accept := in.OnAccept
	if !in.CaretAtPartialEnd {
		accept = nil
	}
	tree.Render(tree.Input{
		Ids:      in.Ids,
		ScopeKey: scope,
		Tree:     inst.built,
		State:    &inst.nav,
		Outline: tree.Column{
			Header: "candidate", Width: paneNameColWidth, Resizable: true,
			Cell: func(r tree.Row) { inst.renderNameCell(in, r, accept) },
		},
		Columns: []tree.Column{
			{Header: "type", Width: paneTypeColWidth, Cell: inst.renderTypeCell},
			{Header: "from", Width: paneSourceColWidth, Cell: inst.renderSourceCell},
			{Header: "what it is", Width: docW, Cell: inst.renderDocCell},
		},
		MaxHeight: h,
	})
}

// renderSilence draws the reason there is nothing, which every empty result
// carries (§SD1). An empty table with no sentence is indistinguishable from a
// pane that failed to render.
func (inst *Pane) renderSilence(res sqlcomplete.Result) {
	msg := res.Silent
	if msg == "" {
		msg = "nothing to complete here"
	}
	for rt := range c.RichTextLabel(msg) {
		rt.Small().Weak()
	}
}

// build turns the result into this frame's rows.
//
// A domain small enough to show whole shows whole, with the matching rows
// marked; a large one is filtered to the matches, because scrolling 597 time
// zones to find the highlighted one is not a glance.
func (inst *Pane) build(res sqlcomplete.Result) {
	whole := len(res.Items) <= PaneWholeDomainMax
	inMatch := make(map[int]struct{}, len(res.Prefix))
	for _, i := range res.Prefix {
		inMatch[i] = struct{}{}
	}

	inst.rows = inst.rows[:0]
	inst.built.Labels = inst.built.Labels[:0]
	inst.built.Parents = inst.built.Parents[:0]
	inst.built.Keys = inst.built.Keys[:0]
	for i := range res.Items {
		_, matched := inMatch[i]
		if !whole && !matched && i != res.Exact {
			continue
		}
		inst.rows = append(inst.rows, paneRow{
			item:  res.Items[i],
			exact: i == res.Exact,
			match: matched,
		})
		inst.built.Labels = append(inst.built.Labels, res.Items[i].Text)
		inst.built.Parents = append(inst.built.Parents, -1)
		inst.built.Keys = append(inst.built.Keys, res.Items[i].Text)
	}
}

func (inst *Pane) row(node int32) *paneRow {
	if node < 0 || int(node) >= len(inst.rows) {
		return nil
	}
	return &inst.rows[node]
}

// renderNameCell draws the candidate and its match state.
//
// The Frame is unconditional and only its stroke varies: c.Frame derives a
// stacked id scope, so a frame that appears and disappears XORs every inner
// widget's id in and out, and egui keys per-widget state by id.
func (inst *Pane) renderNameCell(in PaneInput, r tree.Row, accept func(sqlcomplete.Item, string)) {
	row := inst.row(r.Node)
	if row == nil {
		return
	}
	stroke := float32(0)
	col := paneMatchStroke
	switch {
	case row.exact:
		stroke, col = 1, paneExactStroke
	case row.match && in.Typed != "":
		stroke = 1
	}
	f := c.Frame(in.Ids.PrepareSeq(paneRowIDBase + uint64(r.Node)))
	if stroke > 0 {
		f = f.Stroke(stroke, col)
	}
	for range f.KeepIter() {
		for range c.Horizontal().KeepIter() {
			for range c.HoverText(row.item.Text).KeepIter() {
				c.LabelAtoms(c.Atoms().BeginRichText(row.item.Text).Monospace().End().Keep()).
					Selectable(false).Truncate().Send()
			}
			if accept == nil || !row.match {
				continue
			}
			if c.Button(in.Ids.PrepareSeq(paneInsertIDBase+uint64(r.Node)),
				c.Atoms().Text("↵").Keep()).SendResp().HasPrimaryClicked() {
				accept(row.item, strings.TrimPrefix(row.item.Insert, in.Typed))
			}
		}
	}
}

func (inst *Pane) renderTypeCell(r tree.Row) {
	row := inst.row(r.Node)
	if row == nil {
		return
	}
	text := row.item.Type
	if text == "" {
		text = row.item.Kind.String()
	}
	for range c.HoverText(text).KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(paneFgMuted, paneBgNone, text).Small().End().Keep()).
			Selectable(false).Truncate().Send()
	}
}

func (inst *Pane) renderSourceCell(r tree.Row) {
	row := inst.row(r.Node)
	if row == nil {
		return
	}
	text := row.item.Source
	if len(row.item.Marks) > 0 {
		text = strings.Join(row.item.Marks, " ") + " " + text
	}
	if text == "" {
		return
	}
	for range c.HoverText(text).KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(paneFgMuted, paneBgNone, text).Small().End().Keep()).
			Selectable(false).Truncate().Send()
	}
}

func (inst *Pane) renderDocCell(r tree.Row) {
	row := inst.row(r.Node)
	if row == nil || row.item.Doc == "" {
		return
	}
	for range c.HoverText(row.item.Doc).KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(paneFgMuted, paneBgNone, row.item.Doc).Small().End().Keep()).
			Selectable(false).Truncate().Send()
	}
}

// The pane's two per-row id bases. Far apart so a row's frame and its button
// cannot collide, and derived from the row index so a rebuild that keeps the
// same rows keeps the same ids.
const (
	paneRowIDBase    uint64 = 0x5170_0100
	paneInsertIDBase uint64 = 0x5170_0200
)

// PaneHeading is the sentence over the table saying which position this is.
// Exported because an embedder drawing its own chrome wants the same wording.
func PaneHeading(res sqlcomplete.Result) (s string) {
	if res.Callee != "" && res.Ordinal >= 0 {
		return res.Callee + " argument " + strconv.Itoa(res.Ordinal+1) + " — " + res.Domain.Kind.String()
	}
	if res.Domain.Kind != 0 {
		return res.Domain.Kind.String()
	}
	return "at the caret"
}
