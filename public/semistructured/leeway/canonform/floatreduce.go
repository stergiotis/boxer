package canonform

import (
	"math"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
)

// writeFloatReduced writes a floating-point value in the form's canonical
// shape: the dCBOR §2.5 numeric reduction first — a value numerically equal
// to an integer in [-2^63, 2^64-1] becomes that integer, so 3.0 ≡ 3 and
// -0.0 ≡ 0 — then, for everything that survives it, the shortest float
// encoding that preserves the value (float16/32/64), with all NaNs as
// 0xf97e00 and ±Inf as float16. A float32 arrives widened to float64
// exactly, so f32 x and f64(x) are byte-identical.
//
// The reduction is a rule of the quotient (ADR-0201 SD3), not of the wire, so
// it lives here rather than in the shared writer: the lossless form over the
// same writer (ADR-0210) must keep 3.0 a float.
func writeFloatReduced(cw *runtime.CborWriter, f float64) {
	if cw.Err() != nil {
		return
	}
	if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) {
		switch {
		case f >= 0 && f < 18446744073709551616.0: // < 2^64 → fits uint64 exactly
			cw.Head(runtime.MajorTypeUint, uint64(f))
			return
		case f < 0 && f >= -9223372036854775808.0: // ≥ -2^63 → fits int64 exactly
			cw.WriteInt(int64(f))
			return
		}
	}
	cw.WriteFloatShortest(f)
}
