package jpeg2000

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrUnsupported marks a valid codestream outside the decoded profile; the
// feature is named by [UnsupportedFeature].
var ErrUnsupported = errors.New("unsupported jpeg 2000 feature")

// ErrCorrupt marks a codestream that is not JPEG 2000 or contradicts
// itself: a marker out of place, a length past the end, a packet that
// asks for more bytes than remain.
var ErrCorrupt = errors.New("corrupt jpeg 2000 codestream")

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

// UnsupportedFeature names the feature behind an [ErrUnsupported].
func UnsupportedFeature(err error) (feature string, ok bool) {
	var u *unsupportedError
	if errors.As(err, &u) {
		feature = u.feature
		ok = true
	}
	return
}

func unsupported(feature string) (err error) {
	err = &unsupportedError{feature: feature, err: eb.Build().Str("feature", feature).Errorf("refused: %w", ErrUnsupported)}
	return
}

func corrupt(what string) (err error) {
	err = eb.Build().Str("failure", what).Errorf("codestream: %w", ErrCorrupt)
	return
}
