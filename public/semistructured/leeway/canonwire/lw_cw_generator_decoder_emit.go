package canonwire

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// The decoder's emission (ADR-0207 SD5, SD6). It is split from the planning so
// that every refusal — a canonical type with no wire form, a plain item type
// with no dml setter — has already happened before a byte of code is written.

// emitDispatcher writes the generation-time facts SD5's dispatch reads: which
// signatures need it, which slots each of those covers, and which membership
// channels every section accepts. Then the pluggable interface and the
// built-in ordinal implementation over the same tables.
func emitDispatcher(b *strings.Builder, plan *tablePlan) (err error) {
	ambiguous := false
	for i := range plan.sigCases {
		if plan.sigCases[i].setVar != "" {
			ambiguous = true
		}
	}
	_, err = fmt.Fprintf(b, `
// %s reports whether any signature of %s is carried by more than one slot.
// It is what the decoder's constructor checks: an ambiguous table cannot be
// decoded without a dispatcher, and the refusal is at construction rather than
// at the first attribute that needs one (ADR-0207 SD5).
const %s = %t
`, plan.ambiguousConst, plan.tableName, plan.ambiguousConst, ambiguous)
	if err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `
// %s holds, per slot in slot order, the membership channels each member
// section accepts — one bitmask per section in signature order, over the
// mappingplan channel ordinals. It is the input to the narrowing step of
// ADR-0207 SD5: a section that cannot store the memberships an attribute
// carries is not that attribute's target.
var %s = [][]uint32{
`, plan.masksVar, plan.masksVar)
	if err != nil {
		return
	}
	for i := range plan.slots {
		s := &plan.slots[i]
		parts := make([]string, 0, len(s.sections))
		for g := range s.sections {
			parts = append(parts, fmt.Sprintf("0x%02x", s.sections[g].acceptMask))
		}
		if _, err = fmt.Fprintf(b, "\t{%s}, // %s\n", strings.Join(parts, ", "), s.label); err != nil {
			return
		}
	}
	if _, err = b.WriteString("}\n"); err != nil {
		return
	}

	for i := range plan.sigCases {
		c := &plan.sigCases[i]
		if c.setVar == "" {
			continue
		}
		names := make([]string, 0, len(c.slots))
		for _, o := range c.slots {
			names = append(names, plan.slots[o].enumConst)
		}
		_, err = fmt.Fprintf(b, `
// %s is the ambiguity set of the signature %q: every slot that carries it, in
// slot order. The built-in tagger/dispatcher pair keys on a slot's position in
// this slice.
var %s = []%s{%s}
`, c.setVar, c.signature, c.setVar, plan.slotEnum, strings.Join(names, ", "))
		if err != nil {
			return
		}
	}

	_, err = fmt.Fprintf(b, `
// %s is the decoder half of ADR-0207 SD5's dispatch pair.
//
// It is consulted only for an attribute whose signature more than one slot of
// this table carries *and* whose memberships more than one of those slots can
// store: candidates holds the survivors of the narrowing step, in slot order,
// and is never empty and never of length one — those two cases are decided
// without a call. attr is the attribute as the wire carried it, with nothing
// of either endpoint's table description in it.
//
// The returned slot must be one of the candidates; anything else fails the
// entity.
type %s interface {
	Dispatch(candidates []%s, attr *cwruntime.AttributeView) (slot %s, err error)
}

// %s is the built-in dispatcher of ADR-0207 SD5 and the mirror of %s: it reads
// the attribute's discriminator as the slot's ordinal within the full
// ambiguity set. It round-trips between two tables that declare the same
// sections in the same order, and is explicitly that declaration-order
// coupling — a consumer that wants section-name independence supplies its own
// pair.
type %s struct{}

var _ %s = %s{}

func (inst %s) Dispatch(candidates []%s, attr *cwruntime.AttributeView) (slot %s, err error) {
`, plan.dispatcherIface, plan.dispatcherIface, plan.slotEnum, plan.slotEnum,
		plan.ordinalDispatcher, plan.ordinalTagger, plan.ordinalDispatcher,
		plan.dispatcherIface, plan.ordinalDispatcher, plan.ordinalDispatcher,
		plan.slotEnum, plan.slotEnum)
	if err != nil {
		return
	}
	if !ambiguous {
		_, err = fmt.Fprintf(b, `	_ = candidates
	_ = attr
	err = eb.Build().Str("tableName", %q).Errorf("the table has no ambiguous signature, so no attribute is dispatched: %%w", cwruntime.ErrDispatch)
	return
}
`, plan.tableName)
		return
	}
	_, err = fmt.Fprintf(b, `	if len(candidates) == 0 {
		err = eb.Build().Str("tableName", %q).Errorf("no candidate slot to dispatch to: %%w", cwruntime.ErrDispatch)
		return
	}
	if !attr.HasDiscriminator {
		err = eb.Build().Str("tableName", %q).Errorf("the attribute carries no discriminator, which is what the ordinal dispatcher decides by: %%w", cwruntime.ErrDispatch)
		return
	}
	var set []%s
	switch candidates[0] {
`, plan.tableName, plan.tableName, plan.slotEnum)
	if err != nil {
		return
	}
	for i := range plan.sigCases {
		c := &plan.sigCases[i]
		if c.setVar == "" {
			continue
		}
		names := make([]string, 0, len(c.slots))
		for _, o := range c.slots {
			names = append(names, plan.slots[o].enumConst)
		}
		if _, err = fmt.Fprintf(b, "\tcase %s:\n\t\tset = %s\n", strings.Join(names, ", "), c.setVar); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `	default:
		err = eb.Build().Str("tableName", %q).Stringer("slot", candidates[0]).Errorf("the candidate is not in any ambiguity set: %%w", cwruntime.ErrDispatch)
		return
	}
	if attr.Discriminator >= uint64(len(set)) {
		err = eb.Build().Str("tableName", %q).Uint64("discriminator", attr.Discriminator).Int("ambiguitySet", len(set)).Errorf("the discriminator is outside the ambiguity set: %%w", cwruntime.ErrDispatch)
		return
	}
	slot = set[attr.Discriminator]
	for _, c := range candidates {
		if c == slot {
			return
		}
	}
	err = eb.Build().Str("tableName", %q).Uint64("discriminator", attr.Discriminator).Stringer("slot", slot).Errorf("the discriminator names a slot the attribute's memberships rule out: %%w", cwruntime.ErrDispatch)
	return
}
`, plan.tableName, plan.tableName, plan.tableName)
	return
}

// emitDecoder writes the decoder class: its buffers, its constructor, the
// narrowing helper, and the entity walk that drives the dml.
func emitDecoder(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// %s decodes leeway canonical wire entities (ADR-0207) into a %s.
//
// It drives the generated dml builders directly: the plain setters, then one
// BeginAttribute / container append / AddMembership… / EndAttribute per wire
// attribute, then CommitEntity. No reflection, one typed runtime call per
// column. The buffers are held on the decoder and reused across entities, so
// it is not goroutine-safe; one decoder per decoding goroutine.
//
// A plain section the wire does not carry is simply not set — the entity gets
// the dml's zero for it, which is the entity a writer that never called that
// setter would have produced. Attributes come back in the form's canonical
// order, not the order they were written in.
type %s struct {
	dml        *%s
	dispatcher %s
	r          *cwruntime.CborReader
	er         *cwruntime.EntityReader
	view       cwruntime.AttributeView
	groups     [][]cwruntime.MembershipView
	values     [][]byte
	cands      []%s
`, plan.decoderClass, plan.dmlClass, plan.decoderClass, plan.dmlClass, plan.dispatcherIface, plan.slotEnum)
	if err != nil {
		return
	}
	if len(plan.decScratches) > 0 {
		_, err = b.WriteString("\n\t// A container column arrives as an array head and its elements; the dml\n\t// takes one element at a time, or a plain section's whole slice. One\n\t// reusable slice per column of the wire's key order.\n")
		if err != nil {
			return
		}
		for _, sc := range plan.decScratches {
			if _, err = fmt.Fprintf(b, "\t%s []%s\n", sc.name, sc.goType); err != nil {
				return
			}
		}
	}
	if _, err = b.WriteString("}\n"); err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `
// New%s binds a decoder to dml, which it drives one entity at a time.
//
// dispatcher may be nil only for a table with no ambiguous signature — %s says
// which this is. A table that has one cannot resolve an attribute's slot from
// the key alone, so it is refused here rather than at the first attribute that
// would have needed the hook (ADR-0207 SD5).
func New%s(dml *%s, dispatcher %s) (inst *%s, err error) {
	if dml == nil {
		err = eb.Build().Str("tableName", %q).Errorf("no dml builder to decode into")
		return
	}
	if dispatcher == nil && %s {
		err = eb.Build().Str("tableName", %q).Errorf("the table carries a signature more than one slot claims, which needs a dispatcher: %%w", cwruntime.ErrDispatch)
		return
	}
	inst = &%s{dml: dml, dispatcher: dispatcher}
	inst.r = cwruntime.NewCborReader(nil)
	inst.er = cwruntime.NewEntityReader(inst.r)
	inst.groups = make([][]cwruntime.MembershipView, %d)
	inst.values = make([][]byte, %d)
	return
}

// slotAcceptsChannels is the narrowing step of ADR-0207 SD5: every membership
// the attribute carries must land on a channel the member section at that
// group's position declares. It is also the check an unambiguous slot goes
// through, because a signature match does not imply the carriage matches.
func (inst *%s) slotAcceptsChannels(slot %s) (accepts bool) {
	if int(slot) >= len(%s) {
		return false
	}
	masks := %s[slot]
	if len(masks) != len(inst.view.Groups) {
		return false
	}
	for g, mask := range masks {
		for _, m := range inst.view.Groups[g] {
			if uint(m.Channel) >= 32 || mask&(uint32(1)<<uint(m.Channel)) == 0 {
				return false
			}
		}
	}
	return true
}

// DecodeEntity decodes the one entity item at the start of b and writes it
// through the dml, returning how many bytes it consumed. On any failure the
// entity is rolled back, so the builder is left where it was and the next
// entity can still be decoded into it.
func (inst *%s) DecodeEntity(b []byte) (n int, err error) {
	inst.er.Reset(b)
	inst.dml.BeginEntity()
	err = inst.decodeEntity()
	if err == nil {
		err = inst.dml.CheckErrors()
		if err != nil {
			err = eb.Build().Str("tableName", %q).Errorf("the dml builder rejected the decoded entity: %%w", err)
		}
	}
	if err != nil {
		if rerr := inst.dml.RollbackEntity(); rerr != nil {
			err = eb.Build().Str("tableName", %q).Str("rollbackError", rerr.Error()).Errorf("unable to roll the entity back: %%w", err)
		}
		return 0, err
	}
	err = inst.dml.CommitEntity()
	if err != nil {
		err = eb.Build().Str("tableName", %q).Errorf("unable to commit the decoded entity: %%w", err)
		return 0, err
	}
	return inst.r.Pos(), nil
}

// DecodeAll decodes a CBOR sequence (RFC 8742) of entity items — what
// EncodeAll writes — and returns how many entities it wrote.
func (inst *%s) DecodeAll(b []byte) (nEntities int, err error) {
	for len(b) > 0 {
		var n int
		n, err = inst.DecodeEntity(b)
		if err != nil {
			err = eb.Build().Str("tableName", %q).Int("entityIdx", nEntities).Errorf("unable to decode the entity: %%w", err)
			return
		}
		if n <= 0 {
			err = eb.Build().Str("tableName", %q).Int("entityIdx", nEntities).Errorf("the decoder consumed no bytes: %%w", cwruntime.ErrOutOfRange)
			return
		}
		b = b[n:]
		nEntities++
	}
	return
}
`, plan.decoderClass, plan.ambiguousConst, plan.decoderClass, plan.dmlClass, plan.dispatcherIface,
		plan.decoderClass, plan.tableName, plan.ambiguousConst, plan.tableName, plan.decoderClass,
		plan.maxGroups, plan.maxCols,
		plan.decoderClass, plan.slotEnum, plan.masksVar, plan.masksVar,
		plan.decoderClass, plan.tableName, plan.tableName, plan.tableName,
		plan.decoderClass, plan.tableName, plan.tableName)
	if err != nil {
		return
	}
	return emitDecodeEntity(b, plan)
}

func emitDecodeEntity(b *strings.Builder, plan *tablePlan) (err error) {
	_, err = fmt.Fprintf(b, `
// decodeEntity walks one entity item: the plain sections under their item
// types, then the tagged slots under their CT signatures. It leaves the dml's
// entity open — DecodeEntity commits or rolls it back.
func (inst *%s) decodeEntity() (err error) {
	er := inst.er
	r := inst.r
	er.Begin()
	if err = r.Err(); err != nil {
		err = eb.Build().Str("tableName", %q).Errorf("unable to begin the entity: %%w", err)
		return
	}
	for {
		itemType, nCols, ok := er.NextPlain()
		if !ok {
			break
		}
`, plan.decoderClass, plan.tableName)
	if err != nil {
		return
	}
	if len(plan.plains) == 0 {
		if _, err = b.WriteString("\t\t_ = nCols\n"); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\t\tswitch itemType {\n"); err != nil {
		return
	}
	for i := range plan.plains {
		if err = emitDecodePlain(b, plan, &plan.plains[i]); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `		default:
			err = eb.Build().Stringer("plainItemType", itemType).Errorf("the table declares no plain section of this item type: %%w", cwruntime.ErrUnknownPlain)
			return
		}
	}
	if err = r.Err(); err != nil {
		err = eb.Build().Str("tableName", %q).Errorf("unable to read the entity's plain sections: %%w", err)
		return
	}
	for {
		sig, nAttrs, ok := er.NextSlot()
		if !ok {
			break
		}
`, plan.tableName)
	if err != nil {
		return
	}
	if len(plan.sigCases) == 0 {
		if _, err = b.WriteString("\t\t_ = nAttrs\n"); err != nil {
			return
		}
	}
	if _, err = b.WriteString("\t\tswitch string(sig) {\n"); err != nil {
		return
	}
	for i := range plan.sigCases {
		if err = emitDecodeSigCase(b, plan, &plan.sigCases[i]); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `		default:
			err = eb.Build().Bytes("signature", sig).Errorf("the table has no slot for this canonical type signature: %%w", cwruntime.ErrUnknownSlot)
			return
		}
	}
	if err = r.Err(); err != nil {
		err = eb.Build().Str("tableName", %q).Errorf("unable to read the entity's tagged slots: %%w", err)
		return
	}
	er.End()
	if err = r.Err(); err != nil {
		err = eb.Build().Str("tableName", %q).Errorf("unable to close the entity: %%w", err)
		return
	}
	return
}
`, plan.tableName, plan.tableName)
	return
}

// emitDecodePlain writes one plain section's case: the column count check, the
// typed reads in key order, and the one setter call that takes the whole
// section in declaration order.
func emitDecodePlain(b *strings.Builder, plan *tablePlan, p *plainPlan) (err error) {
	_, err = fmt.Fprintf(b, `		case %s:
			if nCols != %d {
				err = eb.Build().Stringer("plainItemType", itemType).Int("columns", nCols).Int("want", %d).Errorf("the plain section does not carry the number of columns the table declares: %%w", cwruntime.ErrOutOfRange)
				return
			}
`, p.itemConst, len(p.columns), len(p.columns))
	if err != nil {
		return
	}
	for k := range p.columns {
		if err = emitDecodeColumn(b, "\t\t\t", &p.columns[k], -1); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `			if err = r.Err(); err != nil {
				err = eb.Build().Stringer("plainItemType", itemType).Errorf("unable to read the plain section: %%w", err)
				return
			}
`)
	if err != nil {
		return
	}
	args := make([]string, 0, len(p.columns))
	for _, k := range p.scalarArgs {
		args = append(args, p.columns[k].decLocal)
	}
	for _, k := range p.containerArgs {
		args = append(args, "inst."+p.columns[k].decScratch)
	}
	_, err = fmt.Fprintf(b, "\t\t\tinst.dml.%s(%s)\n", p.setter, strings.Join(args, ", "))
	return
}

// emitDecodeSigCase writes one distinct signature's case: the per-attribute
// read, then either the one slot the signature names or the SD5 narrowing and
// dispatch that pick one of the slots that share it.
func emitDecodeSigCase(b *strings.Builder, plan *tablePlan, c *sigCase) (err error) {
	_, err = fmt.Fprintf(b, `		case %s: // %s
			for a := 0; a < nAttrs; a++ {
				ar := er.Attributes()
				hasDisc := ar.Begin(%d, %d)
				for g := 0; g < %d; g++ {
					nm := ar.NextGroup()
					mv := inst.groups[g][:0]
					for j := 0; j < nm; j++ {
						ch, ref, verbatim, params := ar.Membership()
						mv = append(mv, cwruntime.MembershipView{Channel: ch, Ref: ref, Verbatim: verbatim, Params: params})
					}
					inst.groups[g] = mv
				}
				inst.view.Groups = inst.groups[:%d]
`, c.sigConst, c.label, c.nCols, c.nGroups, c.nGroups, c.nGroups)
	if err != nil {
		return
	}
	for k := range c.columns {
		if err = emitDecodeColumn(b, "\t\t\t\t", &c.columns[k], k); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `				inst.view.Values = inst.values[:%d]
				inst.view.HasDiscriminator = hasDisc
				inst.view.Discriminator = 0
				if hasDisc {
					inst.view.Discriminator = ar.Discriminator()
				}
				ar.End()
				if err = r.Err(); err != nil {
					err = eb.Build().Str("slot", %q).Int("attrIdx", a).Errorf("unable to read the attribute: %%w", err)
					return
				}
`, c.nCols, c.label)
	if err != nil {
		return
	}
	if c.setVar == "" {
		s := &plan.slots[c.slots[0]]
		_, err = fmt.Fprintf(b, `				if !inst.slotAcceptsChannels(%s) {
					err = eb.Build().Str("slot", %q).Int("attrIdx", a).Errorf("the slot does not accept a membership channel the attribute carries: %%w", cwruntime.ErrChannelNotAccepted)
					return
				}
`, s.enumConst, s.label)
		if err != nil {
			return
		}
		if err = emitSlotWrite(b, "\t\t\t\t", c, s); err != nil {
			return
		}
		_, err = b.WriteString("\t\t\t}\n")
		return
	}

	_, err = fmt.Fprintf(b, `				cands := inst.cands[:0]
				for _, s := range %s {
					if inst.slotAcceptsChannels(s) {
						cands = append(cands, s)
					}
				}
				inst.cands = cands
				var slot %s
				switch len(cands) {
				case 0:
					err = eb.Build().Str("signature", %q).Int("attrIdx", a).Errorf("no slot sharing the signature accepts the attribute's membership channels: %%w", cwruntime.ErrChannelNotAccepted)
					return
				case 1:
					slot = cands[0]
				default:
					slot, err = inst.dispatcher.Dispatch(cands, &inst.view)
					if err != nil {
						err = eb.Build().Str("signature", %q).Int("attrIdx", a).Errorf("the dispatcher refused the attribute: %%w", err)
						return
					}
					{
						known := false
						for _, cand := range cands {
							if cand == slot {
								known = true
								break
							}
						}
						if !known {
							err = eb.Build().Str("signature", %q).Int("attrIdx", a).Stringer("slot", slot).Errorf("the dispatcher returned a slot that is not a candidate: %%w", cwruntime.ErrDispatch)
							return
						}
					}
				}
				switch slot {
`, c.setVar, plan.slotEnum, c.signature, c.signature, c.signature)
	if err != nil {
		return
	}
	for _, o := range c.slots {
		s := &plan.slots[o]
		if _, err = fmt.Fprintf(b, "\t\t\t\tcase %s:\n", s.enumConst); err != nil {
			return
		}
		if err = emitSlotWrite(b, "\t\t\t\t\t", c, s); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, `				default:
					err = eb.Build().Str("signature", %q).Int("attrIdx", a).Stringer("slot", slot).Errorf("the chosen slot does not carry the signature: %%w", cwruntime.ErrDispatch)
					return
				}
			}
`, c.signature)
	return
}

// emitDecodeColumn writes one value column's read, in key order. A scalar
// lands in a local, a container in the decoder's reusable slice; both capture
// the value's raw bytes for the AttributeView the dispatch reads.
//
// valueIdx is the column's key position for a tagged slot's column, and -1 for
// a plain section's, whose values no dispatch ever sees.
func emitDecodeColumn(b *strings.Builder, indent string, col *columnPlan, valueIdx int) (err error) {
	capture := func() (err error) {
		if valueIdx < 0 {
			return
		}
		_, err = fmt.Fprintf(b, "%s\tinst.values[%d] = r.Since(p)\n", indent, valueIdx)
		return
	}
	openPos := func() (err error) {
		if valueIdx < 0 {
			_, err = fmt.Fprintf(b, "%s{\n", indent)
			return
		}
		_, err = fmt.Fprintf(b, "%s{\n%s\tp := r.Pos()\n", indent, indent)
		return
	}
	switch col.subType {
	case common.IntermediateColumnsSubTypeScalar:
		if valueIdx < 0 {
			// A plain section's value: no dispatch ever sees it, so it needs
			// neither the capture nor the block the capture is scoped in.
			if col.readCall == "" {
				_, err = fmt.Fprintf(b, "%svar %s %s\n%sr.ReadBytesInto(%s[:])\n", indent, col.decLocal, col.goType, indent, col.decLocal)
				return
			}
			_, err = fmt.Fprintf(b, "%s%s := r.%s\n", indent, col.decLocal, col.readCall)
			return
		}
		if _, err = fmt.Fprintf(b, "%svar %s %s\n", indent, col.decLocal, col.goType); err != nil {
			return
		}
		if err = openPos(); err != nil {
			return
		}
		if col.readCall == "" {
			_, err = fmt.Fprintf(b, "%s\tr.ReadBytesInto(%s[:])\n", indent, col.decLocal)
		} else {
			_, err = fmt.Fprintf(b, "%s\t%s = r.%s\n", indent, col.decLocal, col.readCall)
		}
		if err != nil {
			return
		}
		if err = capture(); err != nil {
			return
		}
		_, err = fmt.Fprintf(b, "%s}\n", indent)
		return
	case common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
		head := "ReadArrayHead()"
		if col.subType == common.IntermediateColumnsSubTypeSet {
			head = "ReadSetHead()"
		}
		if err = openPos(); err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `%s	nElem := r.%s
%s	s := inst.%s[:0]
%s	for j := 0; j < nElem; j++ {
`, indent, head, indent, col.decScratch, indent)
		if err != nil {
			return
		}
		if col.readCall == "" {
			_, err = fmt.Fprintf(b, `%s		var e %s
%s		r.ReadBytesInto(e[:])
%s		s = append(s, e)
`, indent, col.goType, indent, indent)
		} else {
			_, err = fmt.Fprintf(b, "%s\t\ts = append(s, r.%s)\n", indent, col.readCall)
		}
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, "%s	}\n%s	inst.%s = s\n", indent, indent, col.decScratch)
		if err != nil {
			return
		}
		if err = capture(); err != nil {
			return
		}
		_, err = fmt.Fprintf(b, "%s}\n", indent)
		return
	}
	err = eb.Build().Stringer("subType", col.subType).Errorf("unhandled column subtype")
	return
}

// emitSlotWrite writes one attribute into the dml sections of one slot: one
// attribute per member section, its scalars to BeginAttribute in declaration
// order, its containers one element at a time, and its membership group
// through the channel-keyed add methods.
func emitSlotWrite(b *strings.Builder, indent string, c *sigCase, s *slotPlan) (err error) {
	for g := range s.sections {
		sec := &s.sections[g]
		if _, err = fmt.Fprintf(b, "%s{ // section %s\n", indent, sec.name); err != nil {
			return
		}
		in := indent + "\t"
		if len(sec.containerArgs) > 1 {
			first := c.columns[sec.containerArgs[0]].decScratch
			if _, err = fmt.Fprintf(b, "%snElem := len(inst.%s)\n", in, first); err != nil {
				return
			}
			for _, k := range sec.containerArgs[1:] {
				_, err = fmt.Fprintf(b, `%sif l := len(inst.%s); l != nElem {
%s	err = eb.Build().Str("section", %q).Int("length", l).Int("want", nElem).Errorf("the section's container columns differ in length: %%w", cwruntime.ErrCoContainerLength)
%s	return
%s}
`, in, c.columns[k].decScratch, in, string(sec.name), in, in)
				if err != nil {
					return
				}
			}
		}
		args := make([]string, 0, len(sec.scalarArgs))
		for _, k := range sec.scalarArgs {
			args = append(args, c.columns[k].decLocal)
		}
		if _, err = fmt.Fprintf(b, "%sat := inst.dml.%s().BeginAttribute(%s)\n", in, sec.dmlGetter, strings.Join(args, ", ")); err != nil {
			return
		}
		switch len(sec.containerArgs) {
		case 0:
		case 1:
			_, err = fmt.Fprintf(b, `%sfor _, v := range inst.%s {
%s	at.%s(v)
%s}
`, in, c.columns[sec.containerArgs[0]].decScratch, in, sec.containerAdd, in)
			if err != nil {
				return
			}
		default:
			elems := make([]string, 0, len(sec.containerArgs))
			for _, k := range sec.containerArgs {
				elems = append(elems, "inst."+c.columns[k].decScratch+"[i]")
			}
			_, err = fmt.Fprintf(b, `%sfor i := 0; i < nElem; i++ {
%s	at.%s(%s)
%s}
`, in, in, sec.containerAdd, strings.Join(elems, ", "), in)
			if err != nil {
				return
			}
		}
		if _, err = fmt.Fprintf(b, "%sfor _, m := range inst.view.Groups[%d] {\n", in, g); err != nil {
			return
		}
		if len(sec.channels) == 0 {
			_, err = fmt.Fprintf(b, `%s	err = eb.Build().Str("section", %q).Uint8("channel", uint8(m.Channel)).Errorf("the section declares no membership: %%w", cwruntime.ErrChannelNotAccepted)
%s	return
%s}
`, in, string(sec.name), in, in)
			if err != nil {
				return
			}
		} else {
			if _, err = fmt.Fprintf(b, "%s\tswitch m.Channel {\n", in); err != nil {
				return
			}
			for _, ch := range sec.channels {
				var call string
				call, err = membershipAddCall(ch)
				if err != nil {
					return
				}
				_, err = fmt.Fprintf(b, "%s\tcase mappingplan.%s:\n%s\t\tat.%s\n", in, membershipChannelConstName(ch), in, call)
				if err != nil {
					return
				}
			}
			_, err = fmt.Fprintf(b, `%s	default:
%s		err = eb.Build().Str("section", %q).Uint8("channel", uint8(m.Channel)).Errorf("the section does not accept the membership channel: %%w", cwruntime.ErrChannelNotAccepted)
%s		return
%s	}
%s}
`, in, in, string(sec.name), in, in, in)
			if err != nil {
				return
			}
		}
		if _, err = fmt.Fprintf(b, "%sat.EndAttributeP()\n%s}\n", in, indent); err != nil {
			return
		}
	}
	return
}
