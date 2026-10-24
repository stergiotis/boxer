//go:build binary_log

package logging

import (
	"bytes"
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Test_ConsoleWriter_matchesZerologDecoder holds the transcoder to zerolog's
// own decoder on every field kind zerolog's CBOR encoder writes, except the
// two where it departs on purpose (integers at or above 2⁶³, byte strings
// that need escaping). One event goes through both readings of the same
// writer; the printed lines must agree.
func Test_ConsoleWriter_matchesZerologDecoder(t *testing.T) {
	require.NoError(t, installConsoleCborMarshalers())

	var ours, theirs bytes.Buffer
	cwOurs, err := NewConsoleWriter(&ours, true)
	require.NoError(t, err)
	cwTheirs, err := NewConsoleWriter(&theirs, true)
	require.NoError(t, err)

	var wire bytes.Buffer
	logger := zerolog.New(&wire)
	_, ipNet, err := net.ParseCIDR("10.1.0.0/16")
	require.NoError(t, err)
	logger.Warn().
		Str("s", "tab\there \"quoted\" ünï").
		Int("i", -42).
		Uint64("u", 42).
		Int64("min", math.MinInt64).
		Float32("f32", 0.1).
		Float64("f64", 1.5).
		Float64("nan", math.NaN()).
		Float64("inf", math.Inf(1)).
		Bool("b", true).
		Time("t", time.Date(2026, 10, 18, 19, 0, 0, 0, time.UTC)).
		Time("tn", time.Date(2026, 10, 18, 19, 0, 0, 500_000_000, time.UTC)).
		Dur("d", 1500*time.Millisecond).
		Bytes("by", []byte("plain")).
		Hex("h", []byte{0xde, 0xad}).
		IPAddr("ip4", net.IPv4(192, 0, 2, 1)).
		IPAddr("ip6", net.ParseIP("2001:db8::1")).
		IPPrefix("pfx", *ipNet).
		MACAddr("mac", net.HardwareAddr{0, 1, 2, 3, 4, 5}).
		Strs("strs", []string{"a", "b"}).
		Ints("ints", []int{1, -2}).
		Interface("obj", map[string]any{"k": 1, "nested": []string{"x"}}).
		RawJSON("rj", []byte(`{"a":[1,2]}`)).
		Dict("dict", zerolog.Dict().Str("inner", "v").Int("n", 3)).
		Err(errors.New("boom")).
		Msg("all kinds")
	ev := wire.Bytes()
	require.Greater(t, ev[0], byte(0x7f), "expected a binary event")

	_, err = cwOurs.Write(ev)
	require.NoError(t, err)
	_, err = cwTheirs.ConsoleWriter.Write(ev)
	require.NoError(t, err)
	require.Equal(t, theirs.String(), ours.String())
}
