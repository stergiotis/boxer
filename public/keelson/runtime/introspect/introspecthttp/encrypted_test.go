package introspecthttp

import (
	"bytes"
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

// sealedStub is a sealed provider whose Open answers from memory (or
// fails), standing in for a dataset record.
type sealedStub struct {
	name      string
	plaintext []byte
	revision  uint64
	openErr   error
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func (s *sealedStub) Name() string                         { return s.name }
func (s *sealedStub) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (s *sealedStub) Schema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
}
func (s *sealedStub) Structure() string { return "id Int64" }
func (s *sealedStub) Revision() uint64  { return s.revision }
func (s *sealedStub) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (s *sealedStub) Open() (io.ReadSeekCloser, uint64, error) {
	if s.openErr != nil {
		return nil, 0, s.openErr
	}
	return nopCloser{bytes.NewReader(s.plaintext)}, s.revision, nil
}

// TestServer_SealedDatasetUnavailable checks that a sealed dataset whose
// record cannot open — retired, closed — answers 503 rather than a
// snapshot: the table source never hands out ciphertext or a stale copy.
func TestServer_SealedDatasetUnavailable(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_secret", revision: 1, openErr: assert.AnError}))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	resp, err := http.Get(s.BaseURL() + "/table/adhoc_secret")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "sealed dataset")
}

// TestServer_SealedDatasetOpens checks that /table serves a sealed
// dataset by opening its record (ADR-0240 §SD3), with the revision the
// reader belongs to in the ETag and ranges answered.
func TestServer_SealedDatasetOpens(t *testing.T) {
	r := introspect.NewRegistry()
	plaintext := []byte("PLAINTEXT-ARROW-STREAM-BYTES")
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_x", plaintext: plaintext, revision: 7}))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	resp, err := http.Get(s.BaseURL() + "/table/adhoc_x")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, `"adhoc_x-r7"`, resp.Header.Get("ETag"))
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, plaintext, body)

	req, _ := http.NewRequest(http.MethodGet, s.BaseURL()+"/table/adhoc_x", nil)
	req.Header.Set("Range", "bytes=10-")
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusPartialContent, resp2.StatusCode)
	tail, _ := io.ReadAll(resp2.Body)
	assert.Equal(t, plaintext[10:], tail)
}
