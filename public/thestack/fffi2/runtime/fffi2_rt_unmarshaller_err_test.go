package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectingUnmarshaller wires an error handler that records rather than
// panics, so a test can assert both the sticky error and how many times the
// handler was told about it.
func collectingUnmarshaller(r io.Reader) (u *Unmarshaller, seen *[]error) {
	got := make([]error, 0, 8)
	u = NewUnmarshaller(r, binary.LittleEndian, func(err error) { got = append(got, err) }, nil)
	return u, &got
}

// Regression, 2026-07-24 review: a failed read returned the zero value and
// only logged, so a dead pipe was indistinguishable from the peer sending
// zeros. Nothing on the reader said "this is not real data".
func TestUnmarshallerReportsReadFailure(t *testing.T) {
	u, _ := collectingUnmarshaller(&bytes.Buffer{}) // empty: every read is EOF
	require.NoError(t, u.Err(), "a fresh reader must start clean")

	v := u.ReadUInt32()
	assert.Equal(t, uint32(0), v, "the zero value is still returned")
	require.Error(t, u.Err(), "but the failure must now be visible")
	assert.ErrorIs(t, u.Err(), io.EOF)
}

// A truncated frame is the dangerous case: the read consumed real bytes and
// then ran out, so the position is inside a frame.
func TestUnmarshallerReportsTruncatedRead(t *testing.T) {
	u, _ := collectingUnmarshaller(bytes.NewReader([]byte{1, 2})) // want 4
	_ = u.ReadUInt32()
	require.Error(t, u.Err())
	assert.ErrorIs(t, u.Err(), io.ErrUnexpectedEOF)
}

// hiccupReader delivers head, then fails once, then delivers tail. It models
// a stream that breaks partway through a frame and keeps producing bytes
// afterwards — the case where reading on silently resynchronises onto the
// wrong byte boundary rather than stopping.
type hiccupReader struct {
	head []byte
	err  error
	tail []byte
}

func (inst *hiccupReader) Read(p []byte) (n int, err error) {
	if len(inst.head) > 0 {
		n = copy(p, inst.head)
		inst.head = inst.head[n:]
		return
	}
	if inst.err != nil {
		err = inst.err
		inst.err = nil
		return
	}
	if len(inst.tail) == 0 {
		err = io.EOF
		return
	}
	n = copy(p, inst.tail)
	inst.tail = inst.tail[n:]
	return
}

// Once the position is unknown, reading on decodes whatever follows as if it
// were the next field. The reader must stay stopped rather than
// resynchronising itself onto arbitrary bytes.
func TestUnmarshallerStopsReadingAfterFailure(t *testing.T) {
	boom := errors.New("pipe hiccup")
	// Two bytes of a uint32, then a failure, then a further four bytes that
	// must never be handed out as the next field.
	src := &hiccupReader{head: []byte{1, 2}, err: boom, tail: []byte{0xAA, 0xBB, 0xCC, 0xDD}}
	u, _ := collectingUnmarshaller(src)

	_ = u.ReadUInt32() // consumes {1,2}, then fails mid-field
	require.Error(t, u.Err())
	first := u.Err()

	assert.Equal(t, uint32(0), u.ReadUInt32(), "a stopped reader must not decode the bytes after the break")
	assert.Equal(t, uint8(0), u.ReadUInt8())
	assert.Equal(t, "", u.ReadString())
	assert.Nil(t, u.ReadBytes())
	assert.Same(t, first, u.Err(), "the first failure must remain the reported one")
}

// The error handler used to fire once per read, which is how a single
// broken pipe turned into a flood on stderr.
func TestUnmarshallerReportsEachFailureEpochOnce(t *testing.T) {
	u, seen := collectingUnmarshaller(&bytes.Buffer{})
	for range 20 {
		_ = u.ReadUInt32()
	}
	assert.Len(t, *seen, 1, "one failure must be reported once, not once per read")
}

// Attaching a new stream is the recovery path the channel uses on
// reconnect, so it has to re-arm reading.
func TestSetInputClearsTheStickyError(t *testing.T) {
	u, _ := collectingUnmarshaller(&bytes.Buffer{})
	_ = u.ReadUInt32()
	require.Error(t, u.Err())

	u.SetInput(bytes.NewReader([]byte{7, 0, 0, 0}))
	require.NoError(t, u.Err(), "a fresh stream must re-arm the reader")
	assert.Equal(t, uint32(7), u.ReadUInt32())
	assert.NoError(t, u.Err())
}

func TestClearErrReArmsReading(t *testing.T) {
	u, _ := collectingUnmarshaller(bytes.NewReader([]byte{1, 2}))
	_ = u.ReadUInt32()
	require.Error(t, u.Err())

	u.ClearErr()
	assert.NoError(t, u.Err())
}

// readBytesNonEmpty used to return the buffer it had just allocated when
// the read failed, so the caller got a full-length slice of untouched
// memory — and through ReadString, a run of NULs that looks like a value.
func TestReadStringDoesNotFabricateOnShortRead(t *testing.T) {
	// A length header promising 8 bytes, followed by only 3.
	var payload bytes.Buffer
	require.NoError(t, binary.Write(&payload, binary.LittleEndian, uint32(8)))
	payload.Write([]byte{'a', 'b', 'c'})

	u, _ := collectingUnmarshaller(bytes.NewReader(payload.Bytes()))
	got := u.ReadString()
	require.Error(t, u.Err(), "the short read must be reported")
	assert.Empty(t, got, "no fabricated string may escape")
	assert.NotContains(t, got, "\x00", "in particular not a run of NULs")
}

func TestReadBytesDoesNotFabricateOnShortRead(t *testing.T) {
	var payload bytes.Buffer
	require.NoError(t, binary.Write(&payload, binary.LittleEndian, uint32(8)))
	payload.Write([]byte{1, 2, 3})

	u, _ := collectingUnmarshaller(bytes.NewReader(payload.Bytes()))
	got := u.ReadBytes()
	require.Error(t, u.Err())
	assert.Nil(t, got, "a partially filled buffer must not be handed back")
}

// An allocator handing back a wrongly sized buffer is a caller bug, but it
// must surface as a failure rather than as silently truncated data.
func TestBadAllocatorSurfacesAsFailure(t *testing.T) {
	var payload bytes.Buffer
	require.NoError(t, binary.Write(&payload, binary.LittleEndian, uint32(8)))
	payload.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8})

	u, _ := collectingUnmarshaller(bytes.NewReader(payload.Bytes()))
	u.SetAllocateBufferFunc(func(l uint32) []byte { return make([]byte, l/2) })

	got := u.ReadBytes()
	require.Error(t, u.Err())
	assert.True(t, errors.Is(u.Err(), StringAllocationError) || u.Err() == StringAllocationError,
		"expected the allocation error, got %v", u.Err())
	assert.Nil(t, got)
}

// The nil-slice sentinel and a genuinely empty slice are both valid
// answers, and neither may be mistaken for a failure.
func TestSuccessfulReadsLeaveErrNil(t *testing.T) {
	m, u, _ := newTestPair()
	m.WriteNilSlice()
	m.WriteSliceLength(0)
	m.WriteString("")
	m.WriteBytes([]byte{})

	l, isNil := u.ReadSliceLength()
	assert.Equal(t, 0, l)
	assert.True(t, isNil)
	l, isNil = u.ReadSliceLength()
	assert.Equal(t, 0, l)
	assert.False(t, isNil)
	assert.Equal(t, "", u.ReadString())
	assert.Empty(t, u.ReadBytes())
	require.NoError(t, u.Err(), "valid empty values must not look like failures")
}

// ReadSliceLength cannot signal failure in its results — (0,false) is a
// legitimate empty slice — so Err is the only thing separating the two.
func TestReadSliceLengthFailureNeedsErrToDetect(t *testing.T) {
	u, _ := collectingUnmarshaller(&bytes.Buffer{})
	l, isNil := u.ReadSliceLength()
	assert.Equal(t, 0, l)
	assert.False(t, isNil, "indistinguishable from an empty slice by value alone")
	require.Error(t, u.Err(), "which is exactly why Err must report it")
}

// The interface, not just the concrete type, must expose the signal —
// consumers hold U UnmarshallReaderI generically.
func TestUnmarshallReaderInterfaceExposesErr(t *testing.T) {
	var reader UnmarshallReaderI = NewUnmarshaller(&bytes.Buffer{}, binary.LittleEndian, func(error) {}, nil)
	require.NoError(t, reader.Err())
	_ = reader.ReadUInt64()
	require.Error(t, reader.Err())
}
