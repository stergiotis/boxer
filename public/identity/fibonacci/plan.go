// Package fibonacci is the tag-planning layer over the fibonacci-coded
// identifier scheme (ADR-0106 SD4): given how many ids a category must hold,
// or how wide a code it may spend, it answers which tag values qualify.
// The bit-level codec lives in fibonaccicode; composing and splitting ids
// lives in identifier.
package fibonacci

import (
	"iter"
	"math"
	"math/bits"

	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MinTagWidth is the width of the shortest fibonacci code, the bare comma
// "11" of tag value 1.
const MinTagWidth = 2

// MaxTagWidthUint32 is the full code width (including the trailing comma
// bit) of the largest uint32 tag values. The predecessor of this constant,
// the pre-ADR-0106 `Uint32TagValueTagWidth = 45`, understated it under every
// width convention and silently bounded the old stats tables.
const MaxTagWidthUint32 = 47

// WidthClass describes the tag values whose codes have exactly one full
// width. Widths partition the tag-value domain: class w covers
// [TagValueMinIncl, TagValueMaxIncl] and class w+1 starts right above.
type WidthClass struct {
	// Width is the full code width in bits, including the trailing comma bit.
	Width int
	// TagValueMinIncl and TagValueMaxIncl bound the class within the uint32
	// tag-value domain. Width 47 is clamped: its mathematical class extends
	// beyond MaxUint32, the excess is simply not addressable as a TagValue.
	TagValueMinIncl identifier.TagValue
	TagValueMaxIncl identifier.TagValue
	// TagValueCount is the number of addressable tag values in the class.
	TagValueCount uint64
	// MaxBodyIncl is the largest body every tag of this class can hold
	// (bodies are minted from 1; body 0 stays the invalid/NULL sentinel).
	MaxBodyIncl uint64
	// EncodingOverhead is Width divided by the bit count of a plain binary
	// encoding of the class's largest tag value — the price of the comma's
	// self-delimitation.
	EncodingOverhead float64
}

// WidthClassOf returns the class of one width; width must lie within
// [MinTagWidth, MaxTagWidthUint32].
func WidthClassOf(width int) (r WidthClass, err error) {
	if width < MinTagWidth || width > MaxTagWidthUint32 {
		err = eb.Build().Int("width", width).Int("minIncl", MinTagWidth).Int("maxIncl", MaxTagWidthUint32).Errorf("tag width out of the uint32 tag-value domain")
		return
	}
	// Tag value v encodes as the code of v-1, so the value-space bounds from
	// fibonaccicode shift up by one: class w is
	// [MaxRepresentableExclByWidth(w-1)+1, MaxRepresentableExclByWidth(w)].
	lo := fibonaccicode.MaxRepresentableExclByWidth(width-1) + 1
	hi := fibonaccicode.MaxRepresentableExclByWidth(width)
	if hi > math.MaxUint32 {
		hi = math.MaxUint32 // width 47 straddles the uint32 boundary
	}
	nBitsPlain := bits.Len64(hi)
	r = WidthClass{
		Width:            width,
		TagValueMinIncl:  identifier.TagValue(lo),
		TagValueMaxIncl:  identifier.TagValue(hi),
		TagValueCount:    hi - lo + 1,
		MaxBodyIncl:      1<<(64-width) - 1,
		EncodingOverhead: float64(width) / float64(nBitsPlain),
	}
	return
}

// WidthClasses returns every class from MinTagWidth through
// MaxTagWidthUint32, ascending. The slice is freshly allocated.
func WidthClasses() (r []WidthClass) {
	r = make([]WidthClass, 0, MaxTagWidthUint32-MinTagWidth+1)
	for w := MinTagWidth; w <= MaxTagWidthUint32; w++ {
		cl, _ := WidthClassOf(w) // width is in range by construction
		r = append(r, cl)
	}
	return
}

// IterateTagValuesWithGivenMinNumberOfLeadingZeros yields every tag value
// whose fibonacci code has full width tagWidth (including the trailing comma
// bit) and at least minNumLeadingZeros leading zero bits in the MSB-aligned
// code, together with the code's actual leading-zero count. Codes with many
// leading zeros are numerically small tags — they compress well as column
// values. A tagWidth outside [MinTagWidth, MaxTagWidthUint32] yields nothing.
func IterateTagValuesWithGivenMinNumberOfLeadingZeros(tagWidth uint8, minNumLeadingZeros uint8) iter.Seq2[identifier.TagValue, uint8] {
	return func(yield func(identifier.TagValue, uint8) bool) {
		cl, err := WidthClassOf(int(tagWidth))
		if err != nil {
			return
		}
		for i := uint64(cl.TagValueMinIncl); i <= uint64(cl.TagValueMaxIncl); i++ {
			r, _ := fibonaccicode.EncodeFibonacciCode(i - 1)
			u := bits.LeadingZeros64(r)
			if u >= int(minNumLeadingZeros) {
				if !yield(identifier.TagValue(i), uint8(u)) {
					return
				}
			}
		}
	}
}
