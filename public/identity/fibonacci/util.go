package fibonacci

import (
	"math/bits"

	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// maxAdvisedTagWidth caps the code width this advisor hands out. Width 46 is
// the widest whose whole tag-value class still fits the uint32 TagValue
// domain (width 47 straddles it, ADR-0106 SD4). Wider tags exist and are
// valid; they are just never *advised*. Before this cap, a small
// maxExpectedIds silently truncated the returned TagValues through the
// uint32 conversion, and maxExpectedIds == 0 panicked via a negative shift.
const maxAdvisedTagWidth = 46

// SelectFittingTagValueRange returns the half-open tag-value interval
// [minTagValIncl, maxTagValExcl) whose fibonacci codes have exactly the
// largest full width (including the trailing comma bit) that still leaves
// room for maxExpectedIds distinct untagged ids in the 64-bit word. Small
// expectations (including 0) are clamped to width maxAdvisedTagWidth.
// maxExpectedIds >= 2^60 is rejected: it would need a tag narrower than the
// 2-bit minimum code plus headroom, so no fitting class exists.
func SelectFittingTagValueRange(maxExpectedIds uint64) (minTagValIncl identifier.TagValue, maxTagValExcl identifier.TagValue, err error) {
	nBitsTag := bits.LeadingZeros64(maxExpectedIds)
	if nBitsTag <= 3 {
		err = eb.Build().Uint64("maxExpectedIds", maxExpectedIds).Errorf("max expectedIds is too large")
		return
	}
	if nBitsTag > maxAdvisedTagWidth {
		nBitsTag = maxAdvisedTagWidth
	}
	maxTagValExcl = identifier.TagValue(fibonaccicode.MaxRepresentableExclByWidth(nBitsTag) + 1)
	minTagValIncl = identifier.TagValue(fibonaccicode.MaxRepresentableExclByWidth(nBitsTag-1) + 1)
	return
}
