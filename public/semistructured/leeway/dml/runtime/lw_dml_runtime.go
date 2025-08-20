package runtime

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrInvalidStateTransition = eh.Errorf("invalid state transition")
