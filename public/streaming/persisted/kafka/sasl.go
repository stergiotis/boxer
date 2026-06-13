// Copyright 2024 Redpanda Data, Inc.
// Copyright 2026 Panos Stergiotis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modifications applied during port (see ./NOTICE):
//   - Dropped the sarama bridge entirely: SaramaSASLField,
//     ApplySaramaSASLFromParsed, cacheAccessTokenProvider,
//     staticAccessTokenProvider, and the saramaField* constants.
//   - Dropped AWS_MSK_IAM (out-of-scope; the upstream binary plugs it in
//     via AWSSASLFromConfigFn).
//   - Dropped REDPANDA_CLOUD_SERVICE_ACCOUNT (depends on
//     redpanda-data/connect/v4/internal/serviceaccount, an internal
//     package).
//   - Replaced service.ParsedConfig field-parsing helpers with a plain
//     SASLConfig struct slice. The same four franz-go mechanisms are
//     produced (PLAIN, OAUTHBEARER, SCRAM-SHA-256, SCRAM-SHA-512) with
//     identical semantics.
//   - Refactored to boxer coding standards: SASLMechanismE enum, named
//     return values, eh.Errorf wrapping, receiver `inst`.

package kafka

import (
	"context"
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// ErrUnsupportedSASLMechanism is returned by [SASLMechanisms] when a
// SASLConfig names a mechanism this package does not implement.
var ErrUnsupportedSASLMechanism = errors.New("unsupported SASL mechanism")

// SASLMechanismE enumerates the SASL mechanisms this package supports.
//
// AWS_MSK_IAM and REDPANDA_CLOUD_SERVICE_ACCOUNT, supported upstream, are
// out of scope for this port; see ./NOTICE.
type SASLMechanismE uint8

const (
	SASLMechanismNone        SASLMechanismE = iota
	SASLMechanismPlain
	SASLMechanismOAuthBearer
	SASLMechanismSCRAMSHA256
	SASLMechanismSCRAMSHA512
)

// String returns the canonical wire-name ("PLAIN", "OAUTHBEARER",
// "SCRAM-SHA-256", "SCRAM-SHA-512") matching the upstream Connect config
// vocabulary. SASLMechanismNone returns "none".
func (inst SASLMechanismE) String() (s string) {
	switch inst {
	case SASLMechanismNone:
		s = "none"
	case SASLMechanismPlain:
		s = "PLAIN"
	case SASLMechanismOAuthBearer:
		s = "OAUTHBEARER"
	case SASLMechanismSCRAMSHA256:
		s = "SCRAM-SHA-256"
	case SASLMechanismSCRAMSHA512:
		s = "SCRAM-SHA-512"
	default:
		s = "unknown"
	}
	return
}

// SASLConfig describes a single SASL mechanism. Multiple SASLConfigs are
// tried by franz-go in slice order; the first one supported by the broker
// is used. A SASLMechanismNone entry is silently skipped, which is useful
// for staged configurations that toggle SASL on and off without restruct-
// uring the surrounding option list.
type SASLConfig struct {
	Mechanism  SASLMechanismE
	Username   string            // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Password   string            // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Token      string            // OAUTHBEARER (static)
	Extensions map[string]string // OAUTHBEARER request extensions
}

// SASLMechanisms returns the franz-go sasl.Mechanism slice corresponding
// to the given SASLConfig list. Empty slice + nil error is returned when
// every entry is SASLMechanismNone.
func SASLMechanisms(configs []SASLConfig) (mechanisms []sasl.Mechanism, err error) {
	for i, c := range configs {
		var m sasl.Mechanism
		switch c.Mechanism {
		case SASLMechanismNone:
			continue
		case SASLMechanismPlain:
			m = plainSasl(c.Username, c.Password)
		case SASLMechanismOAuthBearer:
			m = oauthSasl(c.Token, c.Extensions)
		case SASLMechanismSCRAMSHA256:
			m = scram256Sasl(c.Username, c.Password)
		case SASLMechanismSCRAMSHA512:
			m = scram512Sasl(c.Username, c.Password)
		default:
			err = eh.Errorf("mechanism %v: %w", i, ErrUnsupportedSASLMechanism)
			return
		}
		mechanisms = append(mechanisms, m)
	}
	return
}

func plainSasl(username, password string) (m sasl.Mechanism) {
	m = plain.Plain(func(context.Context) (plain.Auth, error) {
		return plain.Auth{User: username, Pass: password}, nil
	})
	return
}

func oauthSasl(token string, extensions map[string]string) (m sasl.Mechanism) {
	m = oauth.Oauth(func(context.Context) (oauth.Auth, error) {
		return oauth.Auth{Token: token, Extensions: extensions}, nil
	})
	return
}

func scram256Sasl(username, password string) (m sasl.Mechanism) {
	m = scram.Sha256(func(context.Context) (scram.Auth, error) {
		return scram.Auth{User: username, Pass: password}, nil
	})
	return
}

func scram512Sasl(username, password string) (m sasl.Mechanism) {
	m = scram.Sha512(func(context.Context) (scram.Auth, error) {
		return scram.Auth{User: username, Pass: password}, nil
	})
	return
}
