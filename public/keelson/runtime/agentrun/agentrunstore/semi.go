package agentrunstore

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
)

// The shredded attributes of a vault row (ADR-0325 §SD1): a tool call's
// arguments and a reply's calls, exploded by jsonshred. The generated store
// writes and reads them as the Shredded component (ADR-0326); this file
// converts jsonshred's attributes to and from it.

// ContributeShredded adds attrs to the open vault entity as its Shredded
// component; no attribute adds none.
func ContributeShredded(b *BodyRowEntityBuilder, attrs []jsonshred.Attr) (err error) {
	if len(attrs) == 0 {
		return
	}
	var s Shredded
	for _, a := range attrs {
		ok := true
		switch a.Section {
		case jsonshred.SectionString:
			var v string
			if v, ok = a.Value.(string); ok {
				s.Strings = append(s.Strings, ShreddedString{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionSymbol:
			var v string
			if v, ok = a.Value.(string); ok {
				s.Symbols = append(s.Symbols, ShreddedSymbol{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionI64:
			var v int64
			if v, ok = a.Value.(int64); ok {
				s.I64s = append(s.I64s, ShreddedI64{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionF64:
			var v float64
			if v, ok = a.Value.(float64); ok {
				s.F64s = append(s.F64s, ShreddedF64{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionBool:
			var v bool
			if v, ok = a.Value.(bool); ok {
				s.Bools = append(s.Bools, ShreddedBool{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionNull:
			s.Nulls = append(s.Nulls, ShreddedLeaf{Path: a.Path, Params: a.Params})
		case jsonshred.SectionEmptyObject:
			s.EmptyObjects = append(s.EmptyObjects, ShreddedLeaf{Path: a.Path, Params: a.Params})
		case jsonshred.SectionEmptyArray:
			s.EmptyArrays = append(s.EmptyArrays, ShreddedLeaf{Path: a.Path, Params: a.Params})
		default:
			return eb.Build().Stringer("section", a.Section).Str("path", a.Path).Errorf("no vault section for a shredded attribute")
		}
		if !ok {
			return eb.Build().Stringer("section", a.Section).Str("path", a.Path).Errorf("a shredded attribute's value does not match its section")
		}
	}
	b.AddShredded(s)
	return
}

// AttrsOf is the jsonshred attributes of a Shredded component, section by
// section in jsonshred's section order.
func AttrsOf(s Shredded) (attrs []jsonshred.Attr) {
	for _, a := range s.Strings {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionString, Value: a.Value})
	}
	for _, a := range s.Symbols {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionSymbol, Value: a.Value})
	}
	for _, a := range s.I64s {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionI64, Value: a.Value})
	}
	for _, a := range s.F64s {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionF64, Value: a.Value})
	}
	for _, a := range s.Bools {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionBool, Value: a.Value})
	}
	for _, leaves := range []struct {
		as  []ShreddedLeaf
		sec jsonshred.SectionE
	}{{s.Nulls, jsonshred.SectionNull}, {s.EmptyObjects, jsonshred.SectionEmptyObject}, {s.EmptyArrays, jsonshred.SectionEmptyArray}} {
		for _, a := range leaves.as {
			attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: leaves.sec})
		}
	}
	return
}

// VaultRow is one vault row as read: its components, and its shredded
// attributes as jsonshred's.
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
		rec.Release()
		if derr != nil {
			return nil, derr
		}
		for _, ent := range ents {
			row := VaultRow{Ent: ent}
			if ent.Shredded.Has {
				row.Shredded = AttrsOf(ent.Shredded.Val)
			}
			rows = append(rows, row)
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
