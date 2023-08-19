package logging

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
)

func SetupZeroLog() {
	zerolog.ErrorMarshalFunc = eh.MarshalError
}
