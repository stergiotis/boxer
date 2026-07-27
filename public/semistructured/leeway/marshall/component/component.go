// Package component is the registry of leeway component kinds and the law that
// keeps them composable (ADR-0146 D5).
//
// A component is a kind: a set of `(section, membership)` attribute slots plus
// the arity the writer guarantees for each — exactly what
// mappingplan.ReadContract states. An entity's ARCHETYPE is the set of
// components it carries, which is the ECS reading ADR-0075 adopted.
//
// Component identity is not on the wire. A reader selects an attribute by
// `(section, membership)` and nothing more, so two components that claim the
// same slot are indistinguishable on read: the reader cannot tell whose
// attribute is whose, and ADR-0146 D4 makes both decodes fail rather than
// guess. That failure is a runtime symptom of a design error, and the error is
// detectable much earlier — when the two kinds are declared. Registering them
// together is what surfaces it:
//
//	Two components may share a SECTION, but may not claim the same SLOT.
//
// The per-DTO builder already enforces this within one kind (goplan.AddField's
// uniqueness key). Across kinds nothing could see it, because each DTO is
// planned in isolation — which is why an entity assembled from several DTOs
// (ADR-0070 D1) could silently collide. The registry is that missing view.
package component

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Registry holds component kinds and enforces slot disjointness across them.
// The zero value is not usable; call New.
//
// A Registry is not safe for concurrent modification. Registration is a
// start-up concern — the same shape as the renderer registry in ADR-0075 —
// so read-only use after the last Register needs no synchronisation.
type Registry struct {
	order []string                          // registration order
	kinds map[string]mappingplan.ReadContract
	// owners maps a slot key to the kind that claimed it, so a collision names
	// both sides.
	owners map[string]string
	// sectionOwner maps a section to the kind that owns it EXCLUSIVELY (a
	// tuple-family section, ADR-0103). Any other claim on that section
	// collides, whatever its membership.
	sectionOwner map[string]string
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		kinds:        map[string]mappingplan.ReadContract{},
		owners:       map[string]string{},
		sectionOwner: map[string]string{},
	}
}

// slotKey identifies one attribute slot. The separator is NUL so a section or
// membership containing the separator cannot alias a different pair — verbatim
// memberships are arbitrary wire labels.
func slotKey(section, membership string) string {
	return section + "\x00" + membership
}

// Register adds a kind's contract, rejecting it if any of its slots is already
// claimed by another kind. The registry is left unchanged on error, so a
// caller that ignores it does not end up with a half-registered kind.
//
// Re-registering the same kind name is an error rather than a silent replace:
// two DTOs sharing a kind name is the same authoring mistake as two claiming a
// slot, and it would otherwise hide one of them.
func (r *Registry) Register(c mappingplan.ReadContract) (err error) {
	if c.Kind == "" {
		return eb.Build().Errorf("read contract has no kind name")
	}
	if _, dup := r.kinds[c.Kind]; dup {
		return eb.Build().Str("kind", c.Kind).Errorf("kind is already registered")
	}
	if err = r.checkDisjoint(c); err != nil {
		return
	}
	for _, s := range c.Slots {
		if s.OwnsSection {
			r.sectionOwner[s.Section] = c.Kind
			continue
		}
		r.owners[slotKey(s.Section, s.Membership)] = c.Kind
	}
	r.kinds[c.Kind] = c
	r.order = append(r.order, c.Kind)
	return
}

// checkDisjoint reports the first slot of c that another kind already claims.
func (r *Registry) checkDisjoint(c mappingplan.ReadContract) error {
	for _, s := range c.Slots {
		// A tuple-family section belongs to its kind entirely: its attribute
		// count and memberships are per-element data, so no other kind can
		// address anything in it.
		if owner, taken := r.sectionOwner[s.Section]; taken {
			return eb.Build().Str("kind", c.Kind).Str("section", s.Section).Str("owner", owner).
				Errorf("kind %q claims section %s, which kind %q owns exclusively (a tuple-family section carries its memberships per element, so nothing else can address it)", c.Kind, s.Section, owner)
		}
		if s.OwnsSection {
			// This kind wants the whole section, so ANY existing claim on it
			// collides — not just an identical slot.
			for _, prev := range r.order {
				for _, ps := range r.kinds[prev].Slots {
					if ps.Section == s.Section {
						return eb.Build().Str("kind", c.Kind).Str("section", s.Section).Str("other", prev).
							Errorf("kind %q owns section %s exclusively but kind %q already claims a slot in it", c.Kind, s.Section, prev)
					}
				}
			}
			continue
		}
		if owner, taken := r.owners[slotKey(s.Section, s.Membership)]; taken {
			return eb.Build().Str("kind", c.Kind).Str("section", s.Section).Str("membership", s.Membership).Str("owner", owner).
				Errorf("kinds %q and %q both claim slot %s@%s — a reader selects by (section, membership) alone, so their attributes are indistinguishable on the wire", c.Kind, owner, s.Section, s.Membership)
		}
	}
	return nil
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

// Sections returns every section the registry's kinds touch, sorted, with the
// kinds claiming each. It answers "what else reads this section?" — the
// question to ask before adding a slot to it.
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
