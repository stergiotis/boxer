package eb

import (
	"bytes"
	"fmt"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/cbor/builder"
	"github.com/zeebo/xxh3"
	"hash"
	"net"
	"net/netip"
	"reflect"
	"time"
)

type ErrorBuilder struct {
	structuredData *bytes.Buffer
	encoder        *cbor.Encoder
	hasher         hash.Hash
	open           bool
}

var _ builder.CborKVBuilder[*ErrorBuilder] = (*ErrorBuilder)(nil)

func Build() *ErrorBuilder {
	buf := bytes.NewBuffer(make([]byte, 0, 500))
	hasher := xxh3.New()
	enc := cbor.NewEncoder(buf, hasher)
	_, _ = enc.EncodeMapIndefinite()
	return &ErrorBuilder{
		structuredData: buf,
		encoder:        enc,
		hasher:         hasher,
		open:           true,
	}
}
func (inst *ErrorBuilder) Reset() {
	inst.structuredData.Reset()
	inst.encoder.Reset()
	inst.open = true
}
func (inst *ErrorBuilder) Type(key string, val any) *ErrorBuilder {
	return inst.Str(key, reflect.TypeOf(val).String())
}
func (inst *ErrorBuilder) Str(key string, val string) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeString(val)
	return inst
}
func (inst *ErrorBuilder) Strs(key string, val []string) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeString(v)
	}
	return inst
}
func (inst *ErrorBuilder) Bool(key string, val bool) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeBool(val)
	return inst
}
func (inst *ErrorBuilder) Bools(key string, val []bool) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeBool(v)
	}
	return inst
}
func (inst *ErrorBuilder) Uint(key string, val uint) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeUint(uint64(val))
	return inst
}
func (inst *ErrorBuilder) Uints(key string, val []uint) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeUint(uint64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Uint8(key string, val uint8) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeUint(uint64(val))
	return inst
}
func (inst *ErrorBuilder) Uints8(key string, val []uint8) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeUint(uint64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Uint16(key string, val uint16) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeUint(uint64(val))
	return inst
}
func (inst *ErrorBuilder) Uints16(key string, val []uint16) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeUint(uint64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Uint32(key string, val uint32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeUint(uint64(val))
	return inst
}
func (inst *ErrorBuilder) Uints32(key string, val []uint32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeUint(uint64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Uint64(key string, val uint64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeUint(val)
	return inst
}
func (inst *ErrorBuilder) Uints64(key string, val []uint64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeUint(v)
	}
	return inst
}
func (inst *ErrorBuilder) Int(key string, val int) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeInt(int64(val))
	return inst
}
func (inst *ErrorBuilder) Ints(key string, val []int) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeInt(int64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Int8(key string, val int8) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeInt(int64(val))
	return inst
}
func (inst *ErrorBuilder) Ints8(key string, val []int8) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeInt(int64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Int16(key string, val int16) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeInt(int64(val))
	return inst
}
func (inst *ErrorBuilder) Ints16(key string, val []int16) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeInt(int64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Int32(key string, val int32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeInt(int64(val))
	return inst
}
func (inst *ErrorBuilder) Ints32(key string, val []int32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeInt(int64(v))
	}
	return inst
}
func (inst *ErrorBuilder) Int64(key string, val int64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeInt(val)
	return inst
}
func (inst *ErrorBuilder) Ints64(key string, val []int64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeInt(v)
	}
	return inst
}
func (inst *ErrorBuilder) Float32(key string, val float32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeFloat32(val)
	return inst
}
func (inst *ErrorBuilder) Floats32(key string, val []float32) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeFloat32(v)
	}
	return inst
}
func (inst *ErrorBuilder) Float64(key string, val float64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeFloat64(val)
	return inst
}
func (inst *ErrorBuilder) Floats64(key string, val []float64) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeFloat64(v)
	}
	return inst
}
func (inst *ErrorBuilder) Bytes(key string, val []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if val == nil {
		_, _ = inst.encoder.EncodeNil()
	} else {
		_, _ = inst.encoder.EncodeByteSlice(val)
	}
	return inst
}

func (inst *ErrorBuilder) Stringer(key string, val fmt.Stringer) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeString(val.String())
	return inst
}

func (inst *ErrorBuilder) Stringers(key string, val []fmt.Stringer) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeString(v.String())
	}
	return inst
}

func (inst *ErrorBuilder) Hex(key string, val []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeTagSmall(cbor.TagExpectConversionToHex)
	_, _ = inst.encoder.EncodeByteSlice(val)
	return inst
}

func (inst *ErrorBuilder) RawJSON(key string, b []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeJsonPayload(b)
	return inst
}

func (inst *ErrorBuilder) RawCBOR(key string, b []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeTag8(cbor.TagEncodedCBORSequence)
	_, _ = inst.encoder.EncodeByteSlice(b)
	return inst
}

func (inst *ErrorBuilder) Time(key string, val time.Time) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeTimeUTC(val)
	return inst
}

func (inst *ErrorBuilder) Times(key string, val []time.Time) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		_, _ = inst.encoder.EncodeTimeUTC(v)
	}
	return inst
}

func (inst *ErrorBuilder) IPAddr(key string, ip net.IP) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	b := ip.To4()
	if b != nil {
		_, _ = inst.encoder.EncodeIpAddr(netip.AddrFrom4([4]byte(b)))
	} else {
		_, _ = inst.encoder.EncodeIpAddr(netip.AddrFrom16([16]byte(b.To16())))
	}
	return inst
}

func (inst *ErrorBuilder) IsOpen() bool {
	return inst.open
}

func (inst *ErrorBuilder) Errorf(format string, a ...any) error {
	inst.open = false
	_, _ = inst.encoder.EncodeBreak()
	buf := make([]byte, inst.structuredData.Len())
	copy(buf, inst.structuredData.Bytes())
	return eh.ErrorfWithData(buf, format, a...)
}
