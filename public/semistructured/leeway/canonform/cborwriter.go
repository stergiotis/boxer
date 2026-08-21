package canonform

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// cborWriter writes RFC 8949 §4.2 core-deterministic CBOR items straight into
// an io.Writer — the hasher, or a scratch buffer when elements must be sorted
// before they reach the hasher. Heads use the shortest argument encoding;
// floats are reduced per dCBOR §2.5 (ADR-0201 SD3) and then shortened by the
// fxamacker encoder in CoreDetEncOptions mode, the one place the library's
// float logic is reused so the shortest-float rule stays pinned to a library
// the repository already depends on.
//
// The first write error is kept and every later call is a no-op; callers
// check err once per item.
type cborWriter struct {
	w    io.Writer
	hb   [9]byte
	err  error
	fenc *cbor.Encoder // bound to fbuf; reused for every float
	fbuf bytes.Buffer
}

// initFloatEncoder binds the fxamacker encoder (CoreDetEncOptions) to the
// writer's float scratch buffer. Must be called once the cborWriter sits at
// its final address, since the encoder keeps a pointer to fbuf.
func (c *cborWriter) initFloatEncoder(em cbor.EncMode) {
	c.fenc = em.NewEncoder(&c.fbuf)
}

// CBOR major types, pre-shifted into the head byte.
const (
	mtUint   byte = 0 << 5
	mtNeg    byte = 1 << 5
	mtBytes  byte = 2 << 5
	mtText   byte = 3 << 5
	mtArray  byte = 4 << 5
	mtMap    byte = 5 << 5
	mtTag    byte = 6 << 5
	mtSimple byte = 7 << 5
)

const (
	cborFalse = mtSimple | 20
	cborTrue  = mtSimple | 21
	cborNull  = mtSimple | 22
)

// Tag numbers the form uses (IANA CBOR Tags registry).
const (
	tagIPv4         uint64 = 52   // RFC 9164
	tagIPv6         uint64 = 54   // RFC 9164
	tagSet          uint64 = 258  // mathematical finite set
	tagExtendedTime uint64 = 1001 // RFC 9581
)

func newCoreDetEncMode() (cbor.EncMode, error) {
	em, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		return nil, eh.Errorf("canonform: unable to create the core-deterministic CBOR encoder: %w", err)
	}
	return em, nil
}

func (c *cborWriter) reset(w io.Writer) {
	c.w = w
	c.err = nil
}

func (c *cborWriter) write(p []byte) {
	if c.err != nil {
		return
	}
	_, c.err = c.w.Write(p)
}

// head writes the head of a data item: major type and the shortest argument
// encoding of n (RFC 8949 §4.2.1 preferred serialization).
func (c *cborWriter) head(mt byte, n uint64) {
	hb := c.hb[:]
	switch {
	case n < 24:
		hb[0] = mt | byte(n)
		c.write(hb[:1])
	case n <= math.MaxUint8:
		hb[0] = mt | 24
		hb[1] = byte(n)
		c.write(hb[:2])
	case n <= math.MaxUint16:
		hb[0] = mt | 25
		binary.BigEndian.PutUint16(hb[1:], uint16(n))
		c.write(hb[:3])
	case n <= math.MaxUint32:
		hb[0] = mt | 26
		binary.BigEndian.PutUint32(hb[1:], uint32(n))
		c.write(hb[:5])
	default:
		hb[0] = mt | 27
		binary.BigEndian.PutUint64(hb[1:], n)
		c.write(hb[:9])
	}
}

func (c *cborWriter) writeUint(n uint64) { c.head(mtUint, n) }

func (c *cborWriter) writeInt(n int64) {
	if n >= 0 {
		c.head(mtUint, uint64(n))
		return
	}
	// -1 - n is representable for every negative int64, including MinInt64.
	c.head(mtNeg, uint64(-1-n))
}

func (c *cborWriter) writeBytes(b []byte) {
	c.head(mtBytes, uint64(len(b)))
	c.write(b)
}

func (c *cborWriter) writeText(b []byte) {
	c.head(mtText, uint64(len(b)))
	c.write(b)
}

func (c *cborWriter) writeTextString(s string) {
	c.writeText(unsafeperf.UnsafeStringToBytes(s))
}

func (c *cborWriter) writeBytesString(s string) {
	c.writeBytes(unsafeperf.UnsafeStringToBytes(s))
}

func (c *cborWriter) arrayHead(n int) { c.head(mtArray, uint64(n)) }
func (c *cborWriter) mapHead(n int)   { c.head(mtMap, uint64(n)) }
func (c *cborWriter) tag(n uint64)    { c.head(mtTag, n) }

func (c *cborWriter) writeBool(v bool) {
	if v {
		c.write([]byte{cborTrue})
	} else {
		c.write([]byte{cborFalse})
	}
}

func (c *cborWriter) writeNull() { c.write([]byte{cborNull}) }

// writeFloat writes a floating-point value in its canonical form: dCBOR §2.5
// numeric reduction first — a value numerically equal to an integer in
// [-2^63, 2^64-1] becomes that integer, so 3.0 ≡ 3 and -0.0 ≡ 0 — then the
// shortest float encoding that preserves the value (float16/32/64), with all
// NaNs as 0xf97e00 and ±Inf as float16. A float32 arrives widened to float64
// exactly, so f32 x and f64(x) are byte-identical.
func (c *cborWriter) writeFloat(f float64) {
	if c.err != nil {
		return
	}
	if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) {
		switch {
		case f >= 0 && f < 18446744073709551616.0: // < 2^64 → fits uint64 exactly
			c.head(mtUint, uint64(f))
			return
		case f < 0 && f >= -9223372036854775808.0: // ≥ -2^63 → fits int64 exactly
			c.writeInt(int64(f))
			return
		}
	}
	if c.fenc == nil {
		c.err = eh.Errorf("canonform: float encoder not initialized")
		return
	}
	c.fbuf.Reset()
	if err := c.fenc.Encode(f); err != nil {
		c.err = eh.Errorf("canonform: float encoding failed: %w", err)
		return
	}
	c.write(c.fbuf.Bytes())
}
