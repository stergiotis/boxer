package tabhost

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// browserhost.sum records the host for this tree's IDL. When this fails the
// bindings were regenerated without refreshing it: rebuild the browser host
// and run `go run ./public/thestack/cmd/imzero2tab hostdigest --write`.
func TestHostSumRecordsTheTreesIdl(t *testing.T) {
	hs, err := recordedHostSum()
	require.NoError(t, err)
	require.Equalf(t, c.IdlFingerprint, hs.idl,
		"browserhost.sum is for IDL %016x, the bindings are %016x: refresh it with `hostdigest --write`", hs.idl, c.IdlFingerprint)
}

func TestHostSumRoundTrips(t *testing.T) {
	hs := hostSum{sha256: sha256Hex("x"), idl: 0x00ff}
	got, err := parseHostSum(formatHostSum(hs))
	require.NoError(t, err)
	require.Equal(t, hs, got)
	_, err = parseHostSum("# only a comment\n")
	require.Error(t, err)
}

// A fetched host is used only when it matches its digest, is cached under it,
// and a second fetch is served from the cache without a request.
func TestFetchHostChecksAndCaches(t *testing.T) {
	body := []byte("\x00asm-not-really")
	h := sha256.Sum256(body)
	sha := hex.EncodeToString(h[:])
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/tabhost/" + sha + ".wasm":
			_, _ = w.Write(body)
		case "/tabhost/" + sha256Hex("other") + ".wasm":
			_, _ = w.Write([]byte("tampered"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cache, out := t.TempDir(), t.TempDir()

	require.NoError(t, fetchHost(srv.URL+"/tabhost/", sha, cache, filepath.Join(out, "a.wasm")))
	got, err := os.ReadFile(filepath.Join(out, "a.wasm"))
	require.NoError(t, err)
	require.Equal(t, body, got)
	require.Equal(t, 1, requests)

	require.NoError(t, fetchHost(srv.URL+"/tabhost/", sha, cache, filepath.Join(out, "b.wasm")))
	require.Equal(t, 1, requests, "the second fetch is the cache's")

	require.Error(t, fetchHost(srv.URL+"/tabhost/", sha256Hex("other"), cache, filepath.Join(out, "c.wasm")), "a body that does not match its digest")
	require.NoFileExists(t, filepath.Join(out, "c.wasm"))
	require.Error(t, fetchHost(srv.URL+"/tabhost/", sha256Hex("unpublished"), cache, filepath.Join(out, "d.wasm")), "not published")
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
