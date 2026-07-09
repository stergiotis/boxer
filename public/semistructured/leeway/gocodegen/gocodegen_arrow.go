package gocodegen

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// isIPv4Host reports whether ct is a non-CIDR IPv4 address. Such a column
// rides as a big-endian uint32 — the Arrow representation ClickHouse's IPv4
// column round-trips (toIPv4(0x01020304) = '1.2.3.4') — whereas every other
// network shape (IPv6, and the CIDR variants that carry a trailing
// prefix-length byte) stays a packed FixedSizeBinary / [N]byte.
func isIPv4Host(ct canonicaltypes2.NetworkTypeAstNode) bool {
	return ct.BaseType == canonicaltypes2.BaseTypeNetworkIPv4 && ct.CIDRModifier == canonicaltypes2.CIDRModifierNone
}

func ArrowTypeToGoType(ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, useDictionaryEncoding bool) (prefix string, suffix string, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.TemporalTypeAstNode:
		var unit string
		switch ctt.Width {
		case 32:
			unit = "Millisecond"
		case 64:
			unit = "Nanosecond"
		default:
			err = eb.Build().Int("width", int(ctt.Width)).Errorf("unhandled temporal width: %w", common.ErrNotImplemented)
			return
		}
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeTemporalUtcDatetime:
			prefix = ""
			suffix = ".ToTime(arrow." + unit + ")"
		case canonicaltypes2.BaseTypeTemporalZonedDatetime:
			// Reading a zoned datetime as a UTC time.Time silently drops the
			// zone; not implemented end-to-end yet (tracked: zoned temporal).
			err = common.ErrNotImplemented
		case canonicaltypes2.BaseTypeTemporalZonedTime:
			err = common.ErrNotImplemented
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
	case canonicaltypes2.NetworkTypeAstNode:
		// An IPv4 host is an array.Uint32 whose Value(i) is already the uint32 Go
		// type — no conversion. Every other network shape is a FixedSizeBinary:
		// array.FixedSizeBinary.Value(i) returns a []byte view, but the Go-native
		// type is a packed [ByteWidth]byte (see codegen.generateNetworkType), so
		// convert the slice to the fixed-size array. The width matches by
		// construction, so the Go 1.20+ slice-to-array conversion cannot panic.
		// deferred: fixed-width byte strings (StringAstNode + WidthModifierFixed +
		// BaseTypeStringBytes) share the FixedSizeBinary backing and a [width]byte
		// Go type, so they need this same conversion; wire it in when that path is
		// exercised end-to-end (no ctabb abbreviation / golden covers it today).
		if !isIPv4Host(ctt) {
			prefix = fmt.Sprintf("[%d]byte(", ctt.ByteWidth())
			suffix = ")"
		}
	}
	return
}
func GoTypeToArrowType(ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, useDictionaryEncoding bool) (prefix string, suffix string, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeStringUtf8:
			var builderCls string
			builderCls, _, err = CanonicalTypeToArrowBaseClassName(ct, hints, useDictionaryEncoding)
			if err != nil {
				err = eh.Errorf("unable to get arrow builder class name: %w", err)
				return
			}
			if builderCls == "BinaryDictionary" {
				prefix = "unsafeperf.UnsafeStringToBytes("
				suffix = ")"
			}
		case canonicaltypes2.BaseTypeStringBytes:
			break
		case canonicaltypes2.BaseTypeStringBool:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicaltypes2.WidthModifierNone:
			break
		case canonicaltypes2.WidthModifierFixed:
			suffix += "[:]"
		}
	case canonicaltypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeMachineNumericUnsigned:
			break
		case canonicaltypes2.BaseTypeMachineNumericSigned:
			break
		case canonicaltypes2.BaseTypeMachineNumericFloat:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
	case canonicaltypes2.NetworkTypeAstNode:
		// packed [N]byte array: slice to []byte for the FixedSizeBinary builder.
		// An IPv4 host is a uint32 appended directly (no slice).
		if !isIPv4Host(ctt) {
			suffix = "[:]"
		}
	case canonicaltypes2.TemporalTypeAstNode:
		var unit string
		switch ctt.Width {
		case 32:
			unit = ".UnixMilli()"
		case 64:
			unit = ".UnixNano()"
		default:
			err = eb.Build().Int("width", int(ctt.Width)).Errorf("unhandled temporal width: %w", common.ErrNotImplemented)
			return
		}
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeTemporalUtcDatetime:
			prefix = "arrow.Timestamp("
			suffix = unit + ")"
		case canonicaltypes2.BaseTypeTemporalZonedDatetime:
			err = common.ErrNotImplemented
		case canonicaltypes2.BaseTypeTemporalZonedTime:
			err = common.ErrNotImplemented
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
	default:
		err = eb.Build().Type("type", ct).Errorf("unhandled canonical type")
		return
	}
	return
}
func CanonicalTypeToArrowBaseClassName(ct canonicaltypes2.PrimitiveAstNodeI, encodingHints encodingaspects.AspectSet, useDictionaryEncoding bool) (name string, mayError bool, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeStringUtf8:
			name = "String"
		case canonicaltypes2.BaseTypeStringBytes:
			name = "Binary"
		case canonicaltypes2.BaseTypeStringBool:
			name = "Boolean"
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicaltypes2.WidthModifierNone:
			break
		case canonicaltypes2.WidthModifierFixed:
			name = "FixedSize" + name
		}
	case canonicaltypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeMachineNumericUnsigned:
			name = fmt.Sprintf("Uint%d", ctt.Width)
		case canonicaltypes2.BaseTypeMachineNumericSigned:
			name = fmt.Sprintf("Int%d", ctt.Width)
		case canonicaltypes2.BaseTypeMachineNumericFloat:
			name = fmt.Sprintf("Float%d", ctt.Width)
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
	case canonicaltypes2.TemporalTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeTemporalUtcDatetime:
			name = "Timestamp"
		case canonicaltypes2.BaseTypeTemporalZonedDatetime, canonicaltypes2.BaseTypeTemporalZonedTime:
			// Both were silently mapped to a plain Timestamp, dropping the zone;
			// not implemented end-to-end yet (tracked: zoned temporal).
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("zoned temporal not implemented: %w", common.ErrNotImplemented)
			return
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
	case canonicaltypes2.NetworkTypeAstNode:
		// IPv4 host → big-endian uint32 (the ClickHouse IPv4 Arrow type); every
		// other network shape stays a packed FixedSizeBinary.
		if isIPv4Host(ctt) {
			name = "Uint32"
		} else {
			name = "FixedSizeBinary"
		}
	default:
		err = eb.Build().Type("type", ct).Errorf("unhandled canonical type")
		return
	}
	dictEncoding := false
	for _, asp := range encodingHints.IterateAspects() {
		switch asp {
		case encodingaspects.AspectIntraRecordLowCardinality,
			encodingaspects.AspectInterRecordLowCardinality:
			dictEncoding = true
		}
	}
	if dictEncoding && useDictionaryEncoding {
		mayError = true
		switch name {
		case "String":
			name = "BinaryDictionary"
		default:
			name += "Dictionary"
		}
	}
	return

}
