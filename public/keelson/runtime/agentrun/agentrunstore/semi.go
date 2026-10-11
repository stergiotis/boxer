package agentrunstore

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/internal/lowlevel"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The shredded attributes of a vault row (ADR-0325 §SD1): a tool call's
// arguments and a reply's calls, exploded by jsonshred into the row's
// facts sections on the verbatim name + parameters channel, the path as
// the name and the array indices as parameters. The generated store reads
// and writes the components; these attributes go through its contribution
// seam on the way in and through the read-access classes on the way out.
//
// deferred: the generator owning them as a component of their own (plan002
// B6); until then this file is the one place that knows their layout.

// ShreddedKind is the contribution kind the shredded attributes are
// written under.
const ShreddedKind = "agentrunShredded"

// ContributeShredded adds attrs to the open vault entity through b's
// contribution seam, one contribution for all of them.
func ContributeShredded(b *BodyRowEntityBuilder, attrs []jsonshred.Attr) (err error) {
	if len(attrs) == 0 {
		return
	}
	bySection := make(map[jsonshred.SectionE][]jsonshred.Attr, 8)
	for _, a := range attrs {
		if _, perr := membership.EncodeParams(a.Params...); perr != nil {
			return eb.Build().Str("path", a.Path).Errorf("encode the parameters: %w", perr)
		}
		bySection[a.Section] = append(bySection[a.Section], a)
	}
	b.StartContribution(ShreddedKind)
	enc := func(a jsonshred.Attr) (name []byte, params []byte) {
		params, _ = membership.EncodeParams(a.Params...)
		return []byte(a.Path), params
	}
	for sec, as := range bySection {
		switch sec {
		case jsonshred.SectionString:
			b.ContributeStringArray(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionStringArray) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttributeSingle(a.Value.(string)).AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionSymbol:
			b.ContributeSymbol(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionSymbol) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttribute(a.Value.(string)).AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionI64:
			b.ContributeI64Array(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionI64Array) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttributeSingle(a.Value.(int64)).AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionF64:
			b.ContributeF64Array(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionF64Array) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttributeSingle(a.Value.(float64)).AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionBool:
			b.ContributeBool(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionBool) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttribute(a.Value.(bool)).AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionNull:
			b.ContributeNull(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionNull) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttribute().AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionEmptyObject:
			b.ContributeEmptyObject(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionEmptyObject) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttribute().AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		case jsonshred.SectionEmptyArray:
			b.ContributeEmptyArray(ShreddedKind, func(s *lowlevel.InEntityAgentrunbodyTableSectionEmptyArray) error {
				for _, a := range as {
					n, p := enc(a)
					s.BeginAttribute().AddMembershipMixedLowCardVerbatim(n, p).EndAttribute()
				}
				return nil
			})
		default:
			return eb.Build().Stringer("section", sec).Errorf("no vault section for a shredded attribute")
		}
	}
	return
}

// shreddedOf reads the shredded attributes of every entity of a vault
// record, in entity order: the attributes that carry a verbatim name.
func shreddedOf(rec arrow.RecordBatch) (perEntity [][]jsonshred.Attr, err error) {
	str := lowlevel.NewReadAccessAgentrunbodyTableTaggedStringArray()
	sym := lowlevel.NewReadAccessAgentrunbodyTableTaggedSymbol()
	i64 := lowlevel.NewReadAccessAgentrunbodyTableTaggedI64Array()
	f64 := lowlevel.NewReadAccessAgentrunbodyTableTaggedF64Array()
	bl := lowlevel.NewReadAccessAgentrunbodyTableTaggedBool()
	null := lowlevel.NewReadAccessAgentrunbodyTableTaggedNull()
	eo := lowlevel.NewReadAccessAgentrunbodyTableTaggedEmptyObject()
	ea := lowlevel.NewReadAccessAgentrunbodyTableTaggedEmptyArray()
	type loader interface {
		LoadFromRecord(rec raruntime.RecordI) error
		Release()
	}
	for _, l := range []loader{str, sym, i64, f64, bl, null, eo, ea} {
		if err = l.LoadFromRecord(rec); err != nil {
			return nil, eh.Errorf("load the vault's sections: %w", err)
		}
		defer l.Release()
	}
	n := int(rec.NumRows())
	perEntity = make([][]jsonshred.Attr, n)
	named := func(pack *lowlevel.MembershipPackAgentrunbodyTableShared1, e raruntime.EntityIdx, i raruntime.AttributeIdx) (path string, params []uint64, ok bool, err error) {
		for name, raw := range pack.GetMembValueLowCardVerbatimHighCardParams(e, i) {
			if params, err = membership.DecodeParams(raw); err != nil {
				return
			}
			return string(name), params, true, nil
		}
		return
	}
	for ei := range n {
		e := raruntime.EntityIdx(ei)
		var attrs []jsonshred.Attr
		add := func(pack *lowlevel.MembershipPackAgentrunbodyTableShared1, i raruntime.AttributeIdx, sec jsonshred.SectionE, v func() any) (aerr error) {
			path, params, ok, perr := named(pack, e, i)
			if perr != nil || !ok {
				return perr
			}
			attrs = append(attrs, jsonshred.Attr{Path: path, Params: params, Section: sec, Value: v()})
			return
		}
		for i := range raruntime.AttributeIdx(str.GetAttributes().GetNumberOfAttributes(e)) {
			if err = add(str.GetMemberships(), i, jsonshred.SectionString, func() any { return str.GetAttributes().GetAttrValueSingleOrDefault(e, i) }); err != nil {
				return
			}
		}
		for i := range raruntime.AttributeIdx(sym.GetAttributes().GetNumberOfAttributes(e)) {
			if err = add(sym.GetMemberships(), i, jsonshred.SectionSymbol, func() any { return sym.GetAttributes().GetAttrValueValue(e, i) }); err != nil {
				return
			}
		}
		for i := range raruntime.AttributeIdx(i64.GetAttributes().GetNumberOfAttributes(e)) {
			if err = add(i64.GetMemberships(), i, jsonshred.SectionI64, func() any { return i64.GetAttributes().GetAttrValueSingleOrDefault(e, i) }); err != nil {
				return
			}
		}
		for i := range raruntime.AttributeIdx(f64.GetAttributes().GetNumberOfAttributes(e)) {
			if err = add(f64.GetMemberships(), i, jsonshred.SectionF64, func() any { return f64.GetAttributes().GetAttrValueSingleOrDefault(e, i) }); err != nil {
				return
			}
		}
		for i := range raruntime.AttributeIdx(bl.GetAttributes().GetNumberOfAttributes(e)) {
			if err = add(bl.GetMemberships(), i, jsonshred.SectionBool, func() any { return bl.GetAttributes().GetAttrValueValue(e, i) }); err != nil {
				return
			}
		}
		for _, vl := range []struct {
			pack *lowlevel.MembershipPackAgentrunbodyTableShared1
			sec  jsonshred.SectionE
		}{{null.GetMemberships(), jsonshred.SectionNull}, {eo.GetMemberships(), jsonshred.SectionEmptyObject}, {ea.GetMemberships(), jsonshred.SectionEmptyArray}} {
			// A value-less section's attributes are its shredded ones, each
			// with the one verbatim membership.
			for i := range raruntime.AttributeIdx(vl.pack.GetNumberOfMemberItemsMixedLowCardVerbatim(e)) {
				if err = add(vl.pack, i, vl.sec, func() any { return nil }); err != nil {
					return
				}
			}
		}
		perEntity[ei] = attrs
	}
	return
}

// VaultRow is one vault row as read: its components and its shredded
// attributes.
type VaultRow struct {
	Ent      *BodyRowEntity
	Shredded []jsonshred.Attr
}

// ScanVault reads the vault rows predicate selects, in (run, ordinal)
// order, each with its shredded attributes. predicate is trusted SQL over
// the physical columns, as ScanOpts.ExtraPredicate is.
func (inst *BodyRowStore) ScanVault(ctx context.Context, predicate string) (rows []VaultRow, err error) {
	sql := "SELECT * FROM " + inst.readTable()
	if predicate != "" {
		sql += " WHERE " + predicate
	}
	sql += " ORDER BY " + BodyRowColKey + " ASC, " + BodyRowColOrder + " ASC" + agentrunbodyArrowOutputSettings
	for rec, qerr := range inst.exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			return nil, eh.Errorf("read the vault: %w", qerr)
		}
		ents, derr := decodeBodyRowRecord(rec)
		var shredded [][]jsonshred.Attr
		if derr == nil {
			shredded, derr = shreddedOf(rec)
		}
		rec.Release()
		if derr != nil {
			return nil, derr
		}
		for i, ent := range ents {
			rows = append(rows, VaultRow{Ent: ent, Shredded: shredded[i]})
		}
	}
	return
}

// Hasher computes the canonical wire items (ADR-0210) of run, ledger and
// vault rows a writer has not stored (ADR-0325 §SD2, §SD3): it builds the
// row in a store with no executor and drains it as its item. Safe for
// concurrent use.
type Hasher struct {
	mu    sync.Mutex
	run   *RunStore
	entry *EntryStore
	body  *BodyRowStore
}

// NewHasher is a hasher with its scratch stores.
func NewHasher() (inst *Hasher) {
	return &Hasher{run: NewRunStore(nil, nil, RunStoreConfig{}), entry: NewEntryStore(nil, nil, EntryStoreConfig{}), body: NewBodyRowStore(nil, nil, BodyRowStoreConfig{})}
}

// RunItem is the canonical item of the run row r at the zero instant: the
// run's chain seed is taken over it with only the immutable attributes set
// (ADR-0325 §SD2).
func (inst *Hasher) RunItem(r Run) (item []byte, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.run.Begin(r.ID, time.Time{}).AddRun(r).Commit(); err != nil {
		inst.run.DiscardPending()
		return nil, eh.Errorf("compose the run row: %w", err)
	}
	return drain(inst.run.DrainCanonical)
}

// LedgerItem is the canonical item of the ledger row fill composes at
// (run, ordinal), revision zero.
func (inst *Hasher) LedgerItem(run uint64, ordinal uint64, fill func(b *EntryEntityBuilder)) (item []byte, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	b := inst.entry.Begin(run, ordinal, EntryEnvelope{})
	fill(b)
	if err = b.Commit(); err != nil {
		inst.entry.DiscardPending()
		return nil, eh.Errorf("compose the ledger row: %w", err)
	}
	return drain(inst.entry.DrainCanonical)
}

// VaultItem is the canonical item of the vault row fill composes at (run,
// ordinal).
func (inst *Hasher) VaultItem(run uint64, ordinal uint64, fill func(b *BodyRowEntityBuilder) error) (item []byte, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	b := inst.body.Begin(run, ordinal)
	if err = fill(b); err != nil {
		_ = b.Rollback()
		inst.body.DiscardPending()
		return nil, err
	}
	if err = b.Commit(); err != nil {
		inst.body.DiscardPending()
		return nil, eh.Errorf("compose the vault row: %w", err)
	}
	return drain(inst.body.DrainCanonical)
}

func drain(f func(w io.Writer) (int, error)) (item []byte, err error) {
	var buf bytes.Buffer
	n, err := f(&buf)
	if err != nil {
		return
	}
	if n != 1 {
		return nil, eb.Build().Int("items", n).Errorf("a row drained as other than one item")
	}
	return buf.Bytes(), nil
}
