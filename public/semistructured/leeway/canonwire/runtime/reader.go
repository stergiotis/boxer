package runtime

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The strict-subset violations a CborReader reports. Every error a reader
// produces wraps exactly one of them, so a caller can tell a malformed item
// apart from a well-formed one that carries the wrong type or a value the
// target Go type cannot hold. The sentinels carry no stack of their own —
// the wrapping error built at the failing call site carries it.
var (
	// ErrNonShortest is the head's argument not being the shortest encoding
	// of its value (RFC 8949 §4.2.1), a simple value in 24…31 written with
	// the one-byte form, a float wider than the shortest width that preserves
	// it, or one of the reserved additional-information values 28…30 — the
	// deterministic subset admits none of them.
	ErrNonShortest = errors.New("argument is not the shortest well-formed encoding of its value")
	// ErrIndefinite is an indefinite-length item or a break stop code
	// (additional information 31).
	ErrIndefinite = errors.New("indefinite lengths are not part of the deterministic subset")
	// ErrUnexpectedMajor is an item whose major type is not the one the
	// caller asked for.
	ErrUnexpectedMajor = errors.New("unexpected major type")
	// ErrOutOfRange is a well-formed value the requested Go type cannot hold,
	// or one outside what the form allows at that position.
	ErrOutOfRange = errors.New("value is out of the target type's range")
	// ErrTruncated is an item that runs past the end of the buffer.
	ErrTruncated = errors.New("item is truncated")
)

// maxNestingDepth bounds Skip's recursion. Adversarial input is a nest of
// array heads, which costs one byte per level on the wire and one frame per
// level here; the limit turns a stack overflow into an error.
const maxNestingDepth = 512

// CborReader reads RFC 8949 §4.2 core-deterministic CBOR items out of a byte
// slice — one entity item or a CBOR sequence of them, both of which the
// canonical wire holds in memory. It is strict in the direction that matters
// for a canonical form: an encoding a core-deterministic writer would not
// have produced is refused rather than accepted and re-emitted differently.
// Non-shortest arguments, indefinite lengths, reserved additional information
// and non-shortest floats are all errors.
//
// The first error is kept and every later call is a no-op returning zero
// values, so callers check Err once per item instead of once per field.
//
// Not goroutine-safe; one reader per reading goroutine.
type CborReader struct {
	b   []byte
	err error
	pos int
}

// NewCborReader returns a reader over b. The buffer is not copied and must
// outlive every view ReadBytes or ReadText hands out.
func NewCborReader(b []byte) *CborReader {
	return &CborReader{b: b}
}

// Reset points the reader at b, rewinds to its start and clears the sticky
// error.
func (c *CborReader) Reset(b []byte) {
	c.b = b
	c.pos = 0
	c.err = nil
}

// Pos returns the offset of the next byte to be read.
func (c *CborReader) Pos() int { return c.pos }

// Remaining returns how many bytes are left unread.
func (c *CborReader) Remaining() int { return len(c.b) - c.pos }

// Err returns the first error since the last Reset.
func (c *CborReader) Err() error { return c.err }

// Done reports that nothing more can be read: the buffer is exhausted, or an
// error has stuck. It is the loop condition for reading a CBOR sequence,
// paired with an Err check after the loop.
func (c *CborReader) Done() bool { return c.err != nil || c.pos >= len(c.b) }

// fail records err as the sticky error, unless one is already recorded.
func (c *CborReader) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// PeekMajor returns the major type of the next item without consuming
// anything. ok is false when the reader is exhausted or already in error. The
// head is not validated: a well-formed major type may still fail to read.
func (c *CborReader) PeekMajor() (mt MajorTypeE, ok bool) {
	if c.err != nil || c.pos >= len(c.b) {
		return
	}
	return MajorTypeE(c.b[c.pos] & 0xe0), true
}

// IsNull reports whether the next item is CBOR null, without consuming it.
func (c *CborReader) IsNull() bool {
	if c.err != nil || c.pos >= len(c.b) {
		return false
	}
	return c.b[c.pos] == SimpleNull
}

// readHead consumes one head and returns its major type, its additional
// information and its argument. For major type 7 with additional information
// 25, 26 or 27 the argument is the raw float16/32/64 bit pattern; ai tells
// the width apart, which is why the typed float readers go through this
// instead of ReadHead.
func (c *CborReader) readHead() (mt MajorTypeE, ai byte, arg uint64) {
	if c.err != nil {
		return
	}
	if c.pos >= len(c.b) {
		c.fail(eb.Build().Int("pos", c.pos).Errorf("no head byte left: %w", ErrTruncated))
		return
	}
	ib := c.b[c.pos]
	mt = MajorTypeE(ib & 0xe0)
	ai = ib & 0x1f
	if ai < 24 {
		c.pos++
		arg = uint64(ai)
		return
	}
	var width int
	switch ai {
	case 24:
		width = 1
	case 25:
		width = 2
	case 26:
		width = 4
	case 27:
		width = 8
	case 31:
		c.fail(eb.Build().Int("pos", c.pos).Uint8("majorType", byte(mt)>>5).Errorf("indefinite length or break stop code: %w", ErrIndefinite))
		mt, ai = 0, 0
		return
	default:
		c.fail(eb.Build().Int("pos", c.pos).Uint8("majorType", byte(mt)>>5).Uint8("additionalInfo", ai).Errorf("reserved additional information: %w", ErrNonShortest))
		mt, ai = 0, 0
		return
	}
	if c.pos+1+width > len(c.b) {
		c.fail(eb.Build().Int("pos", c.pos).Int("want", 1+width).Int("have", len(c.b)-c.pos).Errorf("head runs past the end of the buffer: %w", ErrTruncated))
		mt, ai = 0, 0
		return
	}
	p := c.b[c.pos+1:]
	switch ai {
	case 24:
		arg = uint64(p[0])
	case 25:
		arg = uint64(binary.BigEndian.Uint16(p))
	case 26:
		arg = uint64(binary.BigEndian.Uint32(p))
	case 27:
		arg = binary.BigEndian.Uint64(p)
	}
	if mt == MajorTypeSimple {
		// Major type 7 does not encode a count: 25…27 are floats, whose
		// shortest-width rule is the typed readers' business, and 24 is a
		// simple value which must be 32 or above.
		if ai == 24 && arg < 32 {
			c.fail(eb.Build().Int("pos", c.pos).Uint64("simpleValue", arg).Errorf("simple value below 32 must use the immediate form: %w", ErrNonShortest))
			mt, ai, arg = 0, 0, 0
			return
		}
		c.pos += 1 + width
		return
	}
	var min uint64
	switch ai {
	case 24:
		min = 24
	case 25:
		min = math.MaxUint8 + 1
	case 26:
		min = math.MaxUint16 + 1
	case 27:
		min = math.MaxUint32 + 1
	}
	if arg < min {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("majorType", byte(mt)>>5).Uint8("additionalInfo", ai).Uint64("argument", arg).Errorf("argument fits a shorter head: %w", ErrNonShortest))
		mt, ai, arg = 0, 0, 0
		return
	}
	c.pos += 1 + width
	return
}

// ReadHead consumes the head of one data item and returns its major type and
// argument. A head whose argument could have been written shorter, an
// indefinite length, a break stop code and the reserved additional
// information values 28…30 are all refused.
//
// For major type 7 with additional information 25…27 the argument is the raw
// float bit pattern and its width is not reported; read floats with
// ReadFloat64 or ReadFloat32, which enforce the shortest-width rule.
func (c *CborReader) ReadHead() (mt MajorTypeE, arg uint64) {
	mt, _, arg = c.readHead()
	return
}

// expect consumes a head and requires it to carry major type want.
func (c *CborReader) expect(want MajorTypeE) (arg uint64) {
	mt, _, arg := c.readHead()
	if c.err != nil {
		return 0
	}
	if mt != want {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("want", byte(want)>>5).Uint8("got", byte(mt)>>5).Errorf("major type mismatch: %w", ErrUnexpectedMajor))
		return 0
	}
	return arg
}

// ReadUint reads an unsigned integer (major type 0).
func (c *CborReader) ReadUint() uint64 { return c.expect(MajorTypeUint) }

// ReadInt reads a signed integer (major type 0 or 1). A magnitude that does
// not fit int64 is out of range.
func (c *CborReader) ReadInt() (v int64) {
	mt, _, arg := c.readHead()
	if c.err != nil {
		return
	}
	switch mt {
	case MajorTypeUint:
		if arg > math.MaxInt64 {
			c.fail(eb.Build().Int("pos", c.pos).Uint64("value", arg).Errorf("unsigned value does not fit int64: %w", ErrOutOfRange))
			return
		}
		return int64(arg)
	case MajorTypeNeg:
		if arg > math.MaxInt64 {
			c.fail(eb.Build().Int("pos", c.pos).Uint64("magnitude", arg).Errorf("negative value does not fit int64: %w", ErrOutOfRange))
			return
		}
		return -1 - int64(arg)
	}
	c.fail(eb.Build().Int("pos", c.pos).Uint8("got", byte(mt)>>5).Errorf("expected an integer: %w", ErrUnexpectedMajor))
	return
}

// ReadU8 reads an unsigned integer that fits uint8.
func (c *CborReader) ReadU8() uint8 { return uint8(c.readUintMax(math.MaxUint8)) }

// ReadU16 reads an unsigned integer that fits uint16.
func (c *CborReader) ReadU16() uint16 { return uint16(c.readUintMax(math.MaxUint16)) }

// ReadU32 reads an unsigned integer that fits uint32.
func (c *CborReader) ReadU32() uint32 { return uint32(c.readUintMax(math.MaxUint32)) }

// ReadU64 reads an unsigned integer.
func (c *CborReader) ReadU64() uint64 { return c.ReadUint() }

func (c *CborReader) readUintMax(max uint64) (v uint64) {
	v = c.expect(MajorTypeUint)
	if c.err != nil {
		return 0
	}
	if v > max {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("value", v).Uint64("max", max).Errorf("unsigned value does not fit the target width: %w", ErrOutOfRange))
		return 0
	}
	return
}

// ReadI8 reads a signed integer that fits int8.
func (c *CborReader) ReadI8() int8 { return int8(c.readIntRange(math.MinInt8, math.MaxInt8)) }

// ReadI16 reads a signed integer that fits int16.
func (c *CborReader) ReadI16() int16 { return int16(c.readIntRange(math.MinInt16, math.MaxInt16)) }

// ReadI32 reads a signed integer that fits int32.
func (c *CborReader) ReadI32() int32 { return int32(c.readIntRange(math.MinInt32, math.MaxInt32)) }

// ReadI64 reads a signed integer.
func (c *CborReader) ReadI64() int64 { return c.ReadInt() }

func (c *CborReader) readIntRange(min int64, max int64) (v int64) {
	v = c.ReadInt()
	if c.err != nil {
		return 0
	}
	if v < min || v > max {
		c.fail(eb.Build().Int("pos", c.pos).Int64("value", v).Int64("min", min).Int64("max", max).Errorf("signed value does not fit the target width: %w", ErrOutOfRange))
		return 0
	}
	return
}

// take consumes n payload bytes and returns them as a view into the buffer.
func (c *CborReader) take(n uint64) (b []byte) {
	if c.err != nil {
		return
	}
	if n > uint64(len(c.b)-c.pos) {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("want", n).Int("have", len(c.b)-c.pos).Errorf("payload runs past the end of the buffer: %w", ErrTruncated))
		return
	}
	b = c.b[c.pos : c.pos+int(n)]
	c.pos += int(n)
	return
}

// ReadBytes reads a byte string (major type 2). The result is a view into the
// reader's buffer, valid until that buffer is reused; copy it to keep it.
func (c *CborReader) ReadBytes() (b []byte) {
	n := c.expect(MajorTypeBytes)
	if c.err != nil {
		return
	}
	return c.take(n)
}

// ReadText reads a text string (major type 3) and checks it is valid UTF-8.
// The result is a view into the reader's buffer, valid until that buffer is
// reused; copy it to keep it.
func (c *CborReader) ReadText() (b []byte) {
	n := c.expect(MajorTypeText)
	if c.err != nil {
		return
	}
	b = c.take(n)
	if c.err != nil {
		return
	}
	if !utf8.Valid(b) {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Errorf("text string is not valid UTF-8: %w", ErrOutOfRange))
		return nil
	}
	return
}

// ReadBytesInto reads a byte string of exactly len(dst) bytes and copies it
// into dst. It is the read form of the fixed-width `y<N>` lane, whose Go type
// is an array rather than a slice, and of the padded fixed-width bytes ADR-0210
// SD3 keeps as content.
func (c *CborReader) ReadBytesInto(dst []byte) {
	b := c.ReadBytes()
	if c.err != nil {
		return
	}
	if len(b) != len(dst) {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Int("want", len(dst)).Errorf("byte string is not the fixed width the column declares: %w", ErrOutOfRange))
		return
	}
	copy(dst, b)
}

// ReadTextString reads a text string and copies it into a Go string.
func (c *CborReader) ReadTextString() string {
	b := c.ReadText()
	if c.err != nil {
		return ""
	}
	return string(b)
}

// ReadTextStringFixed reads a text string of exactly width bytes. It is the
// read form of the fixed-width `sx<N>` lane: the encoder reads such a column
// from a FixedSizeBinary array, so it always writes exactly N bytes, and a
// different length is a value the writer would not have produced (the SD2
// promise that the typed reads catch a width mismatch value by value).
func (c *CborReader) ReadTextStringFixed(width int) string {
	b := c.ReadText()
	if c.err != nil {
		return ""
	}
	if len(b) != width {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Int("want", width).Errorf("text string is not the fixed width the column declares: %w", ErrOutOfRange))
		return ""
	}
	return string(b)
}

// readCount consumes a head of major type want and returns its argument as an
// element count, refusing counts that cannot be present in the remaining
// bytes — one byte is the smallest item, so a count above the bytes left is
// truncation however it is later read.
func (c *CborReader) readCount(want MajorTypeE, perElement int) (n int) {
	arg := c.expect(want)
	if c.err != nil {
		return
	}
	if arg > uint64(len(c.b)-c.pos)/uint64(perElement) {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("count", arg).Int("have", len(c.b)-c.pos).Errorf("element count exceeds the bytes left: %w", ErrTruncated))
		return 0
	}
	return int(arg)
}

// ReadArrayHead reads the head of a definite-length array and returns its
// element count.
func (c *CborReader) ReadArrayHead() int { return c.readCount(MajorTypeArray, 1) }

// ReadMapHead reads the head of a definite-length map and returns its entry
// count. The reader does not check that the keys are sorted; a form that
// needs that checks the keys it reads.
func (c *CborReader) ReadMapHead() int { return c.readCount(MajorTypeMap, 2) }

// ReadTag reads a tag head (major type 6) and returns the tag number. The
// tagged item follows and is read separately.
func (c *CborReader) ReadTag() uint64 { return c.expect(MajorTypeTag) }

// ExpectTag reads a tag head and requires it to be want.
func (c *CborReader) ExpectTag(want uint64) {
	got := c.ReadTag()
	if c.err != nil {
		return
	}
	if got != want {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("want", want).Uint64("got", got).Errorf("unexpected tag number: %w", ErrOutOfRange))
	}
}

// ReadBool reads the simple value false or true.
func (c *CborReader) ReadBool() (v bool) {
	mt, ai, arg := c.readHead()
	if c.err != nil {
		return
	}
	if mt != MajorTypeSimple || ai >= 24 {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("got", byte(mt)>>5).Errorf("expected a bool: %w", ErrUnexpectedMajor))
		return
	}
	switch byte(MajorTypeSimple) | byte(arg) {
	case SimpleFalse:
		return false
	case SimpleTrue:
		return true
	}
	c.fail(eb.Build().Int("pos", c.pos).Uint64("simpleValue", arg).Errorf("simple value is not a bool: %w", ErrOutOfRange))
	return
}

// ReadNull reads the simple value null.
func (c *CborReader) ReadNull() {
	mt, ai, arg := c.readHead()
	if c.err != nil {
		return
	}
	if mt != MajorTypeSimple || ai >= 24 || byte(MajorTypeSimple)|byte(arg) != SimpleNull {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("got", byte(mt)>>5).Uint64("simpleValue", arg).Errorf("expected null: %w", ErrUnexpectedMajor))
	}
}

// float16ToFloat64 widens an IEEE 754 binary16 bit pattern. The widening is
// exact for every input, NaN payloads included: the half's significand is
// zero-padded to the right, which is the reconstruction RFC 8949 §4.2.2
// describes.
func float16ToFloat64(h uint16) float64 {
	sign := uint64(h>>15) << 63
	exp := int(h>>10) & 0x1f
	mant := uint64(h & 0x3ff)
	switch {
	case exp == 0x1f: // Inf or NaN
		return math.Float64frombits(sign | 0x7ff0000000000000 | mant<<42)
	case exp == 0: // zero or subnormal
		if mant == 0 {
			return math.Float64frombits(sign)
		}
		k := 0
		for mant&0x400 == 0 {
			mant <<= 1
			k++
		}
		mant &= 0x3ff
		return math.Float64frombits(sign | uint64(int64(-14-k+1023))<<52 | mant<<42)
	default:
		return math.Float64frombits(sign | uint64(int64(exp-15+1023))<<52 | mant<<42)
	}
}

// float32FitsFloat16 reports whether f is exactly representable as an IEEE 754
// binary16. Infinities and NaNs are not decided here: the deterministic
// encoding rules treat them separately.
func float32FitsFloat16(f float32) bool {
	b := math.Float32bits(f)
	if b&0x7fffffff == 0 { // ±0
		return true
	}
	e := int((b>>23)&0xff) - 127
	mant := b & 0x7fffff
	if e < -24 || e > 15 {
		// Below the smallest binary16 subnormal, or above the largest finite
		// binary16 — this includes every binary32 subnormal.
		return false
	}
	if e >= -14 {
		return mant&0x1fff == 0
	}
	// Subnormal in binary16: the significand is shifted right by -e-1 places
	// and the bits that fall off must be zero.
	shift := uint(-e - 1)
	return uint64(1<<23|mant)&((1<<shift)-1) == 0
}

// canonicalFloatAI returns the additional information (25, 26 or 27) a core
// deterministic writer uses for f: the shortest width that preserves the
// value, with every NaN as binary16 0x7e00 and every infinity as binary16.
// It mirrors the fxamacker CoreDetEncOptions configuration CborWriter encodes
// through (ShortestFloat16, NaNConvert7e00, InfConvertFloat16), so a value
// this reader accepts is one WriteFloatShortest would have produced.
func canonicalFloatAI(f float64) byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 25
	}
	f32 := float32(f)
	if math.Float64bits(float64(f32)) != math.Float64bits(f) {
		return 27
	}
	if float32FitsFloat16(f32) {
		return 25
	}
	return 26
}

// ReadFloat64 reads a binary16, binary32 or binary64 (major type 7,
// additional information 25…27) and returns it widened to float64. The width
// must be the shortest that preserves the value, so bytes this reader accepts
// are bytes WriteFloatShortest would have written: no numeric reduction, −0.0
// keeps its sign, and every NaN must be the one binary16 quiet NaN 0xf97e00 —
// the encoder side collapses NaN payloads (fxamacker's NaNConvert7e00), so a
// payload-carrying NaN on the wire is not canonical.
func (c *CborReader) ReadFloat64() (f float64) {
	mt, ai, arg := c.readHead()
	if c.err != nil {
		return
	}
	if mt != MajorTypeSimple {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("got", byte(mt)>>5).Errorf("expected a float: %w", ErrUnexpectedMajor))
		return
	}
	if !isFloatAI(ai) {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("additionalInfo", ai).Errorf("simple value is not a float: %w", ErrUnexpectedMajor))
		return
	}
	f = floatFromHead(ai, arg)
	c.checkCanonicalFloat(ai, arg, f)
	if c.err != nil {
		return 0
	}
	return
}

// isFloatAI reports whether a major-type-7 additional information carries a
// float rather than a simple value.
func isFloatAI(ai byte) bool { return ai == 25 || ai == 26 || ai == 27 }

// floatFromHead widens a major-type-7 float head's raw bit pattern to float64.
// ai must satisfy isFloatAI.
func floatFromHead(ai byte, arg uint64) (f float64) {
	switch ai {
	case 25:
		return float16ToFloat64(uint16(arg))
	case 26:
		return float64(math.Float32frombits(uint32(arg)))
	}
	return math.Float64frombits(arg)
}

// checkCanonicalFloat records a failure unless the head is the one a core
// deterministic writer would have produced for f: the shortest width that
// preserves the value, and the single quiet NaN 0xf97e00. It is called both by
// the typed float readers and by Skip, so a non-shortest float is refused
// wherever it is passed rather than only where it is decoded.
func (c *CborReader) checkCanonicalFloat(ai byte, arg uint64, f float64) {
	if want := canonicalFloatAI(f); ai != want {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("additionalInfo", ai).Uint8("want", want).Errorf("float is not in its shortest value-preserving width: %w", ErrNonShortest))
		return
	}
	if math.IsNaN(f) && uint16(arg) != 0x7e00 {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("halfBits", arg).Errorf("NaN must be the canonical quiet NaN: %w", ErrNonShortest))
	}
}

// ReadFloat32 reads a float and narrows it to float32, refusing a value that
// is not exactly representable at that width. NaN is allowed and stays NaN.
func (c *CborReader) ReadFloat32() (f float32) {
	v := c.ReadFloat64()
	if c.err != nil {
		return
	}
	if math.IsNaN(v) {
		return float32(math.NaN())
	}
	f = float32(v)
	if math.Float64bits(float64(f)) != math.Float64bits(v) {
		c.fail(eb.Build().Int("pos", c.pos).Float64("value", v).Errorf("float is not exactly representable as float32: %w", ErrOutOfRange))
		return 0
	}
	return
}

// Skip advances past one complete data item, descending into arrays, maps and
// tags. Every head it passes is validated the way a typed read would validate
// it, floats included: a non-shortest width or a payload-carrying NaN is
// refused here as it is in ReadFloat64. What Skip does not check is the content
// rules a form layers on top — a skipped text string is not checked for UTF-8,
// and a skipped map's keys are not checked for order.
func (c *CborReader) Skip() { c.skip(0) }

func (c *CborReader) skip(depth int) {
	if c.err != nil {
		return
	}
	if depth > maxNestingDepth {
		c.fail(eb.Build().Int("pos", c.pos).Int("maxDepth", maxNestingDepth).Errorf("nesting is too deep to skip: %w", ErrOutOfRange))
		return
	}
	mt, ai, arg := c.readHead()
	if c.err != nil {
		return
	}
	switch mt {
	case MajorTypeUint, MajorTypeNeg:
		// The head carried the whole item.
	case MajorTypeSimple:
		// The head carried the whole item, but a float's width is only
		// well-formed if it is the shortest one that preserves the value.
		if isFloatAI(ai) {
			c.checkCanonicalFloat(ai, arg, floatFromHead(ai, arg))
		}
	case MajorTypeBytes, MajorTypeText:
		c.take(arg)
	case MajorTypeArray, MajorTypeMap:
		n := arg
		if mt == MajorTypeMap {
			if n > math.MaxUint64/2 {
				c.fail(eb.Build().Int("pos", c.pos).Uint64("count", n).Errorf("map entry count overflows: %w", ErrTruncated))
				return
			}
			n *= 2
		}
		if n > uint64(len(c.b)-c.pos) {
			c.fail(eb.Build().Int("pos", c.pos).Uint64("count", n).Int("have", len(c.b)-c.pos).Errorf("element count exceeds the bytes left: %w", ErrTruncated))
			return
		}
		for i := uint64(0); i < n; i++ {
			c.skip(depth + 1)
			if c.err != nil {
				return
			}
		}
	case MajorTypeTag:
		c.skip(depth + 1)
	}
}

// since returns the bytes consumed since offset start, as a view into the
// reader's buffer. It is how the forms over this reader capture an item's
// encoding while they read it typed — the alternative, ReadItemBytes, skips
// the item instead of decoding it, so a caller that needs both would read the
// bytes twice.
func (c *CborReader) since(start int) (b []byte) {
	if start < 0 || start > c.pos {
		return nil
	}
	return c.b[start:c.pos]
}

// Since returns the bytes read since offset start — the exported form of the
// capture the forms over this reader use internally. A generated decoder pairs
// it with Pos to keep a value's raw encoding while it reads that value typed,
// which is what puts an attribute's columns into an AttributeView without
// decoding them twice.
//
// The result is a view into the reader's buffer, valid until that buffer is
// reused. A start beyond the current position, or a negative one, yields nil.
func (c *CborReader) Since(start int) (b []byte) { return c.since(start) }

// ReadItemBytes advances past one complete data item and returns its encoded
// bytes as a view into the reader's buffer. It is how a caller checks the
// orderings the form asserts but the reader does not — set elements and
// memberships sorted bytewise, duplicates kept in both — by comparing
// consecutive views with bytes.Compare.
func (c *CborReader) ReadItemBytes() (b []byte) {
	start := c.pos
	c.Skip()
	if c.err != nil {
		return
	}
	return c.b[start:c.pos]
}
