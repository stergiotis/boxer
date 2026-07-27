package component_test

// ADR-0146 D5: the registry's law is that two components may share a SECTION
// but may not claim the same SLOT. The contracts here come from real DTOs
// through marshallreflect, so the test exercises the same derivation the codec
// uses rather than a hand-built approximation of it.

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

// shadowIdentity claims identity's slot — the case the law forbids.
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

func TestRegistry_SharedSectionDistinctSlotsIsAllowed(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[identity](t)))
	require.NoError(t, r.Register(contractOf[posture](t)),
		"two components may share a section when their memberships differ")
	require.NoError(t, r.Register(contractOf[optionalOnly](t)))
	require.Equal(t, []string{"identity", "posture", "optionalOnly"}, r.Kinds())

	sections := r.Sections()
	require.ElementsMatch(t, []string{"identity", "optionalOnly", "posture"}, sections["symbol"])
}

func TestRegistry_SameSlotIsRefused(t *testing.T) {
	r := component.New()
	require.NoError(t, r.Register(contractOf[identity](t)))
	err := r.Register(contractOf[shadowIdentity](t))
	require.Error(t, err)
	require.ErrorContains(t, err, "identity")
	require.ErrorContains(t, err, "shadowIdentity")
	require.ErrorContains(t, err, "symbol")
	require.ErrorContains(t, err, "deviceStatus")

	// The rejected kind must not be half-registered.
	require.Equal(t, []string{"identity"}, r.Kinds())
	_, ok := r.Contract("shadowIdentity")
	require.False(t, ok)
	// And the slot still belongs to its original owner, so a third kind
	// colliding on it is still reported against identity.
	err = r.Register(contractOf[shadowIdentity](t))
	require.ErrorContains(t, err, "identity")
}

func TestRegistry_TupleOwnedSectionExcludesEverythingElse(t *testing.T) {
	// Owner first, then a slot claim in its section.
	r := component.New()
	require.NoError(t, r.Register(contractOf[tupleOwner](t)))
	err := r.Register(contractOf[identity](t))
	require.Error(t, err)
	require.ErrorContains(t, err, "exclusively")
	require.ErrorContains(t, err, "tupleOwner")

	// The other order must fail too — a section already claimed cannot then be
	// taken over wholesale.
	r2 := component.New()
	require.NoError(t, r2.Register(contractOf[identity](t)))
	err = r2.Register(contractOf[tupleOwner](t))
	require.Error(t, err)
	require.ErrorContains(t, err, "exclusively")
	require.ErrorContains(t, err, "identity")
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
	// A second kind may still claim `tag` in a THIRD section, but not in
	// either of these two.
	require.Error(t, r.Register(mappingplan.ReadContract{
		Kind:  "other",
		Slots: []mappingplan.Slot{{Section: "symbol", Membership: "tag", MinAttrs: 1, MaxAttrs: 1}},
	}))
	require.NoError(t, r.Register(mappingplan.ReadContract{
		Kind:  "third",
		Slots: []mappingplan.Slot{{Section: "stringArray", Membership: "tag", MinAttrs: 0, MaxAttrs: 1}},
	}))
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
