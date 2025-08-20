package unittest

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stretchr/testify/require"
)

func NoError(t *testing.T, err error) {
	if err != nil {
		_ = logging.SetupConsoleLogger(os.Stderr)
		zerolog.ErrorMarshalFunc = eh.MarshalError
		log.Error().Err(err).Msg("expecting no error")
		require.Fail(t, err.Error())
	}
}
