package regexedit

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The cache is a single slot keyed by (text, mode): an idle frame must
// reuse the retained job, and a mode flip on the same text must rebuild
// (the three lexings of one buffer are distinct results, see the
// codeview memo-key rationale). The retained holder is not comparable,
// so the assertion counts rebuilds through the test seam.
func TestJobForCacheKeying(t *testing.T) {
	builds := 0
	e := Edit{buildFn: func(text string, mode ModeE) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS]) {
		builds++
		return
	}}
	if _, ok := e.jobFor("", ModeSingle); ok || builds != 0 {
		t.Fatalf("empty buffer must yield no job and no build (builds=%d) — hint text is not part of the buffer", builds)
	}
	if _, ok := e.jobFor(`a(b`, ModeSingle); !ok || builds != 1 {
		t.Fatalf("first non-empty buffer must build exactly once, builds=%d ok=%v", builds, ok)
	}
	e.jobFor(`a(b`, ModeSingle)
	if builds != 1 {
		t.Errorf("idle frame (same text, same mode) must not rebuild, builds=%d", builds)
	}
	e.jobFor(`a(b`, ModeTokens)
	if builds != 2 {
		t.Errorf("mode flip on the same text must rebuild, builds=%d", builds)
	}
	e.jobFor(`a(b c`, ModeTokens)
	if builds != 3 {
		t.Errorf("text change must rebuild, builds=%d", builds)
	}
}

// The real builders accept every mode without panicking on editor-ish
// input — half-typed groups, trailing backslashes, separator runs.
func TestBuildJobModesSmoke(t *testing.T) {
	for _, mode := range []ModeE{ModeSingle, ModeList, ModeTokens} {
		for _, src := range []string{`a(b`, `\`, "x\ny", "tok1  tok2("} {
			buildJob(src, mode)
		}
	}
}
