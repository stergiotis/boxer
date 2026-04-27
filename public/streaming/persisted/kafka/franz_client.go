//go:build llm_generated_opus47

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
//   - Dropped FranzConnectionFields, FranzConnectionOptionalFields,
//     FranzConnectionDetailsFromConfig, and FranzConnectionOptsFromConfig.
//     These were Benthos-config-DSL helpers (service.ConfigField /
//     service.ParsedConfig); the application now populates
//     FranzConnectionDetails directly.
//   - Replaced *service.Logger with *zerolog.Logger; logger wiring goes
//     through KGoLogger in logger.go.
//   - Replaced github.com/redpanda-data/benthos/v4/public/utils/netutil's
//     DialerConfig blob with a plain net.Dialer field. Callers configure
//     keepalive / source-address / etc. directly on the Dialer before
//     handing it to FranzConnectionDetails.
//   - Replaced service.NewErrBackOff(err, time.Minute) in NewFranzClient
//     with a plain wrapped error. Callers that want backoff implement it
//     externally (boxer's standard pattern is a retry loop with
//     cenkalti/backoff/v4 or context-aware sleep).
//   - Refactored to boxer coding standards: receiver `inst`, named
//     returns, eh.Errorf wrapping.

package kafka

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
)

// FranzConnectionDetails describes the information required to create a
// franz-go Kafka client. Zero-value SeedBrokers means the connection is
// not yet configured; see [FranzConnectionDetails.IsConfigured].
//
// The duration fields (MetaMaxAge, RequestTimeoutOverhead,
// ConnIdleTimeout) inherit franz-go's defaults when left at zero —
// [FranzConnectionDetails.FranzOpts] only emits the corresponding
// kgo.Opt when the field is non-zero. Use
// [DefaultFranzConnectionDetails] to start from the upstream Connect
// plugin defaults (1m metadata, 10s timeout overhead, 20s idle).
//
// Logger may be nil; when nil, franz-go uses its default no-op logger.
//
// Dialer is used directly when TLSEnabled is false, and as the underlying
// net dialer of a *tls.Dialer when TLSEnabled is true. The zero value is
// usable: it disables keepalive and uses an unbounded dial deadline,
// matching net.Dialer{}'s defaults.
type FranzConnectionDetails struct {
	SeedBrokers            []string
	ClientID               string
	TLSEnabled             bool
	TLSConf                *tls.Config
	SASL                   []sasl.Mechanism
	MetaMaxAge             time.Duration
	RequestTimeoutOverhead time.Duration
	ConnIdleTimeout        time.Duration
	Dialer                 net.Dialer
	Logger                 *zerolog.Logger
}

// DefaultFranzConnectionDetails returns connection details populated
// with the durations the upstream Connect plugin applied at its YAML
// config layer (1m metadata-max-age, 10s request-timeout-overhead, 20s
// conn-idle-timeout). The caller assigns SeedBrokers, ClientID, SASL,
// TLS, Dialer, and Logger as needed before passing to
// [FranzConnectionDetails.FranzOpts].
func DefaultFranzConnectionDetails() (d FranzConnectionDetails) {
	d = FranzConnectionDetails{
		MetaMaxAge:             time.Minute,
		RequestTimeoutOverhead: 10 * time.Second,
		ConnIdleTimeout:        20 * time.Second,
	}
	return
}

// IsConfigured reports whether at least one seed broker is set.
func (inst *FranzConnectionDetails) IsConfigured() (configured bool) {
	configured = len(inst.SeedBrokers) > 0
	return
}

// FranzOpts returns the kgo.Opt slice that establishes a connection
// described by inst. The slice is freshly allocated and the caller may
// append additional options (consumer group, produce options, etc.)
// before passing it to [NewFranzClient] or kgo.NewClient directly.
//
// Comma-separated entries inside SeedBrokers (e.g. "a:9092,b:9092") are
// expanded into individual broker addresses, mirroring upstream's input
// handling.
func (inst *FranzConnectionDetails) FranzOpts() (opts []kgo.Opt) {
	opts = []kgo.Opt{
		kgo.SeedBrokers(splitSeedBrokers(inst.SeedBrokers)...),
		kgo.SASL(inst.SASL...),
		kgo.ClientID(inst.ClientID),
	}
	// Duration options: zero value defers to kgo's internal defaults.
	// kgo's RequestTimeoutOverhead in particular rejects values below
	// 100ms, so emitting kgo.RequestTimeoutOverhead(0) would fail
	// NewClient construction; suppressing zero values is what makes
	// FranzConnectionDetails{} (or a partially-populated value)
	// usable.
	if inst.MetaMaxAge > 0 {
		opts = append(opts, kgo.MetadataMaxAge(inst.MetaMaxAge))
	}
	if inst.RequestTimeoutOverhead > 0 {
		opts = append(opts, kgo.RequestTimeoutOverhead(inst.RequestTimeoutOverhead))
	}
	if inst.ConnIdleTimeout > 0 {
		opts = append(opts, kgo.ConnIdleTimeout(inst.ConnIdleTimeout))
	}
	if inst.Logger != nil {
		opts = append(opts, kgo.WithLogger(NewKGoLogger(inst.Logger)))
	}
	if inst.TLSEnabled {
		td := &tls.Dialer{
			NetDialer: &inst.Dialer,
			Config:    inst.TLSConf,
		}
		opts = append(opts, kgo.Dialer(td.DialContext))
	} else {
		opts = append(opts, kgo.Dialer(inst.Dialer.DialContext))
	}
	return
}

func splitSeedBrokers(brokers []string) (out []string) {
	for _, b := range brokers {
		out = append(out, strings.Split(b, ",")...)
	}
	return
}

// NewFranzClient constructs a kgo.Client from the given options and
// performs an initial Ping so configuration errors (invalid SASL
// credentials, unreachable brokers in a way that is not retryable)
// surface as a fast Close + error rather than as an endless internal
// retry loop inside franz-go.
func NewFranzClient(ctx context.Context, opts ...kgo.Opt) (client *kgo.Client, err error) {
	client, err = kgo.NewClient(opts...)
	if err != nil {
		return
	}
	err = client.Ping(ctx)
	if err != nil {
		client.Close()
		client = nil
		err = eh.Errorf("kafka client ping: %w", err)
		return
	}
	return
}
