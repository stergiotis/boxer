package httpegress

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A destination's TLS seam. Nothing in the first group dials: tlsConfig is
// pure apart from reading the CA file, so the three states are asserted on
// the *tls.Config the transport would have been handed.

func TestTLSConfig_UnsetLeavesTheTransportAlone(t *testing.T) {
	// No knob set: nil, so newTransport keeps the cloned DefaultTransport's
	// own TLSClientConfig — the system trust store and Go's TLS 1.2 floor.
	assert.Nil(t, Destination{}.tlsConfig(zerolog.Nop()))

	// Clone() materialises a TLSClientConfig of its own to hold NextProtos,
	// so the assertion is not that it is absent but that it carries none of
	// the weakening: MinVersion 0 is Go's implicit TLS 1.2 client floor, and
	// the nil pool means the system trust store.
	tr, ok := Destination{}.newTransport(zerolog.Nop()).(*http.Transport)
	require.True(t, ok)
	if cfg := tr.TLSClientConfig; cfg != nil {
		assert.False(t, cfg.InsecureSkipVerify, "no knob set: verification must be on")
		assert.Zero(t, cfg.MinVersion, "no knob set: leave Go's implicit TLS 1.2 floor alone")
		assert.Nil(t, cfg.RootCAs, "no knob set: the system trust store")
		assert.Nil(t, cfg.CipherSuites, "no knob set: Go's default cipher list")
	}
}

func TestTLSConfig_CAFileKeepsVerificationAndTheFloor(t *testing.T) {
	// CAFile moves the trust anchor and must change nothing else: an
	// internal CA is not a reason to weaken the protocol.
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, selfSignedCAPEM(t), 0o600))

	cfg := Destination{CAFile: path}.tlsConfig(zerolog.Nop())
	require.NotNil(t, cfg)
	assert.False(t, cfg.InsecureSkipVerify, "the CA file must not disable verification")
	assert.NotNil(t, cfg.RootCAs, "the CA file was not loaded")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Nil(t, cfg.CipherSuites, "the CA file must not widen the cipher list")
}

func TestTLSConfig_UnreadableCAFileFallsBackToSystemRoots(t *testing.T) {
	// A bad path or a file with no certificate in it is logged and ignored,
	// leaving the system roots — never an empty pool, which would reject
	// every certificate, and never disabled verification.
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "junk.pem")
	require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0o600))
	for name, path := range map[string]string{"missing": filepath.Join(dir, "absent.pem"), "not-a-ca": notPEM} {
		t.Run(name, func(t *testing.T) {
			cfg := Destination{CAFile: path}.tlsConfig(zerolog.Nop())
			require.NotNil(t, cfg)
			assert.Nil(t, cfg.RootCAs, "want the system roots, not an empty pool")
			assert.False(t, cfg.InsecureSkipVerify)
		})
	}
}

func TestTLSConfig_InsecureAlsoLowersVersionAndCiphers(t *testing.T) {
	cfg := Destination{InsecureTLS: true}.tlsConfig(zerolog.Nop())
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS10), cfg.MinVersion)
	// The static-RSA suites are the point: an old server offers these, and
	// Go leaves every one of them out of its default list.
	assert.Contains(t, cfg.CipherSuites, tls.TLS_RSA_WITH_AES_128_CBC_SHA)
	assert.Contains(t, cfg.CipherSuites, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA)
	assert.Contains(t, cfg.CipherSuites, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
}

func TestLegacyCipherSuites_CoversEverythingCryptoTLSCanSpeak(t *testing.T) {
	ids := legacyCipherSuites()
	for _, cs := range tls.CipherSuites() {
		assert.Contains(t, ids, cs.ID, "missing default suite %s", cs.Name)
	}
	for _, cs := range tls.InsecureCipherSuites() {
		assert.Contains(t, ids, cs.ID, "missing insecure suite %s", cs.Name)
	}
}

func TestNewTransport_InsecureReachesTheTransport(t *testing.T) {
	tr, ok := Destination{InsecureTLS: true}.newTransport(zerolog.Nop()).(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, tr.TLSClientConfig)
	assert.True(t, tr.TLSClientConfig.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS10), tr.TLSClientConfig.MinVersion)
	assert.True(t, tr.ForceAttemptHTTP2, "the clone must keep HTTP/2 despite a custom TLS config")
}

// The round trip through the service, against the two obstacles an old
// server actually puts up. Both servers speak in-process over loopback.

func legacyServer(t *testing.T, minVer, maxVer uint16, suites []uint16) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("tile"))
	}))
	srv.TLS = &tls.Config{MinVersion: minVer, MaxVersion: maxVer, CipherSuites: suites, Certificates: []tls.Certificate{selfSignedRSAServerCert(t)}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func fetchVia(t *testing.T, d Destination, url string) (err error) {
	t.Helper()
	cli, _, _ := serve(t, []DestinationSpec{spec("legacy", d)}, "legacy")
	_, err = cli.Fetch(context.Background(), "legacy", Request{URL: url})
	return
}

func TestInsecureTLS_ReachesATLS10OnlyServer(t *testing.T) {
	srv := legacyServer(t, tls.VersionTLS10, tls.VersionTLS10, nil)
	require.Error(t, fetchVia(t, Destination{Prefixes: []string{srv.URL + "/"}}, srv.URL+"/1"), "a TLS 1.0 server must not be reachable by default")
	require.NoError(t, fetchVia(t, Destination{Prefixes: []string{srv.URL + "/"}, InsecureTLS: true}, srv.URL+"/1"))
}

func TestInsecureTLS_ReachesAStaticRSACipherServer(t *testing.T) {
	srv := legacyServer(t, tls.VersionTLS12, tls.VersionTLS12, []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA})
	require.Error(t, fetchVia(t, Destination{Prefixes: []string{srv.URL + "/"}}, srv.URL+"/1"), "a static-RSA-only server must not be reachable by default")
	require.NoError(t, fetchVia(t, Destination{Prefixes: []string{srv.URL + "/"}, InsecureTLS: true}, srv.URL+"/1"))
}

func selfSignedCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "httpegress test CA"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// selfSignedRSAServerCert is RSA because the static-RSA suites cannot be
// negotiated with an ECDSA certificate.
func selfSignedRSAServerCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "httpegress test server"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
