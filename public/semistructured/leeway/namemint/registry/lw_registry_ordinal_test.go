package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The ordinal regime (ADR-0183 D0): what a name's id is composed from is
// stated beside the name, and the registry refuses the ways two names could
// end up meaning one id.

func newVcsRegistry(t *testing.T) *HumanReadableNaturalKeyRegistry[*contract.VcsManagedContract] {
	t.Helper()
	reg, err := NewNaturalKeyRegistry[*contract.VcsManagedContract](testClaimVcs, 8, naming.LowerSnakeCase, contract.NewVcsManagedContract())
	require.NoError(t, err)
	return reg
}

func TestDeclaredOrdinalIsTheIdsBody(t *testing.T) {
	reg := newVcsRegistry(t)
	first := reg.MustBegin("first", 17).End()
	second := reg.MustBegin("second", 3).End()

	assert.EqualValues(t, 17, first.GetId().RemoveTag().Value(),
		"the ordinal is the body; declaration order has nothing to do with it")
	assert.EqualValues(t, 3, second.GetId().RemoveTag().Value())
	assert.Equal(t, testClaimVcs.Value(), first.GetId().GetTag().GetValue())
}

// Two names on one ordinal are two names on one id — the collision the
// registration-order regime could not even express, since order made ordinals
// unique by construction while making them depend on the source layout.
func TestBeginRefusesAnOrdinalAlreadyHeld(t *testing.T) {
	reg := newVcsRegistry(t)
	reg.MustBegin("holder", 5).End()

	_, err := reg.Begin("latecomer", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "holder", "the error names who holds it")
}

// The same registration re-run from its own call site is idempotent (review
// G-5), but a second ordinal for the same name is a contradiction rather than
// a repeat.
func TestBeginRefusesADisagreeingOrdinalForTheSameName(t *testing.T) {
	reg := newVcsRegistry(t)
	reg.MustBegin("settled", 2).End()

	_, err := reg.Begin("settled", 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two different ordinals")
}

// A body wider than the tag leaves room for would silently decode as a
// different tag; AddTag panics on that, so the registry refuses it first with
// an error naming the ceiling.
func TestBeginRefusesAnOrdinalWiderThanTheTag(t *testing.T) {
	reg := newVcsRegistry(t)
	max := testClaimVcs.Tag().GetMaxPossibleIdIncl()

	_, err := reg.Begin("fits", max)
	require.NoError(t, err)

	_, err = reg.Begin("overflows", max+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not fit below the registry's tag")
}

// The implicit form is what a vcs-managed vocabulary must not have: an id
// derived from how many names came before it moves when the source around it
// moves, and rows already written keep the old one.
func TestVcsManagedContractRefusesRegistrationOrderIds(t *testing.T) {
	reg := newVcsRegistry(t)

	_, err := reg.BeginNext("implicit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares each ordinal in source")

	assert.Panics(t, func() { reg.MustBeginNext("implicit") })
}

// An ephemeral registry keeps the convenience: nothing stored depends on its
// ids, which is the entire difference.
func TestEphemeralContractAllowsRegistrationOrderIds(t *testing.T) {
	reg, err := NewNaturalKeyRegistry[*contract.EphemeralContract](testClaimEphemeral, 8, naming.LowerSnakeCase, contract.NewEphemeralContract())
	require.NoError(t, err)

	a := reg.MustBeginNext("alpha").End()
	b := reg.MustBeginNext("beta").End()
	assert.EqualValues(t, 0, a.GetId().RemoveTag().Value())
	assert.EqualValues(t, 1, b.GetId().RemoveTag().Value())

	// Explicit registration still works there, and the two regimes share one
	// ordinal space rather than two.
	_, err = reg.Begin("gamma", 1)
	require.Error(t, err, "beta already holds ordinal 1")
}
