package canonwire

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The decoder generator of ADR-0210 SD5 and SD6. It reads the same slot table
// the encoder does and emits, for one table, the code that walks an entity
// item and drives that table's generated dml builders.
//
// Two things distinguish it from the encoder. The wire carries values in *key*
// order and the dml takes them in *declaration* order, grouped by subtype —
// scalars to BeginAttribute, containers one element at a time to
// AddToCoContainers — so the generator bakes the permutation. And a signature
// several slots share does not name its section: the decoder narrows the
// candidates by the memberships the attribute carries (SD5 step 1) and asks
// the table's DispatcherI only when more than one survives.

// planDecoder resolves everything the decoder emission needs onto plan: the
// class names, the distinct signature cases with their read calls, the per-
// section dml argument orders, and the accepted-channel masks the narrowing
// step compares against.
func planDecoder(plan *tablePlan, tblDesc *common.TableDesc, slots *SlotTable, clsNamer gocodegen.GoClassNamerI) (err error) {
	if plan.dmlClass, err = clsNamer.ComposeEntityDmlClassName(plan.tableName); err != nil {
		err = eh.Errorf("unable to compose the dml entity class name: %w", err)
		return
	}
	if plan.decoderClass, err = clsNamer.ComposeCanonWireDecoderClassName(plan.tableName); err != nil {
		err = eh.Errorf("unable to compose the decoder class name: %w", err)
		return
	}
	if plan.dispatcherIface, err = clsNamer.ComposeCanonWireDispatcherInterfaceName(plan.tableName); err != nil {
		err = eh.Errorf("unable to compose the dispatcher interface name: %w", err)
		return
	}
	plan.ordinalDispatcher = ordinalDispatcherName(plan.dispatcherIface)
	suffix := decoderSymbolSuffix(plan.decoderClass)
	plan.ambiguousConst = "canonWireAmbiguous" + suffix
	plan.masksVar = "canonWireAcceptMasks" + suffix

	// The sections, in slot order: the dml argument orders and the accepted
	// channel mask are per section and do not depend on the signature case.
	for i := range plan.slots {
		sp := &plan.slots[i]
		src := &slots.Slots[i]
		keyBase := 0
		for g := range sp.sections {
			sec := &sp.sections[g]
			slotSec := &src.Sections[g]
			desc := &tblDesc.TaggedValuesSections[slotSec.SectionIdx]
			sec.dmlGetter = "GetSection" + sec.name.Convert(naming.UpperCamelCase).String()
			sec.keyBase = keyBase
			sec.scalarArgs, sec.containerArgs, err = dmlArgOrder(desc.ValueColumnTypes, slotSec.ColumnOrder, keyBase)
			if err != nil {
				err = eb.Build().Stringer("section", sec.name).Errorf("unable to order the dml arguments: %w", err)
				return
			}
			switch len(sec.containerArgs) {
			case 0:
			case 1:
				sec.containerAdd = "AddToContainerP"
			default:
				sec.containerAdd = "AddToCoContainersP"
			}
			sec.acceptMask = channelMask(slotSec.MembershipSpec)
			keyBase += len(sec.columns)
		}
		if keyBase > plan.maxCols {
			plan.maxCols = keyBase
		}
		if n := len(sp.sections); n > plan.maxGroups {
			plan.maxGroups = n
		}
	}

	// One case per distinct signature, in slot-table order, which is sorted by
	// signature — so the cases and the ambiguity sets are stable.
	seen := make(map[string]int, len(plan.slots))
	for i := range plan.slots {
		sp := &plan.slots[i]
		if at, ok := seen[sp.signature]; ok {
			c := &plan.sigCases[at]
			c.slots = append(c.slots, i)
			c.label += "|" + sp.label
			continue
		}
		seen[sp.signature] = len(plan.sigCases)
		c := sigCase{
			signature: sp.signature,
			sigConst:  sp.sigConst,
			label:     sp.label,
			slots:     []int{i},
			nCols:     sp.nCols,
			nGroups:   len(sp.sections),
		}
		for g := range sp.sections {
			c.columns = append(c.columns, sp.sections[g].columns...)
		}
		plan.sigCases = append(plan.sigCases, c)
	}
	for i := range plan.sigCases {
		c := &plan.sigCases[i]
		if len(c.slots) > 1 {
			c.setVar = fmt.Sprintf("canonWireAmbiguitySet%s%02d", suffix, i)
		}
		for k := range c.columns {
			col := &c.columns[k]
			if col.subType == common.IntermediateColumnsSubTypeScalar {
				col.decLocal = fmt.Sprintf("dv%02d", k)
				continue
			}
			col.decScratch = fmt.Sprintf("scratchSig%02dCol%02d", i, k)
			plan.decScratches = append(plan.decScratches, scratchField{name: col.decScratch, goType: col.goType})
		}
	}

	// The plain sections: the setter, and the same permutation over the plain
	// group's key order.
	for i := range plan.plains {
		pp := &plan.plains[i]
		src := &slots.Plains[i]
		pp.setter = plainSetterName(pp.itemType)
		if pp.setter == "" {
			err = eb.Build().Stringer("plainItemType", pp.itemType).Errorf("plain item type has no dml setter")
			return
		}
		pp.scalarArgs, pp.containerArgs, err = dmlArgOrder(src.ColumnTypes, src.ColumnOrder, 0)
		if err != nil {
			err = eb.Build().Stringer("plainItemType", pp.itemType).Errorf("unable to order the dml arguments: %w", err)
			return
		}
		for k := range pp.columns {
			col := &pp.columns[k]
			if col.subType == common.IntermediateColumnsSubTypeScalar {
				col.decLocal = fmt.Sprintf("dp%02d", k)
				continue
			}
			col.decScratch = "scratchDecoded" + pp.raField + fmt.Sprintf("Col%02d", k)
			plan.decScratches = append(plan.decScratches, scratchField{name: col.decScratch, goType: col.goType})
		}
	}
	return
}

// ordinalDispatcherName names the built-in dispatcher implementation, the way
// ordinalTaggerName names its mirror: the interface's trailing I dropped and
// "Ordinal" put where the family prefix ends.
func ordinalDispatcherName(dispatcherIface string) string {
	base := strings.TrimSuffix(dispatcherIface, "I")
	if rest, ok := strings.CutPrefix(base, "CanonWireDispatcher"); ok {
		return "CanonWireOrdinalDispatcher" + rest
	}
	return base + "Ordinal"
}

// decoderSymbolSuffix is the per-table tail of the decoder's unexported
// symbols. It is derived from the decoder class name rather than from the
// table name so that two tables generated into one package cannot collide
// however the namer spells them.
func decoderSymbolSuffix(decoderClass string) string {
	return strings.TrimPrefix(decoderClass, "CanonWireDecoder")
}

// plainSetterName is the dml entity builder's setter for one plain item type.
// It mirrors dml.itemTypeToSetterName, which is unexported there; a plain item
// type it does not name has no setter and cannot be decoded into.
func plainSetterName(itemType common.PlainItemTypeE) string {
	switch itemType {
	case common.PlainItemTypeEntityId:
		return "SetId"
	case common.PlainItemTypeEntityTimestamp:
		return "SetTimestamp"
	case common.PlainItemTypeEntityRouting:
		return "SetRouting"
	case common.PlainItemTypeEntityLifecycle:
		return "SetLifecycle"
	case common.PlainItemTypeTransaction:
		return "SetTransaction"
	case common.PlainItemTypeOpaque:
		return "SetOpaque"
	}
	return ""
}

// dmlArgOrder maps a section's value columns onto the order the generated dml
// takes them, as key positions.
//
// The dml groups a section's columns by subtype before it emits an argument
// list — the scalars, then the `h` arrays, then the `m` sets, each in
// declaration order — because that is the order the intermediate
// representation holds them in. The wire holds them in key order, which is the
// canonical types sorted. cts is in declaration order and columnOrder maps a
// key position, relative to keyBase, to an index into it.
//
// A set is a co-container in the dml exactly as an array is: one element is
// appended to every container column of the section at once, so containers
// comes back as one list and not two.
func dmlArgOrder(cts []canonicaltypes.PrimitiveAstNodeI, columnOrder []int, keyBase int) (scalars []int, containers []int, err error) {
	keyOf := make([]int, len(cts))
	for k, d := range columnOrder {
		if d < 0 || d >= len(cts) {
			err = eb.Build().Int("column", d).Int("columns", len(cts)).Errorf("the column order names a column the section does not have")
			return
		}
		keyOf[d] = keyBase + k
	}
	arrays := make([]int, 0, len(cts))
	sets := make([]int, 0, len(cts))
	scalars = make([]int, 0, len(cts))
	for d, ct := range cts {
		var sm canonicaltypes.ScalarModifierE
		sm, err = common.ExtractScalarModifier(ct)
		if err != nil {
			return nil, nil, err
		}
		switch sm {
		case canonicaltypes.ScalarModifierNone:
			scalars = append(scalars, keyOf[d])
		case canonicaltypes.ScalarModifierHomogenousArray:
			arrays = append(arrays, keyOf[d])
		case canonicaltypes.ScalarModifierSet:
			sets = append(sets, keyOf[d])
		default:
			err = eb.Build().Stringer("scalarModifier", sm).Stringer("ct", ct).Errorf("unhandled scalar modifier")
			return nil, nil, err
		}
	}
	containers = append(arrays, sets...)
	return
}

// channelMask folds a section's declared membership spec into a bitmask over
// the mappingplan channel ordinals — the form SD5's narrowing step compares an
// attribute's carried channels against.
func channelMask(spec common.MembershipSpecE) (mask uint32) {
	for _, ch := range AllMembershipChannels {
		if SpecAcceptsChannel(spec, ch) {
			mask |= uint32(1) << uint(ch)
		}
	}
	return
}

// membershipAddCall is the dml's per-channel append and its arguments over a
// membership view named m. The method name is the channel table's own
// convention — AddMembership<Suffix>P — and the arguments are the fields the
// channel's identity encoding fills.
func membershipAddCall(ch mappingplan.MembershipChannel) (call string, err error) {
	suffix := ch.AddMethodSuffix()
	if suffix == "" {
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("channel has no dml add method")
		return
	}
	var args string
	switch ch.Identity() {
	case mappingplan.ChannelIdentityRef:
		args = "m.Ref"
	case mappingplan.ChannelIdentityVerbatim:
		args = "m.Verbatim"
	case mappingplan.ChannelIdentityPerRowId:
		args = "m.Ref, m.Params"
	case mappingplan.ChannelIdentityPerRowName:
		args = "m.Verbatim, m.Params"
	case mappingplan.ChannelIdentityPerRowBlob:
		args = "m.Params"
	default:
		err = eb.Build().Uint8("channel", uint8(ch)).Errorf("channel has no identity encoding")
		return
	}
	return "AddMembership" + suffix + "P(" + args + ")", nil
}
