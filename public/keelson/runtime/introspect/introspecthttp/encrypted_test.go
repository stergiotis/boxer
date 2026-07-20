package introspecthttp

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestServer_EncryptedDatasetRefused checks that the HTTP table source
// refuses an ad-hoc dataset with a clear 4xx rather than attempting a
// snapshot: plaintext must never ride HTTP (ADR-0134 SD3).
func TestServer_EncryptedDatasetRefused(t *testing.T) {
	r := introspect.NewRegistry()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	require.NoError(t, r.Register(introspect.NewEncryptedEntry("adhoc_secret", schema, "id Int64", "/p/x.bxad", 1)))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	resp, err := http.Get(s.BaseURL() + "/table/adhoc_secret")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "ad-hoc dataset")
}
