package good

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
)

func ok() (err error) {
	err = eh.Errorf("fine: %w", errors.New("x"))
	return
}
