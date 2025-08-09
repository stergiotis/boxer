package cbor

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"net/netip"
	"reflect"
	"testing"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

func TestEncoderSmoke(t *testing.T) {
	var val interface{}
	var dec *cbor.Decoder
	buf := &bytes.Buffer{}
	const samples = 128

	var err error
	enc := NewEncoder(buf, xxh3.New())
	tagSet := cbor.NewTagSet()
	err = tagSet.Add(cbor.TagOptions{
		DecTag: cbor.DecTagRequired,
		EncTag: cbor.EncTagRequired,
	}, reflect.TypeOf(netip.Addr{}), uint64(TagIPv4))
	require.NoError(t, err)
	var decMode cbor.DecMode
	decMode, err = cbor.DecOptions{}.DecModeWithTags(tagSet)
	require.NoError(t, err)
	check := func(refValu interface{}, handler func(enc *Encoder) (int, error)) {
		enc.Reset()
		var n int
		n, err = handler(enc)

		dec = decMode.NewDecoder(buf)
		require.NoError(t, err)
		require.EqualValues(t, buf.Len(), n)
		h := xxh3.New()
		_, err = h.Write(buf.Bytes())
		require.NoError(t, err)
		h1 := h.Sum(make([]byte, 0, 8))
		require.NoError(t, dec.Decode(&val))
		switch val.(type) {
		case float32, float64:
			require.EqualValues(t, fmt.Sprintf("%g", refValu), fmt.Sprintf("%g", val))
		default:
			require.EqualValues(t, refValu, val)
		}
		require.EqualValues(t, n, dec.NumBytesRead())

		h2 := make([]byte, 0, 8)
		h2, err = enc.Hash(h2)
		require.NoError(t, err)
		require.EqualValues(t, h1, h2)
	}
	check(nil, func(enc *Encoder) (int, error) { return enc.EncodeNil() })
	check(true, func(enc *Encoder) (int, error) { return enc.EncodeBool(true) })
	check(false, func(enc *Encoder) (int, error) { return enc.EncodeBool(false) })
	check(uint64(math.MaxUint64), func(enc *Encoder) (int, error) { return enc.EncodeUint(math.MaxUint64) })
	check(0, func(enc *Encoder) (int, error) { return enc.EncodeUint(0) })
	for i := 0; i < samples; i++ {
		v := rand.Uint64()
		check(v, func(enc *Encoder) (int, error) { return enc.EncodeUint(v) })
	}
	check("", func(enc *Encoder) (int, error) { return enc.EncodeString("") })
	check("a", func(enc *Encoder) (int, error) { return enc.EncodeString("a") })
	check("\"", func(enc *Encoder) (int, error) { return enc.EncodeString("\"") })
	check("ä", func(enc *Encoder) (int, error) { return enc.EncodeString("ä") })
	check("äöüé! ", func(enc *Encoder) (int, error) { return enc.EncodeString("äöüé! ") })
	check([]byte{}, func(enc *Encoder) (int, error) { return enc.EncodeByteSlice([]byte{}) })
	check([]byte{0x00}, func(enc *Encoder) (int, error) { return enc.EncodeByteSlice([]byte{0x00}) })
	check([]byte{0x00, 0x0f}, func(enc *Encoder) (int, error) { return enc.EncodeByteSlice([]byte{0x00, 0x0f}) })
	check([]byte{0x00, 0x0f, 0xff}, func(enc *Encoder) (int, error) { return enc.EncodeByteSlice([]byte{0x00, 0x0f, 0xff}) })
	check(-1, func(enc *Encoder) (int, error) { return enc.EncodeInt(-1) })
	check(-1000, func(enc *Encoder) (int, error) { return enc.EncodeInt(-1000) })
	check(int64(math.MinInt64), func(enc *Encoder) (int, error) { return enc.EncodeInt(math.MinInt64) })
	check([]interface{}{uint64(1), uint64(2), uint64(3), true}, func(enc *Encoder) (n int, err error) {
		var u int
		u, err = enc.EncodeArrayIndefinite()
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(1)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(2)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(3)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeBool(true)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeBreak()
		if err != nil {
			return
		}
		n += u
		return
	})
	check([]interface{}{uint64(1), uint64(2), uint64(3), true}, func(enc *Encoder) (n int, err error) {
		var u int
		u, err = enc.EncodeArrayDefinite(4)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(1)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(2)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeUint(3)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeBool(true)
		if err != nil {
			return
		}
		n += u
		return
	})
	check(cbor.Tag{Number: uint64(TagEncodedCBORSequence), Content: []byte{0xf5}}, func(enc *Encoder) (n int, err error) {
		var u int
		u, err = enc.EncodeTag8(TagEncodedCBORSequence)
		if err != nil {
			return
		}
		n += u
		u, err = enc.EncodeByteSlice([]byte{0xf5})
		if err != nil {
			return
		}
		n += u
		return
	})
	for i := 0; i < 100000; i++ {
		n := rand.Uint64()
		check(n, func(enc *Encoder) (int, error) {
			return enc.EncodeUint(n)
		})
	}
	for i := 0; i < 100000; i++ {
		n := rand.Float64()
		check(n, func(enc *Encoder) (int, error) {
			return enc.EncodeFloat64(n)
		})
	}
	for _, n := range []float64{math.MaxFloat64, -math.MaxFloat64, math.NaN(), math.Inf(1), math.Inf(-1)} {
		check(n, func(enc *Encoder) (int, error) {
			return enc.EncodeFloat64(n)
		})
	}
	check(netip.MustParseAddr("127.0.0.1"), func(enc *Encoder) (n int, err error) {
		return enc.EncodeIpAddr(netip.MustParseAddr("127.0.0.1"))
	})
}
