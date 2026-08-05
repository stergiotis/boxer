// Package regexedit is the syntax-highlighted regex input, extracted
// from the regex_explorer demo app for reuse by the battery search
// boxes (ADR-0164 §SD4). It wraps [c.TextEdit] with the ADR-0130
// HighlightJob seam, fed by the codeview regex lexer family
// (ADR-0015).
//
// The lexer is only a painter: it colours bytes and never decides
// validity. Whatever consumes the buffer (Go's regexp, a search
// battery, ClickHouse) remains the authority on what compiles, and the
// two are allowed to visibly disagree — a pattern painted as a
// well-formed group that the consumer rejects — rather than fail
// silently (ADR-0015).
package regexedit

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ModeE selects how the buffer is lexed. The mode must match how the
// consumer splits the buffer, or the colouring will suggest boundaries
// the semantics do not have.
type ModeE uint8

const (
	// ModeSingle — the whole buffer is one regex (regex_explorer's
	// Pattern box).
	ModeSingle ModeE = iota
	// ModeList — one independent regex per line, group depth reset at
	// each newline (regex_explorer's Multi box, ADR-0015 §SD3).
	ModeList
	// ModeTokens — one independent regex per whitespace-separated
	// token, the battery search-box shape (ADR-0164 §SD2: space means
	// AND, every token its own pattern).
	ModeTokens
)

// buildJob dispatches to the mode's codeview builder. The Build*
// flavour (uncached) is correct for an editor path — content is new on
// every keystroke, the ADR-0125 memo would only churn — and [Edit]
// carries its own single-slot cache for the idle frames in between.
func buildJob(text string, mode ModeE) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS]) {
	switch mode {
	case ModeList:
		job = codeview.BuildRegexList(text)
	case ModeTokens:
		job = codeview.BuildRegexTokens(text)
	default:
		job = codeview.BuildRegex(text)
	}
	return
}

// Edit is one regex input's highlight-job cache. Zero value ready;
// hold one per editor box — an idle frame re-splices the retained job
// instead of re-lexing and re-retaining it, and two boxes sharing one
// Edit would evict each other every frame. Render-thread confined,
// like the text buffer it colours.
type Edit struct {
	src  string
	mode ModeE
	job  typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	ok   bool

	// buildFn substitutes [buildJob] in tests so cache behaviour is
	// observable as a rebuild count (the retained holder itself is not
	// comparable). Nil outside tests.
	buildFn func(text string, mode ModeE) typed.RetainedFffiHolderTyped[c.CodeViewJobS]
}

// Prepare returns the [c.TextEdit] builder for the buffer with the
// mode's highlight job attached; chain the remaining options
// (HintText, DesiredWidth, DesiredRows, …) and Send as usual.
//
// CodeEditor() is set here and is not cosmetic: the Rust highlight
// layouter resolves TextStyle::Monospace unconditionally, so without
// it the field's font would change the moment a character is typed and
// a job appears (ADR-0015 §SD6). An empty buffer gets no job at all —
// there is nothing to colour, and the hint text is not part of the
// buffer.
func (inst *Edit) Prepare(id c.WidgetIdCreatorI, text string, multiline bool, mode ModeE) (edit c.TextEditFluid) {
	edit = c.TextEdit(id, text, multiline).CodeEditor()
	job, ok := inst.jobFor(text, mode)
	if ok {
		edit = edit.HighlightJob(job)
	}
	return
}

// jobFor returns the retained job for (text, mode), rebuilding only
// when either changed since the previous frame.
func (inst *Edit) jobFor(text string, mode ModeE) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if text == "" {
		return
	}
	if !inst.ok || inst.src != text || inst.mode != mode {
		build := inst.buildFn
		if build == nil {
			build = buildJob
		}
		inst.job = build(text, mode)
		inst.src = text
		inst.mode = mode
		inst.ok = true
	}
	job, ok = inst.job, true
	return
}

// ErrorLabel paints msg as dark text on the IDS error fill — the
// visual "this is a compile error" affordance consistent with
// regexr.com and most IDE regex widgets, restated through the IDS
// semantic palette (ADR-0031 §SD2). Same fg-on-solid recipe as the
// badge widget's Solid variant: NeutralBgExtreme reads at high
// contrast against the L≈0.80 ErrorDefault fill.
//
// Lives here so every consumer of the input renders compile errors the
// same way; the caller supplies the message because only it knows its
// validity authority (ADR-0015).
func ErrorLabel(msg string) {
	errFg := color.Hex(styletokens.NeutralBgExtreme.AsHex()).Keep()
	errBg := color.Hex(styletokens.ErrorDefault.AsHex()).Keep()
	atoms := c.Atoms()
	for range atoms.StyledTextColored(errFg, errBg, msg) {
	}
	c.LabelAtoms(atoms.Keep()).Send()
}
