//go:build llm_generated_opus47

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
// Unit tests for the kafka subcommand's flag-parsing helpers
// (buildSASL, buildTLS, parseSASLMechanism). No broker required.

package kafka

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cli "github.com/urfave/cli/v2"

	pkafka "github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

// runWithCommonFlags constructs a cli.Context populated from args (the
// way urfave/cli/v2 sees them in production) and invokes action with
// it. `brokers` is required by commonFlags so a dummy is always
// prepended; tests that examine the brokers value should override it.
func runWithCommonFlags(t *testing.T, args []string, action func(c *cli.Context) error) {
	t.Helper()
	app := cli.NewApp()
	app.Writer = io.Discard
	app.ErrWriter = io.Discard
	app.Flags = commonFlags()
	app.Action = action
	full := append([]string{"test", "--brokers=stub:9092"}, args...)
	if err := app.Run(full); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
}

func TestParseSASLMechanism(t *testing.T) {
	cases := []struct {
		in   string
		want pkafka.SASLMechanismE
		ok   bool
	}{
		{"", pkafka.SASLMechanismNone, true},
		{"none", pkafka.SASLMechanismNone, true},
		{"NONE", pkafka.SASLMechanismNone, true},
		{"PLAIN", pkafka.SASLMechanismPlain, true},
		{"plain", pkafka.SASLMechanismPlain, true}, // case-insensitive
		{"SCRAM-SHA-256", pkafka.SASLMechanismSCRAMSHA256, true},
		{"scram-sha-512", pkafka.SASLMechanismSCRAMSHA512, true},
		{"OAUTHBEARER", pkafka.SASLMechanismOAuthBearer, true},
		{"GSSAPI", 0, false}, // unsupported
		{"bogus", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSASLMechanism(tc.in)
			if tc.ok {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBuildSASL_NoneByDefault(t *testing.T) {
	runWithCommonFlags(t, nil, func(c *cli.Context) error {
		mechs, err := buildSASL(c)
		require.NoError(t, err)
		assert.Empty(t, mechs)
		return nil
	})
}

func TestBuildSASL_PlainHappyPath(t *testing.T) {
	runWithCommonFlags(t,
		[]string{"--sasl-mechanism=PLAIN", "--sasl-username=alice", "--sasl-password=s3cret"},
		func(c *cli.Context) error {
			mechs, err := buildSASL(c)
			require.NoError(t, err)
			require.Len(t, mechs, 1)
			assert.Equal(t, "PLAIN", mechs[0].Name())
			return nil
		})
}

func TestBuildSASL_SCRAM512(t *testing.T) {
	runWithCommonFlags(t,
		[]string{"--sasl-mechanism=SCRAM-SHA-512", "--sasl-username=alice", "--sasl-password=s3cret"},
		func(c *cli.Context) error {
			mechs, err := buildSASL(c)
			require.NoError(t, err)
			require.Len(t, mechs, 1)
			assert.Equal(t, "SCRAM-SHA-512", mechs[0].Name())
			return nil
		})
}

func TestBuildSASL_BogusMechanism(t *testing.T) {
	runWithCommonFlags(t, []string{"--sasl-mechanism=GSSAPI"},
		func(c *cli.Context) error {
			_, err := buildSASL(c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsupported --sasl-mechanism")
			return nil
		})
}

func TestBuildTLS_DisabledWhenNoFlags(t *testing.T) {
	runWithCommonFlags(t, nil, func(c *cli.Context) error {
		enabled, cfg, err := buildTLS(c)
		require.NoError(t, err)
		assert.False(t, enabled)
		assert.Nil(t, cfg)
		return nil
	})
}

func TestBuildTLS_EnableExplicit(t *testing.T) {
	runWithCommonFlags(t, []string{"--tls"}, func(c *cli.Context) error {
		enabled, cfg, err := buildTLS(c)
		require.NoError(t, err)
		assert.True(t, enabled)
		require.NotNil(t, cfg)
		assert.False(t, cfg.InsecureSkipVerify)
		assert.Empty(t, cfg.Certificates)
		return nil
	})
}

func TestBuildTLS_SkipVerifyImpliesEnable(t *testing.T) {
	runWithCommonFlags(t, []string{"--tls-skip-verify"}, func(c *cli.Context) error {
		enabled, cfg, err := buildTLS(c)
		require.NoError(t, err)
		assert.True(t, enabled, "tls-skip-verify alone should enable TLS")
		require.NotNil(t, cfg)
		assert.True(t, cfg.InsecureSkipVerify)
		return nil
	})
}

func TestBuildTLS_CertWithoutKey(t *testing.T) {
	runWithCommonFlags(t, []string{"--tls-cert-file=/tmp/cert.pem"}, func(c *cli.Context) error {
		_, _, err := buildTLS(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--tls-cert-file and --tls-key-file must be set together")
		return nil
	})
}

func TestBuildTLS_BadCAFile(t *testing.T) {
	runWithCommonFlags(t, []string{"--tls-ca-file=/nonexistent/ca.pem"}, func(c *cli.Context) error {
		_, _, err := buildTLS(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read --tls-ca-file")
		return nil
	})
}

func TestBuildTLS_EmptyCAFile(t *testing.T) {
	// A file with no PEM data should error with "no PEM certificates".
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.pem")
	require.NoError(t, os.WriteFile(emptyPath, []byte("not a pem block\n"), 0o600))

	runWithCommonFlags(t, []string{"--tls-ca-file=" + emptyPath}, func(c *cli.Context) error {
		_, _, err := buildTLS(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no PEM certificates")
		return nil
	})
}
