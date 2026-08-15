package marshallreflect

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LookupI resolves a non-verbatim membership name to its uint64 id.
// Used during Marshal whenever a field's lw: tag does NOT carry
// `,verbatim` — the reflect codec needs to call
// AddMembershipLowCardRefP(id) and the id must come from somewhere.
//
// Where the ids come from is the decision, not a detail. For a shared table
// they must come from the vocabulary registry that owns the names — see
// [NewRegistryLookup]. Schema-agnostic targets that use `,verbatim` on every
// membership can pass NoLookup{}.
type LookupI interface {
	LookupMembership(name string) (id uint64, err error)
}

// NoLookup rejects every lookup. Pass when every DTO field uses
// `,verbatim` — bare-ref memberships will fail loudly with a
// clear error instead of silently using zero.
type NoLookup struct{}

// LookupMembership always errors.
func (NoLookup) LookupMembership(name string) (id uint64, err error) {
	err = eb.Build().Str("membership", name).Errorf("no membership lookup configured (use `,verbatim` on the DTO field or pass a LookupI implementation)")
	return
}

// MapLookup is a LookupI backed by a Go map.
//
// A hand-written literal is the **closed-world** spelling: legitimate when the
// same code owns both the writing and the reading and no other producer shares
// the table — anchor demos, fixtures, a self-contained schema. It is the wrong
// spelling for a shared facts table, where the ids belong to a vocabulary
// somebody else also writes against: nothing ties a literal to that registry,
// and a wrong id reads exactly like an honest absence, since an unmatched
// membership is a legal "not present" for every optional slot
// (ADR-0183 D1, the silent class).
//
// For that case build it from the registry's own snapshot with
// [NewRegistryLookup].
type MapLookup map[string]uint64

// LookupMembership returns the mapped id or an error if absent.
func (m MapLookup) LookupMembership(name string) (id uint64, err error) {
	v, ok := m[name]
	if !ok {
		err = eb.Build().Str("membership", name).Errorf("membership not in MapLookup")
		return
	}
	id = v
	return
}

// NewRegistryLookup builds a [MapLookup] from a vocabulary's membership-id
// snapshot — the map a registry produces, keyed the way a DTO's `lw:` tag
// spells the membership.
//
// This is the seam that makes reflect-path ids registry-stable rather than
// hand-copied. The snapshot itself comes from the registry
// (`runtime/factsschema/storegen.MembershipIds` materializes one, and does the
// name-style conversion at that single point); this constructor takes the
// materialized map so the conversion has one home rather than two.
//
// It copies the map, so a later edit to the caller's cannot drift the lookup,
// and it refuses two shapes that would otherwise fail much later and much
// more quietly:
//
//   - An empty snapshot. A vocabulary package populates its registry by
//     package-init side effect, so an empty map usually means the package was
//     never linked — and the lookup would then fail every membership rather
//     than say what happened.
//   - A zero id. Tag value zero is the invalid sentinel (ADR-0106 §SD8), so a
//     zero here is an unresolved name that would be written to the wire as
//     though it were a real membership.
func NewRegistryLookup(ids map[string]uint64) (r MapLookup, err error) {
	if len(ids) == 0 {
		err = eb.Build().Errorf("membership-id snapshot is empty — is the vocabulary package linked into this binary?")
		return
	}
	r = make(MapLookup, len(ids))
	for name, id := range ids {
		if id == 0 {
			err = eb.Build().Str("membership", name).Errorf("membership resolves to the zero id, which is the invalid sentinel")
			r = nil
			return
		}
		r[name] = id
	}
	return
}
