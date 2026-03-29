package marshalling

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

func MapClickHouseToCanonicalType(chType string) (ct canonicaltypes.PrimitiveAstNodeI, err error) {
	switch chType {
	case "UInt8":
		return ctabb.U8, nil
	case "UInt16":
		return ctabb.U16, nil
	case "UInt32":
		return ctabb.U32, nil
	case "UInt64":
		return ctabb.U64, nil
	case "Int8":
		return ctabb.I8, nil
	case "Int16":
		return ctabb.I16, nil
	case "Int32":
		return ctabb.I32, nil
	case "Int64":
		return ctabb.I64, nil
	case "Float32":
		return ctabb.F32, nil
	case "Float64":
		return ctabb.F64, nil
	case "String":
		return ctabb.S, nil
	case "Bool":
		return ctabb.B, nil
	default:
		err = eb.Build().Str("chType", chType).Errorf("unknown ClickHouse type")
		return
	}
}

func MapCanonicalToClickHouseType(ct canonicaltypes.PrimitiveAstNodeI) (chType string, err error) {
	return MapCanonicalToClickHouseTypeStr(ct.String())
}
func MapCanonicalToClickHouseTypeStr(ct string) (chType string, err error) {
	switch ct {
	case "u8":
		return "UInt8", nil
	case "u16":
		return "UInt16", nil
	case "u32":
		return "UInt32", nil
	case "u64":
		return "UInt64", nil
	case "i8":
		return "Int8", nil
	case "i16":
		return "Int16", nil
	case "i32":
		return "Int32", nil
	case "i64":
		return "Int64", nil
	case "f32":
		return "Float32", nil
	case "f64":
		return "Float64", nil
	case "s":
		return "String", nil
	case "b":
		return "Bool", nil
	default:
		err = eb.Build().Str("ct", ct).Errorf("unknown canonical type")
		return
	}
}
