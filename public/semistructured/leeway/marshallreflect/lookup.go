//go:build llm_generated_opus47

package marshallreflect

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LookupI resolves a non-verbatim membership name to its uint64 id.
// Used during Marshal whenever a field's lw: tag does NOT carry
// `,verbatim` — the reflect codec needs to call
// AddMembershipLowCardRefP(id) and the id must come from somewhere.
//
// Pebble's facts target satisfies this by wrapping
// keelson/vdd.KeelsonHrNkRegistry; schema-agnostic targets that use
// `,verbatim` on every membership can pass NoLookup{}.
type LookupI interface {
	LookupMembership(name string) (id uint64, err error)
}

// NoLookup rejects every lookup. Pass when every DTO field uses
// `,verbatim` — bare-ref memberships will fail loudly with a
// clear error instead of silently using zero.
type NoLookup struct{}

// LookupMembership always errors.
func (NoLookup) LookupMembership(name string) (id uint64, err error) {
	err = eb.Build().Str("membership", name).Errorf("marshallreflect: no membership lookup configured (use `,verbatim` on the DTO field or pass a LookupI implementation)")
	return
}

// MapLookup is a trivial LookupI backed by a Go map. Useful for tests
// and for anchor-style targets that maintain a small fixed registry.
type MapLookup map[string]uint64

// LookupMembership returns the mapped id or an error if absent.
func (m MapLookup) LookupMembership(name string) (id uint64, err error) {
	v, ok := m[name]
	if !ok {
		err = eb.Build().Str("membership", name).Errorf("marshallreflect: membership not in MapLookup")
		return
	}
	id = v
	return
}
