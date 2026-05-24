//go:build llm_generated_opus47

// Package marshallgen is the generic Go DTO → leeway codec generator.
// It parses an annotated Go DTO source file and emits a sibling
// `.out.go` carrying the schema-agnostic core: <Kind>Columns SoA
// storage, Append/Row adapters, derived per-section / per-membership
// interfaces, and the generic <Kind>BuildEntities + <Kind>FillFromArrow
// helpers that bind to any leeway DML / RA via Go type inference at
// the call site.
//
// Anything schema-specific — kind-id resolution, dml backend pool,
// per-kind active-fields hints, Marshal/Unmarshal methods, codec
// bridge — lives behind WrapperEmitterI hooks the caller passes in.
// NoOpWrapper produces the schema-agnostic surface only; consumers
// layer their own wrapper for full-stack emit.
//
// The generator never inspects section names or canonical types. Wire
// shape is determined entirely by (Go shape, lw: flag) tuples; the Go
// compiler verifies section / type compatibility at the call site of
// the generated BuildEntities / FillFromArrow against the typed DML.
package marshallgen

// Plan is the parsed DTO ready for emission. ParsePlan produces it from
// a single .go source file; EmitPlan consumes it.
type Plan struct {
	InputPath   string
	PackageName string
	KindType    string // Go struct type name, e.g. "M1Sample"
	KindName    string // entity-level kind tag from the `_` field, e.g. "m1Sample"
	PlainCols   []PlainCol
	Fields      []TaggedField
}

// PlainCol describes one fact-row plain column wired from a Go field
// via an empty-membership `lw:` tag (e.g. `Id uint64 `+"`lw:\",id\"`"+`).
// Plain columns route to physical row columns (id / ts / naturalKey /
// expiresAt) — fixed by the schema, distinct from tagged-value
// sections. Emitted in DTO declaration order.
type PlainCol struct {
	Column  string // wire column name: id / ts / naturalKey / expiresAt
	GoField string // matching DTO struct field name
	GoType  string // matching DTO field Go type, e.g. "uint64" / "time.Time" / "[]byte"
}

// FieldFlags carries the boolean opt-ins parsed from the lw: tag's
// trailing flag positions. They are orthogonal:
//
//   - Unit selects BeginAttributeSingle over BeginAttribute when
//     emitting per-attribute value calls. Meaningful on scalar shapes
//     (T, Option[T]) and on per-element calls under Explode.
//
//   - Explode iterates the multi-element value and emits one attribute
//     per element instead of one container attribute carrying every
//     value. Meaningful on multi-element shapes ([]T, *roaring.Bitmap,
//     [][]byte). Without it, the default for multi-element fields is
//     one attribute with N container values (the bitmap-style 1×N
//     wire shape).
//
//   - Verbatim switches the membership channel from LowCardRef (uint64
//     id resolved by the wrapper via a registry) to LowCardVerbatim
//     (literal []byte of the lw: membership name embedded directly on
//     the wire). Requires the target section to declare
//     `MembershipSpecLowCardVerbatim` in its schema; otherwise the
//     generated code fails to compile at the BuildEntities call site.
//
// Begin-shape combinations:
//
//	(none)         scalar    → BeginAttribute(v)                       1×1
//	Unit           scalar    → BeginAttributeSingle(v)                 1×1
//	(none)         multi     → BeginAttribute()+AddToContainer*N+End   1×N
//	Explode        multi     → for v: BeginAttribute(v)                N×1
//	Explode+Unit   multi     → for v: BeginAttributeSingle(v)          N×1
//
// Unit alone on a multi shape or Explode on a scalar shape is rejected
// by ParsePlan. Verbatim is orthogonal to the above and may combine
// with any. All fields targeting the same section must agree on the
// membership channel (all Verbatim or all Ref) — mixed sections
// rejected because the read-side dispatch loops differ in element
// type.
type FieldFlags struct {
	Unit     bool
	Explode  bool
	Verbatim bool

	// HasConst signals that `,const=<value>` appeared in the lw: tag.
	// Combined with the field being a `_` blank identifier, this
	// produces a TaggedField with IsConst=true and ConstValue set.
	// Non-`_` fields carrying HasConst are rejected by the parser.
	HasConst   bool
	ConstValue string
}

// TaggedField is one DTO field (or constant) bound to a leeway
// membership via an lw: tag.
type TaggedField struct {
	GoFieldName string // DTO struct field name; "" when IsConst is true

	// GoType is the inner element type in source form. For Option[T] it
	// is T; for []T it is T; for *roaring.Bitmap it is "*roaring.Bitmap".
	// For constant fields it is "string" — constant values are always
	// string-typed in this version.
	GoType    string
	IsOption  bool // option.Option[T] wrapper — Option[[]byte] uses the scalar-blob lane
	IsSlice   bool // []T element-slice (top-level, non-byte)
	IsRoaring bool // *roaring.Bitmap

	LWMembership string // first comma-segment of the lw: tag
	LWSection    string // second comma-segment ("" if author omitted; section[:column])
	LWColumn     string // sub-column suffix after ':' (e.g. "beginIncl" / "endExcl") or ""

	Flags FieldFlags // trailing comma-separated flag tokens (unit / explode / verbatim / const)

	// IsConst marks fields that emit a fixed string value on every row
	// rather than reading from a Go DTO field. Declared on `_` blank-
	// identifier fields with `lw:"<membership>,<section>,const=<value>"`.
	// Constant values are string-typed only; section must accept
	// strings (symbol / symbolArray / text / textArray / stringArray).
	IsConst    bool
	ConstValue string
}

// KindVar returns the package-level identifier holding the resolved
// membership id for this field. Wrapper code (FactsWrapper /
// NoOpWrapper) declares the symbol — facts target as a var assigned
// from vdd in init(), anchor target as a const in declaration order.
// The generated BuildEntities references it by name. Multi-sub-column
// sections share a single KindVar per membership.
//
// For const fields the identifier is keyed on LWMembership (no Go
// field name). Verbatim memberships return "" since no kindXxx var is
// declared for them — the literal []byte is embedded at the call site.
func (f TaggedField) KindVar() string {
	if f.Flags.Verbatim {
		return ""
	}
	if f.IsConst {
		return "kind" + upperFirstASCII(f.LWMembership)
	}
	return "kind" + f.GoFieldName
}

func upperFirstASCII(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
