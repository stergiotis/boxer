package harness

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A chain runs in declared order, whatever order the elements were given
// in, and refuses two at one position.
func TestAChainRunsInDeclaredOrder(t *testing.T) {
	type state struct{ trace []string }
	mark := func(name string) func(s *state) {
		return func(s *state) { s.trace = append(s.trace, name) }
	}
	c, err := NewChain(Element[func(s *state)]{Name: "second", Order: 20, Fn: mark("second")},
		Element[func(s *state)]{Name: "first", Order: 10, Fn: mark("first")},
		Element[func(s *state)]{Name: "third", Order: 30, Fn: mark("third")})
	require.NoError(t, err)
	var s state
	c.Apply(&s)
	assert.Equal(t, []string{"first", "second", "third"}, s.trace)
	assert.Equal(t, []string{"first", "second", "third"}, c.Names())

	_, err = NewChain(Element[func(s *state)]{Name: "a", Order: 1, Fn: mark("a")}, Element[func(s *state)]{Name: "b", Order: 1, Fn: mark("b")})
	assert.Error(t, err, "one position, two elements")
	_, err = NewChain(Element[func(s *state)]{Name: "a", Order: 1, Fn: mark("a")}, Element[func(s *state)]{Name: "a", Order: 2, Fn: mark("a")})
	assert.Error(t, err, "one name, two elements")
}

// A first claim stops at the first element that answers.
func TestAFirstClaimStopsAtTheFirstAnswer(t *testing.T) {
	asked := []string{}
	claim := func(name string, answers bool) Element[func(s int) (r string, claimed bool)] {
		return Element[func(s int) (r string, claimed bool)]{Name: name, Order: len(name), Fn: func(s int) (string, bool) {
			asked = append(asked, name)
			return name + "!", answers
		}}
	}
	f, err := NewFirstClaim(claim("ccc", true), claim("a", false), claim("bb", true))
	require.NoError(t, err)
	name, r, claimed := f.Decide(0)
	assert.True(t, claimed)
	assert.Equal(t, "bb", name)
	assert.Equal(t, "bb!", r)
	assert.Equal(t, []string{"a", "bb"}, asked, "the third is not asked")

	none, err := NewFirstClaim(claim("a", false))
	require.NoError(t, err)
	_, _, claimed = none.Decide(0)
	assert.False(t, claimed)
}

// Guards combine by maximum and an error is deny: no element lowers
// another's decision.
func TestGuardsCombineByMaximum(t *testing.T) {
	fixed := func(name string, d DecisionE) Element[func(s struct{}) (DecisionE, error)] {
		return Element[func(s struct{}) (DecisionE, error)]{Name: name, Fn: func(struct{}) (DecisionE, error) { return d, nil }}
	}
	g, err := NewGuards(DecisionDeny, fixed("allow", DecisionAllow), fixed("ask", DecisionAsk), fixed("allow-again", DecisionAllow))
	require.NoError(t, err)
	d, by, err := g.Decide(struct{}{})
	require.NoError(t, err)
	assert.Equal(t, DecisionAsk, d)
	assert.Equal(t, "ask", by)

	failing := Element[func(s struct{}) (DecisionE, error)]{Name: "broken", Fn: func(struct{}) (DecisionE, error) { return DecisionAllow, errors.New("no") }}
	g, err = NewGuards(DecisionDeny, fixed("ask", DecisionAsk), failing)
	require.NoError(t, err)
	d, by, err = g.Decide(struct{}{})
	assert.Error(t, err)
	assert.Equal(t, DecisionDeny, d)
	assert.Equal(t, "broken", by)

	// Associative, commutative, idempotent: the same set in another order
	// decides the same.
	g2, _ := NewGuards(DecisionDeny, fixed("allow-again", DecisionAllow), fixed("ask", DecisionAsk), fixed("ask-twice", DecisionAsk), fixed("allow", DecisionAllow))
	d2, _, _ := g2.Decide(struct{}{})
	assert.Equal(t, DecisionAsk, d2)
	_, err = NewGuards(DecisionDeny, fixed("a", DecisionAllow), fixed("a", DecisionAsk))
	assert.Error(t, err)
}

// A slot holds its default until set once; a second provider is refused.
func TestASlotHoldsOneProvider(t *testing.T) {
	s := NewSlot("default", 1)
	name, v := s.Get()
	assert.Equal(t, "default", name)
	assert.Equal(t, 1, v)
	require.NoError(t, s.Set("other", 2))
	name, v = s.Get()
	assert.Equal(t, "other", name)
	assert.Equal(t, 2, v)
	assert.Error(t, s.Set("third", 3))
}

// A registry refuses a duplicate and lists ids without an order of its own.
func TestARegistryRefusesADuplicate(t *testing.T) {
	var r Registry[int]
	require.NoError(t, r.Add("b", 2))
	require.NoError(t, r.Add("a", 1))
	assert.Error(t, r.Add("a", 3))
	v, ok := r.Lookup("a")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
	assert.Equal(t, []string{"a", "b"}, r.Ids())
}
