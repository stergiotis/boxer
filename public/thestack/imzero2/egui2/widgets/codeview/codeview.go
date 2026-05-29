//go:build llm_generated_opus47

package codeview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// section is the colour-resolved equivalent of a highlighter span — byte
// offsets are relative to whatever source string the highlighter was given.
type section struct {
	start uint32
	stop  uint32
	col   color.Color
}

// highlighterSpec captures everything per-language code paths feed into
// the shared build / buildLines helpers. Per-language files build one of
// these at init() and pass it to the public Build* / Prepare* wrappers.
type highlighterSpec struct {
	// highlight runs the language highlighter on the full source and
	// resolves each span to its palette colour. Byte offsets index into
	// the (possibly tab-expanded) `src` argument.
	highlight func(src string) []section
	// gutterColor is the foreground of the line-number prefix emitted by
	// buildLines.
	gutterColor color.Color
	// plainColor is used for the trailing-newline section in buildLines —
	// the newline byte still needs *some* section so egui's LayoutJob does
	// not collapse the line. Choose the palette's default-text colour.
	plainColor color.Color
	// tabReplace, if non-empty, substitutes every '\t' in the input before
	// the highlighter sees it. egui's LayoutJob renders '\t' with
	// inconsistent width for our font setup; expanding to spaces first
	// keeps section byte offsets aligned with the rendered text.
	tabReplace string
}

// internRgb pre-builds a retained Color32 holder for an opaque RGB triple
// and wraps it as a color.Color. Stash the originating u32 in the literal
// channel so both transports (retained / inline) stay zero-cost. Mirrors
// the FromRgb opcode semantics (sRGB non-premultiplied; the Rust side
// premultiplies at decode).
//
// Unexported: each per-language palette interns its own colours at init().
func internRgb(r, g, b uint8) color.Color {
	holder := c.Color().FromRgb(r, g, b).Keep()
	return color.FromRetainedHolder(holder.Untype(), uint32(r)<<24|uint32(g)<<16|uint32(b)<<8|0xff)
}

// build runs the highlighter once on `src` and emits a retained
// CodeViewJob with one Section per resolved span.
func build(spec highlighterSpec, src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	if spec.tabReplace != "" {
		src = strings.ReplaceAll(src, "\t", spec.tabReplace)
	}
	secs := spec.highlight(src)
	job := c.CodeViewJob(src)
	for _, sec := range secs {
		job = job.Section(sec.start, sec.stop, sec.col)
	}
	return job.Keep()
}

// computeLineStarts returns the byte offset of the first byte of each
// line in src. Line N (0-based) starts at out[N]; line count is len(out).
// A trailing newline does not produce a phantom empty line.
func computeLineStarts(src string) (out []int32) {
	if len(src) == 0 {
		return
	}
	out = make([]int32, 1, 64)
	out[0] = 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && i+1 < len(src) {
			out = append(out, int32(i+1))
		}
	}
	return
}

// buildLines highlights `src` once (so AST-style refinement applies
// across the whole file) and emits a retained CodeViewJob covering only
// the byte slice for 1-based lines [firstLine, lastLine] (inclusive),
// prefixed per line by a right-aligned line-number gutter coloured with
// spec.gutterColor. Spans that cross the window boundary are clipped at
// the edges.
//
// firstLine/lastLine are clamped to the source's line range; an out-of-
// bounds window returns an empty retained holder.
func buildLines(spec highlighterSpec, src string, firstLine, lastLine int32) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	if spec.tabReplace != "" {
		src = strings.ReplaceAll(src, "\t", spec.tabReplace)
	}

	starts := computeLineStarts(src)
	totalLines := int32(len(starts))
	if totalLines == 0 {
		return c.CodeViewJob("").Keep()
	}
	if firstLine < 1 {
		firstLine = 1
	}
	if firstLine > totalLines {
		return c.CodeViewJob("").Keep()
	}
	if lastLine > totalLines {
		lastLine = totalLines
	}
	if lastLine < firstLine {
		return c.CodeViewJob("").Keep()
	}

	byteStart := starts[firstLine-1]
	var byteEnd int32
	if lastLine >= totalLines {
		byteEnd = int32(len(src))
	} else {
		byteEnd = starts[lastLine]
	}
	if byteStart >= byteEnd {
		return c.CodeViewJob("").Keep()
	}

	slice := src[byteStart:byteEnd]
	fullSecs := spec.highlight(src)

	// Clip and shift every section that overlaps the window into slice-
	// relative offsets.
	winSecs := make([]section, 0, len(fullSecs))
	for _, s := range fullSecs {
		if s.stop <= uint32(byteStart) || s.start >= uint32(byteEnd) {
			continue
		}
		cs := s.start
		if cs < uint32(byteStart) {
			cs = uint32(byteStart)
		}
		ce := s.stop
		if ce > uint32(byteEnd) {
			ce = uint32(byteEnd)
		}
		winSecs = append(winSecs, section{
			start: cs - uint32(byteStart),
			stop:  ce - uint32(byteStart),
			col:   s.col,
		})
	}

	segs := sliceLineSegments(slice)
	if len(segs) == 0 {
		return c.CodeViewJob(slice).Keep()
	}

	topLine := firstLine + int32(len(segs)) - 1
	digits := len(strconv.FormatInt(int64(topLine), 10))
	prefixFmt := fmt.Sprintf(" %%%dd │ ", digits)

	var text strings.Builder
	text.Grow(len(slice) + len(segs)*(digits+8))

	out := make([]section, 0, len(winSecs)+len(segs)*2)

	for i, seg := range segs {
		// gutter prefix
		prefixStart := uint32(text.Len())
		text.WriteString(fmt.Sprintf(prefixFmt, firstLine+int32(i)))
		prefixEnd := uint32(text.Len())
		out = append(out, section{prefixStart, prefixEnd, spec.gutterColor})

		// line content (without trailing newline)
		contentStart := uint32(text.Len())
		text.WriteString(slice[seg.start:seg.end])

		for _, s := range winSecs {
			if s.stop <= uint32(seg.start) || s.start >= uint32(seg.end) {
				continue
			}
			cs := s.start
			if cs < uint32(seg.start) {
				cs = uint32(seg.start)
			}
			ce := s.stop
			if ce > uint32(seg.end) {
				ce = uint32(seg.end)
			}
			out = append(out, section{
				start: contentStart + (cs - uint32(seg.start)),
				stop:  contentStart + (ce - uint32(seg.start)),
				col:   s.col,
			})
		}

		// trailing newline (if any) needs its own section so egui's
		// LayoutJob does not drop the byte and collapse adjacent lines.
		if seg.end < int32(len(slice)) && slice[seg.end] == '\n' {
			nlOff := uint32(text.Len())
			text.WriteByte('\n')
			out = append(out, section{nlOff, nlOff + 1, spec.plainColor})
		}
	}

	job := c.CodeViewJob(text.String())
	for _, s := range out {
		job = job.Section(s.start, s.stop, s.col)
	}
	return job.Keep()
}

// lineSegment is a single content line — start/end byte offsets exclude
// the trailing newline, if any.
type lineSegment struct {
	start int32
	end   int32
}

// sliceLineSegments splits slice into per-line content ranges. The
// trailing '\n' is excluded from each segment; the caller infers a
// trailing newline from `seg.end < len(slice) && slice[seg.end] == '\n'`.
func sliceLineSegments(slice string) (out []lineSegment) {
	if len(slice) == 0 {
		return
	}
	out = make([]lineSegment, 0, 16)
	start := int32(0)
	for i := 0; i < len(slice); i++ {
		if slice[i] == '\n' {
			out = append(out, lineSegment{start, int32(i)})
			start = int32(i + 1)
		}
	}
	if start < int32(len(slice)) {
		out = append(out, lineSegment{start, int32(len(slice))})
	}
	return
}
