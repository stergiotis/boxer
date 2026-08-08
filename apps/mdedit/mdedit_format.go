package mdedit

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// The formatting bar wraps the selection in markup.
//
// It rides `insertAtCursor` (ADR-0063), whose contract makes wrapping possible
// at all: the splice REPLACES the current selection and leaves the caret just
// past what it inserted. So Go reads the selected text out of its own copy of
// the buffer, wraps it, and hands the whole thing over as the replacement —
// the text stays authoritative in the TextEdit throughout, and undo, focus and
// the caret are all still egui's (ADR-0178 §Decision 1).
//
// Both halves read the caret from the SAME frame: Go's copy arrives on the
// ReportCursor databinding and Rust splices at its persisted TextEditState,
// and neither is this frame's live value. They agree with each other, which is
// what matters — a click on a toolbar button does not move the caret.
//
// Deliberately absent: heading, list and blockquote. Those prefix a LINE, and
// `insertAtCursor` inserts at the caret, so they would only be correct with
// the caret already at the line start. Doing them properly means rewriting the
// buffer Go-side, which contradicts §Decision 1 and costs the widget's undo
// history — a worse trade than typing `## ` by hand. They wait for a
// line-aware seam.

// formatAction is one button on the bar.
type formatAction struct {
	// key is the id-stack label — stable across relabelling.
	key string
	// label is what the button shows.
	label string
	// open and close bracket the selection.
	open  string
	close string
	// placeholder stands in when there is no selection, so the button always
	// does something visible and leaves a word to overwrite.
	placeholder string
	// tip explains the action at the widget.
	tip string
}

// formatActions is the bar, in the order it renders.
var formatActions = []formatAction{
	{key: "bold", label: "B", open: "**", close: "**", placeholder: "bold", tip: "Bold — wraps the selection in ** **, or inserts a placeholder."},
	{key: "italic", label: "I", open: "*", close: "*", placeholder: "italic", tip: "Italic — wraps the selection in * *, or inserts a placeholder."},
	{key: "code", label: "`", open: "`", close: "`", placeholder: "code", tip: "Inline code — wraps the selection in backticks."},
	{key: "strike", label: "S", open: "~~", close: "~~", placeholder: "struck", tip: "Strikethrough — wraps the selection in ~~ ~~."},
	{key: "mark", label: "H", open: "==", close: "==", placeholder: "marked", tip: "Highlight — wraps the selection in == ==, the Obsidian highlight."},
	{key: "link", label: "link", open: "[", close: "](url)", placeholder: "label", tip: "Link — wraps the selection as a link label and leaves `url` to fill in."},
}

// formatSnippet computes the replacement text for one action against a
// selection given as CHAR offsets, the unit the caret report carries.
//
// A collapsed caret (start == end) has no selection, so the placeholder is
// wrapped instead: the button always produces something, and what it produces
// is a word the reader can immediately type over.
func formatSnippet(src string, act formatAction, selStart, selEnd int) (snippet string) {
	inner := act.placeholder
	if selStart != selEnd {
		lo, hi := selStart, selEnd
		if lo > hi {
			lo, hi = hi, lo
		}
		if sel := src[charToByte(src, lo):charToByte(src, hi)]; sel != "" {
			inner = sel
		}
	}
	return act.open + inner + act.close
}

// renderFormatBar draws the formatting buttons and returns the snippet a click
// produced, or "" when nothing was clicked.
//
// The snippet is returned rather than applied because it has to reach the
// TextEdit as a builder method on the widget itself, which renders later in
// the frame — the same shape play's snippet library uses.
func (inst *App) renderFormatBar() (snippet string) {
	selStart, selEnd := c.UnpackCursorRange(inst.cursor)
	for _, act := range formatActions {
		clicked := false
		for range c.HoverText(act.tip).KeepIter() {
			clicked = c.Button(inst.ids.PrepareStr("fmt-"+act.key), formatAtoms[act.key]).
				SendResp().HasPrimaryClicked()
		}
		if clicked && snippet == "" {
			snippet = formatSnippet(inst.src, act, selStart, selEnd)
		}
	}
	return
}

// formatAtoms holds each button's label, built once — identical retained bytes
// intern to one blob rather than being rebuilt every frame.
var formatAtoms = func() map[string]typed.RetainedFffiHolderTyped[c.AtomsS] {
	out := make(map[string]typed.RetainedFffiHolderTyped[c.AtomsS], len(formatActions))
	for _, act := range formatActions {
		out[act.key] = c.Atoms().Text(act.label).Keep()
	}
	return out
}()

// ---------------------------------------------------------------------------
// Readout
// ---------------------------------------------------------------------------

// wordsPerMinute is the reading pace the estimate divides by. 200 is the
// conventional figure for silent prose reading; it is a round number standing
// in for a distribution, not a measurement of anybody.
const wordsPerMinute = 200

// docStats is the readout under the bar.
type docStats struct {
	Words int
	Chars int
	// ReadMinutes is the reading estimate, rounded UP to a whole minute so a
	// short document reads "1 min" rather than "0 min".
	ReadMinutes int
}

// countStats derives the readout from the lex spans rather than from the raw
// buffer, which is the difference between counting a document and counting its
// markup. Delimiters, list bullets, table pipes, URLs, frontmatter and fenced
// code bodies are all excluded; inline code is kept, because a reader reads it.
//
// Chars counts the same prose, for the same reason.
func countStats(src string, spans []markdownhighlight.Span) (st docStats) {
	for _, s := range spans {
		if !proseCategories[s.Category] {
			continue
		}
		text := src[s.Start:s.Stop]
		st.Chars += len(text)
		st.Words += len(strings.Fields(text))
	}
	if st.Words > 0 {
		st.ReadMinutes = (st.Words + wordsPerMinute - 1) / wordsPerMinute
	}
	return
}

// proseCategories is what counts as something a person reads.
//
// Inline code is in: it sits mid-sentence and is read with the sentence. A
// fenced block's body is out, along with every delimiter, marker, URL, table
// rule and frontmatter field — none of which the reader reads as prose, and
// all of which would inflate the estimate for a technical document most of all.
var proseCategories = map[markdownhighlight.CategoryE]bool{
	markdownhighlight.CategoryPlain:           true,
	markdownhighlight.CategoryHeadingText:     true,
	markdownhighlight.CategoryStrongText:      true,
	markdownhighlight.CategoryEmphasisText:    true,
	markdownhighlight.CategoryStrikeText:      true,
	markdownhighlight.CategoryHighlightText:   true,
	markdownhighlight.CategoryInlineCodeText:  true,
	markdownhighlight.CategoryLinkLabel:       true,
	markdownhighlight.CategoryWikilinkTarget:  true,
	markdownhighlight.CategoryTableHeaderText: true,
	markdownhighlight.CategoryTableCellText:   true,
	markdownhighlight.CategoryCalloutType:     true,
	// A tag's body counts, its `#` does not — the same split the heading
	// marker and its text get. `#project/frontend` is a word the writer chose
	// and reads as one; the marker is punctuation that files it.
	markdownhighlight.CategoryTagText: true,
}
