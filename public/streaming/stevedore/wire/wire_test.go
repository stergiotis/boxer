package wire

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestFramesRoundTripEveryCodec(t *testing.T) {
	for _, codec := range AllCodecs {
		t.Run(codec.String(), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				gen := rapid.SliceOfN(rapid.Byte(), 0, 512)
				if codec == CodecLines {
					gen = rapid.SliceOfN(rapid.Byte().Filter(func(b byte) bool { return b != '\n' && b != '\r' }), 0, 512)
				}
				frames := rapid.SliceOfN(gen, 0, 8).Draw(rt, "frames")
				var buf bytes.Buffer
				w := NewFrameWriter(&buf, codec)
				for _, f := range frames {
					require.NoError(rt, w.Write(f))
				}
				r := NewFrameReader(&buf, codec, 0)
				for i, want := range frames {
					got, err := r.Read()
					require.NoError(rt, err, "frame %d", i)
					require.Equal(rt, want, got, "frame %d", i)
				}
				_, err := r.Read()
				require.ErrorIs(rt, err, io.EOF)
			})
		})
	}
}

func TestFrameFixtures(t *testing.T) {
	cases := []struct {
		codec CodecE
		bytes string
		want  []string
	}{
		{CodecLines, "a\nbc\n", []string{"a", "bc"}},
		{CodecLines, "a\r\n\n", []string{"a", ""}},
		{CodecLengthPrefixedUint32BE, "\x00\x00\x00\x02hi\x00\x00\x00\x00", []string{"hi", ""}},
		{CodecNetstring, "2:hi,0:,", []string{"hi", ""}},
	}
	for _, c := range cases {
		r := NewFrameReader(bytes.NewBufferString(c.bytes), c.codec, 0)
		for _, want := range c.want {
			got, err := r.Read()
			require.NoError(t, err)
			require.Equal(t, want, string(got))
		}
		_, err := r.Read()
		require.ErrorIs(t, err, io.EOF)
	}
}

func TestFrameRefusals(t *testing.T) {
	t.Run("oversize is skipped and the next frame is read", func(t *testing.T) {
		for _, codec := range []CodecE{CodecLengthPrefixedUint32BE, CodecNetstring} {
			var buf bytes.Buffer
			w := NewFrameWriter(&buf, codec)
			require.NoError(t, w.Write(bytes.Repeat([]byte{'x'}, 256)))
			require.NoError(t, w.Write([]byte("ok")))
			r := NewFrameReader(&buf, codec, 16)
			_, err := r.Read()
			require.ErrorIs(t, err, ErrFrameTooLarge)
			got, err := r.Read()
			require.NoError(t, err)
			require.Equal(t, "ok", string(got))
			_, err = r.Read()
			require.ErrorIs(t, err, io.EOF)
		}
	})
	t.Run("oversize line drains to the next frame", func(t *testing.T) {
		in := append(bytes.Repeat([]byte{'x'}, 10_000), '\n', 'o', 'k', '\n')
		r := NewFrameReader(bytes.NewBuffer(in), CodecLines, 16)
		_, err := r.Read()
		require.ErrorIs(t, err, ErrFrameTooLarge)
		got, err := r.Read()
		require.NoError(t, err)
		require.Equal(t, "ok", string(got))
	})
	t.Run("truncated payload is an error, not EOF", func(t *testing.T) {
		r := NewFrameReader(bytes.NewBufferString("5:hi"), CodecNetstring, 0)
		_, err := r.Read()
		require.Error(t, err)
		require.False(t, errors.Is(err, io.EOF))
	})
	t.Run("netstring without comma", func(t *testing.T) {
		r := NewFrameReader(bytes.NewBufferString("2:hi;"), CodecNetstring, 0)
		_, err := r.Read()
		require.Error(t, err)
	})
	t.Run("netstring with non-digit", func(t *testing.T) {
		r := NewFrameReader(bytes.NewBufferString("2x:hi,"), CodecNetstring, 0)
		_, err := r.Read()
		require.Error(t, err)
	})
	t.Run("newline under lines codec is refused on write", func(t *testing.T) {
		var buf bytes.Buffer
		require.Error(t, NewFrameWriter(&buf, CodecLines).Write([]byte("a\nb")))
		require.Zero(t, buf.Len())
	})
}

func TestParseCodec(t *testing.T) {
	for _, c := range AllCodecs {
		got, err := ParseCodec(c.String())
		require.NoError(t, err)
		require.Equal(t, c, got)
	}
	_, err := ParseCodec("cbor")
	require.Error(t, err)
}

func TestPartsRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		parts := rapid.SliceOfN(rapid.SliceOfN(rapid.Byte(), 0, 64), 0, 16).Draw(rt, "parts")
		b := SerializeParts(parts)
		require.Len(rt, b, SerializedPartsLen(parts))
		got, err := DeserializeParts(b)
		require.NoError(rt, err)
		require.Len(rt, got, len(parts))
		for i := range parts {
			require.Equal(rt, parts[i], got[i])
		}
	})
}

func TestPartsFixture(t *testing.T) {
	// Two parts, "ab" and "": count 2, then 2+"ab", then 0.
	b := SerializeParts([][]byte{[]byte("ab"), {}})
	require.Equal(t, "\x00\x00\x00\x02\x00\x00\x00\x02ab\x00\x00\x00\x00", string(b))
	parts, err := DeserializeParts(b)
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte("ab"), {}}, parts)
}

func TestPartsRefusals(t *testing.T) {
	for name, in := range map[string]string{
		"short count":         "\x00\x00",
		"count past bytes":    "\x00\x00\x00\x09",
		"length past end":     "\x00\x00\x00\x01\x00\x00\x00\x05ab",
		"trailing bytes":      "\x00\x00\x00\x01\x00\x00\x00\x01a!",
		"count over capacity": "\xff\xff\xff\xff\x00",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeParts([]byte(in))
			require.ErrorIs(t, err, ErrBadParts)
		})
	}
}
