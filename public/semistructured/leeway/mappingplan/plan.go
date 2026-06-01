//go:build llm_generated_opus47

// Package mappingplan is the schema-agnostic model of a leeway DTO ↔
// codec mapping: the parsed Plan (plain columns + tagged-value fields),
// the lw: tag grammar (SplitLW), per-field validation and assembly
// (PlanBuilder), the membership-channel enum, section grouping, and
// field-shape classification.
//
// Two front-ends produce a Plan and two back-ends drive it: marshallgen
// (go/ast → Plan → generated codec) and marshallreflect (reflect → Plan
// → runtime codec). Keeping the model here lets both depend on it as
// siblings — with no dependency on the code generator and no go/ast or
// reflect pulled into this package.
package mappingplan

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

// MembershipChannel selects one of the leeway membership channels the
// DML exposes via AddMembership*P. Per ADR-0008 D3, every lw:-tagged
// field selects one channel; the section-level uniformity invariant
// (one channel per section) holds across all channels.
//
// Only the four "simple" channels are implemented: LowCardRef (the
// default), LowCardVerbatim, HighCardRef, and HighCardVerbatim. The
// parametrized / mixed channels from ADR-0008 D3 (Cut 2) are recognised
// as flag spellings but rejected by SplitLW until they are implemented.
//
// The default (zero value) is MembershipChannelLowCardRef so existing
// DTOs without a channel flag continue to compile.
type MembershipChannel uint8

const (
	// MembershipChannelLowCardRef is the default channel: a uint64
	// membership id resolved via the wrapper / lookup registry, emitted
	// via AddMembershipLowCardRefP.
	MembershipChannelLowCardRef MembershipChannel = iota
	// MembershipChannelLowCardVerbatim embeds the lw: tag's literal
	// []byte name on the wire via AddMembershipLowCardVerbatimP. The
	// `,verbatim` flag is the original spelling; `,lowCardVerbatim` is
	// the explicit alias added by ADR-0008.
	MembershipChannelLowCardVerbatim
	// MembershipChannelHighCardRef mirrors LowCardRef on the high-card
	// channel: AddMembershipHighCardRefP(uint64).
	MembershipChannelHighCardRef
	// MembershipChannelHighCardVerbatim mirrors LowCardVerbatim on the
	// high-card channel: AddMembershipHighCardVerbatimP([]byte).
	MembershipChannelHighCardVerbatim
)

// String returns the lw: flag spelling for this channel. The default
// (LowCardRef) returns "" because it requires no flag in a tag.
func (c MembershipChannel) String() string {
	switch c {
	case MembershipChannelLowCardRef:
		return ""
	case MembershipChannelLowCardVerbatim:
		return "lowCardVerbatim"
	case MembershipChannelHighCardRef:
		return "highCardRef"
	case MembershipChannelHighCardVerbatim:
		return "highCardVerbatim"
	}
	return "unknown"
}

// EmbedsLiteralName reports whether the channel's wire identity is the
// literal lw: membership name as []byte rather than a uint64 id from
// the lookup registry. True for LowCardVerbatim and HighCardVerbatim.
func (c MembershipChannel) EmbedsLiteralName() bool {
	switch c {
	case MembershipChannelLowCardVerbatim, MembershipChannelHighCardVerbatim:
		return true
	default:
		return false
	}
}

// NeedsKindVar reports whether emitted code requires a package-level
// kindXxx symbol holding the resolved uint64 membership id for this
// channel. True for the ref channels (LowCardRef, HighCardRef); false
// for the verbatim channels, which embed a []byte name directly on the
// wire.
func (c MembershipChannel) NeedsKindVar() bool {
	switch c {
	case MembershipChannelLowCardRef,
		MembershipChannelHighCardRef:
		return true
	default:
		return false
	}
}

// ReadIterElemType returns the source-form Go element type yielded by
// the read-side `GetMembValue<Suffix>` iterator for this channel.
// Used by the emitter to declare the right `iter.Seq[T]` type in the
// generated <Sec>MembsReadI interface.
func (c MembershipChannel) ReadIterElemType() string {
	switch c {
	case MembershipChannelLowCardRef,
		MembershipChannelHighCardRef:
		return "uint64"
	default:
		return "[]byte"
	}
}

// AddMethodSuffix returns the suffix used on the DML's per-channel
// AddMembership<Suffix>P method names (e.g. "LowCardRef" →
// "AddMembershipLowCardRefP"). The suffix also matches the prefix of
// the read-side `GetMembValue<Suffix>` accessor on the readaccess
// runtime.
func (c MembershipChannel) AddMethodSuffix() string {
	switch c {
	case MembershipChannelLowCardRef:
		return "LowCardRef"
	case MembershipChannelLowCardVerbatim:
		return "LowCardVerbatim"
	case MembershipChannelHighCardRef:
		return "HighCardRef"
	case MembershipChannelHighCardVerbatim:
		return "HighCardVerbatim"
	}
	return ""
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
//   - Channel selects one of the eight leeway membership channels.
//     Default is LowCardRef (uint64 id resolved via lookup). Each
//     channel has a matching `lw:` flag spelling — see
//     MembershipChannel.String. The historical `,verbatim` flag is
//     accepted as an alias for `,lowCardVerbatim`.
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
// by ParsePlan. Channel is orthogonal to the above and may combine
// with any. All fields targeting the same section must agree on the
// Channel — mixed-channel sections rejected because the read-side
// dispatch iterator type differs per channel.
type FieldFlags struct {
	Unit    bool
	Explode bool
	Channel MembershipChannel

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
// field name). Channels that do not consult the registry (Verbatim
// and Parametrized non-mixed) return "" — no kindXxx is declared.
func (f TaggedField) KindVar() string {
	if !f.Flags.Channel.NeedsKindVar() {
		return ""
	}
	if f.IsConst {
		return "kind" + UpperFirst(f.LWMembership)
	}
	return "kind" + f.GoFieldName
}

// Section returns the trusted section name from the lw: tag — the
// PascalCase seed for the DML's GetSection<X>() getter. The lw: string
// is trusted verbatim; the Go compiler verifies the resulting call.
func (f TaggedField) Section() string { return f.LWSection }
