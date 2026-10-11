package evidence

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Head is a chain's head: the last ordinal and the digest at it. The zero
// ordinal with the seed as digest is the head of an empty chain.
type Head struct {
	Ordinal uint64
	Digest  string
}

// OriginByE says how a row's origin was established (ADR-0327 (proposed)
// §SD6): a reader weighs the origin by it.
type OriginByE uint8

const (
	// OriginBySelf is an origin the writing process stated itself.
	OriginBySelf OriginByE = 0
	// OriginByEnvelope is an origin the host took from a bus envelope's
	// sender.
	OriginByEnvelope OriginByE = 1
	// OriginByHost is a row the host's own service wrote.
	OriginByHost OriginByE = 2
)

var originByNames = []string{"self", "envelope", "host"}

func (inst OriginByE) String() (s string) {
	if int(inst) < len(originByNames) {
		return originByNames[inst]
	}
	return "unknown"
}

// ParseOriginBy is the value a name spells; false for none.
func ParseOriginBy(s string) (o OriginByE, ok bool) {
	for i, n := range originByNames {
		if n == s {
			return OriginByE(i), true
		}
	}
	return
}

// The values of a written-by component's PrincipalBy: how much a reader may
// trust the principal, as the writer states it.
const (
	PrincipalBySystem = "system"
	PrincipalByEnv    = "env"
	PrincipalByOS     = "os"
	PrincipalByNone   = "none"
)

// Attestation is what a writer says about the identity on its context: how
// the origin was established and how far the principal is to be trusted.
// A context without one is a process stating its own origin, with a
// principal trusted as far as it is present.
type Attestation struct {
	OriginBy    OriginByE
	PrincipalBy string
}

type attestationKey struct{}

// WithAttestation returns ctx carrying a, replacing any already on it.
func WithAttestation(ctx context.Context, a Attestation) context.Context {
	return context.WithValue(ctx, attestationKey{}, a)
}

// AttestationFrom is the attestation on ctx; ok is false for the default.
func AttestationFrom(ctx context.Context) (a Attestation, ok bool) {
	a, ok = ctx.Value(attestationKey{}).(Attestation)
	return
}

// Identity is what a written-by component records (ADR-0327 (proposed)
// §SD6, law L35): the call identity's origin and claims, the batch, and
// the attestation, as read off one context.
type Identity struct {
	Run         string
	App         string
	Instance    uint64
	OriginBy    OriginByE
	Principal   string
	PrincipalBy string
	Purpose     string
	Correlation string
	Batch       string
}

// IdentityFrom reads the identity a row committed under ctx carries. With
// no principal the principal is trusted as none, whatever the attestation
// says; with one and no attestation, as the process's own statement
// (system).
func IdentityFrom(ctx context.Context) (id Identity) {
	ci, _ := callident.CallIdentityFrom(ctx)
	batch, _ := recordstore.BatchIdFrom(ctx)
	a, attested := AttestationFrom(ctx)
	id = Identity{Run: ci.Origin.Run, App: ci.Origin.App, Instance: ci.Origin.Instance, OriginBy: a.OriginBy,
		Principal: ci.Claims.Principal, Purpose: ci.Claims.Purpose, Correlation: ci.Claims.Correlation, Batch: string(batch)}
	switch {
	case id.Principal == "":
		id.PrincipalBy = PrincipalByNone
	case attested && a.PrincipalBy != "":
		id.PrincipalBy = a.PrincipalBy
	default:
		id.PrincipalBy = PrincipalBySystem
	}
	return
}
