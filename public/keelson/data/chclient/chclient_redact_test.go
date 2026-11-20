package chclient

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// A CLICKHOUSE_URL may carry userinfo. A failed request's error, rendered
// with its structured fields or logged, shows the user but not the
// password.
func TestErrorsDoNotCarryTheURLPassword(t *testing.T) {
	c := New(Config{URL: "http://alice:s3cret@127.0.0.1:1/"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range []error{c.Ping(ctx), c.Exec(ctx, "SELECT 1")} {
		require.Error(t, err)
		rendered := eh.FormatErrorPlainS(err)
		assert.NotContains(t, rendered, "s3cret")
		assert.NotContains(t, err.Error(), "s3cret")
		var buf bytes.Buffer
		logger := zerolog.New(&buf)
		logger.Error().Err(err).Msg("x")
		assert.NotContains(t, buf.String(), "s3cret")
	}
	assert.Contains(t, eh.FormatErrorPlainS(c.Ping(ctx)), "alice:xxxxx@", "the structured url field is rendered, redacted")
}
