package cbor

import (
	"bytes"
	"encoding/json"
	"github.com/stergiotis/boxer/public/ea"
	"io"
	"math/rand"
	"testing"
	"time"

	fxamacker "github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func populateCborFromJson(w io.Writer, js string) error {
	var d interface{}
	err := json.Unmarshal([]byte(js), &d)
	if err != nil {
		return err
	}
	encmode, err := fxamacker.CanonicalEncOptions().EncMode()
	if err != nil {
		return err
	}
	return encmode.NewEncoder(w).Encode(d)
}

func TestCborConsumer(t *testing.T) {
	doc := &bytes.Buffer{}
	skipper := NewPullParser(doc)
	out := &bytes.Buffer{}
	docReader, err := ea.NewByteBlockReaderDiscardReader(doc)
	require.NoError(t, err)
	check := func(js string) {
		log.Info().Str("js", js).Msg("checking")
		require.NoError(t, populateCborFromJson(doc, js))
		// append random bytes to see if we complete without using the EOF information (mid-stream)
		nRandomBytesSuffix := rand.Intn(128) + 1
		for i := 0; i < nRandomBytesSuffix; i++ {
			doc.WriteByte(byte(rand.Uint32()))
		}
		docReader, err = ea.NewByteBlockReaderDiscardReader(doc)
		require.NoError(t, err)
		out.Reset()
		skipper.Reset(docReader)
		before := doc.Len()
		token, _, bytesToConsumeBeforeNextCall, _, err := skipper.Consume()
		for err == nil && token != TokenFinished {
			if bytesToConsumeBeforeNextCall > 0 {
				_, err = docReader.Discard(int(bytesToConsumeBeforeNextCall))
				require.NoError(t, err)
			}
			after := doc.Len()
			require.EqualValues(t, before-after, skipper.BytesConsumed())
			token, _, bytesToConsumeBeforeNextCall, _, err = skipper.Consume()
		}
		require.NoError(t, err)
		require.Equal(t, TokenFinished, token)
		var m int
		m, err = docReader.Discard(nRandomBytesSuffix)
		require.NoError(t, err)
		require.EqualValues(t, nRandomBytesSuffix, m)
		_, err = docReader.Discard(1)
		require.Equal(t, io.EOF, err)
	}
	check("[1,2,3,[4,5]]")
	check("true")
	check("[]")
	check("{}")
	check("{\"abc\":true}")
	check("{\"abc\":[{}]}")
	check("{\"abc\":[\"u\"]}")
	check("[1,2,3]")
}

func BenchmarkCborConsumer(b *testing.B) {
	buf := &bytes.Buffer{}
	totalBytes := int64(0)
	skipper := NewPullParser(nil)
	gen := NewGenerator(buf, time.Now().UnixNano(), "abcdefghijklmnopqrstuvwxyz")
	b.ResetTimer()
	tmp := &bytes.Buffer{}
	for lap := 0; lap < b.N; lap++ {
		buf.Reset()
		gen.Reset()
		nBytes, err := gen.GenerateRandomCbor()
		totalBytes += int64(nBytes)
		skipper.Reset(buf)
		require.NoError(b, err)
		b.StartTimer()
		token, _, bytesToConsumeBeforeNextCall, _, err := skipper.Consume()
		for err != nil && token != TokenFinished {
			if bytesToConsumeBeforeNextCall > 0 {
				tmp.Reset()
				tmp.Grow(int(bytesToConsumeBeforeNextCall))
				_, err = buf.Read(tmp.Bytes()[:bytesToConsumeBeforeNextCall])
				require.NoError(b, err)
			}
			token, _, bytesToConsumeBeforeNextCall, _, err = skipper.Consume()
		}
		b.StopTimer()
		require.NoError(b, err)
	}
	b.SetBytes(totalBytes / int64(b.N))
}
