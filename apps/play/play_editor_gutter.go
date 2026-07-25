package play

// The SQL editor's line-number gutter and marks lane (ADR-0130 L3).
//
// Alignment contract: no-wrap. The editor's layouter is switched to
// f32::INFINITY wrap width (TextEdit.NoWrapLayout), so a galley row is
// exactly a logical line and the gutter's Nth row is the editor's Nth line by
// construction. Monospace on both sides makes the row height match without
// measuring anything.
//
// The gutter is ONE widget, not one label per line: a column of N labels
// accumulates item_spacing between them and drifts out of step with the
// editor's rows within a screenful. A single monospace CodeView lays its rows
// out on the same galley rules the editor uses.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	// monoAdvanceRatio is the assumed glyph advance as a fraction of the font
	// size, used to size the editor's scrollable width.
	//
	// It is an average, not an exact advance: the host may leave the
	// monospace face unconfigured (mono_font_ttf empty), in which case
	// TextStyle::Monospace resolves to the proportional main font and per-glyph
	// advances range from ~2.4 px for punctuation to ~8.3 px for capitals at
	// BodyPt. Averaging over is the right side to err on — over-reserving costs
	// only unused scroll range, while under-reserving would clip the tail of the
	// longest line out of reach. Row alignment, which is what the gutter
	// actually depends on, is unaffected either way: row height is uniform per
	// font size regardless of glyph widths.
	monoAdvanceRatio float32 = 0.62
	// textEditTopMarginPx is egui's default TextEdit inner margin on the y
	// axis (Margin::symmetric(4, 2) in egui 0.35's text_edit/builder.rs), plus
	// the frame stroke. The gutter is pushed down by it so row 1 lines up with
	// the editor's first line rather than with its frame.
	textEditTopMarginPx float32 = 3.0
	// gutterTrailingPadPx separates the numbers from the editor frame.
	gutterTrailingPadPx float32 = 4.0
	// editorTrailingCols is slack past the longest line so the caret at
	// end-of-line is still inside the scrollable area.
	editorTrailingCols = 3
	// editorFallbackWidthPx sizes the editor on the first frame, before the
	// previous frame's available-size capture (R18) has anything to report.
	editorFallbackWidthPx float32 = 480.0
)

// gutterMarkE is what the marks lane shows for a line. Higher values win when
// a line qualifies for more than one.
type gutterMarkE uint8

const (
	gutterMarkNone gutterMarkE = iota
	gutterMarkActive
	gutterMarkError
)

// gutterModel is everything the gutter needs for one frame, derived from the
// buffer and the same overlay inputs the styled sections use.
type gutterModel struct {
	lines   int
	marks   []gutterMarkE // one per line
	digits  int           // width of the widest line number
	charPx  float32
	present bool
}

// buildGutterModel derives the per-line marks from the styled overlays already
// computed for this frame, so the gutter and the text decoration can never
// disagree: an error underline puts an error mark on its line, and the active
// statement's tint marks every line it spans.
func (inst *PlayApp) buildGutterModel(buf string, viewOffset int) (m gutterModel) {
	if buf == "" {
		return
	}
	starts := lineStarts(buf)
	m.lines = len(starts)
	m.marks = make([]gutterMarkE, m.lines)
	m.digits = len(strconv.Itoa(m.lines))
	m.charPx = styletokens.ScaledPt(styletokens.BodyPt, inst.density) * monoAdvanceRatio
	m.present = true

	for _, s := range inst.editorStyledSections() {
		mark := gutterMarkNone
		switch {
		case s.Flags&codeview.StyleUnderline != 0 && s.Color == styleErrorTone:
			mark = gutterMarkError
		case s.Flags&codeview.StyleBackground != 0:
			mark = gutterMarkActive
		default:
			continue
		}
		first := lineIndexOf(starts, int(s.Start)-viewOffset)
		last := lineIndexOf(starts, int(s.Stop)-1-viewOffset)
		for i := first; i <= last && i < m.lines; i++ {
			if i >= 0 && mark > m.marks[i] {
				m.marks[i] = mark
			}
		}
	}
	return
}

// lineStarts returns the byte offset of each line's first byte. A trailing
// newline does not manufacture a phantom final line — it is the same rule the
// editor's galley uses, and the two must agree or every mark below the last
// newline is off by one.
func lineStarts(s string) (out []int) {
	out = append(out, 0)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && i+1 < len(s) {
			out = append(out, i+1)
		}
	}
	return
}

// lineIndexOf maps a byte offset to its 0-based line, clamped.
func lineIndexOf(starts []int, off int) int {
	if off < 0 {
		return 0
	}
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// gutterText renders the model as one monospace block: a mark column, then
// right-aligned line numbers. Returned alongside per-line byte ranges for the
// mark column and for the rest of the line, so the caller can colour the mark
// independently.
//
// Both ranges are returned because a CodeViewJob does not gap-fill: bytes no
// section covers are simply absent from the LayoutJob, and egui drops their
// glyphs. Every byte of this text has to be claimed by one span or the other.
func (m gutterModel) gutterText() (text string, markSpans, restSpans [][2]int) {
	var b strings.Builder
	markSpans = make([][2]int, m.lines)
	restSpans = make([][2]int, m.lines)
	for i := 0; i < m.lines; i++ {
		markSpans[i][0] = b.Len()
		switch m.marks[i] {
		case gutterMarkError:
			b.WriteString("!")
		case gutterMarkActive:
			b.WriteString(">")
		default:
			b.WriteString(" ")
		}
		markSpans[i][1] = b.Len()
		restSpans[i][0] = b.Len()
		n := strconv.Itoa(i + 1)
		for p := len(n); p < m.digits; p++ {
			b.WriteByte(' ')
		}
		b.WriteString(n)
		if i+1 < m.lines {
			b.WriteByte('\n') // claimed by the rest span — an unclaimed
			// newline would collapse the following line onto this one
		}
		restSpans[i][1] = b.Len()
	}
	return b.String(), markSpans, restSpans
}

// widthPx is the gutter column's width: mark + digits, plus trailing pad.
func (m gutterModel) widthPx() float32 {
	return float32(m.digits+1)*m.charPx + gutterTrailingPadPx
}

// editorWidthPx is the width the editor must allocate so its longest line is
// reachable by the enclosing scroll area. No-wrap means the galley is as wide
// as the longest line and egui caps the widget's allocation at its desired
// width, so without this the tail of a long line is clipped and unreachable.
func editorWidthPx(buf string, charPx, atLeast float32) float32 {
	longest := 0
	for _, line := range strings.Split(buf, "\n") {
		if n := len([]rune(line)); n > longest {
			longest = n
		}
	}
	w := float32(longest+editorTrailingCols) * charPx
	if w < atLeast {
		return atLeast
	}
	return w
}

// renderEditorGutter draws the gutter column for a model the caller already
// built (it also needs the model's width to size the editor beside it).
func (inst *PlayApp) renderEditorGutter(idSlot string, m gutterModel) {
	if !m.present {
		return
	}
	text, markSpans, restSpans := m.gutterText()
	job := c.CodeViewJob(text)
	weak := color.Hex(styletokens.NeutralTextDisabled.AsHex())
	for i := range markSpans {
		tone := weak
		switch m.marks[i] {
		case gutterMarkError:
			tone = styleErrorTone
		case gutterMarkActive:
			tone = styleWarningTone
		}
		job = job.Section(uint32(markSpans[i][0]), uint32(markSpans[i][1]), tone)
		job = job.Section(uint32(restSpans[i][0]), uint32(restSpans[i][1]), weak)
	}
	c.CodeView(inst.ids.PrepareStr(idSlot), job.Keep()).
		Selectable(false).
		Extend().
		Send()
}
