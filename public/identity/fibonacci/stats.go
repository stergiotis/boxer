package fibonacci

import (
	"fmt"
	"iter"
	"math"
	"math/bits"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// Uint32TagValueTagWidth bounds the width axis of the Stats tables below.
// NOTE: it does NOT cover the full uint32 tag-value domain — the largest
// uint32 tag values need codes up to 47 bits wide (ADR-0106 SD4); widths 46
// and 47 are simply absent from Stats. The table is scheduled to be replaced
// by the tag-planning API of ADR-0106 slice 5.
const Uint32TagValueTagWidth = 45

type StatsHolder struct {
	ExampleLeft            [Uint32TagValueTagWidth + 1]string
	TagValueMinIncl        [Uint32TagValueTagWidth + 1]identifier.TagValue
	TagValueMaxIncl        [Uint32TagValueTagWidth + 1]identifier.TagValue
	TagNBitsRegular        [Uint32TagValueTagWidth + 1]uint64
	TagN                   [Uint32TagValueTagWidth + 1]uint64
	TagFibEncodingOverhead [Uint32TagValueTagWidth + 1]float64

	IdMaxIncl [Uint32TagValueTagWidth + 1]uint64
}

var Stats = generateStats()

func generateStats() (r StatsHolder) {
	for tagWidth := 2; tagWidth <= Uint32TagValueTagWidth; tagWidth++ {
		untaggedIdWidth := 64 - tagWidth
		r.ExampleLeft[tagWidth] = fmt.Sprintf("0b%064b", uint64(0b11)<<untaggedIdWidth)
		{
			// Tag value v is encoded as the fibonacci code of v-1 (v=0 is
			// reserved), so the tag-value class of full code width w is the
			// value class [MaxRepresentableExclByWidth(w-1),
			// MaxRepresentableExclByWidth(w)) shifted up by one. Before
			// ADR-0106 SD9 both bounds were missing the +1 bias, which made
			// IterateTagValuesWithGivenMinNumberOfLeadingZeros skip the first
			// class member with a warning and drop the last one silently.
			minTagIncl := fibonaccicode.MaxRepresentableExclByWidth(tagWidth-1) + 1
			maxTagExcl := fibonaccicode.MaxRepresentableExclByWidth(tagWidth) + 1
			nBitsRegular := math.Ceil(math.Log2(float64(maxTagExcl-1))) + 1
			r.TagValueMinIncl[tagWidth] = identifier.TagValue(minTagIncl)
			r.TagValueMaxIncl[tagWidth] = identifier.TagValue(maxTagExcl - 1)
			r.TagNBitsRegular[tagWidth] = uint64(nBitsRegular)
			r.TagN[tagWidth] = maxTagExcl - minTagIncl
			if nBitsRegular > 0 {
				r.TagFibEncodingOverhead[tagWidth] = float64(tagWidth) / nBitsRegular
			}
		}

		{
			maximumUntaggedIdExcl := uint64(1) << untaggedIdWidth
			r.IdMaxIncl[tagWidth] = maximumUntaggedIdExcl - 1
		}
	}
	return
}

// IterateTagValuesWithGivenMinNumberOfLeadingZeros yields every tag value
// whose fibonacci code has full width tagWidth (including the trailing comma
// bit) and at least minNumLeadingZeros leading zero bits in the MSB-aligned
// code, together with the code's actual leading-zero count. A tagWidth
// outside [2, Uint32TagValueTagWidth] yields nothing (it previously panicked
// via index-out-of-range or unsigned underflow).
func IterateTagValuesWithGivenMinNumberOfLeadingZeros(tagWidth uint8, minNumLeadingZeros uint8) iter.Seq2[identifier.TagValue, uint8] {
	return func(yield func(identifier.TagValue, uint8) bool) {
		if tagWidth < 2 || tagWidth > Uint32TagValueTagWidth {
			return
		}
		l := uint64(Stats.TagValueMinIncl[tagWidth])
		h := uint64(Stats.TagValueMaxIncl[tagWidth])
		for i := l; i <= h; i++ {
			r, nBits := fibonaccicode.EncodeFibonacciCode(i - 1)
			if nBits != int(tagWidth) {
				log.Warn().Int("nBits", nBits).Uint8("tagWidth", tagWidth).Uint64("number", i).Msg("encoding does not match number of expected bits")
				continue
			}
			u := bits.LeadingZeros64(r)
			if u >= int(minNumLeadingZeros) {
				if !yield(identifier.TagValue(i), uint8(u)) {
					return
				}
			}
		}
	}
}
