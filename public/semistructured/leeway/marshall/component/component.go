// Package component is a catalogue of leeway component kinds and the archetype
// question over them (ADR-0146 D5).
//
// A component is a kind: a set of `(section, membership)` attribute slots plus
// the arity the writer guarantees for each — exactly what
// mappingplan.ReadContract states. An entity's ARCHETYPE is the set of
// components it carries, which is the ECS reading ADR-0075 adopted.
//
// # Overlap is expected, not an error
//
// An earlier cut of this package refused to register two kinds claiming the
// same slot, on the theory that such kinds are indistinguishable on read and
// the collision should be caught where they are declared. That was wrong about
// how facts are produced.
//
// A fact is not written once by one owner. Later stages fuse, enrich and
// aggregate entities — event-sourcing rules and similar — and those stages are
// not aware of every component in the system. No process holds the global
// component vocabulary, so a registry can only ever police the subset it
// happens to have been told about, and refusing an overlap at declaration time
// forbids exactly the composition the pipeline exists to perform. Detecting
// overwriting components is a non-goal.
//
// So this package does not judge. It holds contracts and answers questions:
// which kinds are known, which sections they touch, and — with
// ArchetypePresence over per-kind Detect verdicts — which of them a given row
// carries. That last question is the one that matters under fusion, because it
// is answerable by a stage that knows only its own components and tolerates
// every attribute it does not claim.
package component

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Registry holds component kinds by name. The zero value is not usable; call
// New.
//
// A Registry is not safe for concurrent modification. Registration is a
// start-up concern — the same shape as the renderer registry in ADR-0075 — so
// read-only use after the last Register needs no synchronisation.
type Registry struct {
	order []string // registration order
	kinds map[string]mappingplan.ReadContract
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{kinds: map[string]mappingplan.ReadContract{}}
}

// Register adds a kind's contract.
//
// Slot overlap with an already-registered kind is NOT an error: two components
// may claim the same (section, membership), and whether that is a modelling
// mistake or a deliberate fusion point is not something this package can know.
// Use Sections, or Detect per kind, to see what actually overlaps.
//
// Re-registering the same kind NAME is an error, because the registry is keyed
// by name and would otherwise silently drop one of them.
func (r *Registry) Register(c mappingplan.ReadContract) (err error) {
	if c.Kind == "" {
		return eb.Build().Errorf("read contract has no kind name")
	}
	if _, dup := r.kinds[c.Kind]; dup {
		return eb.Build().Str("kind", c.Kind).Errorf("kind is already registered")
	}
	r.kinds[c.Kind] = c
	r.order = append(r.order, c.Kind)
	return
}

// Kinds returns the registered kind names in registration order.
func (r *Registry) Kinds() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Contract returns a registered kind's contract.
func (r *Registry) Contract(kind string) (c mappingplan.ReadContract, ok bool) {
	c, ok = r.kinds[kind]
	return
}

// Sections returns every section the registry's kinds touch, with the kinds
// claiming each, sorted. It answers "what else reads this section?" — useful
// when adding a slot, and the honest form of the overlap question: it reports
// rather than refuses.
func (r *Registry) Sections() map[string][]string {
	out := map[string][]string{}
	for _, kind := range r.order {
		for _, s := range r.kinds[kind].Slots {
			list := out[s.Section]
			if len(list) > 0 && list[len(list)-1] == kind {
				continue // one kind may hold several slots in a section
			}
			out[s.Section] = append(list, kind)
		}
	}
	for _, kinds := range out {
		sort.Strings(kinds)
	}
	return out
}

// SlotClaims returns, per `section@membership` slot, the kinds claiming it,
// sorted. A slot with more than one claimant is where two components read the
// same attribute — worth knowing, and deliberately not prevented.
func (r *Registry) SlotClaims() map[string][]string {
	out := map[string][]string{}
	for _, kind := range r.order {
		for _, s := range r.kinds[kind].Slots {
			key := s.Section
			if s.OwnsSection {
				key += "[owns]"
			} else {
				key += "@" + s.Membership
			}
			out[key] = append(out[key], kind)
		}
	}
	for _, kinds := range out {
		sort.Strings(kinds)
	}
	return out
}

// Archetype is a set of component kinds an entity is expected to carry
// together. It is a plain name list; the registry supplies the contracts.
type Archetype struct {
	Name  string
	Kinds []string
}

// ArchetypePresence folds per-kind verdicts into one for the archetype: the
// weakest verdict wins.
//
//	Absent      — some required component is not carried, so the entity does
//	              not have this archetype.
//	Approximate — every component is carried, but at least one does not
//	              conform (a slot's arity is violated).
//	Exact       — every component is carried and conforms.
//
// The one-sided guarantee is preserved: Exact for the archetype implies Exact
// for every member, so it implies each member's Approximate too. An empty
// verdict set is Absent — an archetype nothing was checked against is not
// carried, rather than vacuously exact.
func ArchetypePresence(verdicts ...mappingplan.PresenceE) mappingplan.PresenceE {
	if len(verdicts) == 0 {
		return mappingplan.PresenceAbsent
	}
	worst := mappingplan.PresenceExact
	for _, v := range verdicts {
		if v < worst {
			worst = v
		}
	}
	return worst
}

// String renders the registry as one line per kind, with the slots it claims —
// the archetype map, for diagnostics.
func (r *Registry) String() string {
	var b strings.Builder
	for _, kind := range r.order {
		b.WriteString(kind)
		b.WriteString(":")
		for _, s := range r.kinds[kind].Slots {
			b.WriteString(" ")
			b.WriteString(s.Section)
			if s.OwnsSection {
				b.WriteString("[owns]")
			} else {
				b.WriteString("@")
				b.WriteString(s.Membership)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
