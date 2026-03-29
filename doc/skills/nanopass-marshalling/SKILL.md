# SKILL: ClickHouse SQL Literal Marshalling

## Overview

The `marshalling` package provides bidirectional conversion between Go values and ClickHouse SQL literal text. It uses a unified `TypedLiteral` type that represents scalars, homogeneous arrays (SoA), heterogeneous arrays, and tuples — each optionally annotated with a canonical cast type for lossless SQL round-trips.

## Package Location

`github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling`

## Core Type: `TypedLiteral`

`TypedLiteral` is the single value type for all ClickHouse SQL literals. It replaces the former separate `Literal` and `TypedLiteral` types.

```go
type TypedLiteral struct {
    Kind              TypedLiteralKind
    // Scalar fields (KindScalar)
    Null              bool
    Unknown           bool
    ScalarType        canonicaltypes.PrimitiveAstNodeI  // ctabb.S, ctabb.U64, ctabb.I64, ctabb.F64, ctabb.B
    StringVal         string
    IntVal            int64
    UintVal           uint64
    FloatVal          float64
    BoolVal           bool
    // Homogeneous array (KindHomogeneousArray)
    HomArray          *HomogeneousArray
    // Heterogeneous array / tuple (KindHeterogeneousArray, KindTuple)
    Elements          []TypedLiteral
    // Cast (any Kind)
    CastTypeCanonical string  // e.g. "u64", "sh", "" if no cast
}
```

### Kinds

| Kind | Storage | Example |
|------|---------|---------|
| `KindScalar` | Flat scalar fields | `42`, `'hello'`, `true`, `NULL` |
| `KindHomogeneousArray` | `*HomogeneousArray` (SoA packed slices) | `[1, 2, 3]` — all same scalar type, no per-element casts |
| `KindHeterogeneousArray` | `[]TypedLiteral` | `[1, 'hello']`, `[[1,2],[3,4]]` — mixed types or nested |
| `KindTuple` | `[]TypedLiteral` | `tuple(1, 'hello')` — inherently heterogeneous |

### `HomogeneousArray` (SoA layout)

```go
type HomogeneousArray struct {
    ElementType  PrimitiveAstNodeI  // which slice is active
    StringVals   []string
    IntVals      []int64
    UintVals     []uint64
    FloatVals    []float64
    BoolVals     []bool
}
```

One type, one cast annotation on parent, packed scalar values. Methods: `Len()`, `GetScalar(i)`, `AppendScalar(lit)`.

### Constructors

```go
// Scalars
NewScalarNull()                          // NULL
NewScalarString("hello")                 // 'hello'
NewScalarUint64(42)                      // 42
NewScalarInt64(-99)                      // -99
NewScalarFloat64(3.14)                   // 3.14
NewScalarBool(true)                      // true

// Homogeneous arrays (SoA)
NewHomogeneousStringArray([]string{"a","b"})
NewHomogeneousUint64Array([]uint64{1,2,3})
NewHomogeneousInt64Array([]int64{-1,0,1})
NewHomogeneousFloat64Array([]float64{1.1,2.2})
NewHomogeneousBoolArray([]bool{true,false})

// Heterogeneous arrays and tuples
NewHeterogeneousArray(elem1, elem2, ...)
NewTupleTyped(elem1, elem2, ...)

// Cast annotation
lit.WithCast("u64")  // returns copy with CastTypeCanonical set
```

### Predicates (all value receivers)

```go
lit.IsScalar()             lit.IsNull()
lit.IsHomogeneousArray()   lit.IsHeterogeneousArray()
lit.IsArray()              // either homogeneous or heterogeneous
lit.IsTuple()              lit.HasCast()
lit.ArrayLen()             // works for both array kinds
```

## API Reference

### Naming Convention

The package uses `Foo` / `FooEx` naming:
- `Foo` — convenience function with built-in default type mappers
- `FooEx` — explicit mapper parameter for custom type systems

### Scalar Operations

```go
// SQL token → TypedLiteral (KindScalar)
lit, err := UnmarshalScalarLiteral("'hello'")
lit, err := UnmarshalScalarLiteral("42")
lit, err := UnmarshalScalarLiteral("NULL")

// TypedLiteral → SQL text
sql, err := MarshalScalarToSQL(lit)

// String escaping
escaped := EscapeString("it's")              // → 'it\'s'
unescaped, err := UnescapeString("'it\\'s'") // → it's
```

### Composite Operations (cast-preserving)

```go
// SQL string → TypedLiteral (uses built-in MapClickHouseToCanonicalType)
tl, err := UnmarshalCompositeLiteral("CAST(1, 'UInt64')")
tl, err := UnmarshalCompositeLiteral("[1, 2, 3]")
tl, err := UnmarshalCompositeLiteral("tuple(CAST(1,'UInt64'), true)")

// SQL string → TypedLiteral (explicit mapper)
tl, err := UnmarshalCompositeLiteralEx(sql, myMapper)

// CST node → TypedLiteral
tl, err := UnmarshalCSTToTypedLiteral(pr, node, myMapper)

// TypedLiteral → SQL (uses built-in MapCanonicalToClickHouseTypeStr)
sql, err := MarshalTypedLiteralToSQL(tl)

// TypedLiteral → SQL (explicit mapper)
sql, err := MarshalTypedLiteralToSQLEx(tl, myMapper)
```

### Go Value Operations

```go
// Go value → SQL (no cast preservation)
sql, err := MarshalGoValueToSQL(int64(42))
sql, err := MarshalGoValueToSQL("hello")
sql, err := MarshalGoValueToSQL([]uint64{1, 2, 3})
sql, err := MarshalGoValueToSQL(tuple)

// Go value → SQL (with cast preservation for narrow types)
opts := MarshalOptions{PreserveCasts: true}
sql, err := MarshalGoValueToSQLWithOptions(float32(1.0), opts)
// → CAST(1, 'Float32')
sql, err := MarshalGoValueToSQLWithOptions([]int32{1,2}, opts)
// → CAST(array(1, 2), 'Array(Int32)')
```

**Supported Go types for `MarshalGoValueToSQL[WithOptions]`:**
`TypedLiteral`, `*TypedLiteral`, `[]TypedLiteral`, `[]any`, `[]int64`, `[]uint64`, `[]float64`, `[]float32`, `[]bool`, `[]string`, `[]int8`, `[]int16`, `[]int32`, `[]uint8`, `[]uint16`, `[]uint32`, `*Tuple`, `string`, `bool`, `int64`, `uint64`, `float64`, `float32`, `int8`, `int16`, `int32`, `uint8`, `uint16`, `uint32`, `nil`

### Conversions

```go
// TypedLiteral → Go any (for interop with Tuple and any-based APIs)
val, err := lit.ToAny()
// KindScalar    → string / uint64 / int64 / float64 / bool / nil
// KindHomArray  → []string / []uint64 / []int64 / []float64 / []bool (copies)
// KindHetArray  → []any (recursive)
// KindTuple     → *Tuple (recursive)

// Homogeneous ↔ Heterogeneous array conversion
het, err := homLit.ToHeterogeneous()    // expand SoA → individual elements
hom, ok  := hetLit.TryHomogeneous()     // pack if all same scalar type, no casts
```

### Type Mapping

```go
// ClickHouse type name → canonical PrimitiveAstNodeI
ct, err := MapClickHouseToCanonicalType("UInt64")  // → ctabb.U64

// Canonical → ClickHouse type name
chType, err := MapCanonicalToClickHouseType(ctabb.U64)      // → "UInt64"
chType, err := MapCanonicalToClickHouseTypeStr("u64")        // → "UInt64"
```

Supported types: `UInt8/16/32/64`, `Int8/16/32/64`, `Float32/64`, `String`, `Bool`.

## `Tuple` (Go `any`-based interop)

`Tuple` provides named/unnamed slot access for Go `any` values. Used by `MarshalGoValueToSQL` and `ToAny()`.

```go
tup := NewUnnamedTuple(int64(1), "hello", true)
tup := NewTuple([]string{"id", "name"})
tup.SetByName("id", int64(42))

val, found := tup.GetByIndex(0)
val, found := tup.GetByName("id")

for i, val := range tup.IterateAll() { ... }
for name, val := range tup.IterateAllWithNames() { ... }
```

## Design Decisions

1. **Unified `TypedLiteral`** — No separate `Literal` type. `TypedLiteral` with `Kind` discriminator handles everything from bare scalars to cast-annotated nested tuples.

2. **Canonical cast types** — `CastTypeCanonical` stores canonical strings (e.g. `"u64"`, `"sh"`), not ClickHouse type names. Type mapping functions required for SQL serialization. Non-homogeneous composite types (e.g. `Tuple(UInt8, String)`) are not stored.

3. **SoA homogeneous arrays** — `HomogeneousArray` uses Struct-of-Arrays layout with one active typed slice. Automatic detection via `TryHomogeneous()` during unmarshal.

4. **`array()` vs `[...]` syntax** — `MarshalGoValueToSQL` uses `array()` function form (safer in SET contexts). `MarshalTypedLiteralToSQL` uses `[...]` bracket form. Both are valid ClickHouse.

5. **`ToAny()` returns typed slices** — Homogeneous arrays produce `[]uint64`, `[]string`, etc. (not `[]any`), preserving Go type safety and enabling direct use with `MarshalGoValueToSQL`.

6. **Value receivers on `TypedLiteral`** — No mutation. All methods return copies. `HomogeneousArray` uses pointer receivers because `AppendScalar` mutates.

7. **`Foo`/`FooEx` pattern** — Convenience functions with built-in mappers alongside explicit-mapper variants. The built-in mappers cover the 12 common ClickHouse primitive types.

## Escape Sequences

`UnescapeString` / `EscapeString` handle:
- `\\` ↔ `\`, `\'` ↔ `'`, `\n` ↔ newline, `\t` ↔ tab, `\r` ↔ CR
- `\0` ↔ NUL, `\b` ↔ backspace, `\f` ↔ form feed, `\a` ↔ bell, `\v` ↔ vtab
- `\xHH` ↔ byte, `\uHHHH` ↔ BMP, `\UHHHHHHHH` ↔ full Unicode
- `''` → `'` (unmarshal only, doubled-quote form)

## Known Limitations

1. **No Map/Date/DateTime/Enum/Decimal/FixedString/LowCardinality** — Only the 12 primitive types are supported in type mappings.
2. **Always `u64`/`i64`** — Does not infer smallest integer type like ClickHouse does.
3. **`MarshalTypedLiteralToSQLEx` always uses `CAST(expr, 'Type')`** — Never `expr::Type`.
4. **`CastTypeCanonical` not stored for non-homogeneous composite types** — e.g. `Tuple(UInt64, String)` cannot be represented as a single canonical string.
5. **`ToAny()` drops `CastTypeCanonical`** — The `any` world has no cast tracking.

## Integration with Nanopass

```go
// In passes package — extract literals, iterate, deserialize
config := passes.NewExtractLiteralsConfig(1)
config.SetMapTypeToCanonical(func(ch string) (canonicaltypes.PrimitiveAstNodeI, error) {
    return marshalling.MapClickHouseToCanonicalType(ch)
})
extracted, _ := passes.ExtractLiterals(config)(sql)

for _, info := range passes.IterateExtractedParams(extracted, "") {
    val, _ := info.Value()         // returns TypedLiteral
    goVal, _ := val.ToAny()        // bridge to any
    sql, _ := marshalling.MarshalTypedLiteralToSQL(val)  // back to SQL
}
```
