package play

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// play_schema_infer.go is the Schema pane's FALLBACK schema derivation.
//
// The faithful path is not here: a leeway table encodes its whole structure —
// sections, membership roles, co-section groups, canonical types, encoding
// hints — into its physical column names, and the play app reconstructs the
// authored [common.TableDesc] from them in exactly one place, [CardDriver]
// (which the Detail card already relies on). resultTableDesc prefers that
// reconstruction.
//
// This file handles only the leftover case: a non-leeway result — an
// aggregation, a join, a projection that renames columns, or a plain
// non-leeway table — whose names don't parse as leeway. Then there is no
// section structure to recover, so every column becomes a plain OPAQUE value
// with a best-effort canonical type off its Arrow physical type.
// LowCardinality (dictionary-encoded) columns keep their one recoverable
// encoding hint.

// inferredTableName titles the inferred schema in the navigator header. Cast
// verbatim rather than validated — it is display-only and never round-trips
// through a naming convention.
const inferredTableName = naming.StylableName("query-result")

// inferOpaqueTableDesc builds a shallow TableDesc for a non-leeway result:
// every field becomes a plain opaque value column with a best-effort canonical
// type. Used only when the CardDriver could not reconstruct a real leeway
// schema (see resultTableDesc).
func inferOpaqueTableDesc(fields []arrow.Field) *common.TableDesc {
	td := &common.TableDesc{
		DictionaryEntry: common.TableDictionaryEntryDescDto{
			Name:    inferredTableName,
			Comment: fmt.Sprintf("inferred from the result's Arrow schema — %d column(s), shown as plain opaque values", len(fields)),
		},
	}
	for i := range fields {
		f := &fields[i]
		ct, lowCard := arrowToCanonical(f.Type)
		hints := encodingaspects.EmptyAspectSet
		if lowCard {
			hints = encodingaspects.EncodeAspectsIgnoreInvalid(encodingaspects.AspectInterRecordLowCardinality)
		}
		td.PlainValuesNames = append(td.PlainValuesNames, columnName(f.Name))
		td.PlainValuesTypes = append(td.PlainValuesTypes, ct)
		td.PlainValuesItemTypes = append(td.PlainValuesItemTypes, common.PlainItemTypeOpaque)
		td.PlainValuesEncodingHints = append(td.PlainValuesEncodingHints, hints)
		td.PlainValuesValueSemantics = append(td.PlainValuesValueSemantics, valueaspects.EmptyAspectSet)
	}
	return td
}

// columnName carries the Arrow field name through verbatim. Result column names
// come straight from arbitrary SQL (`count()`, `a + b`, quoted mixed-case) and
// routinely aren't valid StylableNames; the inspector calls only String() on
// them, so a verbatim cast shows the operator exactly what their result holds
// rather than a normalised approximation.
func columnName(name string) naming.StylableName {
	return naming.StylableName(name)
}

// arrowToCanonical maps an Arrow physical type to the leeway canonical type it
// would have been generated from (the inverse of ddl/arrow's GenerateType).
// lowCard reports that the column is dictionary-encoded (ClickHouse
// LowCardinality), which the caller records as an encoding hint. An unmapped
// type yields (nil, false); the widget renders a nil type as an empty-type
// placeholder rather than failing.
func arrowToCanonical(dt arrow.DataType) (ct canonicaltypes.PrimitiveAstNodeI, lowCard bool) {
	switch dt.ID() {
	case arrow.INT8:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericSigned, 8), false
	case arrow.INT16:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericSigned, 16), false
	case arrow.INT32:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericSigned, 32), false
	case arrow.INT64:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericSigned, 64), false
	case arrow.UINT8:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericUnsigned, 8), false
	case arrow.UINT16:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericUnsigned, 16), false
	case arrow.UINT32:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericUnsigned, 32), false
	case arrow.UINT64:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericUnsigned, 64), false
	case arrow.FLOAT32:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericFloat, 32), false
	case arrow.FLOAT64:
		return machineNumeric(canonicaltypes.BaseTypeMachineNumericFloat, 64), false
	case arrow.BOOL:
		return canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBool}, false
	case arrow.STRING, arrow.LARGE_STRING:
		return canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}, false
	case arrow.BINARY, arrow.LARGE_BINARY:
		return canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes}, false
	case arrow.FIXED_SIZE_BINARY:
		w := dt.(*arrow.FixedSizeBinaryType).ByteWidth
		return canonicaltypes.StringAstNode{
			BaseType:      canonicaltypes.BaseTypeStringBytes,
			WidthModifier: canonicaltypes.WidthModifierFixed,
			Width:         canonicaltypes.Width(w),
		}, false
	case arrow.TIMESTAMP:
		return canonicaltypes.TemporalTypeAstNode{
			BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime,
			Width:    temporalWidth(dt.(*arrow.TimestampType).Unit),
		}, false
	case arrow.DATE32:
		return canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 32}, false
	case arrow.DATE64:
		return canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 64}, false
	case arrow.DICTIONARY:
		// A dictionary is an encoding of its value type — carry the value
		// type's canonical form and flag the low-cardinality hint.
		inner, _ := arrowToCanonical(dt.(*arrow.DictionaryType).ValueType)
		if inner == nil {
			return nil, false
		}
		return inner, true
	case arrow.LIST:
		return promoteList(dt.(*arrow.ListType).Elem())
	case arrow.LARGE_LIST:
		return promoteList(dt.(*arrow.LargeListType).Elem())
	case arrow.FIXED_SIZE_LIST:
		return promoteList(dt.(*arrow.FixedSizeListType).Elem())
	}
	return nil, false
}

// promoteList maps an Arrow list to its element's canonical type carrying the
// homogeneous-array scalar modifier (the inverse of arrow.ListOfNonNullable).
// A list of an unmapped element is itself unmapped.
func promoteList(elem arrow.DataType) (ct canonicaltypes.PrimitiveAstNodeI, lowCard bool) {
	inner, low := arrowToCanonical(elem)
	if inner == nil {
		return nil, false
	}
	return canonicaltypes.PromoteScalarPrim(inner, canonicaltypes.ScalarModifierHomogenousArray), low
}

// machineNumeric builds a scalar machine-numeric node. Byte order is left at
// the canonical default — Arrow batches are native-endian and the canonical
// form doesn't pin one for display.
func machineNumeric(base canonicaltypes.BaseTypeMachineNumericE, width canonicaltypes.Width) canonicaltypes.MachineNumericTypeAstNode {
	return canonicaltypes.MachineNumericTypeAstNode{BaseType: base, Width: width}
}

// temporalWidth buckets an Arrow timestamp unit into the canonical temporal
// widths ddl/arrow emits: 32-bit for second/millisecond resolution, 64-bit for
// the sub-millisecond units that need the wider range.
func temporalWidth(u arrow.TimeUnit) canonicaltypes.Width {
	switch u {
	case arrow.Second, arrow.Millisecond:
		return 32
	default: // Microsecond, Nanosecond
		return 64
	}
}
