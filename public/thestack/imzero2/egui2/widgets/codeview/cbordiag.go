package codeview

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// cborDiagColors is the per-category palette for CBOR diagnostic notation
// (ADR-0219 §SD6), drawn from the JSON and Go palettes so the three read as
// one family: keys like JSON keys, numbers and simple values like number
// literals, text like string literals, byte strings in the type-name teal,
// tag numbers in the keyword blue, comments in the comment green.
var cborDiagColors [diag.CategoryError + 1]color.Color

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray
	lightBlue := internRgb(156, 220, 254)    // map keys (matches JSON keys)
	orange := internRgb(206, 145, 120)       // text strings (matches string literals)
	teal := internRgb(78, 201, 176)          // byte strings (matches Go type names)
	number := internRgb(181, 206, 168)       // integers, floats (matches number literals)
	purple := internRgb(197, 134, 192)       // false / true / null / undefined / simple(n)
	blue := internRgb(86, 156, 214)          // tag numbers (matches keywords)
	dimGreen := internRgb(106, 153, 85)      // comments
	red := internRgb(244, 71, 71)            // the error span

	cborDiagColors[diag.CategoryFiller] = defaultColor
	cborDiagColors[diag.CategoryStructural] = defaultColor
	cborDiagColors[diag.CategoryKey] = lightBlue
	cborDiagColors[diag.CategoryNumber] = number
	cborDiagColors[diag.CategoryText] = orange
	cborDiagColors[diag.CategoryBytes] = teal
	cborDiagColors[diag.CategoryTag] = blue
	cborDiagColors[diag.CategorySimple] = purple
	cborDiagColors[diag.CategoryComment] = dimGreen
	cborDiagColors[diag.CategoryError] = red
}

// BuildCborDiagSpans serializes an already-rendered notation — the spans
// diag.Print returned — into a retained CodeViewJob. It is the tail half of
// BuildCborDiag for a caller that needs the spans' text as well (a copy
// button) and does not want the bytes walked twice.
func BuildCborDiagSpans(spans []diag.Span) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	src := diag.Text(spans)
	secs := make([]section, len(spans))
	for i, s := range spans {
		secs[i] = section{start: uint32(s.Start), stop: uint32(s.Stop), col: cborDiagColors[s.Category]}
	}
	return buildFromSections(src, secs)
}

// BuildCborDiag renders CBOR bytes as diagnostic notation and returns a
// retained CodeViewJob. Every call walks the bytes — use it for one-shot
// work, or when you already hold a cheaper key than the bytes. Malformed
// input renders its degradation (an error span, the remainder as hex); the
// error itself is not surfaced here, ask diag.Print for it.
func BuildCborDiag(b []byte, opts diag.Options) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	spans, _ := diag.Print(b, opts)
	return BuildCborDiagSpans(spans)
}

// PrepareCborDiag renders through the package memo: the same bytes under
// the same options prepared again return the same retained holder without
// a second walk (ADR-0125). An Annotate hook is not part of any key, so a
// call that carries one bypasses the memo and builds.
func PrepareCborDiag(b []byte, opts diag.Options) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	if opts.Annotate != nil {
		return BuildCborDiag(b, opts)
	}
	return memo.prepare(memoKey{lang: langCborDiag, src: cborDiagKey(b, opts)}, func() typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
		return BuildCborDiag(b, opts)
	})
}

// cborDiagKey is the memo key's source: every option that moves the
// rendering, then a separator no option contains, then the bytes.
func cborDiagKey(b []byte, opts diag.Options) string {
	var sb strings.Builder
	sb.Grow(len(b) + 48)
	sb.WriteString(opts.Indent)
	sb.WriteByte(0)
	sb.WriteString(strconv.Itoa(opts.Width))
	sb.WriteByte(0)
	sb.WriteString(strconv.Itoa(opts.BytesFold))
	sb.WriteByte(0)
	sb.WriteByte(flagByte(opts.Compact) | flagByte(opts.FloatPrecision)<<1 | flagByte(opts.TagComments)<<2 | flagByte(opts.Sequence)<<3)
	sb.WriteByte(0)
	sb.Write(b)
	return sb.String()
}

func flagByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
