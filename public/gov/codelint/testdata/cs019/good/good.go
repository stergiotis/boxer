package good

import (
	"errors"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrOne = errors.New("one")

// A sentinel that wraps another is fmt.Errorf with %w: no stack, and
// errors.Is(ErrTwo, ErrOne) holds (CS001 accepts this form).
var ErrTwo = fmt.Errorf("two: %w", ErrOne)

// A function literal runs when it is called, not at init.
var wrap = func(err error) error { return eh.Errorf("wrapped: %w", err) }

func use() (err error) {
	err = eh.Errorf("at the call site: %w", ErrOne)
	return
}

var _ = wrap
var _ = use
