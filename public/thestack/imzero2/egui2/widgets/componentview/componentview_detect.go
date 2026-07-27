package componentview

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/component"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// Detection: turning a leeway row into the components it carries (ADR-0075's
// Detect, built by ADR-0146 D2).
//
// This is the piece ADR-0075 decided on and never built — the demo used to
// construct its Component values unconditionally from an already-decoded fat
// DTO, so the "archetype made visible" claim was hard-coded rather than read
// off the wire.
//
// The shape it takes follows from how facts are produced. An entity is not
// written once by one owner: later stages fuse, enrich and aggregate, and they
// are not aware of every component in the system. So a Binder does not ask
// "what is this row?" — nothing can answer that. It asks each component it
// knows "are you here?", ignores every attribute no binding claims, and reports
// what answered. A row carrying components this process has never heard of
// reads exactly the same as one that does not.

// Binding ties a component kind to the DTO that reads it. Build one with Bind.
type Binding struct {
	kind     ComponentKindE
	contract mappingplan.ReadContract
	// read returns the decoded carrier for row i, or ok=false when the row does
	// not carry the component. It closes over the DTO type, which is how
	// heterogeneous kinds live in one slice.
	read func(readers *marshallreflect.SectionReaders, i int, lookup marshallreflect.LookupI) (value any, ok bool, err error)
}

// Kind reports which component the binding reads.
func (inst Binding) Kind() ComponentKindE { return inst.kind }

// Contract is the read contract of the binding's DTO: the (section, membership)
// slots it claims and the attribute arity each admits.
func (inst Binding) Contract() mappingplan.ReadContract { return inst.contract }

// Bind binds a component kind to the leeway DTO T that reads it, projecting the
// decoded DTO to the carrier the kind's renderer expects.
//
// T is a COMPONENT DTO — it declares only the slots its component owns, not a
// row's worth of fields. That is what makes detection meaningful: a fat DTO
// spanning several components can only ever answer "is all of this here?".
func Bind[T any](kind ComponentKindE, project func(T) any) (b Binding, err error) {
	contract, err := marshallreflect.Contract[T]()
	if err != nil {
		err = eb.Build().Str("kind", string(kind)).Errorf("read contract for component DTO: %w", err)
		return
	}
	if project == nil {
		err = eb.Build().Str("kind", string(kind)).Errorf("projection func is nil")
		return
	}
	return Binding{
		kind:     kind,
		contract: contract,
		read: func(readers *marshallreflect.SectionReaders, i int, lookup marshallreflect.LookupI) (any, bool, error) {
			row, ok, rerr := marshallreflect.ReadComponent[T](readers, i, lookup)
			if rerr != nil || !ok {
				return nil, false, rerr
			}
			return project(row), true, nil
		},
	}, nil
}

// Binder detects the components of a row. It holds the bindings and, alongside
// them, a component.Registry of their contracts — the catalogue answering which
// kinds are known and which slots they claim.
type Binder struct {
	bindings []Binding
	reg      *component.Registry
	// Lookup resolves ref-channel membership names to ids for every binding.
	// Nil means every membership is verbatim.
	Lookup marshallreflect.LookupI
}

// NewBinder returns an empty Binder.
func NewBinder() *Binder {
	return &Binder{reg: component.New()}
}

// Add registers a binding. Two bindings claiming the same slot is NOT an error:
// components overlap when stages fuse facts they each know only part of, and no
// process holds the global component vocabulary to judge it. Use
// Registry().SlotClaims() to see what overlaps.
//
// A repeated KIND is an error — the report renders one panel per kind, so two
// bindings for one kind would silently drop a reader.
func (inst *Binder) Add(b Binding) (err error) {
	if b.read == nil {
		return eb.Build().Str("kind", string(b.kind)).Errorf("binding is not initialised — build it with Bind")
	}
	for _, existing := range inst.bindings {
		if existing.kind == b.kind {
			return eb.Build().Str("kind", string(b.kind)).Errorf("component kind %s is already bound", b.kind)
		}
	}
	// Two component kinds may share one DTO type — the same shape rendered two
	// ways — so a contract already in the catalogue is not a conflict. The
	// registry is keyed by the DTO's `kind:` name, which is why this checks
	// before registering rather than treating the duplicate as an error.
	if _, known := inst.reg.Contract(b.contract.Kind); !known {
		if err = inst.reg.Register(b.contract); err != nil {
			return eb.Build().Str("kind", string(b.kind)).Errorf("%w", err)
		}
	}
	inst.bindings = append(inst.bindings, b)
	return
}

// Registry exposes the contracts of the bound components.
func (inst *Binder) Registry() *component.Registry { return inst.reg }

// Bindings returns the bindings in registration order.
func (inst *Binder) Bindings() []Binding {
	out := make([]Binding, len(inst.bindings))
	copy(out, inst.bindings)
	return out
}

// Detect reports each bound component's presence on row i, in registration
// order, without decoding any value. Cheap enough to run per row over a
// heterogeneous table: it reads only the membership columns of the sections its
// bindings claim.
func (inst *Binder) Detect(readers *marshallreflect.SectionReaders, i int) (out []KindPresence, err error) {
	out = make([]KindPresence, 0, len(inst.bindings))
	for _, b := range inst.bindings {
		var p mappingplan.PresenceE
		p, err = detectFor(b, readers, i, inst.Lookup)
		if err != nil {
			err = eb.Build().Str("kind", string(b.kind)).Int("row", i).Errorf("%w", err)
			return nil, err
		}
		out = append(out, KindPresence{Kind: b.kind, Presence: p})
	}
	return
}

// KindPresence is one component's verdict for a row.
type KindPresence struct {
	Kind     ComponentKindE
	Presence mappingplan.PresenceE
}

// Components decodes row i into the components it carries, ready for
// Dispatcher.RenderReport. Absent components are omitted — the dispatcher's
// ShowAbsent draws them from the registry instead.
//
// A component the row carries but does not CONFORM to (a slot holding more
// attributes than its DTO admits, which fusion can produce) is reported as an
// error rather than silently dropped: it is present, and rendering the report
// without it would misstate the archetype.
func (inst *Binder) Components(readers *marshallreflect.SectionReaders, i int) (out []Component, err error) {
	for _, b := range inst.bindings {
		value, ok, rerr := b.read(readers, i, inst.Lookup)
		if rerr != nil {
			err = eb.Build().Str("kind", string(b.kind)).Int("row", i).Errorf("%w", rerr)
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, Component{Kind: b.kind, Value: value})
	}
	return
}

// detectFor runs the binding's presence check. It goes through the contract
// rather than the read closure so no value column is touched.
func detectFor(b Binding, readers *marshallreflect.SectionReaders, i int, lookup marshallreflect.LookupI) (mappingplan.PresenceE, error) {
	return marshallreflect.DetectContract(readers, i, lookup, b.contract)
}
