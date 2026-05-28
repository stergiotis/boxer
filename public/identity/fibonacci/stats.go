package fibonacci

import (
	"fmt"
	"iter"
	"math"
	"math/bits"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
)

const Uint32TagValueTagWidth = 45

type StatsHolder struct {
	ExampleLeft            [Uint32TagValueTagWidth + 1]string
	ExampleRight           [Uint32TagValueTagWidth + 1]string
	TagValueMinIncl        [Uint32TagValueTagWidth + 1]identifier.TagValue
	TagValueMaxIncl        [Uint32TagValueTagWidth + 1]identifier.TagValue
	TagNBitsRegular        [Uint32TagValueTagWidth + 1]uint64
	TagN                   [Uint32TagValueTagWidth + 1]uint64
	TagFibEncodingOverhead [Uint32TagValueTagWidth + 1]float64

	IdMaxIncl [Uint32TagValueTagWidth + 1]uint64
}

var Stats = generateStats()

func generateStats() (r StatsHolder) {
	//log.Logger = log.Output(zerolog.NewConsoleWriter()) // FIXME

	for tagWidth := 2; tagWidth <= Uint32TagValueTagWidth; tagWidth++ {
		untaggedIdWidth := 64 - tagWidth
		r.ExampleLeft[tagWidth] = fmt.Sprintf("0b%064b", uint64(0b11)<<untaggedIdWidth)
		{
			var minTagIncl, maxTagExcl uint64
			if tagWidth <= 2 {
				minTagIncl = 1
				maxTagExcl = 2
			} else {
				minTagIncl = fibonaccicode.MaxFibonacciCodeRepresentableByWidth(tagWidth - 1) // tagBits includes terminating 1 (which completes the 11 comma)
				maxTagExcl = fibonaccicode.MaxFibonacciCodeRepresentableByWidth(tagWidth)
			}
			if minTagIncl > math.MaxUint32 {
				//log.Panic().Msg("minTagIncl should be smaller than maxUint32")
			}
			if maxTagExcl-1 > math.MaxUint32 {
				//log.Panic().Int("tagBits", tagWidth).Str("maxTagInclStr", fmt.Sprintf("0b%064b", maxTagExcl-1)).Uint64("maxTagIncl", maxTagExcl-1).Msg("maxTagExcl-1 should be smaller than maxUint32")
			}
			if identifier.TagValue(minTagIncl).GetTag().GetTagWidth() != tagWidth {
				//log.Warn().Int("tagWidth", tagWidth).Uint64("minTagIncl", minTagIncl).Int("measured", TagValue(minTagIncl).GetTag().GetTagWidth()).Msg("minTagIncl does not have the correct tagWidth")
			}
			if identifier.TagValue(maxTagExcl-1).GetTag().GetTagWidth() != tagWidth {
				//log.Warn().Int("tagWidth", tagWidth).Uint64("maxTagIncl", maxTagExcl-1).Int("measured", TagValue(maxTagExcl-1).GetTag().GetTagWidth()).Msg("maxTagIncl does not have the correct tagWidth")
			}
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
func IterateTagValuesWithGivenMinNumberOfLeadingZeros(tagWidth uint8, minNumLeadingZeros uint8) iter.Seq2[identifier.TagValue, uint8] {
	return func(yield func(identifier.TagValue, uint8) bool) {
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
