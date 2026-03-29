//go:build llm_generated_opus46

package marshalling

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// TypedLiteralKind discriminates the value category of a TypedLiteral.
type TypedLiteralKind uint8

const (
	// KindScalar indicates a scalar value (string, number, bool, null).
	KindScalar TypedLiteralKind = iota

	// KindHomogeneousArray indicates a flat 1-D array where all elements share the
	// same scalar type. Values are stored in SoA layout via HomogeneousArray.
	KindHomogeneousArray

	// KindHeterogeneousArray indicates an array where elements may have different types,
	// casts, or are themselves composites (nested arrays/tuples). Elements are stored
	// as []TypedLiteral.
	KindHeterogeneousArray

	// KindTuple indicates a tuple value. Elements are stored as []TypedLiteral.
	KindTuple
)

func (k TypedLiteralKind) String() string {
	switch k {
	case KindScalar:
		return "scalar"
	case KindHomogeneousArray:
		return "homogeneous_array"
	case KindHeterogeneousArray:
		return "heterogeneous_array"
	case KindTuple:
		return "tuple"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

// TypedLiteral is the unified value type for ClickHouse SQL literals.
//
// For scalars (Kind == KindScalar), the active value field is determined by ScalarType:
//   - ctabb.S  → StringVal
//   - ctabb.U64 → UintVal
//   - ctabb.I64 → IntVal
//   - ctabb.F64 → FloatVal
//   - ctabb.B  → BoolVal
//   - nil (Null=true) → no value field active
//
// For homogeneous arrays (Kind == KindHomogeneousArray), values are in HomArray.
// For heterogeneous arrays/tuples (Kind == KindHeterogeneousArray, KindTuple),
// values are in Elements.
type TypedLiteral struct {
	Kind TypedLiteralKind

	// --- Scalar fields (valid when Kind == KindScalar) ---
	Null       bool
	Unknown    bool
	ScalarType canonicaltypes.PrimitiveAstNodeI
	StringVal  string
	IntVal     int64
	UintVal    uint64
	FloatVal   float64
	BoolVal    bool

	// --- Homogeneous 1-D array (valid when Kind == KindHomogeneousArray) ---
	HomArray *HomogeneousArray

	// --- Heterogeneous array / tuple (valid when Kind == KindHeterogeneousArray or KindTuple) ---
	Elements []TypedLiteral

	// --- Cast info (valid for any Kind) ---
	// Canonical type string from an explicit cast. Empty if no cast.
	// Only set when representable in the canonical type system.
	CastTypeCanonical string
}

// HomogeneousArray stores a flat 1-D array of scalar values in SoA layout.
// Exactly one value slice is active, determined by ElementType.
type HomogeneousArray struct {
	ElementType canonicaltypes.PrimitiveAstNodeI
	StringVals  []string
	IntVals     []int64
	UintVals    []uint64
	FloatVals   []float64
	BoolVals    []bool
}

// --- HomogeneousArray methods ---

func (a *HomogeneousArray) Len() int {
	if a == nil || a.ElementType == nil {
		return 0
	}
	switch a.ElementType.String() {
	case "s":
		return len(a.StringVals)
	case "i64":
		return len(a.IntVals)
	case "u64":
		return len(a.UintVals)
	case "f64":
		return len(a.FloatVals)
	case "b":
		return len(a.BoolVals)
	default:
		return 0
	}
}

func (a *HomogeneousArray) GetScalar(i int) (result TypedLiteral, err error) {
	if a == nil {
		err = eh.Errorf("HomogeneousArray.GetScalar: nil array")
		return
	}
	n := a.Len()
	if i < 0 || i >= n {
		err = eh.Errorf("HomogeneousArray.GetScalar: index %d out of range [0, %d)", i, n)
		return
	}
	result.Kind = KindScalar
	result.ScalarType = a.ElementType
	switch a.ElementType.String() {
	case "s":
		result.StringVal = a.StringVals[i]
	case "i64":
		result.IntVal = a.IntVals[i]
	case "u64":
		result.UintVal = a.UintVals[i]
	case "f64":
		result.FloatVal = a.FloatVals[i]
	case "b":
		result.BoolVal = a.BoolVals[i]
	default:
		err = eh.Errorf("HomogeneousArray.GetScalar: unsupported element type %s", a.ElementType)
	}
	return
}

func (a *HomogeneousArray) AppendScalar(lit TypedLiteral) error {
	if lit.Kind != KindScalar {
		return eh.Errorf("HomogeneousArray.AppendScalar: expected KindScalar, got %s", lit.Kind)
	}
	if lit.ScalarType == nil || lit.ScalarType.String() != a.ElementType.String() {
		return eh.Errorf("HomogeneousArray.AppendScalar: type mismatch: got %v, expected %s", lit.ScalarType, a.ElementType)
	}
	switch a.ElementType.String() {
	case "s":
		a.StringVals = append(a.StringVals, lit.StringVal)
	case "i64":
		a.IntVals = append(a.IntVals, lit.IntVal)
	case "u64":
		a.UintVals = append(a.UintVals, lit.UintVal)
	case "f64":
		a.FloatVals = append(a.FloatVals, lit.FloatVal)
	case "b":
		a.BoolVals = append(a.BoolVals, lit.BoolVal)
	default:
		return eh.Errorf("HomogeneousArray.AppendScalar: unsupported element type %s", a.ElementType)
	}
	return nil
}

// --- Constructors: Scalars ---

func NewScalarNull() TypedLiteral {
	return TypedLiteral{Kind: KindScalar, Null: true}
}
func NewScalarString(val string) TypedLiteral {
	return TypedLiteral{Kind: KindScalar, ScalarType: ctabb.S, StringVal: val}
}
func NewScalarUint64(val uint64) TypedLiteral {
	return TypedLiteral{Kind: KindScalar, ScalarType: ctabb.U64, UintVal: val}
}
func NewScalarInt64(val int64) TypedLiteral {
	return TypedLiteral{Kind: KindScalar, ScalarType: ctabb.I64, IntVal: val}
}
func NewScalarFloat64(val float64) TypedLiteral {
	return TypedLiteral{Kind: KindScalar, ScalarType: ctabb.F64, FloatVal: val}
}
func NewScalarBool(val bool) TypedLiteral {
	return TypedLiteral{Kind: KindScalar, ScalarType: ctabb.B, BoolVal: val}
}

// --- Constructors: Homogeneous arrays ---

func NewHomogeneousArray(elementType canonicaltypes.PrimitiveAstNodeI, capacity int) *HomogeneousArray {
	a := &HomogeneousArray{ElementType: elementType}
	switch elementType.String() {
	case "s":
		a.StringVals = make([]string, 0, capacity)
	case "i64":
		a.IntVals = make([]int64, 0, capacity)
	case "u64":
		a.UintVals = make([]uint64, 0, capacity)
	case "f64":
		a.FloatVals = make([]float64, 0, capacity)
	case "b":
		a.BoolVals = make([]bool, 0, capacity)
	}
	return a
}

func NewHomogeneousStringArray(vals []string) TypedLiteral {
	return TypedLiteral{Kind: KindHomogeneousArray, HomArray: &HomogeneousArray{ElementType: ctabb.S, StringVals: vals}}
}
func NewHomogeneousUint64Array(vals []uint64) TypedLiteral {
	return TypedLiteral{Kind: KindHomogeneousArray, HomArray: &HomogeneousArray{ElementType: ctabb.U64, UintVals: vals}}
}
func NewHomogeneousInt64Array(vals []int64) TypedLiteral {
	return TypedLiteral{Kind: KindHomogeneousArray, HomArray: &HomogeneousArray{ElementType: ctabb.I64, IntVals: vals}}
}
func NewHomogeneousFloat64Array(vals []float64) TypedLiteral {
	return TypedLiteral{Kind: KindHomogeneousArray, HomArray: &HomogeneousArray{ElementType: ctabb.F64, FloatVals: vals}}
}
func NewHomogeneousBoolArray(vals []bool) TypedLiteral {
	return TypedLiteral{Kind: KindHomogeneousArray, HomArray: &HomogeneousArray{ElementType: ctabb.B, BoolVals: vals}}
}

// --- Constructors: Heterogeneous arrays and tuples ---

func NewHeterogeneousArray(elems ...TypedLiteral) TypedLiteral {
	if elems == nil {
		elems = make([]TypedLiteral, 0)
	}
	return TypedLiteral{Kind: KindHeterogeneousArray, Elements: elems}
}
func NewTupleTyped(elems ...TypedLiteral) TypedLiteral {
	if elems == nil {
		elems = make([]TypedLiteral, 0)
	}
	return TypedLiteral{Kind: KindTuple, Elements: elems}
}

// --- Predicates ---

func (t TypedLiteral) IsScalar() bool             { return t.Kind == KindScalar }
func (t TypedLiteral) IsHomogeneousArray() bool   { return t.Kind == KindHomogeneousArray }
func (t TypedLiteral) IsHeterogeneousArray() bool { return t.Kind == KindHeterogeneousArray }
func (t TypedLiteral) IsArray() bool {
	return t.Kind == KindHomogeneousArray || t.Kind == KindHeterogeneousArray
}
func (t TypedLiteral) IsTuple() bool                  { return t.Kind == KindTuple }
func (t TypedLiteral) IsNull() bool                   { return t.Kind == KindScalar && t.Null }
func (t TypedLiteral) HasCast() bool                  { return t.CastTypeCanonical != "" }
func (t TypedLiteral) WithCast(c string) TypedLiteral { t.CastTypeCanonical = c; return t }

// ArrayLen returns the number of elements for any array kind. Returns 0 for non-arrays.
func (t TypedLiteral) ArrayLen() int {
	switch t.Kind {
	case KindHomogeneousArray:
		return t.HomArray.Len()
	case KindHeterogeneousArray:
		return len(t.Elements)
	default:
		return 0
	}
}

// --- Conversions ---

// ToHeterogeneous converts KindHomogeneousArray to KindHeterogeneousArray
// by expanding packed values into individual TypedLiteral elements.
// Returns a copy. No-op for other kinds.
func (t TypedLiteral) ToHeterogeneous() (result TypedLiteral, err error) {
	if t.Kind != KindHomogeneousArray {
		result = t
		return
	}
	if t.HomArray == nil {
		result = NewHeterogeneousArray()
		result.CastTypeCanonical = t.CastTypeCanonical
		return
	}
	n := t.HomArray.Len()
	elems := make([]TypedLiteral, n)
	for i := 0; i < n; i++ {
		elems[i], err = t.HomArray.GetScalar(i)
		if err != nil {
			err = eh.Errorf("ToHeterogeneous: element %d: %w", i, err)
			return
		}
	}
	result = NewHeterogeneousArray(elems...)
	result.CastTypeCanonical = t.CastTypeCanonical
	return
}

// TryHomogeneous attempts to convert KindHeterogeneousArray to KindHomogeneousArray.
// Succeeds only if all elements are KindScalar with the same ScalarType and no casts.
// Returns (converted, true) on success, (original, false) on failure.
func (t TypedLiteral) TryHomogeneous() (result TypedLiteral, ok bool) {
	if t.Kind != KindHeterogeneousArray || len(t.Elements) == 0 {
		return t, false
	}
	var elementType canonicaltypes.PrimitiveAstNodeI
	for i, elem := range t.Elements {
		if elem.Kind != KindScalar || elem.Null || elem.HasCast() || elem.ScalarType == nil {
			return t, false
		}
		if i == 0 {
			elementType = elem.ScalarType
		} else if elem.ScalarType.String() != elementType.String() {
			return t, false
		}
	}
	homArray := NewHomogeneousArray(elementType, len(t.Elements))
	for _, elem := range t.Elements {
		if appendErr := homArray.AppendScalar(elem); appendErr != nil {
			return t, false
		}
	}
	result = TypedLiteral{
		Kind:              KindHomogeneousArray,
		HomArray:          homArray,
		CastTypeCanonical: t.CastTypeCanonical,
	}
	return result, true
}

// ToAny converts a TypedLiteral to a plain Go value (any).
//
// Conversion rules:
//   - KindScalar, Null=true         → nil
//   - KindScalar, ScalarType "s"    → string
//   - KindScalar, ScalarType "u64"  → uint64
//   - KindScalar, ScalarType "i64"  → int64
//   - KindScalar, ScalarType "f64"  → float64
//   - KindScalar, ScalarType "b"    → bool
//   - KindHomogeneousArray, "s"     → []string
//   - KindHomogeneousArray, "u64"   → []uint64
//   - KindHomogeneousArray, "i64"   → []int64
//   - KindHomogeneousArray, "f64"   → []float64
//   - KindHomogeneousArray, "b"     → []bool
//   - KindHeterogeneousArray        → []any (elements recursively converted)
//   - KindTuple                     → *Tuple (elements recursively converted)
//
// CastTypeCanonical is NOT preserved in the output — use MarshalTypedLiteralToSQL
// or MarshalGoValueToSQLWithOptions for cast-preserving serialization.
func (t TypedLiteral) ToAny() (val any, err error) {
	switch t.Kind {
	case KindScalar:
		return scalarToAny(t)

	case KindHomogeneousArray:
		return homogeneousArrayToAny(t.HomArray)

	case KindHeterogeneousArray:
		return heterogeneousArrayToAny(t.Elements)

	case KindTuple:
		return tupleToAny(t.Elements)

	default:
		err = fmt.Errorf("TypedLiteral.ToAny: unknown kind %s", t.Kind)
		return
	}
}

func scalarToAny(t TypedLiteral) (val any, err error) {
	if t.Null {
		return nil, nil
	}
	if t.ScalarType == nil {
		err = fmt.Errorf("TypedLiteral.ToAny: nil ScalarType on non-null scalar")
		return
	}
	switch t.ScalarType.String() {
	case "s":
		return t.StringVal, nil
	case "u64":
		return t.UintVal, nil
	case "i64":
		return t.IntVal, nil
	case "f64":
		return t.FloatVal, nil
	case "b":
		return t.BoolVal, nil
	default:
		err = fmt.Errorf("TypedLiteral.ToAny: unsupported scalar type %s", t.ScalarType)
		return
	}
}

func homogeneousArrayToAny(a *HomogeneousArray) (val any, err error) {
	if a == nil || a.ElementType == nil {
		return make([]any, 0), nil
	}
	switch a.ElementType.String() {
	case "s":
		// Return a copy to avoid aliasing
		out := make([]string, len(a.StringVals))
		copy(out, a.StringVals)
		return out, nil
	case "u64":
		out := make([]uint64, len(a.UintVals))
		copy(out, a.UintVals)
		return out, nil
	case "i64":
		out := make([]int64, len(a.IntVals))
		copy(out, a.IntVals)
		return out, nil
	case "f64":
		out := make([]float64, len(a.FloatVals))
		copy(out, a.FloatVals)
		return out, nil
	case "b":
		out := make([]bool, len(a.BoolVals))
		copy(out, a.BoolVals)
		return out, nil
	default:
		err = fmt.Errorf("TypedLiteral.ToAny: unsupported homogeneous array element type %s", a.ElementType)
		return
	}
}

func heterogeneousArrayToAny(elems []TypedLiteral) (val any, err error) {
	out := make([]any, len(elems))
	for i, elem := range elems {
		out[i], err = elem.ToAny()
		if err != nil {
			err = fmt.Errorf("TypedLiteral.ToAny: array element %d: %w", i, err)
			return
		}
	}
	return out, nil
}

func tupleToAny(elems []TypedLiteral) (val any, err error) {
	values := make([]any, len(elems))
	for i, elem := range elems {
		values[i], err = elem.ToAny()
		if err != nil {
			err = fmt.Errorf("TypedLiteral.ToAny: tuple element %d: %w", i, err)
			return
		}
	}
	return NewUnnamedTuple(values...), nil
}
