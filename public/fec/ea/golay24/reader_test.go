package golay24

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/ea"
)

func TestPebbleReader24Fuzzing(t *testing.T) {
	buf := &bytes.Buffer{}
	v := &bytes.Buffer{}
	data := &bytes.Buffer{}
	ra := rand.New(rand.NewSource(time.Now().UnixNano()))

	laps := 1000
	if testing.Short() {
		laps = 10
	}
	for l := 0; l < laps; l++ {
		buf.Reset()
		v.Reset()
		data.Reset()

		nAnchorBytes := uint8(ra.Intn(12) + 2)
		w := NewWriter(buf, nAnchorBytes)
		var err error
		{ // garbage
			nBytes := ra.Intn(4096)
			for i := 0; i < nBytes; i++ {
				buf.WriteByte(byte(ra.Intn(0xff + 1)))
			}
		}
		_, err = w.BeginMessage()
		require.NoError(t, err)

		nBytes := ra.Intn(4 * 4096)
		data.Grow(nBytes)
		for i := 0; i < nBytes; i++ {
			err = data.WriteByte(byte(ra.Intn(0xff + 1)))
			require.NoError(t, err)
		}
		_, _, _, _, err = ea.TransferDataWithSplitReadAndWrites(w, nBytes, data, 100, ra)
		require.NoError(t, err)
		_, err = w.EndMessage()
		require.NoError(t, err)

		r := NewGolay24Reader(buf, nAnchorBytes, 0, uint32(2*nBytes+3))
		_, _, _, _, err = ea.TransferDataWithSplitReadAndWrites(v, nBytes, r, 100, ra)
		require.NoError(t, err)
		le := data.Len()
		require.EqualValues(t, data.Bytes(), v.Bytes()[:le])
	}
}

func TestPebbleReader24(t *testing.T) {
	check := func(ra *rand.Rand) {
		buf := &bytes.Buffer{}
		nAnchorBytes := uint8(ra.Intn(13))
		w := NewWriter(buf, nAnchorBytes)
		var err error
		var n int
		n, err = w.BeginMessage()
		require.NoError(t, err)
		require.EqualValues(t, nAnchorBytes, n)

		nBytes := ra.Intn(4096)
		data := make([]byte, 0, nBytes)
		for i := 0; i < nBytes; i++ {
			data = append(data, byte(ra.Intn(0xff+1)))
		}
		_, err = w.Write(data)
		require.NoError(t, err)
		var paddingBits int
		paddingBits, err = w.EndMessage()
		require.NoError(t, err)

		v := &bytes.Buffer{}
		r := NewGolay24Reader(buf, nAnchorBytes, 0, uint32(2*nBytes+3))
		_, _, _, _, err = ea.TransferDataWithSplitReadAndWrites(v, nBytes, r, 100, ra)
		require.NoError(t, err)
		switch paddingBits {
		case 0:
			require.EqualValues(t, data, v.Bytes()) // FIXME flaky test
			break
		case 4, 8:
			l := len(data)
			require.EqualValues(t, data[:l], v.Bytes()[:l])
			break
		default:
			require.Fail(t, "invalid number of padding bits")
		}
	}
	ra := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		check(ra)
	}
}
