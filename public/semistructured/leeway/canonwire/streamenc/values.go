package streamenc

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// The value writers below are the SD3 forms of ADR-0210 read off an Arrow
// lane. They are deliberately parallel to canonform's values.go — the two
// forms read the same lanes — and differ exactly where that ADR differs from
// ADR-0201: no numeric reduction (a float stays a float, -0.0 keeps its
// sign), no IPv4-mapped reduction, and a set keeps its duplicates (the set
// is written by the caller through runtime.SetWriter). Integers, strings,
// bytes, bool and temporal values are the same bytes in both forms.

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

// writeScalar writes element i of arr as the wire form of a value of
// canonical type ct. An Arrow null is refused: the generated encoder never
// produces the null value form (ADR-0210 Neutral), and leeway has no null —
// absence is non-persistence — so a null in a batch is malformed input.
func writeScalar(cw *runtime.CborWriter, arr arrow.Array, i int, ct canonicaltypes.PrimitiveAstNodeI) (err error) {
	if arr.IsNull(i) {
		return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: null values are not part of the wire")
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
	return eb.Build().Stringer("canonicalType", ct).Errorf("streamenc: unsupported canonical type")
}

func writeMachineNumeric(cw *runtime.CborWriter, arr arrow.Array, i int, ct canonicaltypes.MachineNumericTypeAstNode) (err error) {
	switch ct.Width {
	case 8, 16, 32, 64:
	default:
		// 128/256-bit integers are refused, as in ADR-0201 SD3.
		return eb.Build().Stringer("canonicalType", ct).Errorf("streamenc: numeric width not supported")
	}
	switch a := arr.(type) {
	case *array.Uint8:
		cw.WriteUint(uint64(a.Value(i)))
	case *array.Uint16:
		cw.WriteUint(uint64(a.Value(i)))
	case *array.Uint32:
		cw.WriteUint(uint64(a.Value(i)))
	case *array.Uint64:
		cw.WriteUint(a.Value(i))
	case *array.Int8:
		cw.WriteInt(int64(a.Value(i)))
	case *array.Int16:
		cw.WriteInt(int64(a.Value(i)))
	case *array.Int32:
		cw.WriteInt(int64(a.Value(i)))
	case *array.Int64:
		cw.WriteInt(a.Value(i))
	case *array.Float16:
		cw.WriteF32(a.Value(i).Float32())
	case *array.Float32:
		cw.WriteF32(a.Value(i))
	case *array.Float64:
		cw.WriteF64(a.Value(i))
	default:
		return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: Arrow array type not supported for a machine-numeric column")
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

func writeStringLike(cw *runtime.CborWriter, arr arrow.Array, i int, ct canonicaltypes.StringAstNode) (err error) {
	switch ct.BaseType {
	case canonicaltypes.BaseTypeStringBool:
		if ct.WidthModifier != canonicaltypes.WidthModifierNone {
			return eb.Build().Stringer("canonicalType", ct).Errorf("streamenc: bit strings not supported")
		}
		switch a := arr.(type) {
		case *array.Boolean:
			cw.WriteBool(a.Value(i))
		case *array.Uint8: // ClickHouse Bool over some Arrow lanes
			cw.WriteBool(a.Value(i) != 0)
		default:
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: Arrow array type not supported for a bool column")
		}
	case canonicaltypes.BaseTypeStringUtf8:
		b, ok := valueBytes(arr, i)
		if !ok {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: Arrow array type not supported for a text column")
		}
		cw.WriteText(b) // fixed-width padding is content and stays (ADR-0210 SD3)
	case canonicaltypes.BaseTypeStringBytes:
		b, ok := valueBytes(arr, i)
		if !ok {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: Arrow array type not supported for a bytes column")
		}
		cw.WriteBytes(b)
	default:
		return eb.Build().Stringer("canonicalType", ct).Errorf("streamenc: unsupported string-like base type")
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

// writeTemporal writes a UTC instant as RFC 9581 tag 1001 through the one
// emission the runtime has (WriteTemporalParts), which is what the generated
// encoder's WriteTemporal drives too.
func writeTemporal(cw *runtime.CborWriter, arr arrow.Array, i int, ct canonicaltypes.TemporalTypeAstNode) (err error) {
	if ct.BaseType != canonicaltypes.BaseTypeTemporalUtcDatetime {
		return eb.Build().Stringer("canonicalType", ct).Errorf("streamenc: temporal base type not supported")
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
			return eb.Build().Stringer("arrowType", arr.DataType()).Errorf("streamenc: unknown Arrow timestamp unit")
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
		return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: Arrow array type not supported for a temporal column")
	}
	cw.WriteTemporalParts(secs, nanos)
	return
}

// writeNetwork writes an address or prefix as RFC 9164 tag 52 / 54 content,
// with no IPv4-mapped reduction (ADR-0210 SD3): a v → w widening is visible
// in the bytes. A prefix travels masked, through the runtime's one
// implementation of that rule.
func writeNetwork(cw *runtime.CborWriter, arr arrow.Array, i int, ct canonicaltypes.NetworkTypeAstNode) (err error) {
	var addr [17]byte // up to 16 address bytes plus the CIDR prefix byte
	var n int
	switch a := arr.(type) {
	case *array.Uint32: // IPv4 as a big-endian uint32 (the ClickHouse IPv4 Arrow type)
		if ct.BaseType != canonicaltypes.BaseTypeNetworkIPv4 || ct.CIDRModifier != canonicaltypes.CIDRModifierNone {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Errorf("streamenc: uint32 carries only a bare IPv4 address")
		}
		v := a.Value(i)
		addr[0], addr[1], addr[2], addr[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
		n = 4
	default:
		b, ok := valueBytes(arr, i)
		if !ok || len(b) != ct.ByteWidth() {
			return eb.Build().Stringer("canonicalType", ct).Stringer("arrowType", arr.DataType()).Int("len", len(b)).Errorf("streamenc: network value is not the packed byte width of its canonical type")
		}
		n = copy(addr[:], b)
	}
	addrLen := n
	prefix := -1
	if ct.CIDRModifier == canonicaltypes.CIDRModifierVariable {
		addrLen = n - 1
		prefix = int(addr[addrLen])
	}
	tag := runtime.TagIPv4
	if ct.BaseType == canonicaltypes.BaseTypeNetworkIPv6 {
		tag = runtime.TagIPv6
	}
	cw.Tag(tag)
	if prefix < 0 {
		cw.WriteBytes(addr[:addrLen])
		return
	}
	cw.WritePrefixContent(addr[:addrLen], prefix)
	return
}

// errNoView reports a value column the driver framed but never delivered a
// view for — the sink was driven without the ArrowValueSinkI lane.
var errNoView = eh.Errorf("streamenc: value column received no Arrow view; the driver must drive the encoder through ArrowValueSinkI")
