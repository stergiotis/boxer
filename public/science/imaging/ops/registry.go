package ops

import (
	"iter"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var registry = struct {
	mu  sync.RWMutex
	ops map[string]OperatorI
}{ops: make(map[string]OperatorI)}

// Register adds an operator, refusing a declaration a dialog could not be
// built from and an id already taken.
func Register(op OperatorI) error {
	d := op.Declaration()
	if err := checkDeclaration(d); err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, taken := registry.ops[d.Id]; taken {
		return eb.Build().Str("operator", d.Id).Errorf("an operator of that id is registered already")
	}
	registry.ops[d.Id] = op
	return nil
}

// Lookup returns the operator of an id.
func Lookup(id string) (op OperatorI, ok bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	op, ok = registry.ops[id]
	return
}

// All iterates the registered operators in the order of their ids.
func All() iter.Seq[OperatorI] {
	return func(yield func(OperatorI) bool) {
		for _, id := range operatorIds() {
			op, ok := Lookup(id)
			if ok && !yield(op) {
				return
			}
		}
	}
}

func operatorIds() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	ids := make([]string, 0, len(registry.ops))
	for id := range registry.ops {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func mustRegister(op OperatorI) {
	if err := Register(op); err != nil {
		panic(err)
	}
}

func init() {
	mustRegister(exprOperator{})
	mustRegister(axisStatOperator{})
	mustRegister(gaussianOperator{})
	mustRegister(medianFilterOperator{})
	mustRegister(standardizeOperator{})
}
