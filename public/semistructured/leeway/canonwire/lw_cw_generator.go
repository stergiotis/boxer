package canonwire

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The encoder generator of ADR-0210 SD6, and the plan both directions are
// emitted from. It reads the slot table of SD2 and emits, for one table, the
// code that walks that table's generated readaccess classes and writes each
// entity through the canonwire runtime writers; the decoder half is in
// lw_cw_generator_decoder.go and lw_cw_generator_decoder_emit.go.
//
// Everything the wire needs to know about the table is decided here: the slot
// signatures become constants, the ambiguity sets become the slots the tagger
// is consulted for, and each column's canonical type becomes one typed runtime
// call. Nothing of the table description survives into the generated file
// except those decisions, and the generated encoder does no reflection and
// touches no arrow.Array.

func NewGoClassBuilder() *GoClassBuilder {
	return &GoClassBuilder{}
}

func (inst *GoClassBuilder) SetCodeBuilder(s *strings.Builder) {
	inst.builder = s
}

// columnPlan is one value column, in key order, of a slot's section or of a
// plain section.
type columnPlan struct {
	// getter is the readaccess accessor's name, GetAttrValue<Col>.
	getter string
	// subType decides the shape: a scalar is written straight, an `h` column as
	// a definite array collected through a scratch slice, an `m` column through
	// the runtime's SetWriter, which sorts and keeps duplicates.
	subType common.IntermediateColumnSubTypeE
	// goType is the element type the accessor yields; for a container it is the
	// element type, not the slice.
	goType string
	// writeCall is the CborWriter call for one element, over a local named v.
	writeCall string
	// scratch is the encoder field an `h` column collects into, "" otherwise.
	scratch string
	// readCall is the CborReader call for one element, "" for a fixed-width
	// byte column, which is read into an array through ReadBytesInto instead.
	readCall string
	// decLocal is the decoder's local for a scalar column, "" for a container.
	decLocal string
	// decScratch is the decoder field a container column collects into, ""
	// for a scalar.
	decScratch string
}

// sectionPlan is one section of a slot — one membership group of the attribute.
type sectionPlan struct {
	name naming.StylableName
	// raField is the section's field on the readaccess entity class.
	raField string
	// countExpr yields the section's attribute count for an entity, as an
	// int64 expression over `inst` and `entityIdx`.
	countExpr string
	channels  []mappingplan.MembershipChannel
	// hasMemberships reports that the readaccess section carries a membership
	// pack at all; a section declaring MembershipSpecNone does not.
	hasMemberships bool
	columns        []columnPlan
	// dmlGetter is the entity builder's accessor for the section,
	// GetSection<Name>; the decoder writes an attribute through it.
	dmlGetter string
	// keyBase is the key position, within the slot's signature, of the
	// section's first value column.
	keyBase int
	// scalarArgs and containerArgs are key positions into the slot's columns,
	// in the order the generated DML takes them: BeginAttribute takes the
	// scalars, AddToContainer/AddToCoContainers one element of each container.
	scalarArgs    []int
	containerArgs []int
	// containerAdd is the DML's per-element append, AddToContainerP for one
	// container column and AddToCoContainersP for several; "" for a section
	// with none.
	containerAdd string
	// acceptMask is the section's accepted channels as a bitmask over the
	// mappingplan channel ordinals — the input to SD5's narrowing step.
	acceptMask uint32
}

// slotPlan is one wire slot: a co-section group or a standalone section.
type slotPlan struct {
	signature string
	sigConst  string
	enumConst string
	// label is the slot's String(), the section names joined.
	label string
	// ambiguous marks a signature carried by more than one slot of this table:
	// SD5's dispatch resolves it, and the encoder consults its tagger for every
	// attribute of such a slot.
	ambiguous bool
	// ordinalTag is the slot's position within its ambiguity set, which is what
	// the built-in ordinal tagger returns.
	ordinalTag uint64
	awField    string
	nCols      int
	sections   []sectionPlan
}

// plainPlan is one plain section, keyed on the wire by its item type.
type plainPlan struct {
	itemType   common.PlainItemTypeE
	itemConst  string
	raField    string
	groupConst string
	group      string
	columns    []columnPlan
	// setter is the entity builder's plain setter, Set<ItemType>; it takes the
	// whole section at once, containers as slices.
	setter string
	// scalarArgs and containerArgs are key positions into columns, in the
	// order the setter takes them.
	scalarArgs    []int
	containerArgs []int
}

// scratchField is one reusable slice the encoder collects an `h` column into,
// because the readaccess container accessors are iterators and report no length.
type scratchField struct {
	name   string
	goType string
}

// tablePlan is everything the emission needs, resolved once — the slot table
// of SD2 with the class names, the per-column runtime calls of both directions
// and the DML argument orders the decoder writes through.
type tablePlan struct {
	tableName     naming.StylableName
	raClass       string
	encoderClass  string
	taggerIface   string
	ordinalTagger string
	slotEnum      string
	slots         []slotPlan
	plains        []plainPlan
	scratches     []scratchField
	needsSet      bool

	// The decoder half (ADR-0210 SD5, SD6).
	dmlClass          string
	decoderClass      string
	dispatcherIface   string
	ordinalDispatcher string
	// ambiguousConst names the generated constant the decoder's constructor
	// reads to decide whether a nil dispatcher is admissible.
	ambiguousConst string
	// masksVar names the generated per-slot accepted-channel table.
	masksVar string
	// sigCases are the distinct wire signatures, in slot-table order. A
	// signature carried by several slots is one case with several candidates,
	// because the decoder switches on the key it read.
	sigCases []sigCase
	// decScratches are the decoder's reusable container slices.
	decScratches []scratchField
	maxCols      int
	maxGroups    int
}

// sigCase is one distinct wire signature: the slots that carry it and the
// value columns it fixes. Two slots of one signature agree on every column's
// canonical type by construction, so the decoder reads the values once and
// only the write side branches on the slot.
type sigCase struct {
	signature string
	// sigConst is the signature constant of the case's first slot; the others
	// carry the same string under their own names.
	sigConst string
	// label names the slots the case covers, for diagnostics.
	label string
	// slots are indices into tablePlan.slots, in slot-table order.
	slots []int
	// setVar names the generated ambiguity-set slice, "" for a signature one
	// slot carries.
	setVar  string
	nCols   int
	nGroups int
	// columns are the case's value columns in key order, with the decoder's
	// locals and scratch fields resolved.
	columns []columnPlan
}

// ComposeCodec emits the whole canonical-wire surface of one table: the slot
// keys and the slot enum, the SD5 tagger and dispatcher pair with their
// built-in ordinal implementations, the encoder over the table's generated
// readaccess classes and the decoder into its generated dml builders.
func (inst *GoClassBuilder) ComposeCodec(tableName naming.StylableName, tblDesc *common.TableDesc, clsNamer gocodegen.GoClassNamerI) (err error) {
	plan, err := buildTablePlan(tableName, tblDesc, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to plan the encoder: %w", err)
		return
	}
	b := inst.builder
	gocodegen.EmitGeneratingCodeLocation(b)
	if err = emitSlotKeys(b, plan); err != nil {
		return
	}
	if err = emitSlotEnum(b, plan); err != nil {
		return
	}
	if err = emitTagger(b, plan); err != nil {
		return
	}
	if err = emitEncoder(b, plan); err != nil {
		return
	}
	if err = emitDispatcher(b, plan); err != nil {
		return
	}
	if err = emitDecoder(b, plan); err != nil {
		return
	}
	return
}

// buildTablePlan resolves the slot table, the class names and the per-column
// runtime calls before a byte of code is written, so every refusal (an
// unrepresentable canonical type, a section the encoder cannot count) happens
// with the table in hand.
func buildTablePlan(tableName naming.StylableName, tblDesc *common.TableDesc, clsNamer gocodegen.GoClassNamerI) (plan *tablePlan, err error) {
	slots, err := BuildSlotTable(tblDesc)
	if err != nil {
		err = eh.Errorf("unable to build the slot table: %w", err)
		return
	}
	plan = &tablePlan{tableName: tableName}
	if plan.raClass, err = clsNamer.ComposeEntityReadAccessClassName(tableName); err != nil {
		err = eh.Errorf("unable to compose the read access entity class name: %w", err)
		return
	}
	if plan.encoderClass, err = clsNamer.ComposeCanonWireEncoderClassName(tableName); err != nil {
		err = eh.Errorf("unable to compose the encoder class name: %w", err)
		return
	}
	if plan.taggerIface, err = clsNamer.ComposeCanonWireTaggerInterfaceName(tableName); err != nil {
		err = eh.Errorf("unable to compose the tagger interface name: %w", err)
		return
	}
	if plan.slotEnum, err = clsNamer.ComposeCanonWireSlotEnumName(tableName); err != nil {
		err = eh.Errorf("unable to compose the slot enum name: %w", err)
		return
	}
	plan.ordinalTagger = ordinalTaggerName(plan.taggerIface)

	addScratch := func(name string, goType string) {
		plan.scratches = append(plan.scratches, scratchField{name: name, goType: goType})
	}

	for ordinal := range slots.Slots {
		slot := &slots.Slots[ordinal]
		names := make([]naming.StylableName, 0, len(slot.Sections))
		labels := make([]string, 0, len(slot.Sections))
		for _, sec := range slot.Sections {
			names = append(names, sec.Name)
			labels = append(labels, string(sec.Name))
		}
		sp := slotPlan{
			signature: slot.Signature,
			label:     strings.Join(labels, "+"),
			ambiguous: len(slots.BySignature[slot.Signature]) > 1,
		}
		for k, o := range slots.BySignature[slot.Signature] {
			if o == ordinal {
				sp.ordinalTag = uint64(k)
			}
		}
		if sp.sigConst, err = clsNamer.ComposeCanonWireSignatureConstName(tableName, ordinal, names); err != nil {
			err = eh.Errorf("unable to compose the signature constant name: %w", err)
			return nil, err
		}
		if sp.enumConst, err = clsNamer.ComposeCanonWireSlotConstName(tableName, ordinal, names); err != nil {
			err = eh.Errorf("unable to compose the slot constant name: %w", err)
			return nil, err
		}
		sp.awField = "aw" + strings.TrimPrefix(sp.enumConst, "CanonWireSlot")

		for _, sec := range slot.Sections {
			src := &tblDesc.TaggedValuesSections[sec.SectionIdx]
			secPlan := sectionPlan{
				name:           sec.Name,
				raField:        sec.Name.Convert(naming.UpperCamelCase).String(),
				channels:       SpecChannels(sec.MembershipSpec),
				hasMemberships: sec.MembershipSpec != common.MembershipSpecNone,
			}
			for _, ci := range sec.ColumnOrder {
				var col columnPlan
				col, err = buildColumnPlan(src.ValueColumnNames[ci], src.ValueColumnTypes[ci], src.ValueEncodingHints[ci],
					"scratchTagged"+secPlan.raField)
				if err != nil {
					err = eb.Build().Stringer("section", sec.Name).Stringer("column", src.ValueColumnNames[ci]).Errorf("unable to plan the column: %w", err)
					return nil, err
				}
				if col.scratch != "" {
					addScratch(col.scratch, col.goType)
				}
				if col.subType == common.IntermediateColumnsSubTypeSet {
					plan.needsSet = true
				}
				secPlan.columns = append(secPlan.columns, col)
				sp.nCols++
			}
			secPlan.countExpr, err = attributeCountExpr(&secPlan, sec.MembershipSpec)
			if err != nil {
				err = eb.Build().Stringer("section", sec.Name).Errorf("unable to count the section's attributes: %w", err)
				return nil, err
			}
			sp.sections = append(sp.sections, secPlan)
		}
		plan.slots = append(plan.slots, sp)
	}

	for i := range slots.Plains {
		p := &slots.Plains[i]
		hints := plainHints(tblDesc, p.ItemType)
		pp := plainPlan{
			itemType:  p.ItemType,
			itemConst: "common.PlainItemType" + naming.MustBeValidStylableName(p.ItemType.String()).Convert(naming.UpperCamelCase).String(),
			raField:   naming.MustBeValidStylableName(p.ItemType.String()).Convert(naming.UpperCamelCase).String(),
			group:     p.Group,
		}
		if pp.groupConst, err = clsNamer.ComposeCanonWirePlainGroupConstName(tableName, p.ItemType); err != nil {
			err = eh.Errorf("unable to compose the plain group constant name: %w", err)
			return nil, err
		}
		for _, ci := range p.ColumnOrder {
			var col columnPlan
			col, err = buildColumnPlan(p.Names[ci], p.ColumnTypes[ci], hints[ci], "scratchPlain"+pp.raField)
			if err != nil {
				err = eb.Build().Stringer("plainItemType", p.ItemType).Stringer("column", p.Names[ci]).Errorf("unable to plan the column: %w", err)
				return nil, err
			}
			if col.scratch != "" {
				addScratch(col.scratch, col.goType)
			}
			if col.subType == common.IntermediateColumnsSubTypeSet {
				plan.needsSet = true
			}
			pp.columns = append(pp.columns, col)
		}
		plan.plains = append(plan.plains, pp)
	}

	err = planDecoder(plan, tblDesc, &slots, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to plan the decoder: %w", err)
		return nil, err
	}
	return
}

// ordinalTaggerName names the built-in tagger implementation. GoClassNamerI has
// no method of its own for it — ADR-0210 SD6 names the pluggable pair, not the
// built-in — so it is derived from the tagger interface's name: the trailing I
// of the Go interface convention dropped, and "Ordinal" put where the family
// prefix ends. A namer that does not use that prefix gets a suffixed name,
// which is still unique per table.
func ordinalTaggerName(taggerIface string) string {
	base := strings.TrimSuffix(taggerIface, "I")
	if rest, ok := strings.CutPrefix(base, "CanonWireTagger"); ok {
		return "CanonWireOrdinalTagger" + rest
	}
	return base + "Ordinal"
}

// plainHints collects one plain item type's encoding hints in the same
// declaration order BuildSlotTable collected its names and types.
func plainHints(tblDesc *common.TableDesc, itemType common.PlainItemTypeE) (hints []encodingaspects.AspectSet) {
	hints = make([]encodingaspects.AspectSet, 0, len(tblDesc.PlainValuesItemTypes))
	for i, t := range tblDesc.PlainValuesItemTypes {
		if t == itemType {
			hints = append(hints, tblDesc.PlainValuesEncodingHints[i])
		}
	}
	return
}

// attributeCountExpr resolves where a section's per-entity attribute count is
// read from. A section with value columns has GetNumberOfAttributes on its
// attribute class; a value-less one (the JSON mapping's null / undefined) has
// no attribute class at all, and its attributes are counted through the
// membership pack's per-attribute lookup accelerator instead.
func attributeCountExpr(sec *sectionPlan, spec common.MembershipSpecE) (expr string, err error) {
	if len(sec.columns) > 0 {
		return fmt.Sprintf("inst.ra.%s.Attributes.GetNumberOfAttributes(entityIdx)", sec.raField), nil
	}
	chs := SpecChannels(spec)
	if len(chs) == 0 {
		err = eh.Errorf("a section with neither value columns nor memberships carries no attribute count")
		return
	}
	role, err := membershipAccelRole(chs[0])
	if err != nil {
		return
	}
	return fmt.Sprintf("inst.ra.%s.Memberships.Accel%s.GetEntityAttributeCount(int(entityIdx))", sec.raField, role), nil
}

// membershipAccelRole names the readaccess membership pack's per-channel
// lookup accelerator field, Accel<Role>. The role is the channel's own
// identity-bearing column, which is what the pack indexes per attribute.
func membershipAccelRole(ch mappingplan.MembershipChannel) (role string, err error) {
	suffix := ch.AddMethodSuffix()
	if suffix == "" {
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("channel has no read accessor")
		return
	}
	return suffix, nil
}

// membershipAccessor names the readaccess accessor a channel's memberships are
// read through, and reports whether it is the Seq2 carrier form.
func membershipAccessor(ch mappingplan.MembershipChannel) (name string, seq2 bool, err error) {
	if ch.CarrierReadSeq2Types() != "" {
		return "GetMembValue" + ch.CarrierReadMethodSuffix(), true, nil
	}
	suffix := ch.AddMethodSuffix()
	if suffix == "" {
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("channel has no read accessor")
		return
	}
	return "GetMembValue" + suffix, false, nil
}

// buildColumnPlan resolves one column's accessor name, its Go element type and
// the runtime call that writes one element of it.
func buildColumnPlan(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet, scratchPrefix string) (col columnPlan, err error) {
	upper := name.Convert(naming.UpperCamelCase).String()
	col.getter = "GetAttrValue" + upper
	col.goType, col.subType, err = elementGoTypeName(ct, hints)
	if err != nil {
		return
	}
	col.writeCall, err = valueWriteCall(ct)
	if err != nil {
		return
	}
	col.readCall, _, err = valueReadCall(ct)
	if err != nil {
		return
	}
	if col.subType == common.IntermediateColumnsSubTypeHomogenousArray {
		col.scratch = scratchPrefix + upper
	}
	return
}

// elementGoTypeName is the readaccess generator's rule for the Go type an
// accessor yields: the canonical type's Go type, with the slice stripped off a
// container so what is left is the element type the iterator yields.
func elementGoTypeName(ct canonicaltypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (typeName string, subType common.IntermediateColumnSubTypeE, err error) {
	sm, err := common.ExtractScalarModifier(ct)
	if err != nil {
		return
	}
	typeName, _, _, err = codegen.GenerateGoCode(ct, hints)
	if err != nil {
		err = eh.Errorf("unable to get the go type for the canonical type: %w", err)
		return
	}
	switch sm {
	case canonicaltypes.ScalarModifierNone:
	case canonicaltypes.ScalarModifierSet, canonicaltypes.ScalarModifierHomogenousArray:
		typeName = strings.TrimPrefix(typeName, "[]")
	default:
		err = eb.Build().Stringer("scalarModifier", sm).Stringer("ct", ct).Errorf("unhandled scalar modifier")
		return
	}
	subType = common.GetSubTypeByScalarModifier(sm)
	return
}

// valueWriteCall maps a column's canonical type to the runtime writer call for
// one value of it (ADR-0210 SD3), over a local named v. The container modifier
// is stripped first: an `h` array and an `m` set write the same element form,
// and only their framing differs.
//
// The refusals are the ADR's: 128-bit integers, bit strings, and the zoned
// temporal types `d` and `t`, none of which any lane in this repository
// carries. They are also exactly what the readaccess generator refuses, so a
// table this rejects has no accessors to call either.
func valueWriteCall(ct canonicaltypes.PrimitiveAstNodeI) (call string, err error) {
	switch n := canonicaltypes.DemoteToScalarPrim(ct).(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeMachineNumericUnsigned:
			switch n.Width {
			case 8, 16, 32, 64:
				return "WriteUint(uint64(v))", nil
			}
		case canonicaltypes.BaseTypeMachineNumericSigned:
			switch n.Width {
			case 8, 16, 32, 64:
				return "WriteInt(int64(v))", nil
			}
		case canonicaltypes.BaseTypeMachineNumericFloat:
			switch n.Width {
			case 32:
				return "WriteF32(v)", nil
			case 64:
				return "WriteF64(v)", nil
			}
		}
	case canonicaltypes.StringAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeStringBool:
			if n.WidthModifier == canonicaltypes.WidthModifierNone {
				return "WriteBool(v)", nil
			}
		case canonicaltypes.BaseTypeStringUtf8:
			// A fixed-width `s` still lands on Go's string, padding kept.
			return "WriteTextString(v)", nil
		case canonicaltypes.BaseTypeStringBytes:
			if n.WidthModifier == canonicaltypes.WidthModifierFixed {
				return "WriteBytes(v[:])", nil
			}
			return "WriteBytes(v)", nil
		}
	case canonicaltypes.TemporalTypeAstNode:
		if n.BaseType == canonicaltypes.BaseTypeTemporalUtcDatetime {
			switch n.Width {
			case 32, 64:
				return "WriteTemporal(v)", nil
			}
		}
	case canonicaltypes.NetworkTypeAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeNetworkIPv4:
			if n.CIDRModifier == canonicaltypes.CIDRModifierNone {
				// A bare IPv4 rides the lane as a big-endian uint32.
				return "WriteIPv4Raw(v)", nil
			}
			return "WriteIPv4PrefixRaw(v)", nil
		case canonicaltypes.BaseTypeNetworkIPv6:
			if n.CIDRModifier == canonicaltypes.CIDRModifierNone {
				return "WriteIPv6Raw(v)", nil
			}
			return "WriteIPv6PrefixRaw(v)", nil
		}
	}
	err = eb.Build().Stringer("ct", ct).Errorf("canonical type has no canonical wire value form")
	return
}

// valueReadCall is the inverse of valueWriteCall: the runtime reader call that
// yields one element of a column, over a reader named r, as an expression
// assignable to the column's Go type.
//
// A fixed-width byte column has no expression form — its Go type is an array —
// so it comes back with an empty call and its width, and the emission uses
// ReadBytesInto over the array's slice instead. Everything else is one call.
func valueReadCall(ct canonicaltypes.PrimitiveAstNodeI) (call string, fixedWidth int, err error) {
	switch n := canonicaltypes.DemoteToScalarPrim(ct).(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeMachineNumericUnsigned:
			switch n.Width {
			case 8:
				return "ReadU8()", 0, nil
			case 16:
				return "ReadU16()", 0, nil
			case 32:
				return "ReadU32()", 0, nil
			case 64:
				return "ReadU64()", 0, nil
			}
		case canonicaltypes.BaseTypeMachineNumericSigned:
			switch n.Width {
			case 8:
				return "ReadI8()", 0, nil
			case 16:
				return "ReadI16()", 0, nil
			case 32:
				return "ReadI32()", 0, nil
			case 64:
				return "ReadI64()", 0, nil
			}
		case canonicaltypes.BaseTypeMachineNumericFloat:
			switch n.Width {
			case 32:
				return "ReadF32()", 0, nil
			case 64:
				return "ReadF64()", 0, nil
			}
		}
	case canonicaltypes.StringAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeStringBool:
			if n.WidthModifier == canonicaltypes.WidthModifierNone {
				return "ReadBool()", 0, nil
			}
		case canonicaltypes.BaseTypeStringUtf8:
			// A fixed-width `s` lands on Go's string here as it does on the
			// write side; the padding rides in the string.
			return "ReadTextString()", 0, nil
		case canonicaltypes.BaseTypeStringBytes:
			if n.WidthModifier == canonicaltypes.WidthModifierFixed {
				return "", int(n.Width), nil
			}
			return "ReadBytes()", 0, nil
		}
	case canonicaltypes.TemporalTypeAstNode:
		if n.BaseType == canonicaltypes.BaseTypeTemporalUtcDatetime {
			switch n.Width {
			case 32, 64:
				return "ReadTemporal()", 0, nil
			}
		}
	case canonicaltypes.NetworkTypeAstNode:
		switch n.BaseType {
		case canonicaltypes.BaseTypeNetworkIPv4:
			if n.CIDRModifier == canonicaltypes.CIDRModifierNone {
				return "ReadIPv4Raw()", 0, nil
			}
			return "ReadIPv4PrefixRaw()", 0, nil
		case canonicaltypes.BaseTypeNetworkIPv6:
			if n.CIDRModifier == canonicaltypes.CIDRModifierNone {
				return "ReadIPv6Raw()", 0, nil
			}
			return "ReadIPv6PrefixRaw()", 0, nil
		}
	}
	err = eb.Build().Stringer("ct", ct).Errorf("canonical type has no canonical wire value form")
	return
}

func emitSlotKeys(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// The slot keys of ADR-0210 SD2: a tagged section's CT group, or a co-section
// group's CT signature. They key the entity item's tagged map and are the only
// thing on the wire that comes from %s's description: section names, column
// names, aspects and hints are not.
`, plan.tableName)
	if err != nil {
		return
	}
	for i := range plan.slots {
		s := &plan.slots[i]
		_, err = fmt.Fprintf(b, "\n// %s keys the slot of section %s.\nconst %s = %q\n", s.sigConst, s.label, s.sigConst, s.signature)
		if err != nil {
			return
		}
	}
	if len(plan.plains) > 0 {
		_, err = b.WriteString(`
// The plain sections' CT groups. A plain section is keyed on the wire by its
// item type, which is fixed leeway vocabulary (ADR-0210 SD2, fork 1); its group
// is emitted so a decoder can check the two tables agree on the types before it
// reads a single entity.
`)
		if err != nil {
			return
		}
	}
	for i := range plan.plains {
		p := &plan.plains[i]
		_, err = fmt.Fprintf(b, "\n// %s is the %s plain section's CT group.\nconst %s = %q\n",
			p.groupConst, p.itemType, p.groupConst, p.group)
		if err != nil {
			return
		}
	}
	return
}

func emitSlotEnum(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// %s names the table's tagged slots.
//
// They are in slot-table order — by signature, then by first section index —
// so the ordinals do not depend on map iteration.
type %s uint8
`, plan.slotEnum, plan.slotEnum)
	if err != nil {
		return
	}
	if len(plan.slots) > 0 {
		if _, err = b.WriteString("\nconst (\n"); err != nil {
			return
		}
		for i := range plan.slots {
			s := &plan.slots[i]
			if i == 0 {
				_, err = fmt.Fprintf(b, "\t%s %s = iota\n", s.enumConst, plan.slotEnum)
			} else {
				_, err = fmt.Fprintf(b, "\t%s\n", s.enumConst)
			}
			if err != nil {
				return
			}
		}
		if _, err = b.WriteString(")\n"); err != nil {
			return
		}
	}

	_, err = fmt.Fprintf(b, "\nfunc (inst %s) String() string {\n\tswitch inst {\n", plan.slotEnum)
	if err != nil {
		return
	}
	for i := range plan.slots {
		s := &plan.slots[i]
		if _, err = fmt.Fprintf(b, "\tcase %s:\n\t\treturn %q\n", s.enumConst, s.label); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\t}\n\treturn \"<invalid>\"\n}\n"); err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `
// Signature returns the slot's CT signature — the key it rides under in an
// entity's tagged map.
func (inst %s) Signature() string {
	switch inst {
`, plan.slotEnum)
	if err != nil {
		return
	}
	for i := range plan.slots {
		s := &plan.slots[i]
		if _, err = fmt.Fprintf(b, "\tcase %s:\n\t\treturn %s\n", s.enumConst, s.sigConst); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\t}\n\treturn \"\"\n}\n"); err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `
// Ambiguous reports whether the slot's signature is carried by more than one
// slot of this table. Those are the slots ADR-0210 SD5's dispatch has to
// resolve: the encoder consults its tagger for every attribute of one, and for
// no attribute of any other.
func (inst %s) Ambiguous() bool {
`, plan.slotEnum)
	if err != nil {
		return
	}
	var cases strings.Builder
	for i := range plan.slots {
		s := &plan.slots[i]
		if !s.ambiguous {
			continue
		}
		if _, err = fmt.Fprintf(&cases, "\tcase %s:\n\t\treturn true\n", s.enumConst); err != nil {
			return
		}
	}
	if cases.Len() > 0 {
		if _, err = fmt.Fprintf(b, "\tswitch inst {\n%s\t}\n", cases.String()); err != nil {
			return
		}
	} else {
		// Every signature of this table is carried by one slot.
		if _, err = b.WriteString("\t_ = inst\n"); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\treturn false\n}\n"); err != nil {
		return
	}
	return
}

func emitTagger(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// %s is the encoder half of ADR-0210 SD5's dispatch pair.
//
// It is consulted for every attribute of a slot whose signature is ambiguous
// in this table, and its small opaque integer rides as the attribute item's
// trailing element for the decoding side's DispatcherI to read. Attributes of
// an unambiguous slot never reach it and never carry a discriminator.
type %s interface {
	Tag(slot %s, entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) uint64
}

// %s is the built-in tagger of ADR-0210 SD5.
//
// It returns the slot's ordinal within its ambiguity set, which round-trips
// between two tables that declare the same sections in the same order and is
// explicitly that declaration-order coupling — a consumer that wants
// section-name independence supplies its own pair.
type %s struct{}

var _ %s = %s{}

func (inst %s) Tag(slot %s, entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) uint64 {
`, plan.taggerIface, plan.taggerIface, plan.slotEnum,
		plan.ordinalTagger, plan.ordinalTagger, plan.taggerIface, plan.ordinalTagger,
		plan.ordinalTagger, plan.slotEnum)
	if err != nil {
		return
	}
	var cases strings.Builder
	for i := range plan.slots {
		s := &plan.slots[i]
		if !s.ambiguous || s.ordinalTag == 0 {
			continue
		}
		if _, err = fmt.Fprintf(&cases, "\tcase %s:\n\t\treturn %d\n", s.enumConst, s.ordinalTag); err != nil {
			return
		}
	}
	if cases.Len() > 0 {
		if _, err = fmt.Fprintf(b, "\tswitch slot {\n%s\t}\n", cases.String()); err != nil {
			return
		}
	} else {
		// No ambiguity set has a second member, so every slot's ordinal is zero.
		if _, err = b.WriteString("\t_ = slot\n"); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\treturn 0\n}\n"); err != nil {
		return
	}
	return
}

func emitEncoder(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// %s writes entities of a loaded %s as the leeway canonical wire (ADR-0210).
//
// It reads the generated readaccess accessors directly: no reflection, no
// arrow.Array type switch, one typed runtime call per column. The buffers are
// held on the encoder and reused across entities, so it is not goroutine-safe;
// one encoder per encoding goroutine.
type %s struct {
	ra     *%s
	tagger %s
	ew     *cwruntime.EntityWriter
`, plan.encoderClass, plan.raClass, plan.encoderClass, plan.raClass, plan.taggerIface)
	if err != nil {
		return
	}
	for i := range plan.slots {
		if _, err = fmt.Fprintf(b, "\t%s *cwruntime.AttributeWriter\n", plan.slots[i].awField); err != nil {
			return
		}
	}
	if plan.needsSet {
		if _, err = b.WriteString("\tsetWriter *cwruntime.SetWriter\n"); err != nil {
			return
		}
	}
	if len(plan.scratches) > 0 {
		if _, err = b.WriteString("\n\t// The readaccess container accessors are iterators and report no\n\t// length, so an `h` column's elements are collected before the array\n\t// head can be written. One reusable slice per column.\n"); err != nil {
			return
		}
		for _, sc := range plan.scratches {
			if _, err = fmt.Fprintf(b, "\t%s []%s\n", sc.name, sc.goType); err != nil {
				return
			}
		}
	}
	if _, err = b.WriteString("}\n"); err != nil {
		return
	}

	// Constructor.
	_, err = fmt.Fprintf(b, `
// New%s binds an encoder to ra, which must already be loaded from a batch.
//
// tagger may be nil: no attribute then carries an SD5 discriminator, and the
// decoding side has to resolve an ambiguous signature from content alone.
func New%s(ra *%s, tagger %s) (inst *%s, err error) {
	if ra == nil {
		err = eb.Build().Str("tableName", %q).Errorf("no read access to encode from")
		return
	}
	inst = &%s{ra: ra, tagger: tagger}
	inst.ew, err = cwruntime.NewEntityWriter()
	if err != nil {
		inst = nil
		err = eb.Build().Str("tableName", %q).Errorf("unable to create the entity writer: %%w", err)
		return
	}
`, plan.encoderClass, plan.encoderClass, plan.raClass, plan.taggerIface, plan.encoderClass,
		plan.tableName, plan.encoderClass, plan.tableName)
	if err != nil {
		return
	}
	for i := range plan.slots {
		s := &plan.slots[i]
		_, err = fmt.Fprintf(b, `	inst.%s, err = cwruntime.NewAttributeWriter(%d, %d)
	if err != nil {
		inst = nil
		err = eb.Build().Str("tableName", %q).Str("slot", %q).Errorf("unable to create the attribute writer: %%w", err)
		return
	}
`, s.awField, s.nCols, len(s.sections), plan.tableName, s.label)
		if err != nil {
			return
		}
	}
	if plan.needsSet {
		_, err = fmt.Fprintf(b, `	inst.setWriter, err = cwruntime.NewSetWriter()
	if err != nil {
		inst = nil
		err = eb.Build().Str("tableName", %q).Errorf("unable to create the set writer: %%w", err)
		return
	}
`, plan.tableName)
		if err != nil {
			return
		}
	}
	if _, err = b.WriteString("\treturn\n}\n"); err != nil {
		return
	}

	if err = emitEncodeEntity(b, plan); err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `
// EncodeAll writes every entity of the loaded batch to w as a CBOR sequence
// (RFC 8742): one entity item after another, with no framing and no header.
func (inst *%s) EncodeAll(w io.Writer) (err error) {
	n := inst.ra.GetNumberOfEntities()
	for i := range n {
		err = inst.EncodeEntity(runtime.EntityIdx(i), w)
		if err != nil {
			err = eb.Build().Str("tableName", %q).Int("entityIdx", i).Errorf("unable to encode the entity: %%w", err)
			return
		}
	}
	return
}
`, plan.encoderClass, plan.tableName)
	return
}

func emitEncodeEntity(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// EncodeEntity writes one entity item to w: the plain sections under their item
// types, then the tagged slots under their CT signatures, both in the order the
// form fixes (ADR-0210 SD1–SD3). The attributes are held back only until the
// item is complete, which is what lets the writer sort them into canonical
// order.
func (inst *%s) EncodeEntity(entityIdx runtime.EntityIdx, w io.Writer) (err error) {
	ew := inst.ew
	ew.Begin()
`, plan.encoderClass)
	if err != nil {
		return
	}

	for i := range plan.plains {
		p := &plan.plains[i]
		_, err = fmt.Fprintf(b, "\t{ // plain section %s — %s\n\t\tcw := ew.BeginPlain(%s, %d)\n",
			p.itemType, p.group, p.itemConst, len(p.columns))
		if err != nil {
			return
		}
		for j := range p.columns {
			col := &p.columns[j]
			valueExpr := fmt.Sprintf("inst.ra.%s.%s(entityIdx)", p.raField, col.getter)
			if err = emitPlainColumn(b, "\t\t", col, valueExpr); err != nil {
				return
			}
		}
		if _, err = b.WriteString("\t\tew.EndPlain()\n\t}\n"); err != nil {
			return
		}
	}

	for i := range plan.slots {
		s := &plan.slots[i]
		if err = emitSlot(b, plan, s); err != nil {
			return
		}
	}

	_, err = fmt.Fprintf(b, `	err = ew.Flush(w)
	if err != nil {
		err = eb.Build().Str("tableName", %q).Int("entityIdx", int(entityIdx)).Errorf("unable to flush the entity: %%w", err)
	}
	return
}
`, plan.tableName)
	return
}

func emitSlot(b *strings.Builder, plan *tablePlan, s *slotPlan) (err error) {
	_, err = fmt.Fprintf(b, "\t{ // slot %s — %q\n\t\tsw := ew.Slot(%s)\n\t\taw := inst.%s\n\t\tn := %s\n",
		s.label, s.signature, s.sigConst, s.awField, s.sections[0].countExpr)
	if err != nil {
		return
	}
	// A co-section group's members are written as one attribute, so they must
	// agree on how many attributes the entity has. The DML enforces it on the
	// way in; the assertion turns a batch that lost it into an error instead of
	// a truncated entity.
	for g := 1; g < len(s.sections); g++ {
		_, err = fmt.Fprintf(b, `		if m := %s; m != n {
			err = eb.Build().Str("slot", %q).Str("section", %q).Int("entityIdx", int(entityIdx)).Int64("attributes", m).Int64("want", n).Errorf("co-section members of one slot must carry the same number of attributes")
			return
		}
`, s.sections[g].countExpr, s.label, string(s.sections[g].name))
		if err != nil {
			return
		}
	}
	if _, err = b.WriteString("\t\tfor attrIdx := runtime.AttributeIdx(0); int64(attrIdx) < n; attrIdx++ {\n\t\t\taw.Begin()\n"); err != nil {
		return
	}
	for g := range s.sections {
		sec := &s.sections[g]
		if !sec.hasMemberships {
			continue
		}
		for _, ch := range sec.channels {
			if err = emitMembership(b, sec, g, ch); err != nil {
				return
			}
		}
	}
	for g := range s.sections {
		sec := &s.sections[g]
		for j := range sec.columns {
			col := &sec.columns[j]
			valueExpr := fmt.Sprintf("inst.ra.%s.Attributes.%s(entityIdx, attrIdx)", sec.raField, col.getter)
			if err = emitAttributeColumn(b, "\t\t\t", col, valueExpr); err != nil {
				return
			}
		}
	}
	if s.ambiguous {
		_, err = fmt.Fprintf(b, `			if inst.tagger != nil {
				aw.Discriminator(inst.tagger.Tag(%s, entityIdx, attrIdx))
			}
`, s.enumConst)
		if err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `			attr, aerr := aw.End()
			if aerr != nil {
				err = eb.Build().Str("slot", %q).Int("entityIdx", int(entityIdx)).Int("attrIdx", int(attrIdx)).Errorf("unable to finish the attribute: %%w", aerr)
				return
			}
			sw.Add(attr)
		}
	}
`, s.label)
	return
}

func emitMembership(b *strings.Builder, sec *sectionPlan, group int, ch mappingplan.MembershipChannel) (err error) {
	accessor, seq2, err := membershipAccessor(ch)
	if err != nil {
		return
	}
	chConst := "mappingplan." + membershipChannelConstName(ch)
	if seq2 {
		args := "v1, nil, v2"
		if ch.Identity() != mappingplan.ChannelIdentityPerRowId {
			args = "0, v1, v2"
		}
		_, err = fmt.Fprintf(b, `			for v1, v2 := range inst.ra.%s.Memberships.%s(entityIdx, attrIdx) {
				aw.Membership(%d, %s, %s)
			}
`, sec.raField, accessor, group, chConst, args)
		return
	}
	var args string
	switch ch.Identity() {
	case mappingplan.ChannelIdentityRef:
		args = "v, nil, nil"
	case mappingplan.ChannelIdentityVerbatim:
		args = "0, v, nil"
	case mappingplan.ChannelIdentityPerRowBlob:
		args = "0, nil, v"
	default:
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("channel has no single-value read form")
		return
	}
	_, err = fmt.Fprintf(b, `			for v := range inst.ra.%s.Memberships.%s(entityIdx, attrIdx) {
				aw.Membership(%d, %s, %s)
			}
`, sec.raField, accessor, group, chConst, args)
	return
}

// membershipChannelConstName names the mappingplan constant of a channel. The
// enum's own String is the lw: tag spelling, which is empty for the default
// channel, so the constant is rebuilt from the method suffix the channel table
// carries — the same suffix the readaccess accessors are named after.
func membershipChannelConstName(ch mappingplan.MembershipChannel) string {
	return "MembershipChannel" + ch.AddMethodSuffix()
}

// emitPlainColumn writes one column of a plain section into the writer
// BeginPlain handed out.
func emitPlainColumn(b *strings.Builder, indent string, col *columnPlan, valueExpr string) (err error) {
	if _, err = fmt.Fprintf(b, "%s{\n", indent); err != nil {
		return
	}
	if err = emitColumnValue(b, indent+"\t", "cw", col, valueExpr); err != nil {
		return
	}
	_, err = fmt.Fprintf(b, "%s}\n", indent)
	return
}

// emitAttributeColumn writes one value column of an attribute, in key order:
// the attribute writer hands out the scratch the value is written into, and
// closing it is what derives the column's cardinality from the bytes.
func emitAttributeColumn(b *strings.Builder, indent string, col *columnPlan, valueExpr string) (err error) {
	if _, err = fmt.Fprintf(b, "%s{\n%s\tcw := aw.BeginValue()\n", indent, indent); err != nil {
		return
	}
	if err = emitColumnValue(b, indent+"\t", "cw", col, valueExpr); err != nil {
		return
	}
	_, err = fmt.Fprintf(b, "%s\taw.EndValue()\n%s}\n", indent, indent)
	return
}

// emitColumnValue writes one value into cwVar: a scalar straight through, an
// `h` column as a definite array over a reusable scratch slice, an `m` column
// through the runtime's SetWriter, which sorts the elements and keeps
// duplicates.
func emitColumnValue(b *strings.Builder, indent string, cwVar string, col *columnPlan, valueExpr string) (err error) {
	switch col.subType {
	case common.IntermediateColumnsSubTypeScalar:
		_, err = fmt.Fprintf(b, `%sv := %s
%s%s.%s
`, indent, valueExpr, indent, cwVar, col.writeCall)
	case common.IntermediateColumnsSubTypeHomogenousArray:
		_, err = fmt.Fprintf(b, `%ss := inst.%s[:0]
%sfor v := range %s {
%s	s = append(s, v)
%s}
%sinst.%s = s
%s%s.ArrayHead(len(s))
%sfor _, v := range s {
%s	%s.%s
%s}
`, indent, col.scratch, indent, valueExpr, indent, indent, indent, col.scratch,
			indent, cwVar, indent, indent, cwVar, col.writeCall, indent)
	case common.IntermediateColumnsSubTypeSet:
		_, err = fmt.Fprintf(b, `%sinst.setWriter.Begin()
%sfor v := range %s {
%s	inst.setWriter.Elem().%s
%s	inst.setWriter.EndElem()
%s}
%sinst.setWriter.Flush(%s)
`, indent, indent, valueExpr, indent, col.writeCall, indent, indent, indent, cwVar)
	default:
		err = eb.Build().Stringer("subType", col.subType).Errorf("unhandled column subtype")
	}
	return
}
