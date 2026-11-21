package good

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// ErrBase is a sentinel; ErrWrapped wraps it and one from another package
// without recording a stack.
var ErrBase = errors.New("base")

var ErrWrapped = fmt.Errorf("wrapped: %w (%w)", ErrBase, fs.ErrNotExist)

func ok() (err error) {
	err = eh.Errorf("fine: %w", errors.New("x"))
	return
}
