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

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

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

	// Canonical is the authoritative leeway canonical type for this column's
	// value — the single source of truth. The Go-facing type is derived from
	// it on demand by GoType(), never stored. Plain columns are always scalar.
	Canonical canonicaltypes.PrimitiveAstNodeI
}

// GoType returns the plain column's Go type in source form (e.g. "uint64",
// "time.Time", "[]byte"), derived from Canonical — the single source of truth.
func (p PlainCol) GoType() string {
	goType, _, _, err := deriveGoShape(p.Canonical)
	if err != nil {
		panic("mappingplan: PlainCol.GoType on a column with no canonical type — corrupt plan: " + err.Error())
	}
	return goType
}

// MembershipChannel selects one of the leeway membership channels the
// DML exposes via AddMembership*P. Per ADR-0008 D3, every lw:-tagged
// field selects one channel; the section-level uniformity invariant
// (one channel per section) holds across all channels.
//
// All eight channels are implemented (ADR-0008 D3, Cut-2 complete): the
// four "simple" channels — LowCardRef (the default), LowCardVerbatim,
// HighCardRef, HighCardVerbatim — plus the four carrier channels —
// MixedLowCardRef, MixedLowCardVerbatim, and the two parametrized
// channels — which pair a value field with a marshalltypes carrier
// sibling (UsesCarrier reports true).
//
// Naming: a "mixed" channel pairs a low-card membership identity (a uint64
// id or a verbatim []byte name) with a high-card per-row params blob. The
// leeway DML protocol exposes no high-card-identity mixed method, so
// MixedLowCardRef and MixedLowCardVerbatim are the only mixed channels;
// high-card identities are reached via HighCardRef / HighCardVerbatim (no
// params) or HighCardRefParametrized (opaque params-only). There is
// deliberately no MixedHighCard* channel.
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
	// MembershipChannelMixedLowCardRef is the first Cut-2 channel
	// (ADR-0008 D3 Cut-2 update): AddMembershipMixedLowCardRefP(id uint64,
	// params []byte). Both the id and the params are per-row carrier data
	// (a marshalltypes.MixedLowCardRef sibling field), not a registry
	// lookup — so NeedsKindVar is false. UsesCarrier reports true.
	MembershipChannelMixedLowCardRef
	// MembershipChannelMixedLowCardVerbatim is the verbatim sibling:
	// AddMembershipMixedLowCardVerbatimP(name []byte, params []byte). The
	// membership label is embedded literally (a marshalltypes.MixedLowCardVerbatim
	// carrier's []byte Name) rather than a uint64 id, but the carrier /
	// one-membership-per-section dispatch model is identical to MixedLowCardRef.
	MembershipChannelMixedLowCardVerbatim
	// MembershipChannelLowCardRefParametrized and …HighCardRefParametrized
	// are the Cut-2 parametrized channels: AddMembership<X>RefParametrizedP(blob
	// []byte). The whole membership identity is an opaque per-row params blob
	// (a marshalltypes.Parametrized carrier — no separate id/name field), so
	// the read is a single Seq[[]byte] rather than the mixed channels' Seq2.
	// CarrierValueField is "" for both, which discriminates them from the
	// mixed channels everywhere the codec branches.
	MembershipChannelLowCardRefParametrized
	MembershipChannelHighCardRefParametrized
)

// The three carriage axes of a membership channel (ADR-0072 plane D). The flat
// MembershipChannel enum stays the dispatch key; these make the product it
// encodes — cardinality × identity × params — first-class and queryable. The
// realized eight channels are a sparse subset of the grid, validated below.

// ChannelCardinalityE is the dictionary-cardinality axis: Low (dictionary-
// encodable) or High.
type ChannelCardinalityE uint8

const (
	ChannelCardinalityLow ChannelCardinalityE = iota
	ChannelCardinalityHigh
)

// ChannelIdentityE is the identity-encoding axis. Ref resolves a registry
// uint64 id; Verbatim embeds the literal lw: name as []byte; PerRow carries the
// identity as per-row carrier data (a uint64 id, a []byte name, or the opaque
// params blob — CarrierValueField holds the sub-distinction).
type ChannelIdentityE uint8

const (
	ChannelIdentityRef ChannelIdentityE = iota
	ChannelIdentityVerbatim
	ChannelIdentityPerRow
)

// channelDescriptor is the single per-channel fact row. Every accessor
// method on MembershipChannel reads exactly one field from here — adding
// a channel is one new table entry, not an edit to N parallel switches.
// This table is also the de-facto registry coupling the schema-agnostic
// model to the leeway runtime's method-naming convention (the DML's
// AddMembership<addMethodSuffix>P, the RA's GetMembValue<…> accessors,
// and the marshalltypes carrier struct + field names).
type channelDescriptor struct {
	// flag is the lw: tag spelling (String). "" for the default LowCardRef.
	flag string
	// addMethodSuffix is the DML AddMembership<Suffix>P / RA GetMembValue<Suffix> suffix.
	addMethodSuffix string
	// carrierType is the marshalltypes carrier struct name a carrier channel pairs with.
	carrierType string
	// carrierReadSuffix is the RA combined-accessor suffix (GetMembValue<Suffix>) for carriers.
	carrierReadSuffix string
	// carrierSeq2Types is the iter.Seq2 element-type list for a mixed channel's combined read.
	carrierSeq2Types string
	// carrierValueField is the carrier struct field holding the membership value ("Id"/"Name").
	carrierValueField string
	// readIterElemType is the source-form element type of the read-side membership iterator.
	readIterElemType string
	// usesCarrier marks the mixed / parametrized channels (membership identity is per-row carrier data).
	usesCarrier bool
	// carrierValueBytes marks a []byte carrier value field (needs a defensive copy on read).
	carrierValueBytes bool
	// embedsLiteralName marks channels whose wire identity is the literal lw: name as []byte.
	embedsLiteralName bool
	// needsKindVar marks ref channels that require a package-level kindXxx resolved id symbol.
	needsKindVar bool

	// The three ADR-0072 carriage axes, made explicit (plane D). The enum value
	// stays the dispatch key; these summarise its product. validateChannelTable
	// asserts they cannot drift from the behavioural fields above.
	cardinality ChannelCardinalityE
	identity    ChannelIdentityE
	hasParams   bool
}

// channelTable is keyed by the MembershipChannel constant, so reordering the
// constants cannot misalign a row. Keep one entry per channel.
var channelTable = [...]channelDescriptor{
	MembershipChannelLowCardRef:              {flag: "", addMethodSuffix: "LowCardRef", readIterElemType: "uint64", needsKindVar: true, cardinality: ChannelCardinalityLow, identity: ChannelIdentityRef},
	MembershipChannelLowCardVerbatim:         {flag: "lowCardVerbatim", addMethodSuffix: "LowCardVerbatim", readIterElemType: "[]byte", embedsLiteralName: true, cardinality: ChannelCardinalityLow, identity: ChannelIdentityVerbatim},
	MembershipChannelHighCardRef:             {flag: "highCardRef", addMethodSuffix: "HighCardRef", readIterElemType: "uint64", needsKindVar: true, cardinality: ChannelCardinalityHigh, identity: ChannelIdentityRef},
	MembershipChannelHighCardVerbatim:        {flag: "highCardVerbatim", addMethodSuffix: "HighCardVerbatim", readIterElemType: "[]byte", embedsLiteralName: true, cardinality: ChannelCardinalityHigh, identity: ChannelIdentityVerbatim},
	MembershipChannelMixedLowCardRef:         {flag: "mixedLowCardRef", addMethodSuffix: "MixedLowCardRef", carrierType: "MixedLowCardRef", carrierReadSuffix: "LowCardRefHighCardParams", carrierSeq2Types: "uint64, []byte", carrierValueField: "Id", readIterElemType: "[]byte", usesCarrier: true, cardinality: ChannelCardinalityLow, identity: ChannelIdentityPerRow, hasParams: true},
	MembershipChannelMixedLowCardVerbatim:    {flag: "mixedLowCardVerbatim", addMethodSuffix: "MixedLowCardVerbatim", carrierType: "MixedLowCardVerbatim", carrierReadSuffix: "LowCardVerbatimHighCardParams", carrierSeq2Types: "[]byte, []byte", carrierValueField: "Name", readIterElemType: "[]byte", usesCarrier: true, carrierValueBytes: true, cardinality: ChannelCardinalityLow, identity: ChannelIdentityPerRow, hasParams: true},
	MembershipChannelLowCardRefParametrized:  {flag: "lowCardRefParametrized", addMethodSuffix: "LowCardRefParametrized", carrierType: "Parametrized", carrierReadSuffix: "LowCardRefParametrized", readIterElemType: "[]byte", usesCarrier: true, cardinality: ChannelCardinalityLow, identity: ChannelIdentityPerRow, hasParams: true},
	MembershipChannelHighCardRefParametrized: {flag: "highCardRefParametrized", addMethodSuffix: "HighCardRefParametrized", carrierType: "Parametrized", carrierReadSuffix: "HighCardRefParametrized", readIterElemType: "[]byte", usesCarrier: true, cardinality: ChannelCardinalityHigh, identity: ChannelIdentityPerRow, hasParams: true},
}

// desc returns the channel's descriptor row, or the zero descriptor for an
// out-of-range value (no real enum value lands there; the zero row keeps the
// accessors total).
func (c MembershipChannel) desc() channelDescriptor {
	if int(c) >= 0 && int(c) < len(channelTable) {
		return channelTable[c]
	}
	return channelDescriptor{}
}

// String returns the lw: flag spelling for this channel. The default
// (LowCardRef) returns "" because it requires no flag in a tag; an
// out-of-range value returns "unknown".
func (c MembershipChannel) String() string {
	if int(c) < 0 || int(c) >= len(channelTable) {
		return "unknown"
	}
	return channelTable[c].flag
}

// UsesCarrier reports whether the channel carries its membership identity
// as per-row sibling data (a marshalltypes carrier struct) rather than a
// registry id or a literal lw: name. True for the mixed / parametrized
// channels; a field on such a channel pairs with a carrier field sharing
// its (membership, section) and emits/decodes the membership-side data
// from that carrier. False for the four Cut-1 channels.
func (c MembershipChannel) UsesCarrier() bool { return c.desc().usesCarrier }

// CarrierTypeName returns the marshalltypes carrier struct name a field on
// this channel must pair with (e.g. "MixedLowCardRef"), or "" for channels
// that take no carrier. Used by the front-ends to check that a field's
// channel flag and its sibling carrier's Go type agree.
func (c MembershipChannel) CarrierTypeName() string { return c.desc().carrierType }

// CarrierReadMethodSuffix returns the read-side combined-accessor suffix
// for a carrier channel: the readaccess runtime exposes
// GetMembValue<Suffix> returning an iter.Seq2 that yields the per-row
// membership data (id/name + params) together. "" for non-carrier channels.
func (c MembershipChannel) CarrierReadMethodSuffix() string { return c.desc().carrierReadSuffix }

// CarrierReadSeq2Types returns the comma-separated iter.Seq2 element types
// the carrier channel's combined read accessor yields — e.g. "uint64, []byte"
// for mixedLowCardRef (an id and a params blob). "" for non-carrier channels.
func (c MembershipChannel) CarrierReadSeq2Types() string { return c.desc().carrierSeq2Types }

// CarrierValueField returns the carrier struct field holding the membership
// value for this channel — "Id" (uint64) for mixedLowCardRef, "Name"
// ([]byte) for mixedLowCardVerbatim. "" for non-carrier channels. The
// Params field name is uniform ("Params") across carriers.
func (c MembershipChannel) CarrierValueField() string { return c.desc().carrierValueField }

// CarrierValueIsBytes reports whether the carrier's membership-value field
// is a []byte (needing a defensive copy out of the Arrow buffer on read)
// rather than a scalar id. True for mixedLowCardVerbatim.
func (c MembershipChannel) CarrierValueIsBytes() bool { return c.desc().carrierValueBytes }

// EmbedsLiteralName reports whether the channel's wire identity is the
// literal lw: membership name as []byte rather than a uint64 id from
// the lookup registry. True for LowCardVerbatim and HighCardVerbatim.
func (c MembershipChannel) EmbedsLiteralName() bool { return c.desc().embedsLiteralName }

// NeedsKindVar reports whether emitted code requires a package-level
// kindXxx symbol holding the resolved uint64 membership id for this
// channel. True for the ref channels (LowCardRef, HighCardRef); false
// for the verbatim channels, which embed a []byte name directly on the
// wire.
func (c MembershipChannel) NeedsKindVar() bool { return c.desc().needsKindVar }

// ReadIterElemType returns the source-form Go element type yielded by
// the read-side `GetMembValue<Suffix>` iterator for this channel.
// Used by the emitter to declare the right `iter.Seq[T]` type in the
// generated <Sec>MembsReadI interface.
func (c MembershipChannel) ReadIterElemType() string { return c.desc().readIterElemType }

// AddMethodSuffix returns the suffix used on the DML's per-channel
// AddMembership<Suffix>P method names (e.g. "LowCardRef" →
// "AddMembershipLowCardRefP"). The suffix also matches the prefix of
// the read-side `GetMembValue<Suffix>` accessor on the readaccess
// runtime.
func (c MembershipChannel) AddMethodSuffix() string { return c.desc().addMethodSuffix }

// Cardinality / Identity / HasParams expose the channel's three ADR-0072
// carriage axes (plane D) as a queryable product. They are documentation, not
// dispatch: every behavioural branch switches the flat enum via the
// denormalised UsesCarrier / EmbedsLiteralName / NeedsKindVar booleans (each
// equivalent to one Identity value), and these accessors are currently read
// only by TestChannelTableAxes — validateChannelTable keeps the two consistent
// but reads the descriptor fields, not these methods. Wiring dispatch through
// the axes is gated on the identity axis becoming bijective (ADR-0072 Open
// question 2: split PerRow into PerRowId/PerRowName/PerRowBlob so the triple
// keys the channel); until then they answer "where on the product grid is this
// channel" for humans and schema/playground tooling, not the codec.
func (c MembershipChannel) Cardinality() ChannelCardinalityE { return c.desc().cardinality }
func (c MembershipChannel) Identity() ChannelIdentityE       { return c.desc().identity }
func (c MembershipChannel) HasParams() bool                  { return c.desc().hasParams }

// validateChannelTable asserts the carriage-axis fields stay consistent with
// the behavioural dispatch fields they summarise, so the ADR-0072 product and
// the method-dispatch facts cannot drift as channels are added. A static-table
// invariant check (see TestChannelTableAxes), not a runtime path.
func validateChannelTable() (err error) {
	for i := range channelTable {
		c := MembershipChannel(i)
		d := channelTable[i]
		if (d.identity == ChannelIdentityRef) != d.needsKindVar {
			return eb.Build().Str("channel", c.String()).Errorf("identity=Ref must match needsKindVar")
		}
		if (d.identity == ChannelIdentityVerbatim) != d.embedsLiteralName {
			return eb.Build().Str("channel", c.String()).Errorf("identity=Verbatim must match embedsLiteralName")
		}
		perRow := d.identity == ChannelIdentityPerRow
		if perRow != d.usesCarrier {
			return eb.Build().Str("channel", c.String()).Errorf("identity=PerRow must match usesCarrier")
		}
		if perRow != d.hasParams {
			return eb.Build().Str("channel", c.String()).Errorf("hasParams must match the PerRow (carrier) identity")
		}
	}
	return nil
}

// init fail-fasts if the static channelTable's carriage axes are inconsistent
// with its dispatch fields — a mis-edit is a programming error, caught at load
// for every importer rather than only under test.
func init() {
	if err := validateChannelTable(); err != nil {
		panic("mappingplan: inconsistent channelTable — " + err.Error())
	}
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

	IsOption bool // option.Option[T] wrapper — Option[[]byte] uses the scalar-blob lane

	// Canonical is the authoritative leeway canonical type the field's value
	// maps to — the single source of truth for the value type. The Go-facing
	// forms (GoType / IsSlice / IsRoaring) are derived from it on demand by the
	// methods below, never stored. nil for const fields (GoType() → "string").
	Canonical canonicaltypes.PrimitiveAstNodeI

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

	// CarrierField / CarrierType wire a mixed / parametrized value field
	// (Flags.Channel.UsesCarrier()) to its sibling carrier — the Go field
	// name and the marshalltypes struct name (e.g. "MixedLowCardRef").
	// PlanBuilder.Finish resolves them from the carrier field declared on
	// the same (membership, section). The codec reads the membership-side
	// data (id / params) from this carrier per row. Both are "" for the
	// Cut-1 channels.
	CarrierField string
	CarrierType  string

	// CarrierIsSlice is true when the sibling carrier is a slice
	// (`[]marshalltypes.X`), paired element-wise with an exploded value
	// field; false for a scalar carrier paired with a scalar / Option /
	// container value (one carrier per attribute). Set by PlanBuilder.Finish.
	CarrierIsSlice bool
}

// GoType returns the field's value type in Go source form (e.g. "uint64",
// "string", "[]byte", "[16]byte", "time.Time", "*roaring.Bitmap"), derived
// from Canonical via the single canonical→Go rule (deriveGoShape) — the inner
// element type for []T / Option[T]. Const fields are always "string". Canonical
// is the plan's source of truth; this is a derived view, never stored.
func (f TaggedField) GoType() string {
	if f.IsConst {
		return "string"
	}
	goType, _, _, err := deriveGoShape(f.Canonical)
	if err != nil {
		panic("mappingplan: TaggedField.GoType on a field with no canonical type — corrupt plan (PlanBuilder validates this at AddField): " + err.Error())
	}
	return goType
}

// IsSlice reports whether the field's value is a top-level []T element-slice
// (the canonical HomogenousArray modifier). Derived from Canonical; false for
// const / nil-canonical fields.
func (f TaggedField) IsSlice() bool {
	if f.Canonical == nil {
		return false
	}
	mod, _ := canonicaltypes.GetScalarModifier(f.Canonical)
	return mod == canonicaltypes.ScalarModifierHomogenousArray
}

// IsRoaring reports whether the field's value is a *roaring.Bitmap (the
// canonical Set modifier). Derived from Canonical; false for const / nil.
func (f TaggedField) IsRoaring() bool {
	if f.Canonical == nil {
		return false
	}
	mod, _ := canonicaltypes.GetScalarModifier(f.Canonical)
	return mod == canonicaltypes.ScalarModifierSet
}

// IsMulti reports whether the field emits multiple values (a []T container or
// a roaring set) rather than a single scalar attribute.
func (f TaggedField) IsMulti() bool {
	return f.IsSlice() || f.IsRoaring()
}

// KindVar returns the package-level identifier holding the resolved
// membership id for this field. Wrapper code (FactsWrapper /
// NoOpWrapper) declares the symbol — facts target as a var assigned
// from vdd in init(), anchor target as a const in declaration order.
// The generated BuildEntities references it by name. Multi-sub-column
// sections share a single KindVar per membership.
//
// For const fields the identifier is keyed on LWMembership (no Go field
// name), so several consts on one membership share a single symbol; value
// fields key on the Go field name instead. PlanBuilder.Finish rejects a
// const and a value field sharing one ref-channel membership, because the
// two keyings would mint colliding kindXxx symbols. Channels that do not
// consult the registry (Verbatim and Parametrized non-mixed) return "" —
// no kindXxx is declared.
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
