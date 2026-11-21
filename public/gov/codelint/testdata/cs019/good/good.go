package good

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrOne = errors.New("one")

// A function literal runs when it is called, not at init.
var wrap = func(err error) error { return eh.Errorf("wrapped: %w", err) }

func use() (err error) {
	err = eh.Errorf("at the call site: %w", ErrOne)
	return
}

var _ = wrap
var _ = use
