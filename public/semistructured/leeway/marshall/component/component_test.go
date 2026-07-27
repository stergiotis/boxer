package component_test

// ADR-0146 D5: the registry is a CATALOGUE, not a gatekeeper. Facts are fused,
// enriched and aggregated by later stages that do not know every component, so
// no process holds the global vocabulary and slot overlap cannot be policed at
// declaration time — detecting overwriting components is a non-goal. What the
// registry answers is which kinds are known and what they claim; which of them
// a row actually carries is Detect's question.
//
// The contracts here come from real DTOs through marshallreflect, so the tests
// exercise the same derivation the codec uses rather than a hand-built
// approximation of it.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/component"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// --- component DTOs, each authored independently of the others ---

type identity struct {
	_      struct{} `kind:"identity"`
	ID     uint64   `lw:",id"`
	Status string   `lw:"deviceStatus,symbol"`
}

// posture shares the `symbol` SECTION with identity but claims a different
// membership — the case the law permits.
type posture struct {
	_     struct{} `kind:"posture"`
	ID    uint64   `lw:",id"`
	Grade string   `lw:"devicePosture,symbol"`
}

// shadowIdentity claims identity's slot. Two components reading one attribute
// is legal: it is what a fusion stage produces when it does not know both.
type shadowIdentity struct {
	_     struct{} `kind:"shadowIdentity"`
	ID    uint64   `lw:",id"`
	Other string   `lw:"deviceStatus,symbol"`
}

type tagged struct {
	_    struct{} `kind:"tagged"`
	ID   uint64   `lw:",id"`
	Tags []string `lw:"deviceTags,symbolArray"`
}

// crossSection is the DTO the pre-ADR-0146 uniqueness key wrongly rejected: one
// membership name reused across two DIFFERENT sections, which is unambiguous on
// read because each section has its own reader.
type crossSection struct {
	_    struct{} `kind:"crossSection"`
	ID   uint64   `lw:",id"`
	Sym  string   `lw:"tag,symbol"`
	Nums []uint64 `lw:"tag,u64Array"`
}

// symElem / tupleOwner is a dynamic-membership tuple: it owns `symbol`
// entirely, because its memberships are per-element data.
type symElem struct {
	Label string `lw:"@membership,verbatim"`
	Value string `lw:"symbol:value"`
}

type tupleOwner struct {
	_     struct{}  `kind:"tupleOwner"`
	ID    uint64    `lw:",id"`
	Elems []symElem `lw:"symbol"`
}

type optionalOnly struct {
	_    struct{}              `kind:"optionalOnly"`
	ID   uint64                `lw:",id"`
	Note option.Option[string] `lw:"deviceNote,symbol"`
}

func contractOf[T any](t *testing.T) mappingplan.ReadContract {
	t.Helper()
	c, err := marshallreflect.Contract[T]()
	require.NoError(t, err)
	return c
}

func TestRegistry_SharedSectionIsAllowed(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[identity](t)))
	require.NoError(t, r.Register(contractOf[posture](t)),
		"two components may share a section when their memberships differ")
	require.NoError(t, r.Register(contractOf[optionalOnly](t)))
	require.Equal(t, []string{"identity", "posture", "optionalOnly"}, r.Kinds())

	sections := r.Sections()
	require.ElementsMatch(t, []string{"identity", "optionalOnly", "posture"}, sections["symbol"])
}

func TestRegistry_SameSlotIsAllowedAndReported(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[identity](t)))
	require.NoError(t, r.Register(contractOf[shadowIdentity](t)),
		"two components claiming one slot is not the registry's business to refuse")
	require.Equal(t, []string{"identity", "shadowIdentity"}, r.Kinds())

	// It is reported, though — a slot with two claimants is worth seeing.
	claims := r.SlotClaims()
	require.ElementsMatch(t, []string{"identity", "shadowIdentity"}, claims["symbol@deviceStatus"])
}

func TestRegistry_TupleOwnedSectionDoesNotExcludeOthers(t *testing.T) {
	// A tuple owns its section on the WIRE (its memberships are per-element),
	// but the registry does not use that to refuse a co-registration: whether
	// two kinds reading one section is a mistake depends on how the facts were
	// produced, which the registry cannot see.
	r := component.New()
	require.NoError(t, r.Register(contractOf[tupleOwner](t)))
	require.NoError(t, r.Register(contractOf[identity](t)))
	require.ElementsMatch(t, []string{"identity", "tupleOwner"}, r.Sections()["symbol"])

	r2 := component.New()
	require.NoError(t, r2.Register(contractOf[identity](t)))
	require.NoError(t, r2.Register(contractOf[tupleOwner](t)), "order does not matter either")
}

// A tuple-owning kind claims EVERY attribute in its section, so its overlap
// with a flat claimant must not vanish into a separate key: the owner is
// listed on each flat slot of its section too.
func TestRegistry_OwnerIsAClaimantOfEveryFlatSlotInItsSection(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[tupleOwner](t)))
	require.NoError(t, r.Register(contractOf[identity](t)))

	claims := r.SlotClaims()
	require.Equal(t, []string{"identity", "tupleOwner"}, claims["symbol@deviceStatus"])
	require.Equal(t, []string{"tupleOwner"}, claims["symbol[owns]"],
		"a flat claimant does not claim the whole section, so there is no reverse fold")
}

func TestRegistry_TupleOwnerDoesNotBlockOtherSections(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[tupleOwner](t)))
	require.NoError(t, r.Register(contractOf[tagged](t)),
		"owning `symbol` says nothing about `symbolArray`")
}

func TestRegistry_DuplicateKindNameIsRefused(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[identity](t)))
	err := r.Register(contractOf[identity](t))
	require.ErrorContains(t, err, "already registered")
	require.Len(t, r.Kinds(), 1)
}

func TestRegistry_EmptyKindNameIsRefused(t *testing.T) {
	r := component.New()
	require.ErrorContains(t, r.Register(mappingplan.ReadContract{}), "no kind name")
}

// One membership reused across two sections is one kind's business and stays
// legal — the section-scoped uniqueness key (ADR-0146 D5) — and it registers
// as two distinct slots.
func TestRegistry_CrossSectionMembershipReuse(t *testing.T) {
	c := contractOf[crossSection](t)
	require.Len(t, c.Slots, 2)

	r := component.New()
	require.NoError(t, r.Register(c))
	require.Equal(t, []string{"crossSection"}, r.SlotClaims()["symbol@tag"])
	require.Equal(t, []string{"crossSection"}, r.SlotClaims()["u64Array@tag"])
}

func TestArchetypePresence(t *testing.T) {
	cases := []struct {
		name string
		in   []mappingplan.PresenceE
		want mappingplan.PresenceE
	}{
		{"empty is absent, not vacuously exact", nil, mappingplan.PresenceAbsent},
		{"all exact", []mappingplan.PresenceE{mappingplan.PresenceExact, mappingplan.PresenceExact}, mappingplan.PresenceExact},
		{"one approximate degrades the archetype", []mappingplan.PresenceE{mappingplan.PresenceExact, mappingplan.PresenceApproximate}, mappingplan.PresenceApproximate},
		{"one absent means the archetype is not carried", []mappingplan.PresenceE{mappingplan.PresenceExact, mappingplan.PresenceAbsent}, mappingplan.PresenceAbsent},
		{"absent dominates approximate", []mappingplan.PresenceE{mappingplan.PresenceApproximate, mappingplan.PresenceAbsent}, mappingplan.PresenceAbsent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, component.ArchetypePresence(tc.in...))
		})
	}
}

// ArchetypePresence folds by taking the weakest verdict, which relies on the
// PresenceE constants being ordered Absent < Approximate < Exact. Pin that, so
// reordering the enum cannot silently invert the fold.
func TestArchetypePresenceDependsOnEnumOrdering(t *testing.T) {
	require.Less(t, mappingplan.PresenceAbsent, mappingplan.PresenceApproximate)
	require.Less(t, mappingplan.PresenceApproximate, mappingplan.PresenceExact)
}
