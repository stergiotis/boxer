package buscodec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundtripPayload exercises the json: tag fallback path — fxamacker/cbor
// reads cbor: tags first and falls back to json:, so existing payload
// types migrate without retagging.
type roundtripPayload struct {
	Name  string `json:"name"`
	Count int32  `json:"count"`
	Data  []byte `json:"data,omitempty"`
	Flag  bool   `json:"flag,omitempty"`
}

func TestDefaultIsCBOR(t *testing.T) {
	c := Default()
	assert.Equal(t, "cbor", c.Name())
	assert.Equal(t, "application/cbor", c.ContentType())
}

func TestRoundTripCBOR(t *testing.T) {
	in := roundtripPayload{Name: "x", Count: 42, Data: []byte{1, 2, 3}, Flag: true}
	b, err := Encode(in)
	require.NoError(t, err)
	require.NotEmpty(t, b)
	out, err := Decode[roundtripPayload](b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestRoundTripJSON(t *testing.T) {
	c := NewJSON()
	assert.Equal(t, "json", c.Name())
	in := roundtripPayload{Name: "y", Count: 7}
	b, err := c.Encode(in)
	require.NoError(t, err)
	var out roundtripPayload
	err = c.Decode(b, &out)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestSetDefaultSwapsAndRestores(t *testing.T) {
	original := Default()
	t.Cleanup(func() { SetDefault(original) })
	SetDefault(NewJSON())
	assert.Equal(t, "json", Default().Name())
	SetDefault(original)
	assert.Equal(t, "cbor", Default().Name())
}

func TestSetDefaultNilIsNoOp(t *testing.T) {
	before := Default().Name()
	SetDefault(nil)
	assert.Equal(t, before, Default().Name())
}

func TestEncodeErrorNamesType(t *testing.T) {
	// Channels can't be encoded by either codec — verify the wrap names
	// the offending type so brokers get actionable diagnostics.
	_, err := Encode(make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "buscodec: encode")
	assert.Contains(t, err.Error(), "chan int")
}

func TestDecodeErrorNamesType(t *testing.T) {
	_, err := Decode[roundtripPayload]([]byte{0xff, 0xff, 0xff, 0xff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "buscodec: decode")
}

type capturePublisher struct {
	subject string
	payload []byte
}

func (inst *capturePublisher) Publish(subject string, payload []byte) (err error) {
	inst.subject = subject
	inst.payload = append([]byte(nil), payload...)
	return
}

func TestReplyEncodesAndPublishes(t *testing.T) {
	pub := &capturePublisher{}
	in := roundtripPayload{Name: "z", Count: 99}
	err := Reply(pub.Publish, "test.subject", in)
	require.NoError(t, err)
	assert.Equal(t, "test.subject", pub.subject)
	out, err := Decode[roundtripPayload](pub.payload)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestCanonicalEncodingIsDeterministic(t *testing.T) {
	// Wire-byte determinism is a guarantee callers rely on (replay
	// diffing, content-addressing). Encode the same value twice and
	// require identical output.
	in := roundtripPayload{Name: "det", Count: 1, Data: []byte{9, 8, 7}}
	b1, err := Encode(in)
	require.NoError(t, err)
	b2, err := Encode(in)
	require.NoError(t, err)
	assert.Equal(t, b1, b2)
}

// --- Per-type registry (ADR-0042 M12). ---

// recordingCodec is a CodecI that records each Encode / Decode call so
// the registry-dispatch tests can verify that a registered codec
// actually receives the call (rather than the value silently falling
// back to Default()).
type recordingCodec struct {
	name      string
	encodeN   int
	decodeN   int
	encodeOut []byte // payload to return from Encode (mock wire)
	decodeFn  func([]byte, any) error
}

var _ CodecI = (*recordingCodec)(nil)

func (inst *recordingCodec) Name() string        { return inst.name }
func (inst *recordingCodec) ContentType() string { return "application/x-" + inst.name }
func (inst *recordingCodec) Encode(v any) (b []byte, err error) {
	inst.encodeN++
	b = append([]byte(nil), inst.encodeOut...)
	return
}
func (inst *recordingCodec) Decode(b []byte, v any) (err error) {
	inst.decodeN++
	if inst.decodeFn != nil {
		err = inst.decodeFn(b, v)
	}
	return
}

// fakePayload is a distinct type for the registry tests so registering
// it doesn't collide with roundtripPayload above.
type fakePayload struct {
	Note string
}

func TestLookupFallsBackToDefaultForUnregistered(t *testing.T) {
	got := Lookup[fakePayload]()
	assert.Equal(t, Default().Name(), got.Name())
}

func TestRegisterRoutesEncodeAndDecodeThroughRegistry(t *testing.T) {
	t.Cleanup(func() { Register[fakePayload](nil) })

	rc := &recordingCodec{
		name:      "fake",
		encodeOut: []byte{0xa, 0xb, 0xc},
		decodeFn: func(b []byte, v any) error {
			ptr, ok := v.(*fakePayload)
			if !ok {
				return assert.AnError
			}
			*ptr = fakePayload{Note: "from-registry"}
			return nil
		},
	}
	Register[fakePayload](rc)

	b, err := Encode(fakePayload{Note: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, []byte{0xa, 0xb, 0xc}, b)
	assert.Equal(t, 1, rc.encodeN)

	out, err := Decode[fakePayload]([]byte{0xa, 0xb, 0xc})
	require.NoError(t, err)
	assert.Equal(t, "from-registry", out.Note)
	assert.Equal(t, 1, rc.decodeN)
}

func TestRegisterNilUnregisters(t *testing.T) {
	rc := &recordingCodec{name: "fake", encodeOut: []byte{0x01}}
	Register[fakePayload](rc)
	assert.Equal(t, "fake", Lookup[fakePayload]().Name())

	Register[fakePayload](nil)
	assert.Equal(t, Default().Name(), Lookup[fakePayload]().Name())
}

func TestRegisterIsTypeScoped(t *testing.T) {
	t.Cleanup(func() {
		Register[fakePayload](nil)
		Register[roundtripPayload](nil)
	})
	rcFake := &recordingCodec{name: "fakeCodec", encodeOut: []byte{0xff}}
	Register[fakePayload](rcFake)

	// roundtripPayload is NOT registered → falls back to Default (CBOR).
	rt := roundtripPayload{Name: "still-cbor", Count: 1}
	b, err := Encode(rt)
	require.NoError(t, err)
	require.NotEmpty(t, b)
	assert.NotEqual(t, []byte{0xff}, b, "roundtripPayload should not hit fakeCodec")
	assert.Equal(t, 0, rcFake.encodeN, "fakeCodec should not see roundtripPayload")

	// fakePayload IS registered → uses fakeCodec.
	_, err = Encode(fakePayload{Note: "x"})
	require.NoError(t, err)
	assert.Equal(t, 1, rcFake.encodeN)
}

func TestReplyHonoursPerTypeRegistration(t *testing.T) {
	t.Cleanup(func() { Register[fakePayload](nil) })
	rc := &recordingCodec{name: "fake", encodeOut: []byte{0xab, 0xcd}}
	Register[fakePayload](rc)

	pub := &capturePublisher{}
	err := Reply(pub.Publish, "subj", fakePayload{Note: "z"})
	require.NoError(t, err)
	assert.Equal(t, "subj", pub.subject)
	assert.Equal(t, []byte{0xab, 0xcd}, pub.payload)
	assert.Equal(t, 1, rc.encodeN)
}
