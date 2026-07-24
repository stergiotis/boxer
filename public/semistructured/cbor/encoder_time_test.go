package cbor

import (
	"bytes"
	"testing"
	"time"

	fxcbor "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

func encodeTime(t *testing.T, v time.Time) (out []byte) {
	t.Helper()
	buf := &bytes.Buffer{}
	enc := NewEncoder(buf, nil)
	n, err := enc.EncodeTimeUTC(v)
	require.NoError(t, err)
	out = buf.Bytes()
	require.Equal(t, len(out), n, "reported byte count disagrees with what was written")
	require.NoError(t, fxcbor.Wellformed(out), "payload is not well-formed CBOR: % x", out)
	return
}

// decodeTime reverses EncodeTimeUTC's two shapes and returns the
// reconstructed instant, so losslessness is asserted against a value rather
// than against bytes.
func decodeTime(t *testing.T, b []byte) (out time.Time) {
	t.Helper()
	var raw fxcbor.RawTag
	require.NoError(t, fxcbor.Unmarshal(b, &raw))
	switch raw.Number {
	case uint64(TagEpochDateTimeNumber):
		var secs int64
		require.NoError(t, fxcbor.Unmarshal(raw.Content, &secs))
		return time.Unix(secs, 0).UTC()
	case uint64(TagExtendedTime):
		var m map[int64]int64
		require.NoError(t, fxcbor.Unmarshal(raw.Content, &m))
		secs, ok := m[1]
		require.True(t, ok, "extended time is missing the base-seconds key 1: %v", m)
		return time.Unix(secs, m[-9]).UTC()
	default:
		t.Fatalf("unexpected time tag %d", raw.Number)
		return
	}
}

// Whole seconds keep tag 1 with an integer payload. Pinned as literal bytes
// because this is the compatibility promise of the sub-second fix: values
// already on disk in this shape must not move.
func TestEncodeTimeUTCWholeSecondsUsesTag1(t *testing.T) {
	require.Equal(t,
		[]byte{0xc1, 0x19, 0x03, 0xe8},
		encodeTime(t, time.Unix(1000, 0)),
		"whole-second encoding changed")

	require.Equal(t,
		[]byte{0xc1, 0x00},
		encodeTime(t, time.Unix(0, 0)))

	// Pre-epoch whole second: CBOR negative integer.
	require.Equal(t,
		[]byte{0xc1, 0x20},
		encodeTime(t, time.Unix(-1, 0)))
}

// Sub-second values must not claim tag 1 — its content is defined as
// seconds, and the old encoder put a nanosecond count there.
func TestEncodeTimeUTCSubSecondUsesExtendedTime(t *testing.T) {
	b := encodeTime(t, time.Unix(1000, 500000000))
	require.Equal(t, byte(0xd9), b[0], "expected a 16-bit tag head, got % x", b)

	var raw fxcbor.RawTag
	require.NoError(t, fxcbor.Unmarshal(b, &raw))
	require.Equal(t, uint64(TagExtendedTime), raw.Number)
	require.NotEqual(t, uint64(TagEpochDateTimeNumber), raw.Number,
		"sub-second value still tagged as epoch-seconds")

	var m map[int64]int64
	require.NoError(t, fxcbor.Unmarshal(raw.Content, &m))
	require.Equal(t, map[int64]int64{1: 1000, -9: 500000000}, m)
}

// The defect this replaces: float64 nanoseconds-since-epoch has a 256 ns
// step at present-day instants, so neighbouring timestamps encoded
// identically. Natural keys are derived from these bytes, so that was a
// collision, not just a rounding error.
func TestEncodeTimeUTCIsLosslessAtNanosecondSteps(t *testing.T) {
	base := time.Unix(1753000000, 123456789).UTC()
	seen := make(map[string]time.Time, 512)
	for i := range 512 {
		v := base.Add(time.Duration(i) * time.Nanosecond)
		b := encodeTime(t, v)
		if prev, dup := seen[string(b)]; dup {
			t.Fatalf("distinct instants share an encoding: %v and %v -> % x", prev, v, b)
		}
		seen[string(b)] = v
		require.True(t, decodeTime(t, b).Equal(v), "round-trip lost %v", v)
	}
}

func TestEncodeTimeUTCRoundTrips(t *testing.T) {
	cases := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, 1),
		time.Unix(1000, 0),
		time.Unix(1753000000, 999999999),
		time.Unix(1753000000, 1),
		time.Unix(-1, 0),
		time.Unix(-1, 500000000),           // pre-epoch, sub-second
		time.Unix(-62135596800, 0),         // year 1
		time.Unix(253402300799, 999999999), // year 9999
	}
	for _, v := range cases {
		v = v.UTC()
		t.Run(v.Format(time.RFC3339Nano), func(t *testing.T) {
			got := decodeTime(t, encodeTime(t, v))
			require.True(t, got.Equal(v), "round-trip: got %v want %v", got, v)
			require.Equal(t, v.UnixNano(), got.UnixNano())
		})
	}
}

// Go normalises Unix()/Nanosecond() so the fraction is never negative; the
// RFC 9581 rule is to *add* it to the base, so pre-epoch instants must not
// come back a second off.
func TestEncodeTimeUTCPreEpochFractionAddsToBase(t *testing.T) {
	v := time.Unix(-1, 500000000).UTC() // 1969-12-31T23:59:59.5Z
	var raw fxcbor.RawTag
	require.NoError(t, fxcbor.Unmarshal(encodeTime(t, v), &raw))
	var m map[int64]int64
	require.NoError(t, fxcbor.Unmarshal(raw.Content, &m))
	require.Equal(t, int64(-1), m[1], "base seconds must be the floored second")
	require.Equal(t, int64(500000000), m[-9], "fraction must be non-negative")
	require.True(t, decodeTime(t, encodeTime(t, v)).Equal(v))
}

// The encoder normalises to UTC, so the same instant in a different zone
// must encode identically — natural keys must not depend on the caller's
// location.
func TestEncodeTimeUTCNormalisesZone(t *testing.T) {
	zone := time.FixedZone("test+5", 5*3600)
	utc := time.Unix(1753000000, 123456789).UTC()
	require.Equal(t, encodeTime(t, utc), encodeTime(t, utc.In(zone)))
}

func TestEncodeTimeUTCHashesFractionalPart(t *testing.T) {
	// Two instants one nanosecond apart must not hash alike through the
	// encoder's hashing path, which is what natural keys consume.
	hashOf := func(v time.Time) []byte {
		buf := &bytes.Buffer{}
		enc := NewEncoder(buf, xxh3.New())
		_, err := enc.EncodeTimeUTC(v)
		require.NoError(t, err)
		h, hErr := enc.Hash(nil)
		require.NoError(t, hErr)
		return h
	}
	a := time.Unix(1753000000, 123456789).UTC()
	require.NotEqual(t, hashOf(a), hashOf(a.Add(time.Nanosecond)))
}
