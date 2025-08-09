package golay24

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/fec/anchor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/fec/code/golay24"
)

func TestPebbleWriter24Padding4(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf, 0)
	var n int
	var err error
	n, err = w.BeginMessage()
	require.Zero(t, n)
	require.NoError(t, err)
	err = w.WriteByte(0xe3)
	require.NoError(t, err)
	var paddingBits int
	paddingBits, err = w.EndMessage()
	require.NoError(t, err)
	require.EqualValues(t, 3, buf.Len())
	require.EqualValues(t, 3, w.totalBytesWritten)
	require.EqualValues(t, 4, paddingBits)
	require.EqualValues(t, golay24.EncodingUint8Triples[3*0xe30:3*0xe30+3], buf.Bytes())
}

func TestPebbleWriter24Padding8(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf, 0)
	var n int
	var err error
	n, err = w.BeginMessage()
	require.Zero(t, n)
	require.NoError(t, err)
	err = w.WriteByte(0x7f)
	require.NoError(t, err)
	err = w.WriteByte(0x3e)
	require.NoError(t, err)
	var paddingBits int
	paddingBits, err = w.EndMessage()
	require.NoError(t, err)
	require.EqualValues(t, 6, buf.Len())
	require.EqualValues(t, 6, w.totalBytesWritten)
	require.EqualValues(t, 8, paddingBits)
	require.EqualValues(t, golay24.EncodingUint8Triples[3*0x7f3:3*0x7f3+3], buf.Bytes()[:3])
	require.EqualValues(t, golay24.EncodingUint8Triples[3*0xe00:3*0xe00+3], buf.Bytes()[3:6])
}

func TestPebbleWriter24Regular(t *testing.T) {
	ra := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	check := func(m uint16, nAnchorBytes uint8, split bool) {
		//log.Debug().Uint16("m", m).Int("nAnchorBytes", nAnchorBytes).Bool("split", split).Msg("executing check")
		buf := &bytes.Buffer{}
		w := NewWriter(buf, nAnchorBytes)
		var n int
		var err error
		n, err = w.BeginMessage()
		require.NoError(t, err)
		require.EqualValues(t, nAnchorBytes, n)
		require.EqualValues(t, n, w.totalBytesWritten)
		require.EqualValues(t, n, buf.Len())
		for i := 0; i < n; i++ {
			b, err := buf.ReadByte()
			require.NoError(t, err)
			require.EqualValues(t, anchor.MagicByteAnchor, b)
		}
		nibbles := make([]byte, 0, 3*m)
		for i := uint16(0); i < m; i++ {
			nibbles = append(nibbles, byte(i>>8))
			nibbles = append(nibbles, byte(i>>4&0x0f))
			nibbles = append(nibbles, byte(i&0x0f))
		}
		if len(nibbles)%2 == 1 {
			nibbles = append(nibbles, 0x00)
		}
		if split {
			b := &bytes.Buffer{}
			for i := 0; i < len(nibbles); i += 2 {
				err = b.WriteByte(nibbles[i]<<4 | nibbles[i+1])
				require.NoError(t, err)
			}
			_, _, _, n, err = ea.TransferDataWithSplitReadAndWrites(w, b.Len(), b, 10, ra)
			require.NoError(t, err)
			require.EqualValues(t, n, len(nibbles)/2)
		} else {
			for i := 0; i < len(nibbles); i += 2 {
				err = w.WriteByte(nibbles[i]<<4 | nibbles[i+1])
				require.NoError(t, err)
			}
		}

		paddingBits, err := w.EndMessage()
		assert.EqualValues(t, (12-len(nibbles)*4%12)%12, paddingBits)
		require.NoError(t, err)
		if m > 0 {
			require.EqualValues(t, golay24.EncodingUint8Triples[:3*m], buf.Bytes()[:3*m])
		}
	}
	driver := func(split bool) {
		check(0, 0, split)
		check(1, 1, split)
		check(2, 2, split)
		check(3, 7, split)
		check(4, 3, split)
		check(5, 5, split)
		check(6, 5, split)
		check(100, 5, split)
		check(101, 6, split)
		check(102, 7, split)
		check(4095, 1, split)
	}
	driver(false)
	for i := 0; i < 10; i++ {
		driver(true)
	}
}
