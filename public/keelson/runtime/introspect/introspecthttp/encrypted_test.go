package introspecthttp

import (
	"bytes"
	"context"
	"io"
	"net"
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
func (s *sealedStub) Snapshot(context.Context, introspect.Projection) (arrow.RecordBatch, error) {
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
	require.NoError(t, s.Start(t.Context()))
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
	require.NoError(t, s.Start(t.Context()))
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

// A sealed dataset's handle is what /table opens it by, so the listing
// every local process can read leaves it out.
func TestServer_TablesListingOmitsSealed(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_secret", revision: 1}))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	resp, err := http.Get(s.BaseURL() + "/tables")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "adhoc_secret")
}

// A request naming another host — a page's own name re-pointed at
// 127.0.0.1 — or another port is refused before any route runs; the bound
// address and localhost on the bound port are served.
func TestServer_RefusesAForeignHostHeader(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_secret", revision: 1, plaintext: []byte("x")}))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	_, port, err := net.SplitHostPort(s.Addr())
	require.NoError(t, err)

	status := func(host string) int {
		req, reqErr := http.NewRequest(http.MethodGet, s.BaseURL()+"/table/adhoc_secret", nil)
		require.NoError(t, reqErr)
		req.Host = host
		resp, doErr := http.DefaultClient.Do(req)
		require.NoError(t, doErr)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	assert.Equal(t, http.StatusMisdirectedRequest, status("attacker.example:"+port))
	assert.Equal(t, http.StatusMisdirectedRequest, status("attacker.example"))
	assert.Equal(t, http.StatusMisdirectedRequest, status("127.0.0.1:1"))
	assert.Equal(t, http.StatusOK, status("localhost:"+port))
	assert.Equal(t, http.StatusOK, status(s.Addr()))
}
