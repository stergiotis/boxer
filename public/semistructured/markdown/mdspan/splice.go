package mdspan

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Splice replaces span s of src with with. changed is the lines the new text
// occupies in out; for a pure deletion it is the line the cut closed on.
func Splice(src []byte, s Span, with []byte) (out []byte, changed LineSpan, err error) {
	if s.Start < 0 || s.End < s.Start || s.End > len(src) {
		err = eb.Build().Int("start", s.Start).Int("end", s.End).Int("len", len(src)).Errorf("span outside the source")
		return
	}
	out = make([]byte, 0, len(src)-(s.End-s.Start)+len(with))
	out = append(out, src[:s.Start]...)
	out = append(out, with...)
	out = append(out, src[s.End:]...)
	changed = NewLineIndex(out).LineSpanOf(Span{Start: s.Start, End: s.Start + len(with)})
	return
}
