package harness

import (
	"cmp"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Element is one contribution to a seam: a name a profile and a journal
// cite it by, and for a chain or a first claim the position it runs at.
type Element[F any] struct {
	Name  string
	Order int
	Fn    F
}

// Chain runs its elements in declared order, each seeing what the previous
// left in the state. It is the one shape an ADR must justify, since its
// result depends on the order (ADR-0310 §SD1).
type Chain[S any] struct {
	elements []Element[func(s *S)]
}

// NewChain sorts the elements by Order, refusing two at one position or
// two of one name.
func NewChain[S any](elements ...Element[func(s *S)]) (c Chain[S], err error) {
	if err = checkPositions(elements); err != nil {
		return
	}
	c.elements = slices.Clone(elements)
	slices.SortStableFunc(c.elements, func(a, b Element[func(s *S)]) int { return cmp.Compare(a.Order, b.Order) })
	return
}

// Apply runs the chain over s.
func (inst Chain[S]) Apply(s *S) {
	for _, e := range inst.elements {
		e.Fn(s)
	}
}

// Names are the elements' names in the order they run.
func (inst Chain[S]) Names() (names []string) { return namesOf(inst.elements) }

// FirstClaim asks its elements in declared order and takes the first
// answer; the rest are not asked.
type FirstClaim[S any, R any] struct {
	elements []Element[func(s S) (r R, claimed bool)]
}

// NewFirstClaim sorts the elements by Order, refusing two at one position
// or two of one name.
func NewFirstClaim[S any, R any](elements ...Element[func(s S) (r R, claimed bool)]) (f FirstClaim[S, R], err error) {
	if err = checkPositions(elements); err != nil {
		return
	}
	f.elements = slices.Clone(elements)
	slices.SortStableFunc(f.elements, func(a, b Element[func(s S) (r R, claimed bool)]) int { return cmp.Compare(a.Order, b.Order) })
	return
}

// Decide is the first element's answer, with its name; claimed is false
// when none answered.
func (inst FirstClaim[S, R]) Decide(s S) (name string, r R, claimed bool) {
	for _, e := range inst.elements {
		if r, claimed = e.Fn(s); claimed {
			return e.Name, r, true
		}
	}
	return
}

// Names are the elements' names in the order they are asked.
func (inst FirstClaim[S, R]) Names() (names []string) { return namesOf(inst.elements) }

// DecisionE is the lattice an authority guard answers in: Allow ≤ Ask ≤
// Deny, combined by maximum.
type DecisionE uint8

const (
	DecisionAllow DecisionE = iota
	DecisionAsk
	DecisionDeny
)

func (inst DecisionE) String() (s string) {
	switch inst {
	case DecisionAllow:
		return "allow"
	case DecisionAsk:
		return "ask"
	case DecisionDeny:
		return "deny"
	}
	return "unknown"
}

// Guards combine their elements' decisions by maximum over an ordered
// lattice D, so an element can only add refusals; an element's error is
// the lattice's deny. Order does not matter, and the elements may be asked
// in any.
type Guards[S any, D cmp.Ordered] struct {
	elements []Element[func(s S) (d D, err error)]
	deny     D
}

// NewGuards takes the elements and the value an error counts as, refusing
// two elements of one name.
func NewGuards[S any, D cmp.Ordered](deny D, elements ...Element[func(s S) (d D, err error)]) (g Guards[S, D], err error) {
	if err = checkNames(elements); err != nil {
		return
	}
	g.elements, g.deny = slices.Clone(elements), deny
	return
}

// Decide is the maximum over the elements' decisions and by the name of the
// first element that reached it; err is the first error, which counted as
// deny.
func (inst Guards[S, D]) Decide(s S) (d D, by string, err error) {
	for i, e := range inst.elements {
		v, verr := e.Fn(s)
		if verr != nil {
			v = inst.deny
			if err == nil {
				err = verr
			}
		}
		if i == 0 || v > d {
			d, by = v, e.Name
		}
	}
	return
}

// Names are the elements' names.
func (inst Guards[S, D]) Names() (names []string) { return namesOf(inst.elements) }

// Slot holds exactly one provider, or the declared default when none was
// set.
type Slot[T any] struct {
	name  string
	value T
	set   bool
}

// NewSlot is a slot holding its default.
func NewSlot[T any](defaultName string, defaultValue T) (s Slot[T]) {
	return Slot[T]{name: defaultName, value: defaultValue}
}

// Set fills the slot once; a second provider is refused, since a slot has
// no order to settle two by.
func (inst *Slot[T]) Set(name string, value T) (err error) {
	if inst.set {
		err = eb.Build().Str("slot", inst.name).Str("provider", name).Errorf("the slot is filled")
		return
	}
	inst.name, inst.value, inst.set = name, value, true
	return
}

// Get is the provider and its name.
func (inst Slot[T]) Get() (name string, value T) { return inst.name, inst.value }

// Registry is a map of contributions by id that refuses a duplicate, so
// contributions commute.
type Registry[T any] struct {
	byId map[string]T
	ids  []string
}

// Add registers one contribution.
func (inst *Registry[T]) Add(id string, value T) (err error) {
	if _, dup := inst.byId[id]; dup {
		err = eb.Build().Str("id", id).Errorf("the registry holds the id")
		return
	}
	if inst.byId == nil {
		inst.byId = make(map[string]T)
	}
	inst.byId[id] = value
	inst.ids = append(inst.ids, id)
	return
}

// Lookup is the contribution by id.
func (inst *Registry[T]) Lookup(id string) (value T, ok bool) {
	value, ok = inst.byId[id]
	return
}

// Ids are the registered ids, sorted: a registry has no order of its own.
func (inst *Registry[T]) Ids() (ids []string) {
	ids = slices.Clone(inst.ids)
	slices.Sort(ids)
	return
}

func namesOf[F any](elements []Element[F]) (names []string) {
	names = make([]string, 0, len(elements))
	for _, e := range elements {
		names = append(names, e.Name)
	}
	return
}

func checkNames[F any](elements []Element[F]) (err error) {
	seen := make(map[string]bool, len(elements))
	for _, e := range elements {
		if e.Name == "" {
			err = eb.Build().Errorf("an element has no name")
			return
		}
		if seen[e.Name] {
			err = eb.Build().Str("name", e.Name).Errorf("two elements share a name")
			return
		}
		seen[e.Name] = true
	}
	return
}

func checkPositions[F any](elements []Element[F]) (err error) {
	if err = checkNames(elements); err != nil {
		return
	}
	at := make(map[int]string, len(elements))
	for _, e := range elements {
		if other, taken := at[e.Order]; taken {
			err = eb.Build().Str("name", e.Name).Str("other", other).Int("order", e.Order).Errorf("two elements declare one position")
			return
		}
		at[e.Order] = e.Name
	}
	return
}
