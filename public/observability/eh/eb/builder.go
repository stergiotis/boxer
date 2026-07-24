package eb

import (
	"bytes"
	"fmt"
	"hash"
	"net"
	"net/netip"
	"reflect"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/cbor/builder"
	"github.com/zeebo/xxh3"
)

type ErrorBuilder struct {
	structuredData *bytes.Buffer
	encoder        *cbor.Encoder
	hasher         hash.Hash
	open           bool
	withoutStack   bool
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
		withoutStack:   false,
	}
}
func (inst *ErrorBuilder) Reset() {
	inst.structuredData.Reset()
	inst.encoder.Reset()
	// Re-open the indefinite map that Build() emits. Without this the
	// reused builder produces a headerless key/value run terminated by a
	// stray break, which no CBOR decoder accepts.
	_, _ = inst.encoder.EncodeMapIndefinite()
	inst.open = true
	inst.withoutStack = false
}
func (inst *ErrorBuilder) WithoutStack() *ErrorBuilder {
	inst.withoutStack = true
	return inst
}
func (inst *ErrorBuilder) WithStack() *ErrorBuilder {
	inst.withoutStack = false
	return inst
}
func (inst *ErrorBuilder) Type(key string, val any) *ErrorBuilder {
	t := reflect.TypeOf(val)
	if t == nil {
		return inst.Str(key, "<unknown>")
	} else {
		return inst.Str(key, t.String())
	}
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

// Stringer encodes val.String() under key. A nil Stringer encodes as CBOR
// null rather than panicking — this builder runs on error paths, where a
// panic would replace the report with a crash.
func (inst *ErrorBuilder) Stringer(key string, val fmt.Stringer) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if val == nil {
		_, _ = inst.encoder.EncodeNil()
	} else {
		_, _ = inst.encoder.EncodeString(val.String())
	}
	return inst
}

func (inst *ErrorBuilder) Stringers(key string, val []fmt.Stringer) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	_, _ = inst.encoder.EncodeArrayDefinite(uint64(len(val)))
	for _, v := range val {
		if v == nil {
			_, _ = inst.encoder.EncodeNil()
		} else {
			_, _ = inst.encoder.EncodeString(v.String())
		}
	}
	return inst
}

// Hex encodes val as a byte string tagged for hex rendering. A nil slice
// encodes as CBOR null; emitting the tag alone would leave a dangling tag
// with no content item and make the whole payload undecodable.
func (inst *ErrorBuilder) Hex(key string, val []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if val == nil {
		_, _ = inst.encoder.EncodeNil()
		return inst
	}
	_, _ = inst.encoder.EncodeTagSmall(cbor.TagExpectConversionToHex)
	_, _ = inst.encoder.EncodeByteSlice(val)
	return inst
}

func (inst *ErrorBuilder) RawJSON(key string, b []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if b == nil {
		_, _ = inst.encoder.EncodeNil()
		return inst
	}
	_, _ = inst.encoder.EncodeJsonPayload(b)
	return inst
}

func (inst *ErrorBuilder) RawCBOR(key string, b []byte) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if b == nil {
		_, _ = inst.encoder.EncodeNil()
		return inst
	}
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

// IPAddr encodes ip as a tagged CBOR address: 4 bytes when it has an IPv4
// form, 16 otherwise. A nil or malformed net.IP (any length other than 4
// or 16) encodes as CBOR null.
func (inst *ErrorBuilder) IPAddr(key string, ip net.IP) *ErrorBuilder {
	if !inst.open {
		return inst
	}
	_, _ = inst.encoder.EncodeString(key)
	if b := ip.To4(); b != nil {
		_, _ = inst.encoder.EncodeIpAddr(netip.AddrFrom4([4]byte(b)))
		return inst
	}
	if b := ip.To16(); b != nil {
		_, _ = inst.encoder.EncodeIpAddr(netip.AddrFrom16([16]byte(b)))
		return inst
	}
	_, _ = inst.encoder.EncodeNil()
	return inst
}

func (inst *ErrorBuilder) IsOpen() bool {
	return inst.open
}

// Errorf closes the payload and returns the error carrying it. Calling it
// more than once returns further errors over the same already-closed
// payload; only the first call emits the map's break.
func (inst *ErrorBuilder) Errorf(format string, a ...any) error {
	if inst.open {
		inst.open = false
		_, _ = inst.encoder.EncodeBreak()
	}
	buf := make([]byte, inst.structuredData.Len())
	copy(buf, inst.structuredData.Bytes())
	if inst.withoutStack {
		return eh.ErrorfWithDataWithoutStack(buf, format, a...)
	} else {
		return eh.ErrorfWithData(buf, format, a...)
	}
}
