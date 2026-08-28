package runtime

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// MajorType is a CBOR major type, pre-shifted into the head byte.
type MajorType byte

const (
	MajorUint   MajorType = 0 << 5
	MajorNeg    MajorType = 1 << 5
	MajorBytes  MajorType = 2 << 5
	MajorText   MajorType = 3 << 5
	MajorArray  MajorType = 4 << 5
	MajorMap    MajorType = 5 << 5
	MajorTag    MajorType = 6 << 5
	MajorSimple MajorType = 7 << 5
)

// The three simple values the wire uses, as their whole one-byte encoding.
const (
	SimpleFalse = byte(MajorSimple) | 20
	SimpleTrue  = byte(MajorSimple) | 21
	SimpleNull  = byte(MajorSimple) | 22
)

// Tag numbers the forms over this writer use (IANA CBOR Tags registry).
const (
	TagIPv4         uint64 = 52   // RFC 9164
	TagIPv6         uint64 = 54   // RFC 9164
	TagSet          uint64 = 258  // mathematical finite set
	TagExtendedTime uint64 = 1001 // RFC 9581
)

// NewCoreDetEncMode returns an fxamacker encoding mode in RFC 8949 §4.2 core
// deterministic configuration. It is the one place a library encoder is used:
// the shortest-float rule stays pinned to a library the repository already
// depends on instead of being reimplemented here.
func NewCoreDetEncMode() (em cbor.EncMode, err error) {
	em, err = cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		err = eh.Errorf("unable to create the core-deterministic CBOR encoder: %w", err)
		return
	}
	return
}

// CborWriter writes RFC 8949 §4.2 core-deterministic CBOR items straight into
// an io.Writer — a hasher, a frame buffer, or a scratch buffer when elements
// must be sorted before they reach their destination. Heads use the shortest
// argument encoding and floats the shortest value-preserving width; no
// quotient rule is applied here, so the writer is usable by a lossless form
// (ADR-0210) and by canonform's quotient (ADR-0201) alike.
//
// The first write error is kept and every later call is a no-op; callers
// check Err once per item.
//
// Not goroutine-safe; one writer per writing goroutine.
type CborWriter struct {
	w    io.Writer
	hb   [9]byte
	err  error
	fenc *cbor.Encoder // bound to fbuf; reused for every float
	fbuf bytes.Buffer
}

// NewCborWriter returns a writer over w with its float encoder bound. The
// encoder keeps a pointer to the writer's float scratch buffer, so a
// CborWriter must not be copied or moved once constructed — a value returned
// by this constructor sits at its final address and satisfies that.
func NewCborWriter(w io.Writer) (inst *CborWriter, err error) {
	em, err := NewCoreDetEncMode()
	if err != nil {
		return
	}
	inst = &CborWriter{w: w}
	inst.fenc = em.NewEncoder(&inst.fbuf)
	return
}

// Reset points the writer at w and clears the sticky error. The float encoder
// binding survives.
func (c *CborWriter) Reset(w io.Writer) {
	c.w = w
	c.err = nil
}

// Err returns the first write error since the last Reset.
func (c *CborWriter) Err() error { return c.err }

// Write passes p through verbatim — for bytes that are already an encoded
// item, e.g. a cached key encoding or an element sorted in a scratch buffer.
func (c *CborWriter) Write(p []byte) {
	if c.err != nil {
		return
	}
	_, c.err = c.w.Write(p)
}

// Head writes the head of a data item: major type and the shortest argument
// encoding of n (RFC 8949 §4.2.1 preferred serialization).
func (c *CborWriter) Head(mt MajorType, n uint64) {
	hb := c.hb[:]
	switch {
	case n < 24:
		hb[0] = byte(mt) | byte(n)
		c.Write(hb[:1])
	case n <= math.MaxUint8:
		hb[0] = byte(mt) | 24
		hb[1] = byte(n)
		c.Write(hb[:2])
	case n <= math.MaxUint16:
		hb[0] = byte(mt) | 25
		binary.BigEndian.PutUint16(hb[1:], uint16(n))
		c.Write(hb[:3])
	case n <= math.MaxUint32:
		hb[0] = byte(mt) | 26
		binary.BigEndian.PutUint32(hb[1:], uint32(n))
		c.Write(hb[:5])
	default:
		hb[0] = byte(mt) | 27
		binary.BigEndian.PutUint64(hb[1:], n)
		c.Write(hb[:9])
	}
}

func (c *CborWriter) WriteUint(n uint64) { c.Head(MajorUint, n) }

func (c *CborWriter) WriteInt(n int64) {
	if n >= 0 {
		c.Head(MajorUint, uint64(n))
		return
	}
	// -1 - n is representable for every negative int64, including MinInt64.
	c.Head(MajorNeg, uint64(-1-n))
}

func (c *CborWriter) WriteBytes(b []byte) {
	c.Head(MajorBytes, uint64(len(b)))
	c.Write(b)
}

func (c *CborWriter) WriteText(b []byte) {
	c.Head(MajorText, uint64(len(b)))
	c.Write(b)
}

func (c *CborWriter) WriteTextString(s string) {
	c.WriteText(unsafeperf.UnsafeStringToBytes(s))
}

func (c *CborWriter) WriteBytesString(s string) {
	c.WriteBytes(unsafeperf.UnsafeStringToBytes(s))
}

func (c *CborWriter) ArrayHead(n int) { c.Head(MajorArray, uint64(n)) }
func (c *CborWriter) MapHead(n int)   { c.Head(MajorMap, uint64(n)) }
func (c *CborWriter) Tag(n uint64)    { c.Head(MajorTag, n) }

func (c *CborWriter) WriteBool(v bool) {
	if v {
		c.Write([]byte{SimpleTrue})
	} else {
		c.Write([]byte{SimpleFalse})
	}
}

func (c *CborWriter) WriteNull() { c.Write([]byte{SimpleNull}) }

// WriteFloatShortest writes f as the shortest float encoding that preserves
// the value (float16/32/64), with every NaN as 0xf97e00 and ±Inf as float16.
// A float32 arrives widened to float64 exactly, so f32 x and f64(x) are
// byte-identical. No numeric reduction is applied: 3.0 stays a float, and
// -0.0 keeps its sign. A form that wants the dCBOR §2.5 reduction folds the
// integer-valued cases itself before calling this.
func (c *CborWriter) WriteFloatShortest(f float64) {
	if c.err != nil {
		return
	}
	if c.fenc == nil {
		c.err = eh.Errorf("float encoder not initialized; the writer was not built by NewCborWriter")
		return
	}
	c.fbuf.Reset()
	if err := c.fenc.Encode(f); err != nil {
		c.err = eh.Errorf("float encoding failed: %w", err)
		return
	}
	c.Write(c.fbuf.Bytes())
}
