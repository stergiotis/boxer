package naturalkey

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-json-experiment/json"
	"github.com/stergiotis/boxer/public/identity/identifier"
	cbor2 "github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/unittest"
	"github.com/stretchr/testify/require"
)

func TestNaturalKeyEncoderHappy(t *testing.T) {
	enc := NewEncoder()
	enc.Begin()
	enc.AddBool(true)
	enc.AddBytes([]byte{0, 1, 2})
	enc.AddInt8(-1)
	enc.AddInt16(-2)
	enc.AddInt32(-3)
	enc.AddInt64(-4)
	enc.AddUint8(4)
	enc.AddUint16(5)
	enc.AddUint32(6)
	enc.AddUint64(7)
	enc.AddStr("hello!")
	id := identifier.TagValue(3).GetTag().ComposeId(identifier.UntaggedId(17))
	enc.AddId(id)
	ts := time.Date(2008, 5, 2, 13, 5, 8, 0, time.Local)
	enc.AddTimeUTC(ts)
	key, err := enc.End(SerializationFormatCbor)
	unittest.NoError(t, err)
	var r any
	err = cbor.Unmarshal(key, &r)
	require.NoError(t, err)
	require.EqualValues(t, []any{true, []byte{0, 1, 2}, int64(-1), int64(-2), int64(-3), int64(-4), uint64(4), uint64(5), uint64(6), uint64(7), "hello!", cbor.Tag{
		Number:  uint64(cbor2.TagIdentifier),
		Content: id.Value(),
	}, ts}, r)

	key, err = enc.End(SerializationFormatJson)
	unittest.NoError(t, err)
	err = json.Unmarshal(key, &r)
	unittest.NoError(t, err)
	tidjson := EncodeTaggedIdJson(id)
	require.EqualValues(t, []any{true, "AAEC", float64(-1), float64(-2), float64(-3), float64(-4), float64(4), float64(5), float64(6), float64(7), "hello!", tidjson, ts.Format(time.RFC3339)}, r)
	require.True(t, IsTaggedIdJson(tidjson))
	require.EqualValues(t, id, DecodeTaggedIdJson(tidjson))
}
