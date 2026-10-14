package recordtable

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// CellFrameSalt lifts a body cell's *inset-frame* id off the cell's own
// (button) id, so the frame and the button it wraps get distinct widget ids.
// [Chrome.SelectableCell] mints it for every cell. A host that packs its cell
// ids as base + row*stride + col keeps bit 40 clear for any result it can
// hold; [RenderGrid] scopes its cells per row and keys them by column
// position, far below it.
const CellFrameSalt uint64 = 1 << 40

// HeaderFrameSalt does the same for a header cell's inset frame, on a bit of
// its own so a header frame can never collide with a body-cell frame.
const HeaderFrameSalt uint64 = 1 << 60

// MinContentWidth is the narrowest run of *content* a column keeps when the
// user drags it in — a couple of monospace glyphs, enough to see that
// something is there and to find the resize handle again. The width floor a
// grid actually enforces is this plus the cell inset on both sides
// ([DragMinWidth]): counting only the leading inset left the trailing
// gridline sitting in the glyphs of the header.
const MinContentWidth float32 = 24

// MaxColumnWidth is the widest a column may be dragged, and the ceiling stored
// overrides are clamped to. One constant for both so the live drag and the
// stored value cannot disagree — a column pinned at the drag ceiling would
// otherwise be re-clamped on load and read as a change.
const MaxColumnWidth float32 = 1200

// CellPadX is the horizontal inset [Chrome.Inset] gives a cell on each side at
// density d.
func CellPadX(d styletokens.DensityE) (pad float32) {
	return styletokens.PaddingTight(d)
}

// DragMinWidth is the narrowest a resizable column may get: the content floor
// plus the inset [Chrome.Inset] spends on *each* side. Hand it both to
// egui_table (EtColumn.RangeMinMax, the live drag floor) and to the width
// resolver (colwidth.Opts.MinPoints, the floor stored overrides are clamped
// to): a drag floor below the stored floor would be corrected only on the next
// load, and a column would appear to jump on restart.
func DragMinWidth(d styletokens.DensityE) (w float32) {
	return MinContentWidth + 2*CellPadX(d)
}

// SelectionFill is the accent wash painted behind a selected cell when
// [Cell.SelectedFill] asks for one. It is styletokens.AccentDefault at ~35%
// alpha — the same accent, and roughly the same weight, as egui_table's own
// selected-row tint (ACCENT_DEFAULT.gamma_multiply(0.35), interpreter.rs).
// Built via color.Hex(token.AsHex()) with the alpha byte overridden, so it
// stays a token reference (designlint L2 flags raw color.RGBA, not Hex).
var SelectionFill = color.Hex((styletokens.AccentDefault.AsHex() & 0xffffff00) | 0x59)

// Chrome draws the parts every egui_table grid cell needs and egui_table does
// not provide: an inset on both sides, a header that lines up with its values,
// and a body cell whose whole rect is the click target.
//
// It is the cell layer only — the host still emits the columns, the table and
// the header/cell blocks. [RenderGrid] is built on it; a grid whose rows are
// not record rows (play's per-attribute view, several grid rows per entity)
// uses it directly. The zero value is unusable: Ids is required.
type Chrome struct {
	// Ids derives every frame and button id from the id the caller passes.
	Ids *c.WidgetIdStack
	// Note, when set, is handed the wire id of every cell button
	// [Chrome.SelectableCell] emits. A grid that replays captured cell bytes
	// ([RenderGrid] with a CellsKey) uses it to learn which response ids gate
	// a replay.
	Note func(wire uint64)
}

// Cell is one body cell's content and look for [Chrome.SelectableCell]. The
// zero value is an empty, unselected, centred, truncated cell.
type Cell struct {
	Text string
	// Weak dims the text (a placeholder, a gutter).
	Weak bool
	// Selected marks the cell's button selected.
	Selected bool
	// SelectedFill paints [SelectionFill] behind the cell while it is
	// Selected. Leave it off where egui_table already paints the selected
	// row (EndETable.SelectedRow); turn it on where one source row spans
	// several grid rows, which a single SelectedRow index cannot cover.
	SelectedFill bool
	// LeftAlign moves the text to the cell's left edge instead of centring
	// it, for free-text columns so their values line up under the
	// left-aligned header.
	LeftAlign bool
	// Tone colours the text when Toned is set (a ✓ in success, a ✗ in error).
	Tone  color.Color
	Toned bool
	// Link, when non-empty, makes the cell a hyperlink to that URL instead
	// of a button: clicking it opens the link through the host's opener and
	// does not select the row — the row's other cells still do — since a
	// hyperlink's click is consumed by the widget and not reported back.
	Link string
	// Hover, when non-empty, is shown over the cell (the full value of a
	// shortened one).
	Hover string
	// Untruncated lets the text overflow the column. Set it on the one frame
	// a grid asks egui_table to fit its columns: egui_table sizes a column in
	// a sizing pass with each cell rect shrunk to the column minimum and
	// takes the cell's allocated width, so a truncated button reports its
	// truncated width and a cell wider than its header (`4111 •••• •••• 1111
	// ✓`, a long URL) stays truncated for good. Untruncated on that frame,
	// the button reports its intrinsic width and the column fits it;
	// egui_table discards and re-runs the frame, so the overflow is never
	// shown. Cut the text with [FitRunes] on that frame, so a column is not
	// sized to a paragraph.
	Untruncated bool
}

// Inset gives one grid cell — header or body — the same horizontal margin on
// each side and cuts its content at the inner edge.
//
// egui_table adds no margins of its own ("Does not add any margins to cells",
// its docs say, "add them yourself") and clips each cell to the column rect.
// A bare leading AddSpace pads the left only: the column reads off-centre, and
// anything wider than the column — a header label, a truncated value — is cut
// on the gridline itself, so the vertical border sits in the glyphs. It is
// most obvious at the narrowest width a column can be dragged to, where every
// header is wider than its column.
//
// A Frame carries the margin on both sides. Its background, when it has one,
// still spans the whole cell: egui paints the frame's *outer* rect, margins
// included, so a selected cell's accent band stays continuous across the row
// while its text is inset. UiClipToMaxRect then moves the cut from the
// gridline to the inner edge, which is what keeps the border off the glyphs;
// without it the Frame would pad content that fits and change nothing for
// content that does not. The margin also joins what a sizing pass measures,
// so an auto-fitted column fits its content *and* its insets.
//
// fill is painted behind the cell when filled is set; an unfilled Frame is
// transparent and paints nothing. id is the frame's own id; callers that
// wrap a widget keyed by the same number salt it ([HeaderFrameSalt],
// [CellFrameSalt]).
func (inst *Chrome) Inset(id uint64, padX float32, fill color.Color, filled bool, body func()) {
	fr := c.Frame(inst.Ids.PrepareSeq(id)).
		InnerMarginSides(padX, padX, 0, 0)
	if filled {
		fr = fr.Fill(fill)
	}
	for range fr.KeepIter() {
		c.UiClipToMaxRect()
		body()
	}
}

// HeaderCell insets a header cell the same way body cells are inset, so a
// column title lines up with the values beneath it on both edges. id is the
// header's own number (a column position); the frame takes it salted with
// [HeaderFrameSalt], so the body may key a sort button by id unsalted.
func (inst *Chrome) HeaderCell(id uint64, padX float32, body func()) {
	inst.Inset(id^HeaderFrameSalt, padX, color.Transparent, false, body)
}

// SelectableCell renders one grid cell as a full-width, cross-justified
// selectable button so a primary click anywhere in the cell — not only on the
// painted glyphs — selects the row. egui sizes a frameless button to its
// content, and egui_table senses no clicks of its own (the delegate implements
// no row_ui), so a bare per-cell button leaves the cell's left inset and every
// blank cell as a dead click target. That reads as "finicky" where short
// values sit in wide columns, and outright breaks a grid that blanks most
// cells (play's per-attribute view). The cross-justified wrapper stretches the
// button to the column width; a non-small egui Button already floors its
// height at interact_size.y, so the hit area covers the whole row-height cell.
// The justify wrapper adds no id-stack scope, so the button is keyed exactly by
// id. Returns true on a primary click. padX is the inset the headers get, on
// both sides ([Chrome.Inset]), so cell text aligns under its header.
//
// A frameless egui Button never paints a background even when Selected (see
// button.rs), which is why [Cell.SelectedFill] is a fill on the inset Frame
// rather than the button's own; the band still spans the column because egui
// paints the frame's outer rect, and the text does not shift versus an
// unselected cell because every cell wears the same Frame either way.
//
// [Cell.LeftAlign] only swaps the cross-axis alignment of the justifying
// wrapper (Center → Min); everything else — the full-width justify that grows
// the hit area, the button id, the height flooring, the selection band — is
// unchanged, since VerticalCenteredJustified is exactly this same top-down
// cross-justified layout with Center rather than Min cross-alignment.
func (inst *Chrome) SelectableCell(id uint64, padX float32, cell Cell) (clicked bool) {
	if cell.Hover != "" {
		hover := cell.Hover
		cell.Hover = ""
		for range c.HoverText(hover).KeepIter() {
			clicked = inst.SelectableCell(id, padX, cell)
		}
		return
	}
	emitButton := func() {
		if cell.Link != "" {
			c.HyperlinkTo(cell.Text, cell.Link).OpenInNewTab(true).Send()
			return
		}
		var rt c.RichTextScope
		if cell.Toned {
			rt = c.Atoms().BeginRichTextColored(cell.Tone, color.Transparent, cell.Text).Monospace()
		} else {
			rt = c.Atoms().BeginRichText(cell.Text).Monospace()
		}
		if cell.Weak {
			rt = rt.Weak()
		}
		// Derive once and hand the button the derived id: Note keys a
		// response gate on the wire id, and Derive consumes the prepared seq.
		wire := inst.Ids.PrepareSeq(id).Derive()
		if inst.Note != nil {
			inst.Note(wire)
		}
		btn := c.Button(c.MakeAbsoluteIdHighEntropy(wire), rt.End().Keep()).
			Frame(false).
			Selected(cell.Selected)
		if !cell.Untruncated {
			btn = btn.Truncate()
		}
		clicked = btn.SendResp().HasPrimaryClicked()
	}
	inst.Inset(id^CellFrameSalt, padX, SelectionFill, cell.SelectedFill && cell.Selected, func() {
		if cell.LeftAlign {
			for range c.UiWithLayout().
				MainDirTopDown().
				CrossAlignMin().
				CrossJustify(true).
				KeepIter() {
				emitButton()
			}
		} else {
			for range c.VerticalCenteredJustified().KeepIter() {
				emitButton()
			}
		}
	})
	return
}
