package cbor

import (
	"bytes"
	"encoding/binary"
	"github.com/stergiotis/boxer/public/observability/eh"
	"hash"
	"io"
	"math"
	"net/netip"
	"time"
)

type MajorType uint8

const MajorTypePositiveInt MajorType = 0
const MajorTypeNegativeInt MajorType = 1
const MajorTypeByteString MajorType = 2
const MajorTypeUtf8String MajorType = 3
const MajorTypeArray MajorType = 4
const MajorTypeMap MajorType = 5
const MajorTypeTag MajorType = 6
const MajorTypeFloatOrSimple MajorType = 7

type EncoderWriter interface {
	io.ByteWriter
	io.Writer
	io.StringWriter
}

type Encoder struct {
	w          EncoderWriter
	buf        *bytes.Buffer
	hasher     hash.Hash
	flushLimit int
	scratch8   []byte
}

var _ BasicEncoder = (*Encoder)(nil)
var _ IndefiniteContainerEncoder = (*Encoder)(nil)
var _ HashingEncoder = (*Encoder)(nil)

func NewEncoder(w EncoderWriter, hasher hash.Hash) *Encoder {
	flushLimit := 512 / 8
	if hasher != nil {
		flushLimit = hasher.Size()
	}
	return &Encoder{
		w:          w,
		buf:        bytes.NewBuffer(make([]byte, 0, 128)),
		hasher:     hasher,
		flushLimit: flushLimit,
		scratch8:   make([]byte, 8, 8),
	}
}

func (inst *Encoder) Reset() {
	inst.buf.Reset()
	if inst.hasher != nil {
		inst.hasher.Reset()
	}
}
func (inst *Encoder) SetHasher(hasher hash.Hash) {
	inst.hasher = hasher
	inst.hasher.Reset()
	inst.flushLimit = hasher.BlockSize()
}
func (inst *Encoder) SetWriter(w EncoderWriter) {
	inst.w = w
}

func (inst *Encoder) EncodeUint(val uint64) (int, error) {
	return inst.encodeHead(MajorTypePositiveInt, val)
}

func (inst *Encoder) EncodeInt(val int64) (int, error) {
	if val >= 0 {
		return inst.encodeHead(MajorTypePositiveInt, uint64(val))
	} else {
		return inst.encodeHead(MajorTypeNegativeInt, uint64(-1-val))
	}
}

func (inst *Encoder) encodeHead(majorType MajorType, val uint64) (int, error) {
	if val < 24 {
		return inst.encodeHeadSmall(majorType, uint8(val))
	} else if val <= 255 {
		return inst.encodeHead8Bit(majorType, uint8(val))
	} else if val <= 65535 {
		return inst.encodeHead16Bit(majorType, uint16(val))
	} else if val <= 4294967295 {
		return inst.encodeHead32Bit(majorType, uint32(val))
	} else {
		return inst.encodeHead64Bit(majorType, val)
	}
}

func (inst *Encoder) EncodeByteSlice(b []byte) (n int, err error) {
	if b == nil {
		err = eh.Errorf("byte slice may not be nil")
		return
	}
	l := len(b)
	n, err = inst.encodeHead(MajorTypeByteString, uint64(l))
	if err != nil {
		return
	}
	return inst.writeBytes(b, n)
}

func (inst *Encoder) EncodeString(str string) (n int, err error) {
	l := len(str)
	n, err = inst.encodeHead(MajorTypeUtf8String, uint64(l))
	if err != nil {
		return
	}
	return inst.writeString(str, n)
}

func (inst *Encoder) EncodeArrayDefinite(len uint64) (n int, err error) {
	return inst.encodeHead(MajorTypeArray, len)
}

func (inst *Encoder) EncodeMapDefinite(len uint64) (n int, err error) {
	return inst.encodeHead(MajorTypeMap, len)
}

func (inst *Encoder) encodeTagUnchecked(val uint64) (int, error) {
	return inst.encodeHead(MajorTypeTag, val)
}

func (inst *Encoder) EncodeArrayIndefinite() (n int, err error) {
	return inst.writeSingleByte(0x9f, 0)
}

func (inst *Encoder) EncodeMapIndefinite() (n int, err error) {
	return inst.writeSingleByte(0xbf, 0)
}

func (inst *Encoder) EncodeBreak() (n int, err error) {
	return inst.writeSingleByte(0xff, 0)
}

func (inst *Encoder) EncodeBool(val bool) (n int, err error) {
	if val {
		return inst.writeSingleByte(0xf5, 0)
	} else {
		return inst.writeSingleByte(0xf4, 0)
	}
}
func (inst *Encoder) EncodeFloat32(val float32) (n int, err error) {
	n, err = inst.encodeHeadSmall(MajorTypeFloatOrSimple, 26)
	binary.BigEndian.AppendUint32(inst.scratch8[:0], math.Float32bits(val))

	return inst.writeBytes(inst.scratch8, n)
}
func (inst *Encoder) EncodeFloat64(val float64) (n int, err error) {
	n, err = inst.encodeHeadSmall(MajorTypeFloatOrSimple, 27)
	binary.BigEndian.AppendUint64(inst.scratch8[:0], math.Float64bits(val))

	return inst.writeBytes(inst.scratch8, n)
}

func (inst *Encoder) EncodeNil() (n int, err error) {
	return inst.writeSingleByte(0xf6, 0)
}

func (inst *Encoder) EncodeCborPayload(val []byte) (n int, err error) {
	n, err = inst.EncodeTag8(TagEncodedCBORSequence)
	if err != nil {
		return
	}
	var u int
	u, err = inst.EncodeByteSlice(val)
	n += u
	return
}
func (inst *Encoder) EncodeJsonPayload(val []byte) (n int, err error) {
	n, err = inst.TagUint16(TagEmbeddedJSON)
	if err != nil {
		return
	}
	var u int
	u, err = inst.EncodeByteSlice(val)
	n += u
	return
}
func (inst *Encoder) EncodeTimeUTC(val time.Time) (n int, err error) {
	val = val.UTC()
	n, err = inst.EncodeTagSmall(TagEpochDateTimeNumber)
	if err != nil {
		return
	}
	var u int
	if val.Nanosecond() > 0 {
		u, err = inst.EncodeFloat64(float64(val.Unix())*1.0e9 + float64(val.Nanosecond()))
	} else {
		u, err = inst.EncodeInt(val.Unix())
	}
	n += u
	return
}

func (inst *Encoder) EncodeIpAddr(val netip.Addr) (n int, err error) {
	if val.Is4() {
		n, err = inst.EncodeTag8(TagIPv4)
	} else {
		n, err = inst.EncodeTag8(TagIPv6)
	}
	if err != nil {
		return
	}
	var u int
	u, err = inst.EncodeByteSlice(val.AsSlice())
	n += u
	return
}

func (inst *Encoder) writeSingleByte(b byte, bytesWrittenBefore int) (n int, err error) {
	err = inst.buf.WriteByte(b)
	if err != nil {
		err = eh.Errorf("unable to write byte to internal hashing buffer: %w", err)
		return
	}
	// TODO good practice? check bytesWrittenBefore? random?
	err = inst.flushHashBuffer(false)
	if err != nil {
		err = eh.Errorf("unable to flush internal hashing buffer: %w", err)
		return
	}
	err = inst.w.WriteByte(b)
	if err != nil {
		return bytesWrittenBefore, err
	}
	return bytesWrittenBefore + 1, nil
}

func (inst *Encoder) writeBytes(b []byte, bytesWrittenBefore int) (n int, err error) {
	var u int
	err = inst.flushHashBuffer(true)
	if err != nil {
		return
	}
	if inst.hasher != nil {
		_, err = inst.hasher.Write(b)
		if err != nil {
			err = eh.Errorf("unable to write byte to internal hashing buffer: %w", err)
			return
		}
	}
	n = bytesWrittenBefore
	u, err = inst.w.Write(b)
	n += u
	return
}

func (inst *Encoder) writeString(s string, bytesWrittenBefore int) (n int, err error) {
	var u int
	err = inst.flushHashBuffer(false)
	if err != nil {
		return
	}
	_, err = inst.buf.WriteString(s)
	if err != nil {
		err = eh.Errorf("unable to write byte to internal hashing buffer: %w", err)
		return
	}
	err = inst.flushHashBuffer(false)
	if err != nil {
		return
	}
	n = bytesWrittenBefore
	u, err = inst.w.WriteString(s)
	n += u
	return
}

func (inst *Encoder) flushHashBuffer(force bool) (err error) {
	if inst.hasher == nil {
		return
	}
	buf := inst.buf
	if force == false && buf.Len() < inst.flushLimit {
		return
	}
	_, err = inst.hasher.Write(buf.Bytes())
	if err != nil {
		return eh.Errorf("unable to flush internal hashing buffer: %w", err)
	}
	buf.Reset()
	return nil
}

func (inst *Encoder) Hash(b []byte) (hash []byte, err error) {
	err = inst.flushHashBuffer(true)
	if err != nil {
		err = eh.Errorf("unable to calculate hash: %w", err)
		return
	}
	hash = inst.hasher.Sum(b)
	return
}

func (inst *Encoder) encodeHeadSmall(majorType MajorType, val uint8) (int, error) {
	return inst.writeSingleByte(uint8(majorType)<<5|val, 0)
}
func (inst *Encoder) EncodeTagSmall(val TagSmall) (int, error) {
	return inst.encodeHeadSmall(6, uint8(val))
}

func (inst *Encoder) encodeHead8Bit(majorType MajorType, val uint8) (n int, err error) {
	n, err = inst.writeSingleByte(uint8(majorType)<<5|24, 0)
	if err != nil {
		return
	}

	return inst.writeSingleByte(val, n)
}
func (inst *Encoder) EncodeTag8(val TagUint8) (int, error) {
	return inst.encodeHead8Bit(MajorTypeTag, uint8(val))
}
func (inst *Encoder) EncodeTag16(val TagUint16) (int, error) {
	return inst.encodeHead16Bit(MajorTypeTag, uint16(val))
}
func (inst *Encoder) EncodeTag32(val TagUint32) (int, error) {
	return inst.encodeHead32Bit(MajorTypeTag, uint32(val))
}
func (inst *Encoder) EncodeTag64(val TagUint64) (int, error) {
	return inst.encodeHead64Bit(MajorTypeTag, uint64(val))
}

func (inst *Encoder) encodeHead16Bit(majorType MajorType, val uint16) (n int, err error) {
	n, err = inst.writeSingleByte(uint8(majorType)<<5|25, 0)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>8), n)
	if err != nil {
		return
	}

	return inst.writeSingleByte(byte(val), n)
}
func (inst *Encoder) TagUint16(val TagUint16) (int, error) {
	return inst.encodeHead16Bit(MajorTypeTag, uint16(val))
}

func (inst *Encoder) encodeHead32Bit(majorType MajorType, val uint32) (n int, err error) {
	n, err = inst.writeSingleByte(uint8(majorType)<<5|26, 0)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>24), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>16), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>8), n)
	if err != nil {
		return
	}

	return inst.writeSingleByte(byte(val), n)
}
func (inst *Encoder) TagUint32(val TagUint32) (int, error) {
	return inst.encodeHead32Bit(MajorTypeTag, uint32(val))
}

func (inst *Encoder) encodeHead64Bit(majorType MajorType, val uint64) (n int, err error) {
	n, err = inst.writeSingleByte(uint8(majorType)<<5|27, 0)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>56), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>48), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>40), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>32), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>24), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>16), n)
	if err != nil {
		return
	}

	n, err = inst.writeSingleByte(byte(val>>8), n)
	if err != nil {
		return
	}

	return inst.writeSingleByte(byte(val), n)
}
func (inst *Encoder) TagUint64(val TagUint64) (int, error) {
	return inst.encodeHead64Bit(MajorTypeTag, uint64(val))
}
