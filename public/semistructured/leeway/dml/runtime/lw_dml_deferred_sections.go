package runtime

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DeferredSectionBuffer holds one entity's section contributions until the
// frame closes, so that several components can write to one section.
//
// # Why anything is deferred at all
//
// A DML opens every section when the entity frame opens, and `EndSection`
// closes one for good: writing to it afterwards is an invalid state
// transition, reported at commit. A component driver that opens, writes and
// closes its sections is therefore correct alone and wrong in company — the
// second component to reach a section finds it closed. Three write spellings
// each solved that differently, or not at all: the reflect `RowComposer`
// buffered contributions and flushed one frame per section, the generated
// typed builders emitted immediately and degraded silently when two
// components met, and the facts encoders hand-rolled the same buffering once
// per encoder (ADR-0183 D4).
//
// This is that mechanism, once. It holds no schema, no reflection and no DML:
// a contribution is a closure the caller supplies, and closing a section is a
// callback the caller supplies. What it owns is the part all three got
// separately — the ORDER (first-seen section order, contributions within a
// section in call order), the attribution (which component, which section),
// and the frame invariants below.
//
// # The frame invariants
//
// One contribution per kind per entity ([DeferredSectionBuffer.StartKind]
// refuses a second), and typed contributions are exclusive with raw DML use
// ([DeferredSectionBuffer.MarkRaw] and StartKind refuse each other). Both were previously silent: a double Add marked the
// entity un-mirrorable and carried on, which meant the read-back shape of a
// row depended on a call the writer had probably made by accident.
// Multiplicity WITHIN a kind is the container shapes' job, not the frame's.
//
// The zero value is ready to use; [DeferredSectionBuffer.Reset] returns it to
// that state for the next entity.
type DeferredSectionBuffer struct {
	order    []string
	bySec    map[string][]contribution
	kinds    []string
	rawInUse bool
}

// contribution is one component's attributes for one section, waiting for the
// frame to close.
type contribution struct {
	kind string
	emit func() error
}

// StartKind records that a component kind contributes to this entity.
//
// It refuses a second contribution from the same kind, and any contribution on
// an entity already using [DeferredSectionBuffer.MarkRaw]. Call it once per
// component, before that component's [DeferredSectionBuffer.Enqueue] calls.
func (inst *DeferredSectionBuffer) StartKind(kind string) (err error) {
	if inst.rawInUse {
		err = eb.Build().Str("kind", kind).Errorf("the component cannot be added to an entity that is being written through Raw() — the two spellings are exclusive per entity")
		return
	}
	if slices.Contains(inst.kinds, kind) {
		err = eb.Build().Str("kind", kind).Errorf("the component is already on this entity — one contribution per kind per entity; use a container field for multiplicity within the kind")
		return
	}
	inst.kinds = append(inst.kinds, kind)
	return
}

// MarkRaw records that the entity is being written through the raw DML.
//
// It refuses when a component has already contributed: raw writes cannot be
// mirrored into the entity bag, so an entity that mixes them reads back as
// something neither spelling promised.
func (inst *DeferredSectionBuffer) MarkRaw() (err error) {
	if len(inst.kinds) > 0 {
		err = eb.Build().Str("kind", inst.kinds[0]).Errorf("Raw() cannot be used on an entity that already carries a component — the two spellings are exclusive per entity")
		return
	}
	inst.rawInUse = true
	return
}

// IsRaw reports whether this entity is being written through the raw DML.
func (inst *DeferredSectionBuffer) IsRaw() bool {
	return inst.rawInUse
}

// Enqueue records one component's attributes for one section. emit runs at
// flush, with the section still open; it must not close the section.
//
// A kind enqueues once per section it touches, and the same kind may enqueue
// to several sections — that is one contribution in
// [DeferredSectionBuffer.StartKind]'s sense.
func (inst *DeferredSectionBuffer) Enqueue(section string, kind string, emit func() error) {
	if inst.bySec == nil {
		inst.bySec = make(map[string][]contribution, 4)
	}
	if _, seen := inst.bySec[section]; !seen {
		inst.order = append(inst.order, section)
	}
	inst.bySec[section] = append(inst.bySec[section], contribution{kind: kind, emit: emit})
}

// Sections returns the sections that have contributions, in first-seen order.
func (inst *DeferredSectionBuffer) Sections() []string {
	return inst.order
}

// IsEmpty reports whether anything was enqueued.
func (inst *DeferredSectionBuffer) IsEmpty() bool {
	return len(inst.order) == 0
}

// Flush runs every contribution and closes each section exactly once.
//
// Sections run in first-seen order and contributions within a section in call
// order, which is the order the reflect composer already used and the order
// both front-ends now share. endSection is called for a section after its
// contributions, whether or not they succeeded — a section left open would
// fail the entity commit with a state-transition error naming nothing, which
// is a worse report than the one being returned.
//
// The first failing contribution's error is returned, naming the component and
// the section: an emit failure surfaces far from the Add that supplied it, so
// it has to carry where it came from.
func (inst *DeferredSectionBuffer) Flush(endSection func(section string) error) (err error) {
	for _, section := range inst.order {
		for _, c := range inst.bySec[section] {
			cerr := c.emit()
			if cerr != nil && err == nil {
				err = eb.Build().Str("kind", c.kind).Str("section", section).Errorf("component failed to write section: %w", cerr)
			}
		}
		if endSection != nil {
			cerr := endSection(section)
			if cerr != nil && err == nil {
				err = eb.Build().Str("section", section).Errorf("unable to close section: %w", cerr)
			}
		}
	}
	return
}

// Reset empties the buffer for the next entity, keeping the allocated
// capacity.
func (inst *DeferredSectionBuffer) Reset() {
	inst.order = inst.order[:0]
	inst.kinds = inst.kinds[:0]
	inst.rawInUse = false
	clear(inst.bySec)
}
