package logging

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Test_NewConsoleWriter_expandsBoolFields guards the hmi.sh log
// regression: SetupConsoleLogger installs a process-global
// InterfaceMarshalFunc (embeddAsCbor) that CBOR-embeds every value
// zerolog's console writer routes through the marshaler — which, per
// zerolog console.go writeFields, is every field that is neither a
// string nor a json.Number (notably bool). Only the FormatFieldValue
// that NewConsoleWriter installs expands those embedded
// `data:application/cbor;base64,…` blobs back to a scalar.
//
// The facts-log-bridge operator passthrough (thestack/cmd/imzero2) once
// hand-rolled a bare ConsoleWriter without that formatter, so the
// chlocalbroker audit booleans (cacheable / streaming / cache_hit)
// printed as raw CBOR data URIs once the bridge replaced log.Logger.
func Test_NewConsoleWriter_expandsBoolFields(t *testing.T) {
	var sink bytes.Buffer
	// Side effect under test: install the global InterfaceMarshalFunc that
	// turns bool fields into CBOR blobs. This is the precondition the
	// bridge passthrough runs under in production.
	require.NoError(t, SetupConsoleLogger(&sink))

	// A *separately constructed* console writer, exactly as the bridge
	// passthrough builds one — it must still expand the blobs.
	var buf bytes.Buffer
	cw, err := NewConsoleWriter(&buf, true /* noColor: keep assertions plain */)
	require.NoError(t, err)

	logger := zerolog.New(cw)
	logger.Info().
		Bool("cache_hit", false).
		Bool("cacheable", true).
		Int("bytes_out", 520).
		Str("format", "ArrowStream").
		Msg("chlocalbroker: exec")

	out := buf.String()
	require.NotContainsf(t, out, "data:application/cbor;base64",
		"bool field leaked as a raw CBOR data URI: %q", out)
	require.Containsf(t, out, "cache_hit=false", "got: %q", out)
	require.Containsf(t, out, "cacheable=true", "got: %q", out)
	require.Containsf(t, out, "bytes_out=520", "got: %q", out)
	require.Containsf(t, out, "format=ArrowStream", "got: %q", out)

	// Negative control: the pre-fix bare ConsoleWriter still leaks the
	// blob under the same global marshaler. This proves the assertions
	// above are sensitive to FormatFieldValue and not vacuously passing.
	var bare bytes.Buffer
	bareLogger := zerolog.New(zerolog.ConsoleWriter{Out: &bare, NoColor: true})
	bareLogger.Info().Bool("cache_hit", false).Msg("x")
	require.Containsf(t, bare.String(), "data:application/cbor;base64",
		"expected the bare writer to reproduce the bug; got: %q", bare.String())
}
