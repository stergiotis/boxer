package logging

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Test_NewConsoleWriter_rendersStructuredErrors guards the error half of a
// console log line while the facts log bridge is installed. The bridge
// makes eh.MarshalError the process-wide ErrorMarshalFunc, so the console
// writer receives the structured shape; NewConsoleWriter excludes the error
// field from the inline fields, and before the fix every .Err(...) on
// every console line vanished.
func Test_NewConsoleWriter_rendersStructuredErrors(t *testing.T) {
	require.NoError(t, installConsoleCborMarshalers())
	orig := zerolog.ErrorMarshalFunc
	t.Cleanup(func() { zerolog.ErrorMarshalFunc = orig })
	zerolog.ErrorMarshalFunc = eh.MarshalError

	var buf bytes.Buffer
	cw, err := NewConsoleWriter(&buf, true)
	require.NoError(t, err)
	logger := zerolog.New(cw)
	logger.Warn().Err(eh.Errorf("store unavailable: %w", eh.New("operation not supported"))).Msg("service start failed")

	out := buf.String()
	require.True(t, strings.Contains(out, "Error: store unavailable"), out)
	require.True(t, strings.Contains(out, "cause: operation not supported"), out)
}
