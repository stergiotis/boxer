package httpegress

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"

	"github.com/rs/zerolog"
)

// newTransport is the destination's transport: a clone of the default
// transport under the destination's trust policy.
func (d Destination) newTransport(log zerolog.Logger) http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	t = t.Clone()
	if cfg := d.tlsConfig(log); cfg != nil {
		t.TLSClientConfig = cfg
	}
	return t
}

// tlsConfig is the destination's TLS configuration, or nil to leave the
// transport's own default in place — the system trust store and Go's TLS
// 1.2 floor.
//
// The two knobs are deliberately not symmetric. CAFile says "verification
// stays on, the chain just ends somewhere else", so it changes the roots
// and nothing else. InsecureTLS says "do not authenticate this peer at
// all", which is a strictly larger statement, and once it is made the
// protocol floor and the cipher list stop protecting anything: an attacker
// who can present an arbitrary certificate is already the peer, so
// declining their TLS 1.0 or their CBC suite buys nothing. Both are
// therefore lowered along with it — without that, the handshake against
// the legacy server this knob exists for fails on version or cipher
// negotiation rather than on the certificate, and the operator sees a knob
// that does not work.
func (d Destination) tlsConfig(log zerolog.Logger) *tls.Config {
	if d.CAFile == "" && !d.InsecureTLS {
		return nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if d.CAFile != "" {
		pem, err := os.ReadFile(d.CAFile)
		if err != nil {
			log.Error().Err(err).Str("path", d.CAFile).Msg("httpegress: CA file unreadable; keeping the system roots")
		} else if pool := x509.NewCertPool(); !pool.AppendCertsFromPEM(pem) {
			log.Error().Str("path", d.CAFile).Msg("httpegress: CA file holds no certificate; keeping the system roots")
		} else {
			cfg.RootCAs = pool
		}
	}
	if d.InsecureTLS {
		cfg.InsecureSkipVerify = true //nolint:gosec // the operator's explicit knob, logged
		cfg.MinVersion = tls.VersionTLS10
		cfg.CipherSuites = legacyCipherSuites()
		log.Warn().Msg("httpegress: TLS verification disabled by configuration; the protocol floor is TLS 1.0 and the legacy cipher suites are admitted")
	}
	return cfg
}

// legacyCipherSuites is every TLS 1.0–1.2 suite crypto/tls can speak,
// including the ones it keeps out of its default list: the static-RSA key
// exchanges an old server typically offers, and 3DES and RC4. Only
// reachable under InsecureTLS.
//
// Config.CipherSuites is intersected against what the library supports and
// re-ordered by Go's own preference, so listing a suite only permits it;
// the TLS 1.3 ids in the slice are ignored, since TLS 1.3 suites are not
// configurable and stay enabled.
func legacyCipherSuites() []uint16 {
	secure, insecure := tls.CipherSuites(), tls.InsecureCipherSuites()
	ids := make([]uint16, 0, len(secure)+len(insecure))
	for _, cs := range secure {
		ids = append(ids, cs.ID)
	}
	for _, cs := range insecure {
		ids = append(ids, cs.ID)
	}
	return ids
}
