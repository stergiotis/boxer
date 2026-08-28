package runtime

import (
	"encoding/hex"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// requireCoreDet is the ADR-0201 M1 self-check applied to one emitted item:
// decoding it with the library and re-encoding under CoreDetEncOptions must
// reproduce the bytes. It catches a well-formedness slip and a determinism
// slip at once, against an implementation this repository does not own.
func requireCoreDet(t *testing.T, b []byte) {
	t.Helper()
	dm, err := cbor.DecOptions{}.DecMode()
	require.NoError(t, err)
	em, err := cbor.CoreDetEncOptions().EncMode()
	require.NoError(t, err)
	var v any
	require.NoError(t, dm.Unmarshal(b, &v), hex.EncodeToString(b))
	again, err := em.Marshal(v)
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(b), hex.EncodeToString(again),
		"re-encoding under CoreDetEncOptions must reproduce the item")
}

// hexOf encodes one value and returns its hex.
func hexOf(t *testing.T, f func(c *CborWriter)) string {
	t.Helper()
	b, err := encBytes(f)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// pin encodes one value, runs the library self-check over the bytes and
// returns their hex together with a reader over them.
func pin(t *testing.T, f func(c *CborWriter)) (string, *CborReader) {
	t.Helper()
	b, err := encBytes(f)
	require.NoError(t, err)
	requireCoreDet(t, b)
	return hex.EncodeToString(b), NewCborReader(b)
}

// form is pin with the encoding stated up front.
func form(t *testing.T, want string, f func(c *CborWriter)) *CborReader {
	t.Helper()
	got, r := pin(t, f)
	require.Equal(t, want, got)
	return r
}

func TestFloatValueForms(t *testing.T) {
	// A float32 widens to float64 exactly, so the two lanes share bytes.
	require.Equal(t, "f93e00", hexOf(t, func(c *CborWriter) { c.WriteF32(1.5) }))
	require.Equal(t, "f93e00", hexOf(t, func(c *CborWriter) { c.WriteF64(1.5) }))
	require.Equal(t, "fa3dcccccd", hexOf(t, func(c *CborWriter) { c.WriteF32(0.1) }))

	cases32 := []float32{0, float32(math.Copysign(0, -1)), 1, -1, 0.1, 1e30, math.MaxFloat32,
		math.SmallestNonzeroFloat32, float32(math.Inf(1)), float32(math.Inf(-1))}
	for _, f := range cases32 {
		_, r := pin(t, func(c *CborWriter) { c.WriteF32(f) })
		got := r.ReadF32()
		require.NoError(t, r.Err())
		require.Equal(t, math.Float32bits(f), math.Float32bits(got), "%v", f)
	}
	cases64 := []float64{0, math.Copysign(0, -1), 1, -1, 0.1, 1e300, math.MaxFloat64,
		math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), 3.0, float64(float32(0.1))}
	for _, f := range cases64 {
		_, r := pin(t, func(c *CborWriter) { c.WriteF64(f) })
		got := r.ReadF64()
		require.NoError(t, r.Err())
		require.Equal(t, math.Float64bits(f), math.Float64bits(got), "%v", f)
	}

	// -0.0 keeps its sign and 3.0 stays a float: SD3 applies no numeric
	// reduction, which is where it parts company with ADR-0201.
	r := form(t, "f98000", func(c *CborWriter) { c.WriteF64(math.Copysign(0, -1)) })
	require.Equal(t, math.Float64bits(math.Copysign(0, -1)), math.Float64bits(r.ReadF64()))
	require.Equal(t, "f94200", hexOf(t, func(c *CborWriter) { c.WriteF64(3.0) }))

	// A float32 that is not representable as a float16 travels as a float32.
	require.Equal(t, "fa3dcccccd", hexOf(t, func(c *CborWriter) { c.WriteF32(0.1) }))
	// One that is, travels as a float16 — the width is the value's, not the
	// lane's.
	require.Equal(t, "f93c00", hexOf(t, func(c *CborWriter) { c.WriteF32(1) }))

	// A NaN payload does not survive: the writer's encoder is configured
	// NaNConvert7e00, so every NaN collapses to the one quiet NaN. That is a
	// deviation from ADR-0207 SD1's "NaN payloads survive", recorded here so
	// the loss is a tested fact rather than a surprise.
	payload := math.Float64frombits(0x7ff8_0000_0000_00ab)
	r = form(t, "f97e00", func(c *CborWriter) { c.WriteF64(payload) })
	require.True(t, math.IsNaN(r.ReadF64()))
	require.NoError(t, r.Err())
}

func TestTemporalForm(t *testing.T) {
	cases := []struct {
		name  string
		t     time.Time
		want  string
		secs  int64
		nanos int
	}{
		{"the epoch", time.Unix(0, 0), "d903e9a10100", 0, 0},
		{"a whole second", time.Unix(1, 0), "d903e9a10101", 1, 0},
		{"one second before the epoch", time.Unix(-1, 0), "d903e9a10120", -1, 0},
		{"nanoseconds are the second key", time.Unix(1234567890, 123456789),
			"d903e9a2011a499602d2281a075bcd15", 1234567890, 123456789},
		{"pre-epoch floors the seconds and keeps the nanoseconds non-negative",
			time.Date(1969, 12, 31, 23, 59, 59, 500_000_000, time.UTC),
			"d903e9a20120281a1dcd6500", -1, 500_000_000},
		{"a millisecond lane value", time.Unix(0, 1_000_000),
			"d903e9a20100281a000f4240", 0, 1_000_000},
	}
	for _, tc := range cases {
		r := form(t, tc.want, func(c *CborWriter) { c.WriteTemporal(tc.t) })
		got := r.ReadTemporal()
		require.NoError(t, r.Err(), tc.name)
		require.Equal(t, tc.secs, got.Unix(), tc.name)
		require.Equal(t, tc.nanos, got.Nanosecond(), tc.name)
		require.Equal(t, time.UTC, got.Location(), tc.name)
		require.True(t, got.Equal(tc.t), tc.name)
	}

	// A location is not content: the same instant in another zone is the same
	// bytes.
	berlin := time.FixedZone("CET", 3600)
	require.Equal(t, "d903e9a10101", hexOf(t, func(c *CborWriter) { c.WriteTemporal(time.Unix(1, 0).In(berlin)) }))

	// Encodings the writer never produces are refused.
	bad := []struct {
		name string
		item string
	}{
		{"zero nanoseconds must be omitted", "d903e9a201002800"},
		{"the keys must be in canonical order", "d903e9a2281a1dcd6500011a499602d2"},
		{"an unknown key", "d903e9a10200"},
		{"nanoseconds at one second", "d903e9a20100281a3b9aca00"},
		{"three entries", "d903e9a3010028000200"},
		{"the wrong tag", "d9010aa10100"},
		{"an array where the map belongs", "d903e9820100"},
	}
	for _, tc := range bad {
		r := rd(t, tc.item)
		r.ReadTemporal()
		require.Error(t, r.Err(), tc.name)
	}
}

func TestNetworkAddressForms(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want string
	}{
		{"IPv4", "1.2.3.4", "d8344401020304"},
		{"IPv4 zero", "0.0.0.0", "d8344400000000"},
		{"IPv6 loopback", "::1", "d8365000000000000000000000000000000001"},
		{"IPv6", "2001:db8::1", "d8365020010db8000000000000000000000001"},
		{"an IPv4-mapped address stays IPv6", "::ffff:1.2.3.4", "d8365000000000000000000000ffff01020304"},
	}
	for _, tc := range cases {
		a := netip.MustParseAddr(tc.addr)
		r := form(t, tc.want, func(c *CborWriter) { c.WriteAddr(a) })
		got := r.ReadAddr()
		require.NoError(t, r.Err(), tc.name)
		require.Equal(t, a, got, tc.name)
		require.Equal(t, a.Is4(), got.Is4(), tc.name)
	}

	// The family-specific readers refuse the other family's tag.
	r := rd(t, "d8344401020304")
	r.ReadIPv6()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
	r = rd(t, "d8365000000000000000000000000000000001")
	r.ReadIPv4()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)

	// So do the writers.
	_, err := encBytes(func(c *CborWriter) { c.WriteIPv6(netip.MustParseAddr("1.2.3.4")) })
	require.ErrorIs(t, err, ErrOutOfRange)
	_, err = encBytes(func(c *CborWriter) { c.WriteIPv4(netip.MustParseAddr("::ffff:1.2.3.4")) })
	require.ErrorIs(t, err, ErrOutOfRange, "an IPv4-mapped address is not IPv4 on this wire")
	_, err = encBytes(func(c *CborWriter) { c.WriteAddr(netip.Addr{}) })
	require.ErrorIs(t, err, ErrOutOfRange)
	_, err = encBytes(func(c *CborWriter) { c.WriteAddr(netip.MustParseAddr("fe80::1%eth0")) })
	require.ErrorIs(t, err, ErrOutOfRange, "a zone is not part of the form")

	// A width that does not match the tag, and a tag that is not a network
	// tag, are both refused.
	r = rd(t, "d8344501020304ff")
	r.ReadAddr()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
	r = rd(t, "d90102820102")
	r.ReadAddr()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
}

func TestNetworkPrefixForms(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
	}{
		{"trailing zero bytes are omitted", "10.0.0.0/8", "d8348208410a"},
		{"a partial byte keeps its set bits", "10.128.0.0/9", "d8348209420a80"},
		{"the whole IPv4 address", "1.2.3.4/32", "d8348218204401020304"},
		{"the default IPv4 route is an empty byte string", "0.0.0.0/0", "d834820040"},
		{"the default IPv6 route", "::/0", "d836820040"},
		{"an IPv6 prefix", "2001:db8::/32", "d8368218204420010db8"},
		{"the whole IPv6 address", "::1/128", "d8368218805000000000000000000000000000000001"},
		{"an IPv4-mapped prefix stays IPv6", "::ffff:1.2.3.4/128",
			"d8368218805000000000000000000000ffff01020304"},
	}
	for _, tc := range cases {
		p := netip.MustParsePrefix(tc.prefix)
		r := form(t, tc.want, func(c *CborWriter) { c.WritePrefix(p) })
		got := r.ReadPrefix()
		require.NoError(t, r.Err(), tc.name)
		require.Equal(t, p.Masked(), got, tc.name)
	}

	// Host bits are not content: an unmasked prefix travels masked, which is
	// the one place the form is not a bijection on its Go input.
	require.Equal(t, "d8348208410a", hexOf(t, func(c *CborWriter) {
		c.WritePrefix(netip.MustParsePrefix("10.1.2.3/8"))
	}))

	// The family-specific readers and writers refuse the other family.
	r := rd(t, "d8348208410a")
	r.ReadIPv6Prefix()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
	_, err := encBytes(func(c *CborWriter) { c.WriteIPv4Prefix(netip.MustParsePrefix("::/0")) })
	require.ErrorIs(t, err, ErrOutOfRange)

	// Encodings the writer never produces are refused.
	bad := []struct {
		name string
		item string
	}{
		{"a prefix longer than the address", "d8348218214501020304ff"},
		{"more bytes than the prefix needs", "d8348208420a00"},
		{"a trailing zero byte", "d8348210420000"},
		{"bits set beyond the prefix", "d8348209420ac0"},
		{"a one-element array", "d8348108"},
		{"an address where a prefix belongs", "d8344401020304"},
	}
	for _, tc := range bad {
		r := rd(t, tc.item)
		r.ReadPrefix()
		require.Error(t, r.Err(), tc.name)
	}
}

// The Raw variants are the lane shapes the generated readaccess getters
// return and the generated dml builders take: a bare IPv4 is a big-endian
// uint32, a bare IPv6 sixteen bytes, and a CIDR value its address bytes plus
// one trailing prefix-length byte.
func TestNetworkRawLanes(t *testing.T) {
	r := form(t, "d8344401020304", func(c *CborWriter) { c.WriteIPv4Raw(0x01020304) })
	require.Equal(t, uint32(0x01020304), r.ReadIPv4Raw())
	require.NoError(t, r.Err())

	var v6 [16]byte
	v6[0], v6[1], v6[15] = 0x20, 0x01, 0x01
	r = form(t, "d8365020010000000000000000000000000001", func(c *CborWriter) { c.WriteIPv6Raw(v6) })
	require.Equal(t, v6, r.ReadIPv6Raw())
	require.NoError(t, r.Err())

	r = form(t, "d8348208410a", func(c *CborWriter) { c.WriteIPv4PrefixRaw([5]byte{10, 0, 0, 0, 8}) })
	require.Equal(t, [5]byte{10, 0, 0, 0, 8}, r.ReadIPv4PrefixRaw())
	require.NoError(t, r.Err())

	// The masking the form applies is visible on the raw lane too.
	r = form(t, "d8348208410a", func(c *CborWriter) { c.WriteIPv4PrefixRaw([5]byte{10, 1, 2, 3, 8}) })
	require.Equal(t, [5]byte{10, 0, 0, 0, 8}, r.ReadIPv4PrefixRaw())
	require.NoError(t, r.Err())

	var p6 [17]byte
	p6[0], p6[1], p6[2], p6[3], p6[16] = 0x20, 0x01, 0x0d, 0xb8, 32
	r = form(t, "d8368218204420010db8", func(c *CborWriter) { c.WriteIPv6PrefixRaw(p6) })
	require.Equal(t, p6, r.ReadIPv6PrefixRaw())
	require.NoError(t, r.Err())

	// A prefix length wider than the address is refused at the writer.
	_, err := encBytes(func(c *CborWriter) { c.WriteIPv4PrefixRaw([5]byte{10, 0, 0, 0, 33}) })
	require.ErrorIs(t, err, ErrOutOfRange)
}

// The set form sorts bytewise on the encoded elements and keeps duplicates:
// set order is not content, but a set's length is — it is a co-container of
// the section's arrays — so a duplicate stays where the sort puts it.
func TestSetWriter(t *testing.T) {
	sw, err := NewSetWriter()
	require.NoError(t, err)

	collect := func(f func()) string {
		b, err := encBytes(func(c *CborWriter) {
			sw.Begin()
			f()
			sw.Flush(c)
		})
		require.NoError(t, err)
		requireCoreDet(t, b)
		return hex.EncodeToString(b)
	}
	uints := func(vs ...uint64) func() {
		return func() {
			for _, v := range vs {
				sw.Elem().WriteUint(v)
				sw.EndElem()
			}
		}
	}

	require.Equal(t, "d901028401020303", collect(uints(3, 1, 3, 2)), "m of [3,1,3,2] is the bytes of [1,2,3,3]")
	require.Equal(t, "d901028401020303", collect(uints(1, 2, 3, 3)), "and does not depend on the written order")
	require.Equal(t, "d9010283010203", collect(uints(3, 1, 2)), "distinct elements sort and stay three")
	require.Equal(t, "d9010280", collect(uints()), "an empty set")
	require.Equal(t, "d9010283010101", collect(uints(1, 1, 1)), "three equal elements stay three")

	// The order is bytewise over the encoded item, so the head's length leads:
	// "b" (0x6162) sorts before "aa" (0x626161), and the repeated "b" stays.
	require.Equal(t, "d9010284616261626261616461616161", collect(func() {
		for _, s := range []string{"aaaa", "b", "aa", "b"} {
			sw.Elem().WriteTextString(s)
			sw.EndElem()
		}
	}))

	// Elements are complete items of any shape, including nested ones.
	b, err := encBytes(func(c *CborWriter) {
		sw.Begin()
		e := sw.Elem()
		e.ArrayHead(2)
		e.WriteUint(2)
		e.WriteUint(1)
		sw.EndElem()
		e = sw.Elem()
		e.WriteTemporal(time.Unix(0, 0))
		sw.EndElem()
		require.Equal(t, 2, sw.Len())
		sw.Flush(c)
	})
	require.NoError(t, err)
	require.Equal(t, "d9010282820201d903e9a10100", hex.EncodeToString(b))
	requireCoreDet(t, b)

	// Reading the set back, ReadItemBytes hands the caller what it needs to
	// verify the ordering the form asserts.
	r := NewCborReader(b)
	require.Equal(t, 2, r.ReadSetHead())
	require.Equal(t, "820201", hex.EncodeToString(r.ReadItemBytes()))
	require.Equal(t, "d903e9a10100", hex.EncodeToString(r.ReadItemBytes()))
	require.NoError(t, r.Err())
	require.True(t, r.Done())

	// A misuse of the element protocol is reported, not silently accepted.
	sw.Begin()
	sw.Elem()
	sw.Elem()
	require.Error(t, sw.Err())
	sw.Begin()
	sw.EndElem()
	require.Error(t, sw.Err())
	sw.Begin()
	require.NoError(t, sw.Err(), "Begin starts over")
}

func TestReadSetHead(t *testing.T) {
	require.Equal(t, 3, rd(t, "d9010283010203").ReadSetHead())
	r := rd(t, "83010203")
	r.ReadSetHead()
	require.ErrorIs(t, r.Err(), ErrUnexpectedMajor)
	r = rd(t, "d9010183010203")
	r.ReadSetHead()
	require.ErrorIs(t, r.Err(), ErrOutOfRange)
}

func TestValueRoundTripProperties(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		secs := rapid.Int64Range(-4_000_000_000, 4_000_000_000).Draw(rt, "secs")
		nanos := rapid.Int64Range(0, 999_999_999).Draw(rt, "nanos")
		want := time.Unix(secs, nanos).UTC()
		f32 := math.Float32frombits(rapid.Uint32().Draw(rt, "f32"))
		var v4 [4]byte
		copy(v4[:], rapid.SliceOfN(rapid.Byte(), 4, 4).Draw(rt, "v4"))
		var v6 [16]byte
		copy(v6[:], rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "v6"))
		p4 := netip.PrefixFrom(netip.AddrFrom4(v4), rapid.IntRange(0, 32).Draw(rt, "bits4")).Masked()
		p6 := netip.PrefixFrom(netip.AddrFrom16(v6), rapid.IntRange(0, 128).Draw(rt, "bits6")).Masked()

		b, err := encBytes(func(c *CborWriter) {
			c.WriteTemporal(want)
			c.WriteF32(f32)
			c.WriteAddr(netip.AddrFrom4(v4))
			c.WriteAddr(netip.AddrFrom16(v6))
			c.WritePrefix(p4)
			c.WritePrefix(p6)
		})
		if err != nil {
			rt.Fatalf("write: %v", err)
		}
		r := NewCborReader(b)
		if got := r.ReadTemporal(); !got.Equal(want) {
			rt.Fatalf("temporal: got %v want %v", got, want)
		}
		got32 := r.ReadF32()
		if math.IsNaN(float64(f32)) {
			if !math.IsNaN(float64(got32)) {
				rt.Fatalf("f32: NaN did not survive")
			}
		} else if math.Float32bits(got32) != math.Float32bits(f32) {
			rt.Fatalf("f32: got %v want %v", got32, f32)
		}
		if got := r.ReadAddr(); got != netip.AddrFrom4(v4) {
			rt.Fatalf("v4: got %v", got)
		}
		if got := r.ReadAddr(); got != netip.AddrFrom16(v6) {
			rt.Fatalf("v6: got %v", got)
		}
		if got := r.ReadPrefix(); got != p4 {
			rt.Fatalf("p4: got %v want %v", got, p4)
		}
		if got := r.ReadPrefix(); got != p6 {
			rt.Fatalf("p6: got %v want %v", got, p6)
		}
		if err := r.Err(); err != nil {
			rt.Fatalf("read: %v", err)
		}
		if !r.Done() {
			rt.Fatalf("%d bytes left over", r.Remaining())
		}
	})
}
