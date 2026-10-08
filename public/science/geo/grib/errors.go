package grib

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrUnsupported marks a message that is valid GRIB and uses a feature this
// reader does not implement. The refused feature is named by
// [UnsupportedFeature]. A refusal is the designed outcome for anything
// outside the subset (ADR-0292 §R1, §R3): a reader that guesses returns
// plausible wrong numbers, which is the failure the survey behind the ADR
// found in every library it looked at.
var ErrUnsupported = errors.New("unsupported grib feature")

// ErrMalformed marks bytes that are not the GRIB they claim to be: a missing
// indicator, a section running past its message, a message running past its
// file, an end marker that is not there.
var ErrMalformed = errors.New("malformed grib")

// ErrInconsistent marks a message whose sections contradict one another —
// a data section shorter than its bit count, a bitmap with a different point
// count from its grid, increments that do not reach the last grid point. The
// two numbers are fields of the error (ADR-0292 §R3).
var ErrInconsistent = errors.New("inconsistent grib message")

// unsupportedError carries the refused feature beside the structured error,
// so a caller can record which feature a producer's files need without
// decoding the error's payload.
type unsupportedError struct {
	feature string
	err     error
}

func (inst *unsupportedError) Error() (s string) {
	return inst.err.Error()
}

func (inst *unsupportedError) Unwrap() (err error) {
	return inst.err
}

// UnsupportedFeature names the GRIB feature behind an [ErrUnsupported]:
// "packing template 5.42", "grid template 3.30 points", "bitmap indicator
// 254", "grib1 data". The names are stable and are what the fixtures'
// expectations assert on.
func UnsupportedFeature(err error) (feature string, ok bool) {
	var u *unsupportedError
	if errors.As(err, &u) {
		feature = u.feature
		ok = true
	}
	return
}

func unsupportedE(feature string) (err error) {
	err = &unsupportedError{
		feature: feature,
		err:     eb.Build().Str("feature", feature).Errorf("refused: %w", ErrUnsupported),
	}
	return
}
