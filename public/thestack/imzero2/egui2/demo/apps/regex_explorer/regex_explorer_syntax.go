package regex_explorer

// Pattern syntax highlighting (ADR-0015).
//
// The two pattern editors get their bytes coloured through the ADR-0130
// TextEdit.HighlightJob seam: Go lexes the buffer into byte-range
// sections, the Rust layouter reconciles those (one frame stale) against
// the live buffer and applies them.
//
// The lexer is *only* a painter. Go's regexp remains this app's validity
// authority (ADR-0054): [App.getCompiledRegexp] and the red compile-error
// label decide what is true. So the two can visibly disagree — a pattern
// painted as a well-formed group that the label calls a compile error —
// and that is the intended failure mode, rather than a silent one.
//
// Both caches are render-thread-confined, like [App.pattern] itself.

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// highlightCache memoises one editor's retained CodeViewJob against the
// buffer it was built from, so an idle frame re-splices the retained
// holder instead of re-lexing. One per editor: the two hold different
// text and would otherwise evict each other every frame.
type highlightCache struct {
	src string
	job typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	ok  bool
}

// jobFor returns the retained job for src, rebuilding when the buffer
// changed. build is the codeview entry point for this editor's flavour.
// An empty buffer gets no job at all — there is nothing to colour, and
// the hint text is not part of the buffer.
func (cache *highlightCache) jobFor(src string, build func(string) typed.RetainedFffiHolderTyped[c.CodeViewJobS]) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if src == "" {
		return
	}
	if !cache.ok || cache.src != src {
		cache.job = build(src)
		cache.src = src
		cache.ok = true
	}
	job, ok = cache.job, true
	return
}

// patternHighlightJob is the single-pattern editor's job: the buffer is
// one regex, so group depth runs across the whole of it.
func (inst *App) patternHighlightJob(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	return inst.patternHl.jobFor(src, codeview.BuildRegex)
}

// patternListHighlightJob is the multi-pattern editor's job: one
// independent regex per line, with group depth reset at each newline
// (ADR-0015 §SD3). Lexing it as a single pattern would let an unclosed
// `(` on line 1 mis-colour line 7 — and this editor is precisely where
// a half-typed line sits above finished ones.
func (inst *App) patternListHighlightJob(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	return inst.patternListHl.jobFor(src, codeview.BuildRegexList)
}
