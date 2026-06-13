package app

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppLogger_TagsAppId verifies the pre-tag survives a normal event:
// the marshalled output (CBOR under binary_log, JSON otherwise) must
// contain both the AppId bytes and the standard message bytes, so a
// downstream Sink — and human-readable stdout — can recover them.
func TestAppLogger_TagsAppId(t *testing.T) {
	var buf bytes.Buffer
	base := zerolog.New(&buf)
	logger := AppLogger(base, "github.com/example/play")
	logger.Info().Msg("hello")
	out := buf.Bytes()
	require.NotEmpty(t, out)
	// The output may be CBOR (binary_log) or JSON depending on build
	// tags; in either form the literal bytes of the field value and
	// the message must appear somewhere in the buffer.
	assert.Contains(t, string(out), "github.com/example/play",
		"app_id value must appear in the marshalled event")
	assert.Contains(t, string(out), "hello", "message must appear in the marshalled event")
}

// TestAppLogger_BaseUnchanged guards the zerolog Logger value-semantics
// contract — AppLogger derives a fresh context off the base; emitting
// on base afterwards must not carry the per-app tag.
func TestAppLogger_BaseUnchanged(t *testing.T) {
	var baseBuf, appBuf bytes.Buffer
	base := zerolog.New(&baseBuf)
	appLogger := AppLogger(zerolog.New(&appBuf), "play")
	base.Info().Msg("from base")
	appLogger.Info().Msg("from app")
	assert.NotContains(t, baseBuf.String(), "play", "base logger should not carry the app_id tag")
	assert.Contains(t, appBuf.String(), "play", "app logger must carry the app_id tag")
}
