package fibonacci

import (
	"math/bits"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
)

func SelectFittingTagValueRange(maxExpectedIds uint64) (minTagValIncl identifier.TagValue, maxTagValExcl identifier.TagValue, err error) {
	nBitsTag := bits.LeadingZeros64(maxExpectedIds)
	if nBitsTag <= 3 {
		err = eb.Build().Uint64("maxExpectedIds", maxExpectedIds).Errorf("max expectedIds is too large")
		return
	}
	maxTagValExcl = identifier.TagValue(fibonaccicode.MaxFibonacciCodeRepresentableByWidth(nBitsTag) + 1)
	minTagValIncl = identifier.TagValue(fibonaccicode.MaxFibonacciCodeRepresentableByWidth(nBitsTag-1) + 1)
	return
}
