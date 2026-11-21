package loopback

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateExposureDeclaration(t *testing.T) {
	g := Gate{Name: "gateway", Exposure: &Exposure{Serves: "devices on the local network", Reason: "they post readings to this process"}}
	require.NoError(t, g.Check("0.0.0.0:80"))
	exposure, err := g.check("0.0.0.0:80")
	require.NoError(t, err)
	assert.Equal(t, ExposureDeclared, exposure)

	g.Exposure.Reason = ""
	require.Error(t, g.Check("0.0.0.0:80"), "a declaration says why")
	require.Error(t, g.Check("127.0.0.1:80"), "an incomplete declaration is refused on loopback too")
}

func TestGatePeerTLS(t *testing.T) {
	cert, pool := selfSigned(t)
	good := &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	exposure, err := Gate{Name: "mtls", PeerTLS: good}.check("0.0.0.0:443")
	require.NoError(t, err)
	assert.Equal(t, ExposurePeerTLS, exposure)

	for name, c := range map[string]*tls.Config{
		"no client auth":       {Certificates: []tls.Certificate{cert}},
		"client cert optional": {Certificates: []tls.Certificate{cert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool},
		"no CAs":               {Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert},
		"no server cert":       {ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool},
	} {
		require.Error(t, Gate{Name: "mtls", PeerTLS: c}.Check("0.0.0.0:443"), name)
	}
}

// A PeerTLS listener serves TLS and refuses a peer without a client
// certificate.
func TestGatePeerTLSListenEnforces(t *testing.T) {
	cert, pool := selfSigned(t)
	ln, err := Gate{Name: "mtls", PeerTLS: &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}}.Listen(t.Context(), "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		_ = c.(*tls.Conn).Handshake()
		_ = c.Close()
	}()
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: pool, ServerName: "localhost"})
	if err == nil {
		// TLS 1.3 reports the missing certificate on the first read.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = conn.Read(make([]byte, 1))
		_ = conn.Close()
	}
	require.Error(t, err)
}

func TestOpenListenersTracksTheGate(t *testing.T) {
	ln, err := Gate{Name: "tracked-fixture", Exposure: &Exposure{Serves: "tests", Reason: "fixture"}}.Listen(t.Context(), "127.0.0.1:0")
	require.NoError(t, err)
	find := func() (info ListenerInfo, ok bool) {
		for _, l := range OpenListeners() {
			if l.Name == "tracked-fixture" {
				return l, true
			}
		}
		return
	}
	info, ok := find()
	require.True(t, ok)
	assert.Equal(t, ln.Addr().String(), info.Addr)
	assert.Equal(t, ExposureLoopback, info.Exposure, "a loopback bind needs no declaration, whatever the gate carries")
	require.NoError(t, ln.Close())
	_, ok = find()
	assert.False(t, ok)
	_ = ln.Close()
}

func selfSigned(t *testing.T) (cert tls.Certificate, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	cert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}
	return
}
