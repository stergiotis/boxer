package lwsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// This file is the spec→name seam of ADR-0181 §SD6: compose physical leeway
// column names from hand-authored specs (the write-direction dual of Resolve),
// and parse the vocabulary-prefixed spec tokens the LW_ constructor family
// carries. The pass (§SD2), the `leeway ddl compose` CLI, and tests all
// consume this API, so a spec token means the same thing everywhere.
//
// It generalizes the NameConditions precedent (ADR-0121): hand-built
// IntermediateColumnContext/Props through MapIntermediateToPhysicalColumns,
// never a hand-assembled string.

// DefaultSeparator is the physical-name component separator every in-tree
// leeway table uses; TableSegments defaults to it.
const DefaultSeparator = ":"

// Spec-token vocabulary prefixes (ADR-0181 §SD2). Aspect names collide across
// the three closed vocabularies (`json*`/`cbor*` exist twice), so every token
// must carry its routing prefix.
const (
	SpecTokenPrefixItem = "item:"
	SpecTokenPrefixEnc  = "enc:"
	SpecTokenPrefixSem  = "sem:"
	SpecTokenPrefixUse  = "use:"
)

// ErrUseAspectOnPlainColumn rejects a `use:` token on a plain-column spec.
// Use aspects are section-level and tagged-only; the error names the fix.
var ErrUseAspectOnPlainColumn = eb.Build().Errorf("use aspects are section-level and tagged-only; a plain column cannot carry one — make the column a tagged section (LW_TV) or drop the use: token")

// ErrItemTokenOnTaggedColumn rejects an `item:` token on a tagged-column
// spec: the item type is what files a plain column, a tagged column has none.
var ErrItemTokenOnTaggedColumn = eb.Build().Errorf("tagged columns carry no item type; item: belongs to plain-column specs (LW_PLAIN)")

// ErrMissingItemToken rejects a plain-column spec without its mandatory
// item: token (ADR-0181 §SD2 — prefixes carry semantics, calls read complete).
var ErrMissingItemToken = eb.Build().Errorf("plain column spec requires exactly one item: token (e.g. item:oq, item:id)")

// PlainSpecTokens is the parsed token list of a plain-column spec.
type PlainSpecTokens struct {
	Item           common.PlainItemTypeE
	EncodingHints  encodingaspects.AspectSet
	ValueSemantics valueaspects.AspectSet
}

// TaggedSpecTokens is the parsed token list of a tagged-value-column spec.
type TaggedSpecTokens struct {
	EncodingHints  encodingaspects.AspectSet
	ValueSemantics valueaspects.AspectSet
	UseAspects     useaspects.AspectSet
}

func parseAspectName[A interface{ String() string }](all []A, vocabulary string, name string) (aspect A, err error) {
	for _, a := range all {
		if a.String() == name {
			return a, nil
		}
	}
	err = eb.Build().Str("aspect", name).Str("vocabulary", vocabulary).Errorf("unknown aspect name")
	return
}

// parsePlainItemType maps the physical-name prefix spellings (id, ts, ro, lc,
// tx, oq) to the item type. The spellings are the ddl package's exported
// prefix constants, so they cannot drift from the naming convention.
func parsePlainItemType(s string) (item common.PlainItemTypeE, err error) {
	switch s {
	case ddl.IdPrefix:
		return common.PlainItemTypeEntityId, nil
	case ddl.TimestampPrefix:
		return common.PlainItemTypeEntityTimestamp, nil
	case ddl.RoutingPrefix:
		return common.PlainItemTypeEntityRouting, nil
	case ddl.LifecyclePrefix:
		return common.PlainItemTypeEntityLifecycle, nil
	case ddl.TransactionPrefix:
		return common.PlainItemTypeTransaction, nil
	case ddl.OpaquePrefix:
		return common.PlainItemTypeOpaque, nil
	}
	err = eb.Build().Str("item", s).Strs("known", []string{ddl.IdPrefix, ddl.TimestampPrefix, ddl.RoutingPrefix, ddl.LifecyclePrefix, ddl.TransactionPrefix, ddl.OpaquePrefix}).Errorf("unknown plain item type")
	return
}

// splitSpecTokens routes each token by its vocabulary prefix. Tokens without a
// known prefix are a loud error — a bare aspect name cannot be routed, because
// the vocabularies overlap.
func splitSpecTokens(tokens []string, allowItem bool, allowUse bool) (item common.PlainItemTypeE, enc []encodingaspects.AspectE, sem []valueaspects.AspectE, use []useaspects.AspectE, itemSeen bool, err error) {
	item = common.PlainItemTypeNone
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, SpecTokenPrefixItem):
			if !allowItem {
				err = eb.Build().Str("token", tok).Errorf("%w", ErrItemTokenOnTaggedColumn)
				return
			}
			if itemSeen {
				err = eb.Build().Str("token", tok).Errorf("duplicate item: token")
				return
			}
			item, err = parsePlainItemType(strings.TrimPrefix(tok, SpecTokenPrefixItem))
			if err != nil {
				return
			}
			itemSeen = true
		case strings.HasPrefix(tok, SpecTokenPrefixEnc):
			var a encodingaspects.AspectE
			a, err = parseAspectName(encodingaspects.AllAspects, "encoding", strings.TrimPrefix(tok, SpecTokenPrefixEnc))
			if err != nil {
				return
			}
			enc = append(enc, a)
		case strings.HasPrefix(tok, SpecTokenPrefixSem):
			var a valueaspects.AspectE
			a, err = parseAspectName(valueaspects.AllAspects, "value-semantics", strings.TrimPrefix(tok, SpecTokenPrefixSem))
			if err != nil {
				return
			}
			sem = append(sem, a)
		case strings.HasPrefix(tok, SpecTokenPrefixUse):
			if !allowUse {
				err = eb.Build().Str("token", tok).Errorf("%w", ErrUseAspectOnPlainColumn)
				return
			}
			var a useaspects.AspectE
			a, err = parseAspectName(useaspects.AllAspects, "use", strings.TrimPrefix(tok, SpecTokenPrefixUse))
			if err != nil {
				return
			}
			use = append(use, a)
		default:
			err = eb.Build().Str("token", tok).Errorf("spec token carries no vocabulary prefix; expected one of item:/enc:/sem:/use:")
			return
		}
	}
	return
}

// ParsePlainSpecTokens parses a plain-column spec's token list. The item:
// token is mandatory and unique; use: tokens are rejected with the fix named
// (ErrUseAspectOnPlainColumn); aspect sets are validated against the family
// exclusivity registries (ADR-0182 §SD3).
func ParsePlainSpecTokens(tokens []string) (out PlainSpecTokens, err error) {
	item, enc, sem, _, itemSeen, err := splitSpecTokens(tokens, true, false)
	if err != nil {
		return
	}
	if !itemSeen {
		err = ErrMissingItemToken
		return
	}
	out.Item = item
	out.EncodingHints, err = encodingaspects.EncodeAspects(enc...)
	if err != nil {
		return
	}
	out.ValueSemantics, err = valueaspects.EncodeAspects(sem...)
	if err != nil {
		return
	}
	err = checkFamilies(out.EncodingHints, out.ValueSemantics, useaspects.EmptyAspectSet)
	return
}

// ParseTaggedSpecTokens parses a tagged-value-column spec's token list.
// item: tokens are rejected (ErrItemTokenOnTaggedColumn); use: tokens apply
// to the column's section segment.
func ParseTaggedSpecTokens(tokens []string) (out TaggedSpecTokens, err error) {
	_, enc, sem, use, _, err := splitSpecTokens(tokens, false, true)
	if err != nil {
		return
	}
	out.EncodingHints, err = encodingaspects.EncodeAspects(enc...)
	if err != nil {
		return
	}
	out.ValueSemantics, err = valueaspects.EncodeAspects(sem...)
	if err != nil {
		return
	}
	out.UseAspects, err = useaspects.EncodeAspects(use...)
	if err != nil {
		return
	}
	err = checkFamilies(out.EncodingHints, out.ValueSemantics, out.UseAspects)
	return
}

func checkFamilies(enc encodingaspects.AspectSet, sem valueaspects.AspectSet, use useaspects.AspectSet) (err error) {
	err = encodingaspects.CheckFamilyExclusivity(enc)
	if err != nil {
		return
	}
	err = valueaspects.CheckFamilyExclusivity(sem)
	if err != nil {
		return
	}
	err = useaspects.CheckFamilyExclusivity(use)
	return
}

// TableSegments carries the table-level naming inputs a single column spec
// cannot determine (ADR-0181 background §2.2): the component separator, the
// table row config, and the grouping keys. Zero value plus DefaultTableSegments
// covers a fresh table; adopt a live table's pair via Resolver.TableSegments
// so minted names re-parse into it, the way NameConditions does.
//
// The grouping keys are per-section, not table-wide: set them only when
// every column minted through this Composer belongs to that co-section /
// streaming group. Minting into a table whose sections carry differing
// groups needs one Composer per group — a name with the wrong group segment
// discovers as a different section. StreamingGroup applies to tagged lanes
// and opaque plain columns; the other plain item types carry no streaming
// segment (the discover direction rejects one), so PlainColumn ignores it
// for them.
type TableSegments struct {
	Separator      string
	TableRowConfig common.TableRowConfigE
	CoSectionGroup naming.Key
	StreamingGroup naming.Key
}

// DefaultTableSegments is the fresh-table default: ':'-separated,
// TableRowConfigMultiAttributesPerRow (the only config that exists), no
// grouping keys.
func DefaultTableSegments() TableSegments {
	return TableSegments{
		Separator:      DefaultSeparator,
		TableRowConfig: common.TableRowConfigMultiAttributesPerRow,
	}
}

// TableSegments returns the table-level naming segments adopted from a live
// table — its separator and row config — so a name composed with them
// re-parses into that table. ok is false when the table is not leeway-shaped;
// grouping keys are per-section and stay empty.
func (inst *Resolver) TableSegments(dbName string, tableName string) (seg TableSegments, ok bool) {
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return
	}
	seg = TableSegments{
		Separator:      idx.meta.separator,
		TableRowConfig: idx.meta.tableRowConfig,
	}
	ok = true
	return
}

// Composer mints physical leeway column names from authoring specs, one
// column per call. Value columns are authored in full; membership and support
// columns are constructed by channel/role with machine-chosen properties
// (ADR-0181 §SD2) — the properties come from the same LoadSection path the
// DDL generator runs, ClickHouse-filtered, so a minted lane cannot drift from
// what the generator would emit. Not safe for concurrent use.
type Composer struct {
	seg        TableSegments
	conv       common.NamingConventionI
	parser     *canonicaltypes.Parser
	membership common.TechnologySpecificMembershipSetGenI
}

// NewComposer builds a Composer over the given table segments. An empty
// separator means DefaultSeparator.
func NewComposer(seg TableSegments) (inst *Composer, err error) {
	if seg.Separator == "" {
		seg.Separator = DefaultSeparator
	}
	if !seg.TableRowConfig.IsValid() {
		err = eb.Build().Stringer("tableRowConfig", seg.TableRowConfig).Errorf("invalid table row config")
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(seg.Separator)
	if err != nil {
		err = eb.Build().Str("separator", seg.Separator).Errorf("unable to build naming convention: %w", err)
		return
	}
	inst = &Composer{
		seg:        seg,
		conv:       conv,
		parser:     canonicaltypes.NewParser(),
		membership: clickhouse.NewTechnologySpecificCodeGenerator(),
	}
	return
}

func (inst *Composer) name(kind string, s string) (n naming.StylableName, err error) {
	n, err = naming.MakeStylableName(s)
	if err != nil {
		err = eb.Build().Str(kind, s).Str("kind", kind).Errorf("invalid name: %w", err)
		return
	}
	if strings.Contains(string(n), inst.seg.Separator) {
		err = eb.Build().Str(kind, s).Str("separator", inst.seg.Separator).Errorf("the name contains the name separator")
		return
	}
	return
}

func (inst *Composer) canonicalType(ctype string) (ct canonicaltypes.PrimitiveAstNodeI, err error) {
	ct, err = inst.parser.ParsePrimitiveTypeAst(ctype)
	if err != nil {
		err = eb.Build().Str("canonicalType", ctype).Errorf("unable to parse canonical type: %w", err)
		return
	}
	return
}

// PlainColumn composes the physical name of a plain (backbone) column from
// its logical name, canonical type, and spec tokens (item: mandatory).
func (inst *Composer) PlainColumn(columnName string, canonicalType string, tokens []string) (physical string, err error) {
	spec, err := ParsePlainSpecTokens(tokens)
	if err != nil {
		return
	}
	name, err := inst.name("column", columnName)
	if err != nil {
		return
	}
	ct, err := inst.canonicalType(canonicalType)
	if err != nil {
		return
	}
	// Only opaque plain columns carry a streaming group; the discover
	// direction rejects the segment on every other plain item type, so
	// stamping it would mint a name the pass's own shape check refuses.
	streamingGroup := inst.seg.StreamingGroup
	if spec.Item != common.PlainItemTypeOpaque {
		streamingGroup = ""
	}
	cc := common.IntermediateColumnContext{
		Scope:          spec.Item.GetIntermediateColumnScope(),
		StreamingGroup: streamingGroup,
		UseAspects:     useaspects.EmptyAspectSet,
		PlainItemType:  spec.Item,
	}
	cp := common.NewIntermediateColumnsProps()
	cp.Add(name, common.ColumnRoleValue, ct, spec.EncodingHints, spec.ValueSemantics)
	return inst.compose(cc, cp)
}

// TaggedValueColumn composes the physical name of a tagged value column in
// the given section from its spec tokens (use: applies to the section
// segment of this column's name).
func (inst *Composer) TaggedValueColumn(sectionName string, columnName string, canonicalType string, tokens []string) (physical string, err error) {
	spec, err := ParseTaggedSpecTokens(tokens)
	if err != nil {
		return
	}
	section, err := inst.name("section", sectionName)
	if err != nil {
		return
	}
	name, err := inst.name("column", columnName)
	if err != nil {
		return
	}
	ct, err := inst.canonicalType(canonicalType)
	if err != nil {
		return
	}
	cc := common.IntermediateColumnContext{
		Scope:          common.IntermediateColumnScopeTagged,
		StreamingGroup: inst.seg.StreamingGroup,
		SectionName:    section,
		UseAspects:     spec.UseAspects,
		CoSectionGroup: inst.seg.CoSectionGroup,
		PlainItemType:  common.PlainItemTypeNone,
	}
	cp := common.NewIntermediateColumnsProps()
	cp.Add(name, common.ColumnRoleValue, ct, spec.EncodingHints, spec.ValueSemantics)
	return inst.compose(cc, cp)
}

// ParseMembershipSpec maps a channel name (the MembershipSpecE spellings:
// low-card-ref, high-card-verbatim, …) to its single-bit spec, mixed channels
// included — the schema-level parse the DDL path uses.
func ParseMembershipSpec(channel string) (m common.MembershipSpecE, err error) {
	known := make([]string, 0, len(common.AllMembershipSpecs))
	for _, cand := range common.AllMembershipSpecs {
		if cand == common.MembershipSpecNone || cand.Count() != 1 {
			continue
		}
		if cand.String() == channel {
			m = cand
			return
		}
		known = append(known, cand.String())
	}
	err = eb.Build().Str("channel", channel).Strs("known", known).Errorf("unknown membership channel")
	return
}

// ParseMembershipChannel is the per-column authoring variant of
// ParseMembershipSpec: mixed channels are rejected, because they carry two
// lanes and a one-expression constructor cannot mint two columns (ADR-0181
// §SD8 defers the mixed/parametrized front-end).
func ParseMembershipChannel(channel string) (m common.MembershipSpecE, err error) {
	m, err = ParseMembershipSpec(channel)
	if err != nil {
		return
	}
	if m.ContainsMixed() {
		err = eb.Build().Str("channel", channel).Errorf("mixed membership channels carry two lanes and are not authorable per column (ADR-0181 §SD8)")
		return
	}
	return
}

// MembershipColumn composes the physical name of a section's membership lane
// for the named channel. Role, canonical type, and encoding hints are
// machine-chosen through the ClickHouse-filtered ResolveMembership — exactly
// what the DDL generator would emit for the same section.
func (inst *Composer) MembershipColumn(sectionName string, channel string) (physical string, err error) {
	m, err := ParseMembershipChannel(channel)
	if err != nil {
		return
	}
	section, err := inst.name("section", sectionName)
	if err != nil {
		return
	}
	desc, err := inst.loadSyntheticSection(section, m, nil)
	if err != nil {
		return
	}
	if desc.Membership.Length() != 1 {
		err = eb.Build().Str("channel", channel).Int("lanes", desc.Membership.Length()).Errorf("channel did not resolve to exactly one membership lane")
		return
	}
	return inst.composeSectionLane(section, desc.Membership, 0)
}

// supportChannelForRole maps a support role to the synthetic section that
// makes the production LoadSection path emit exactly that support column:
// a membership channel for the `<role>card` family, a dummy array/set value
// column for len/card. Cumulative-offset companions (cusumlen/cusumcard) are
// not generator-emitted today and are rejected rather than given invented
// properties.
func supportChannelForRole(role common.ColumnRoleE) (m common.MembershipSpecE, valueCT canonicaltypes.PrimitiveAstNodeI, err error) {
	switch role {
	case common.ColumnRoleLength:
		return common.MembershipSpecNone, ctabb.U64h, nil
	case common.ColumnRoleCardinality:
		return common.MembershipSpecNone, ctabb.U64m, nil
	case common.ColumnRoleHighCardRefCardinality:
		return common.MembershipSpecHighCardRef, nil, nil
	case common.ColumnRoleHighCardRefParametrizedCardinality:
		return common.MembershipSpecHighCardRefParametrized, nil, nil
	case common.ColumnRoleHighCardVerbatimCardinality:
		return common.MembershipSpecHighCardVerbatim, nil, nil
	case common.ColumnRoleLowCardRefCardinality:
		return common.MembershipSpecLowCardRef, nil, nil
	case common.ColumnRoleLowCardRefParametrizedCardinality:
		return common.MembershipSpecLowCardRefParametrized, nil, nil
	case common.ColumnRoleLowCardVerbatimCardinality:
		return common.MembershipSpecLowCardVerbatim, nil, nil
	case common.ColumnRoleMixedLowCardRefCardinality:
		return common.MembershipSpecMixedLowCardRefHighCardParameters, nil, nil
	case common.ColumnRoleMixedLowCardVerbatimCardinality:
		return common.MembershipSpecMixedLowCardVerbatimHighCardParameters, nil, nil
	}
	err = eb.Build().Stringer("role", role).Errorf("not a machine-derivable support role (expected len, card, or a <role>card membership cardinality)")
	return
}

// SupportColumn composes the physical name of a section's support column by
// its role as it appears in physical names (len, card, lrcard, hrcard, …).
// Canonical type and encoding hints are machine-chosen; hand-authoring them
// is exactly the mistake this seam exists to prevent.
func (inst *Composer) SupportColumn(sectionName string, role string) (physical string, err error) {
	r, err := common.ParseColumnRole(role)
	if err != nil {
		err = eb.Build().Str("role", role).Errorf("unknown support role: %w", err)
		return
	}
	m, valueCT, err := supportChannelForRole(r)
	if err != nil {
		return
	}
	section, err := inst.name("section", sectionName)
	if err != nil {
		return
	}
	desc, err := inst.loadSyntheticSection(section, m, valueCT)
	if err != nil {
		return
	}
	for _, props := range []*common.IntermediateColumnProps{desc.MembershipSupport, desc.NonScalarHomogenousArraySupport, desc.NonScalarSetSupport} {
		for i, pr := range props.Roles {
			if pr == r {
				return inst.composeSectionLane(section, props, i)
			}
		}
	}
	err = eb.Build().Str("section", sectionName).Str("role", role).Errorf("support role did not materialize from the generator path")
	return
}

// loadSyntheticSection runs the production section-loading path (the same
// one the DDL generator uses, ResolveMembership included) over a minimal
// synthetic section, so machine-chosen lane properties are derived, never
// restated. valueCT, when non-nil, adds one dummy value column whose only
// purpose is to make the matching value-support column materialize; the
// dummy's own name never leaves this function.
func (inst *Composer) loadSyntheticSection(section naming.StylableName, m common.MembershipSpecE, valueCT canonicaltypes.PrimitiveAstNodeI) (desc *common.IntermediateTaggedValuesDesc, err error) {
	sec := common.TaggedValuesSection{
		Name:           section,
		MembershipSpec: m,
		UseAspects:     useaspects.EmptyAspectSet,
		CoSectionGroup: inst.seg.CoSectionGroup,
		StreamingGroup: inst.seg.StreamingGroup,
	}
	if valueCT != nil {
		sec.ValueColumnNames = []naming.StylableName{"v"}
		sec.ValueColumnTypes = []canonicaltypes.PrimitiveAstNodeI{valueCT}
		sec.ValueEncodingHints = []encodingaspects.AspectSet{encodingaspects.EmptyAspectSet}
		sec.ValueSemantics = []valueaspects.AspectSet{valueaspects.EmptyAspectSet}
	}
	desc = common.NewIntermediateTaggedValueDesc()
	err = desc.LoadSection(&sec, inst.membership)
	if err != nil {
		err = eb.Build().Stringer("section", section).Stringer("membership", m).Errorf("unable to resolve section lanes: %w", err)
		return
	}
	return
}

func (inst *Composer) composeSectionLane(section naming.StylableName, props *common.IntermediateColumnProps, i int) (physical string, err error) {
	cc := common.IntermediateColumnContext{
		Scope:          common.IntermediateColumnScopeTagged,
		StreamingGroup: inst.seg.StreamingGroup,
		SectionName:    section,
		UseAspects:     useaspects.EmptyAspectSet,
		CoSectionGroup: inst.seg.CoSectionGroup,
		PlainItemType:  common.PlainItemTypeNone,
	}
	lane := props.Slice(i, i+1)
	return inst.compose(cc, &lane)
}

func (inst *Composer) compose(cc common.IntermediateColumnContext, cp *common.IntermediateColumnProps) (physical string, err error) {
	phys, err := inst.conv.MapIntermediateToPhysicalColumns(cc, *cp, nil, inst.seg.TableRowConfig)
	if err != nil {
		err = eb.Build().Errorf("unable to compose physical column name: %w", err)
		return
	}
	if len(phys) != 1 {
		err = eb.Build().Int("columns", len(phys)).Errorf("expected exactly one composed column")
		return
	}
	physical = phys[0].String()
	return
}
