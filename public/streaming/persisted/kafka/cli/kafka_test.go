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

package cli

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
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

func TestNetstringWriter(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
		want  string
	}{
		{"hello", []byte("hello"), "5:hello,"},
		{"empty", []byte{}, "0:,"},
		{"nil", nil, "0:,"},
		{"binary", []byte{0xff, 0x00, 0x01}, "3:\xff\x00\x01,"},
		{"unicode (byte length, not rune)", []byte("héllo"), "6:héllo,"}, // é is 2 bytes UTF-8
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := netstringWriter(&buf, &kgo.Record{Value: tc.value})
			require.NoError(t, err)
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

func TestCBORWriter_RoundTrip(t *testing.T) {
	rec := &kgo.Record{
		Topic:     "events",
		Partition: 7,
		Offset:    42,
		Timestamp: time.UnixMilli(1700000000000),
		Key:       []byte("user-123"),
		Value:     []byte("payload"),
		Headers: []kgo.RecordHeader{
			{Key: "trace-id", Value: []byte("abc")},
			{Key: "trace-id", Value: []byte("xyz")}, // duplicate keys allowed
		},
	}
	var buf bytes.Buffer
	require.NoError(t, cborWriter(&buf, rec))

	var got cborRecord
	require.NoError(t, cbor.Unmarshal(buf.Bytes(), &got))

	assert.Equal(t, "events", got.Topic)
	assert.Equal(t, int32(7), got.Partition)
	assert.Equal(t, int64(42), got.Offset)
	assert.Equal(t, int64(1700000000000), got.TimestampMs)
	assert.Equal(t, []byte("user-123"), got.Key)
	assert.Equal(t, []byte("payload"), got.Value)
	require.Len(t, got.Headers, 2)
	assert.Equal(t, "trace-id", got.Headers[0].Key)
	assert.Equal(t, []byte("abc"), got.Headers[0].Value)
	assert.Equal(t, "trace-id", got.Headers[1].Key) // duplicate preserved
	assert.Equal(t, []byte("xyz"), got.Headers[1].Value)
}

func TestCBORWriter_TombstonePreservesNilValue(t *testing.T) {
	// A Kafka tombstone has Value=nil; the encoded CBOR must preserve
	// that as null (NOT omit the field), so downstream consumers can
	// distinguish "delete marker" from "empty payload".
	rec := &kgo.Record{
		Topic:     "deletes",
		Partition: 0,
		Offset:    1,
		Timestamp: time.UnixMilli(0),
		Key:       []byte("k"),
		Value:     nil,
	}
	var buf bytes.Buffer
	require.NoError(t, cborWriter(&buf, rec))

	// Round-trip into a generic map to confirm the "value" key exists.
	var got map[string]any
	require.NoError(t, cbor.Unmarshal(buf.Bytes(), &got))
	assert.Contains(t, got, "value", "value key must be present even for tombstones")
	assert.Nil(t, got["value"], "tombstone value must be CBOR null")
}

func TestCBORWriter_NoHeadersOmitsField(t *testing.T) {
	rec := &kgo.Record{
		Topic: "x", Value: []byte("v"), Timestamp: time.UnixMilli(0),
	}
	var buf bytes.Buffer
	require.NoError(t, cborWriter(&buf, rec))
	var got map[string]any
	require.NoError(t, cbor.Unmarshal(buf.Bytes(), &got))
	_, hasHeaders := got["headers"]
	assert.False(t, hasHeaders, "headers field omitted when none present")
}

func TestCBORWriter_SelfDelimiting(t *testing.T) {
	// Two records concatenated should decode as two separate records.
	r1 := &kgo.Record{Topic: "t", Partition: 0, Offset: 1, Timestamp: time.UnixMilli(0), Value: []byte("a")}
	r2 := &kgo.Record{Topic: "t", Partition: 0, Offset: 2, Timestamp: time.UnixMilli(0), Value: []byte("b")}
	var buf bytes.Buffer
	require.NoError(t, cborWriter(&buf, r1))
	require.NoError(t, cborWriter(&buf, r2))

	dec := cbor.NewDecoder(&buf)
	var g1, g2 cborRecord
	require.NoError(t, dec.Decode(&g1))
	require.NoError(t, dec.Decode(&g2))
	assert.Equal(t, []byte("a"), g1.Value)
	assert.Equal(t, []byte("b"), g2.Value)
}

func TestReadNetstring_HappyPaths(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []byte
	}{
		{"hello", "5:hello,", []byte("hello")},
		{"empty", "0:,", []byte{}},
		{"binary", "3:\xff\x00\x01,", []byte{0xff, 0x00, 0x01}},
		{"unicode", "6:héllo,", []byte("héllo")}, // é is 2 bytes UTF-8
		{"leading-zero", "005:hello,", []byte("hello")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewReader([]byte(tc.in)))
			value, ok, err := readNetstring(r)
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, tc.want, value)
		})
	}
}

func TestReadNetstring_MultipleFramesSequentially(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("3:foo,5:hello,3:bar,")))
	want := [][]byte{[]byte("foo"), []byte("hello"), []byte("bar")}
	for _, w := range want {
		value, ok, err := readNetstring(r)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, w, value)
	}
	// Next read should hit clean EOF.
	value, ok, err := readNetstring(r)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, value)
}

func TestReadNetstring_CleanEOFAtStart(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(nil))
	value, ok, err := readNetstring(r)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, value)
}

func TestReadNetstring_ErrorPaths(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		errSubstr string
	}{
		{"missing colon at EOF", "5", "unexpected EOF"},
		{"non-numeric length", "abc:hello,", "invalid netstring length"},
		{"negative length", "-1:,", "invalid netstring length"},
		{"truncated value", "5:hel", "read netstring value"},
		{"missing terminator", "5:hello", "read netstring terminator"},
		{"wrong terminator", "5:hello.", "expected ','"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewReader([]byte(tc.in)))
			_, _, err := readNetstring(r)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSubstr)
		})
	}
}

func TestBuildRecord_NoKeyDelim(t *testing.T) {
	rec := buildRecord("t", []byte("hello"), "")
	assert.Equal(t, "t", rec.Topic)
	assert.Nil(t, rec.Key)
	assert.Equal(t, []byte("hello"), rec.Value)
}

func TestBuildRecord_KeyDelim_Found(t *testing.T) {
	rec := buildRecord("t", []byte("k=v=foo"), "=")
	// Split on first '=' only — value preserves further occurrences.
	assert.Equal(t, []byte("k"), rec.Key)
	assert.Equal(t, []byte("v=foo"), rec.Value)
}

func TestBuildRecord_KeyDelim_NotFound(t *testing.T) {
	rec := buildRecord("t", []byte("hello"), "=")
	// Falls through to value-only when delimiter absent.
	assert.Nil(t, rec.Key)
	assert.Equal(t, []byte("hello"), rec.Value)
}

func TestBuildRecord_KeyDelim_AtStart(t *testing.T) {
	rec := buildRecord("t", []byte("=hello"), "=")
	assert.Equal(t, []byte{}, rec.Key)
	assert.Equal(t, []byte("hello"), rec.Value)
}

func TestMakeRecordWriter_Routing(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"format default", nil, ""},
		{"format explicit", []string{"--output-mode=format"}, ""},
		{"cbor", []string{"--output-mode=cbor"}, ""},
		{"netstring", []string{"--output-mode=netstring"}, ""},
		{"bogus", []string{"--output-mode=avro"}, "invalid --output-mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runWithCommonAndConsumeFlags(t, tc.args, func(c *cli.Context) error {
				fn, err := makeRecordWriter(c)
				if tc.wantErr != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.wantErr)
					return nil
				}
				require.NoError(t, err)
				assert.NotNil(t, fn)
				return nil
			})
		})
	}
}

// runWithCommonAndConsumeFlags is like runWithCommonFlags but layers
// the consume command's own flags on top so --output-mode and
// --format parse correctly.
func runWithCommonAndConsumeFlags(t *testing.T, args []string, action func(c *cli.Context) error) {
	t.Helper()
	app := cli.NewApp()
	app.Writer = io.Discard
	app.ErrWriter = io.Discard
	cmd := consumeCmd()
	cmd.Action = action
	app.Commands = []*cli.Command{cmd}
	full := append([]string{"test", "consume", "--brokers=stub:9092", "--topic=stub"}, args...)
	if err := app.Run(full); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
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
