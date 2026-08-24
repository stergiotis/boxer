package portolan

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests of the loader's own seams — what Leaflet leaves to the browser: the
// decode's alpha convention and the negative cache's bookkeeping.

func TestDecodeTile_StraightAlpha(t *testing.T) {
	// A half-transparent red pixel must come out as 0xff000080, not the
	// premultiplied 0x80000080: the host's texture upload premultiplies
	// itself (Color32::from_rgba_unmultiplied), so a premultiplied decode
	// would darken translucent tiles twice.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0x80})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	px, w, h, err := decodeTile(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, 2, w)
	assert.Equal(t, 1, h)
	assert.Equal(t, uint32(0xff000080), px[0], "straight alpha: red untouched by the half alpha")
	assert.Equal(t, uint32(0x102030ff), px[1])
}

type countingTransport struct {
	calls  atomic.Int32
	status int
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func drainOne(t *testing.T, l *TileLoader) TileArrival {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a := l.Drain(); len(a) > 0 {
			require.Len(t, a, 1)
			return a[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no arrival")
	return TileArrival{}
}

func TestTileLoader_NegativeCacheHitIsNotAFailure(t *testing.T) {
	// A re-request inside the TTL is answered from the negative cache: it
	// is delivered as failed, but it neither fetches, nor extends the
	// entry, nor counts in Health — so the tile is retried once the TTL
	// from the real failure has passed, however often it was asked for
	// meanwhile (ADR-0204 §SD4 Q3).
	tr := &countingTransport{status: http.StatusInternalServerError}
	l := NewTileLoader(LoaderOptions{Workers: 1, NegativeTTL: 80 * time.Millisecond, Transport: tr})
	defer l.Close()
	c := TileCoords{1, 2, 3}

	l.Request(c, c, "http://tiles.example/1")
	a := drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Equal(t, "http://tiles.example/1", a.URL)
	assert.Equal(t, int32(1), tr.calls.Load())
	assert.Equal(t, 1, l.Health().ConsecutiveFailures)

	// Inside the TTL: served from the negative cache.
	l.Request(c, c, "http://tiles.example/1")
	a = drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Contains(t, a.Err.Error(), "failed recently")
	assert.Equal(t, int32(1), tr.calls.Load(), "no fetch")
	assert.Equal(t, 1, l.Health().ConsecutiveFailures, "a cache hit is not a new failure")

	// After the TTL from the real failure: fetched again.
	time.Sleep(120 * time.Millisecond)
	l.Request(c, c, "http://tiles.example/1")
	a = drainOne(t, l)
	assert.True(t, a.Failed)
	assert.Equal(t, int32(2), tr.calls.Load(), "retried after the TTL")
	assert.Equal(t, 2, l.Health().ConsecutiveFailures)
}

// The tile client's TLS seam. Nothing here dials: tlsClientConfig is pure
// apart from reading the CA file, so the three states are asserted on the
// *tls.Config the transport would have been handed.

func TestTLSClientConfig_UnsetLeavesTheTransportAlone(t *testing.T) {
	// No knob set: nil, so newTransport keeps the cloned DefaultTransport's
	// own TLSClientConfig — the system trust store and Go's TLS 1.2 floor.
	// A non-nil config here would mean an unconfigured deployment fetching
	// from tile.openstreetmap.org under settings this package chose.
	assert.Nil(t, LoaderOptions{}.tlsClientConfig())

	// Clone() materialises a TLSClientConfig of its own to hold NextProtos,
	// so the assertion is not that it is absent but that it carries none of
	// our weakening: MinVersion 0 is Go's implicit TLS 1.2 client floor
	// (Config.supportedVersions drops everything below 1.2 when MinVersion
	// is unset), and the nil pool means the system trust store.
	l := &TileLoader{opts: LoaderOptions{}.withDefaults()}
	tr, ok := l.newTransport().(*http.Transport)
	require.True(t, ok)
	if cfg := tr.TLSClientConfig; cfg != nil {
		assert.False(t, cfg.InsecureSkipVerify, "no knob set: verification must be on")
		assert.Zero(t, cfg.MinVersion, "no knob set: leave Go's implicit TLS 1.2 floor alone")
		assert.Nil(t, cfg.RootCAs, "no knob set: the system trust store")
		assert.Nil(t, cfg.CipherSuites, "no knob set: Go's default cipher list")
	}
}

func TestTLSClientConfig_CAFileKeepsVerificationAndTheFloor(t *testing.T) {
	// CAFile is the smaller statement: it moves the trust anchor and must
	// change nothing else. In particular it must NOT pick up the TLS 1.0
	// floor or the legacy suites that InsecureTLS carries — an internal CA
	// is not a reason to weaken the protocol.
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(path, selfSignedCAPEM(t), 0o600))

	cfg := LoaderOptions{CAFile: path}.tlsClientConfig()
	require.NotNil(t, cfg)
	assert.False(t, cfg.InsecureSkipVerify, "the CA file must not disable verification")
	assert.NotNil(t, cfg.RootCAs, "the CA file was not loaded")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Nil(t, cfg.CipherSuites, "the CA file must not widen the cipher list")
}

func TestTLSClientConfig_UnreadableCAFileFallsBackToSystemRoots(t *testing.T) {
	// A bad path or a file with no certificate in it is logged and ignored,
	// leaving the system roots — it must not silently become an empty pool,
	// which would reject every certificate, nor disable verification.
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "junk.pem")
	require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0o600))

	for name, path := range map[string]string{
		"missing":  filepath.Join(dir, "absent.pem"),
		"not-a-ca": notPEM,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := LoaderOptions{CAFile: path}.tlsClientConfig()
			require.NotNil(t, cfg)
			assert.Nil(t, cfg.RootCAs, "want the system roots, not an empty pool")
			assert.False(t, cfg.InsecureSkipVerify)
		})
	}
}

func TestTLSClientConfig_InsecureAlsoLowersVersionAndCiphers(t *testing.T) {
	// InsecureTLS stops authenticating the peer, so the protocol floor and
	// the cipher list are no longer defending anything and come down with
	// it. Without both, the handshake against a server old enough to need
	// this knob fails on version or cipher negotiation instead of on the
	// certificate, and the knob reads as broken.
	cfg := LoaderOptions{InsecureTLS: true}.tlsClientConfig()
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS10), cfg.MinVersion)

	// The static-RSA suites are the point: an old server offers these, and
	// Go leaves every one of them out of its default list, so lowering
	// MinVersion alone would still not complete a handshake.
	assert.Contains(t, cfg.CipherSuites, tls.TLS_RSA_WITH_AES_128_CBC_SHA)
	assert.Contains(t, cfg.CipherSuites, tls.TLS_RSA_WITH_AES_256_CBC_SHA)
	assert.Contains(t, cfg.CipherSuites, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA)
	// ...and the modern ones are still there, so a current server is not
	// pushed onto a legacy suite by the knob.
	assert.Contains(t, cfg.CipherSuites, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
}

func TestLegacyCipherSuites_CoversEverythingCryptoTLSCanSpeak(t *testing.T) {
	// The list is derived, not hand-written, so it cannot drift behind the
	// standard library. crypto/tls filters it against what it supports and
	// applies its own preference order, so listing a suite only permits it.
	ids := legacyCipherSuites()
	for _, cs := range tls.CipherSuites() {
		assert.Contains(t, ids, cs.ID, "missing default suite %s", cs.Name)
	}
	for _, cs := range tls.InsecureCipherSuites() {
		assert.Contains(t, ids, cs.ID, "missing insecure suite %s", cs.Name)
	}
}

// TestNewTransport_InsecureReachesTheTransport walks the whole seam the way
// NewTileLoader does, since that is the path a deployment actually takes: the
// options must survive into the *http.Transport the client dials with.
func TestNewTransport_InsecureReachesTheTransport(t *testing.T) {
	l := &TileLoader{opts: LoaderOptions{InsecureTLS: true}.withDefaults()}
	tr, ok := l.newTransport().(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, tr.TLSClientConfig)
	assert.True(t, tr.TLSClientConfig.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS10), tr.TLSClientConfig.MinVersion)
	assert.True(t, tr.ForceAttemptHTTP2, "the clone must keep HTTP/2 despite a custom TLS config")
}

// selfSignedCAPEM is a throwaway CA certificate, enough for AppendCertsFromPEM
// to accept the file as holding one.
func selfSignedCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "portolan test CA"},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(1<<31, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// The round trip. Asserting the *tls.Config fields says the knob is wired;
// these say it works, against the two obstacles an old tile server actually
// puts up. Both servers speak in-process over a loopback listener.

// legacyTileServer serves one PNG tile under a self-signed RSA certificate,
// pinned to the TLS version and cipher suites given.
func legacyTileServer(t *testing.T, minVer, maxVer uint16, suites []uint16) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	require.NoError(t, png.Encode(&buf, img))
	body := buf.Bytes()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	srv.TLS = &tls.Config{
		MinVersion:   minVer,
		MaxVersion:   maxVer,
		CipherSuites: suites,
		Certificates: []tls.Certificate{selfSignedRSAServerCert(t)},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// fetchOne runs one tile through a loader built from opts and reports the
// arrival, so the two halves below differ only in the options.
func fetchOne(t *testing.T, opts LoaderOptions, url string) TileArrival {
	t.Helper()
	l := NewTileLoader(opts)
	defer l.Close()
	c := TileCoords{1, 2, 3}
	l.Request(c, c, url)
	return drainOne(t, l)
}

func TestInsecureTLS_ReachesATLS10OnlyServer(t *testing.T) {
	// A server that speaks nothing above TLS 1.0 — the case lowering
	// MinVersion exists for. Its cipher suites are left at Go's defaults so
	// that the version is the only obstacle.
	srv := legacyTileServer(t, tls.VersionTLS10, tls.VersionTLS10, nil)

	// Without the knob: the handshake dies on the version, before the
	// certificate is ever an issue.
	a := fetchOne(t, LoaderOptions{Workers: 1}, srv.URL+"/1")
	require.True(t, a.Failed, "a TLS 1.0 server must not be reachable by default")
	assert.Contains(t, a.Err.Error(), "tile fetch failed")

	// With it: the tile arrives. This is what asserting MinVersion alone
	// cannot show.
	a = fetchOne(t, LoaderOptions{Workers: 1, InsecureTLS: true}, srv.URL+"/1")
	require.False(t, a.Failed, "InsecureTLS did not reach the TLS 1.0 server: %v", a.Err)
	assert.Equal(t, 1, a.W)
	assert.Equal(t, uint32(0x112233ff), a.Pixels[0])
}

func TestInsecureTLS_ReachesAStaticRSACipherServer(t *testing.T) {
	// The other half, and the one lowering MinVersion alone would miss: a
	// server offering only a static-RSA suite, which crypto/tls keeps out
	// of its default client list at every version. TLS 1.2 throughout, so
	// the cipher is the only obstacle.
	srv := legacyTileServer(t, tls.VersionTLS12, tls.VersionTLS12,
		[]uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA})

	a := fetchOne(t, LoaderOptions{Workers: 1}, srv.URL+"/1")
	require.True(t, a.Failed, "a static-RSA-only server must not be reachable by default")

	a = fetchOne(t, LoaderOptions{Workers: 1, InsecureTLS: true}, srv.URL+"/1")
	require.False(t, a.Failed, "InsecureTLS did not reach the static-RSA server: %v", a.Err)
	assert.Equal(t, uint32(0x112233ff), a.Pixels[0])
}

// selfSignedRSAServerCert is a throwaway leaf for 127.0.0.1. RSA rather than
// ECDSA because the static-RSA suites above cannot be negotiated with an
// ECDSA certificate.
func selfSignedRSAServerCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "portolan test tile server"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
