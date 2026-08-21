package canonform

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// scalarOf returns ct with its scalar modifier (h / m) cleared — the element
// type of a container column.
func scalarOf(ct canonicaltypes.PrimitiveAstNodeI) canonicaltypes.PrimitiveAstNodeI {
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.MachineNumericTypeAstNode:
		m := *n
		m.ScalarModifier = canonicaltypes.ScalarModifierNone
		return m
	case canonicaltypes.StringAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.StringAstNode:
		m := *n
		m.ScalarModifier = canonicaltypes.ScalarModifierNone
		return m
	case canonicaltypes.TemporalTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.TemporalTypeAstNode:
		m := *n
		m.ScalarModifier = canonicaltypes.ScalarModifierNone
		return m
	case canonicaltypes.NetworkTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.NetworkTypeAstNode:
		m := *n
		m.ScalarModifier = canonicaltypes.ScalarModifierNone
		return m
	}
	return ct
}

// writeScalar writes element i of arr as the canonical form of a value of
// canonical type ct (ADR-0201 SD3). The Arrow array decides how the value is
// read; ct decides what it means (text vs. bytes, temporal, network). An
// Arrow null writes CBOR null.
func writeScalar(cw *cborWriter, arr arrow.Array, i int, ct canonicaltypes.PrimitiveAstNodeI) (err error) {
	if arr.IsNull(i) {
		cw.writeNull()
		return
	}
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		return writeMachineNumeric(cw, arr, i, n)
	case *canonicaltypes.MachineNumericTypeAstNode:
		return writeMachineNumeric(cw, arr, i, *n)
	case canonicaltypes.StringAstNode:
		return writeStringLike(cw, arr, i, n)
	case *canonicaltypes.StringAstNode:
		return writeStringLike(cw, arr, i, *n)
	case canonicaltypes.TemporalTypeAstNode:
		return writeTemporal(cw, arr, i, n)
	case *canonicaltypes.TemporalTypeAstNode:
		return writeTemporal(cw, arr, i, *n)
	case canonicaltypes.NetworkTypeAstNode:
		return writeNetwork(cw, arr, i, n)
	case *canonicaltypes.NetworkTypeAstNode:
		return writeNetwork(cw, arr, i, *n)
	}
	return eb.Build().Stringer("canonicalType", ct).Errorf("canonform: unsupported canonical type")
}

func writeMachineNumeric(cw *cborWriter, arr arrow.Array, i int, ct canonicaltypes.MachineNumericTypeAstNode) (err error) {
	switch ct.Width {
	case 8, 16, 32, 64:
	default:
		// 128/256-bit integers: refused at M0 (ADR-0201 SD3); they become
		// bignums (tags 2/3) when a lane produces them, byte-compatible for
		// every value that fits 64 bits.
		return eb.Build().Stringer("canonicalType", ct).Errorf("canonform: numeric width not supported")
	}
	switch a := arr.(type) {
	case *array.Uint8:
		cw.writeUint(uint64(a.Value(i)))
	case *array.Uint16:
		cw.writeUint(uint64(a.Value(i)))
	case *array.Uint32:
		cw.writeUint(uint64(a.Value(i)))
	case *array.Uint64:
		cw.writeUint(a.Value(i))
	case *array.Int8:
		cw.writeInt(int64(a.Value(i)))
	case *array.Int16:
		cw.writeInt(int64(a.Value(i)))
	case *array.Int32:
		cw.writeInt(int64(a.Value(i)))
	case *array.Int64:
		cw.writeInt(a.Value(i))
	case *array.Float16:
		cw.writeFloat(float64(a.Value(i).Float32()))
	case *array.Float32:
		cw.writeFloat(float64(a.Value(i)))
	case *array.Float64:
		cw.writeFloat(a.Value(i))
	default:
		return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: Arrow array type not supported for a machine-numeric column")
	}
	return
}

// valueBytes returns the bytes of a string-like Arrow element without copying.
func valueBytes(arr arrow.Array, i int) (b []byte, ok bool) {
	ok = true
	switch a := arr.(type) {
	case *array.String:
		b = unsafeperf.UnsafeStringToBytes(a.Value(i))
	case *array.LargeString:
		b = unsafeperf.UnsafeStringToBytes(a.Value(i))
	case *array.Binary:
		b = a.Value(i)
	case *array.LargeBinary:
		b = a.Value(i)
	case *array.FixedSizeBinary:
		b = a.Value(i)
	default:
		ok = false
	}
	return
}

func writeStringLike(cw *cborWriter, arr arrow.Array, i int, ct canonicaltypes.StringAstNode) (err error) {
	switch ct.BaseType {
	case canonicaltypes.BaseTypeStringBool:
		if ct.WidthModifier != canonicaltypes.WidthModifierNone {
			// Bit strings are unimplemented on every lane (ADR-0201 SD3).
			return eb.Build().Stringer("canonicalType", ct).Errorf("canonform: bit strings not supported")
		}
		switch a := arr.(type) {
		case *array.Boolean:
			cw.writeBool(a.Value(i))
		case *array.Uint8: // ClickHouse Bool over some Arrow lanes
			cw.writeBool(a.Value(i) != 0)
		default:
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: Arrow array type not supported for a bool column")
		}
	case canonicaltypes.BaseTypeStringUtf8:
		b, ok := valueBytes(arr, i)
		if !ok {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: Arrow array type not supported for a text column")
		}
		cw.writeText(b) // verbatim: no Unicode normalization, the fixed-width modifier is erased, padding is kept (SD3)
	case canonicaltypes.BaseTypeStringBytes:
		b, ok := valueBytes(arr, i)
		if !ok {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: Arrow array type not supported for a bytes column")
		}
		cw.writeBytes(b)
	default:
		return eb.Build().Stringer("canonicalType", ct).Errorf("canonform: unsupported string-like base type")
	}
	return
}

// floorDivMod splits v by d (> 0) into floor quotient and non-negative
// remainder, so a pre-epoch instant keeps nanoseconds in [0, d).
func floorDivMod(v int64, d int64) (q int64, r int64) {
	q = v / d
	r = v % d
	if r < 0 {
		q--
		r += d
	}
	return
}

// writeTemporal writes a UTC instant as RFC 9581 tag 1001: key 1 = integer
// Unix seconds (floor), key -9 = integer nanoseconds, present only when
// non-zero (ADR-0201 SD3). The Arrow unit and the canonical width are erased
// by integer arithmetic; a whole-second instant encodes identically from z32
// and z64. The keys sort 0x01 < 0x28 bytewise, so the map is written 1, -9.
func writeTemporal(cw *cborWriter, arr arrow.Array, i int, ct canonicaltypes.TemporalTypeAstNode) (err error) {
	if ct.BaseType != canonicaltypes.BaseTypeTemporalUtcDatetime {
		// Zoned datetime / time are unimplemented on every lane (SD3).
		return eb.Build().Stringer("canonicalType", ct).Errorf("canonform: temporal base type not supported")
	}
	var secs, nanos int64
	switch a := arr.(type) {
	case *array.Timestamp:
		v := int64(a.Value(i))
		switch a.DataType().(*arrow.TimestampType).Unit {
		case arrow.Second:
			secs = v
		case arrow.Millisecond:
			secs, nanos = floorDivMod(v, 1_000)
			nanos *= 1_000_000
		case arrow.Microsecond:
			secs, nanos = floorDivMod(v, 1_000_000)
			nanos *= 1_000
		case arrow.Nanosecond:
			secs, nanos = floorDivMod(v, 1_000_000_000)
		default:
			return eb.Build().Stringer("arrowType", arr.DataType()).Errorf("canonform: unknown Arrow timestamp unit")
		}
	case *array.Uint32: // ClickHouse DateTime over Arrow: seconds
		secs = int64(a.Value(i))
	case *array.Int64: // a raw epoch integer: nanoseconds at width 64, seconds at width 32
		if ct.Width == 64 {
			secs, nanos = floorDivMod(a.Value(i), 1_000_000_000)
		} else {
			secs = a.Value(i)
		}
	default:
		return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: Arrow array type not supported for a temporal column")
	}
	cw.tag(tagExtendedTime)
	if nanos == 0 {
		cw.mapHead(1)
		cw.writeUint(1)
		cw.writeInt(secs)
		return
	}
	cw.mapHead(2)
	cw.writeUint(1)
	cw.writeInt(secs)
	cw.writeInt(-9)
	cw.writeUint(uint64(nanos))
	return
}

// ipv4MappedPrefixLen is the prefix length of ::ffff:0:0/96, the IPv4-mapped
// IPv6 range (RFC 4291 §2.5.5.2).
const ipv4MappedPrefixLen = 96

func isIPv4Mapped(addr []byte) bool {
	if len(addr) != 16 {
		return false
	}
	for _, b := range addr[:10] {
		if b != 0 {
			return false
		}
	}
	return addr[10] == 0xff && addr[11] == 0xff
}

// writeNetwork writes an address or prefix as RFC 9164 tag 52 / 54 content
// (ADR-0201 SD3): an address is its 4- or 16-byte string; a prefix is
// [prefix-length, address bytes with the unused bits zeroed and trailing zero
// bytes omitted]. An IPv6 value in the IPv4-mapped range is reduced to IPv4
// (a prefix of length ≥ 96 reduces by 96), so a v → w widening is invariant.
func writeNetwork(cw *cborWriter, arr arrow.Array, i int, ct canonicaltypes.NetworkTypeAstNode) (err error) {
	var addr [17]byte // up to 16 address bytes plus the CIDR prefix byte
	var n int
	switch a := arr.(type) {
	case *array.Uint32: // IPv4 as a big-endian uint32 (the ClickHouse IPv4 Arrow type)
		if ct.BaseType != canonicaltypes.BaseTypeNetworkIPv4 || ct.CIDRModifier != canonicaltypes.CIDRModifierNone {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("canonform: uint32 carries only a bare IPv4 address")
		}
		v := a.Value(i)
		addr[0], addr[1], addr[2], addr[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
		n = 4
	default:
		b, ok := valueBytes(arr, i)
		if !ok || len(b) != ct.ByteWidth() {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Int("len", len(b)).Errorf("canonform: network value is not the packed byte width of its canonical type")
		}
		n = copy(addr[:], b)
	}
	// Split the packed form into address bytes and an optional prefix length.
	addrLen := n
	prefix := -1
	if ct.CIDRModifier == canonicaltypes.CIDRModifierVariable {
		addrLen = n - 1
		prefix = int(addr[addrLen])
	}
	tag := tagIPv4
	if ct.BaseType == canonicaltypes.BaseTypeNetworkIPv6 {
		tag = tagIPv6
		if addrLen == 16 && isIPv4Mapped(addr[:16]) && (prefix < 0 || prefix >= ipv4MappedPrefixLen) {
			copy(addr[:4], addr[12:16])
			addrLen = 4
			tag = tagIPv4
			if prefix >= 0 {
				prefix -= ipv4MappedPrefixLen
			}
		}
	}
	cw.tag(tag)
	if prefix < 0 {
		cw.writeBytes(addr[:addrLen])
		return
	}
	if prefix > addrLen*8 {
		return eb.Build().Int("prefix", prefix).Int("addrLen", addrLen).Errorf("canonform: prefix length exceeds the address width")
	}
	// RFC 9164 §3.2 encoder rules: zero the bits beyond the prefix, then omit
	// trailing zero bytes.
	full := prefix / 8
	if rem := prefix % 8; rem != 0 {
		addr[full] &= byte(0xff << (8 - rem))
		full++
	}
	for k := full; k < addrLen; k++ {
		addr[k] = 0
	}
	end := full
	for end > 0 && addr[end-1] == 0 {
		end--
	}
	cw.arrayHead(2)
	cw.writeUint(uint64(prefix))
	cw.writeBytes(addr[:end])
	return
}

// errNoView reports a value column the driver framed but never delivered a
// view for — the sink was driven without the ArrowValueSinkI lane.
var errNoView = eh.Errorf("canonform: value column received no Arrow view; the driver must drive the encoder through ArrowValueSinkI")
