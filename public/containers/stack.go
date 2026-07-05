package containers

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Stack is a slice-backed LIFO stack. The zero value is an empty,
// usable stack; NewStack / NewStackSized pre-size the backing storage.
// Vacated slots are cleared on Pop/PopDefault/Reset so pointer-valued
// elements don't keep their referents reachable past their stack
// lifetime. Not safe for concurrent use.
type Stack[T any] struct {
	items []T
}

func NewStack[T any]() *Stack[T] {
	return &Stack[T]{items: make([]T, 0, 16)}
}

func NewStackSized[T any](n int) *Stack[T] {
	return &Stack[T]{items: make([]T, 0, n)}
}

// Reset empties the stack, clearing the vacated slots and keeping the
// capacity for reuse.
func (inst *Stack[T]) Reset() {
	clear(inst.items)
	inst.items = inst.items[:0]
}

func (inst *Stack[T]) Depth() int {
	return len(inst.items)
}

// Items returns the backing slice, bottom of the stack first, as a
// read-only view for diagnostics and logging. It stays valid only until
// the next mutating call, and writing through it bypasses the stack's
// slot-clearing — use Push/Pop/Swap to mutate.
func (inst *Stack[T]) Items() []T {
	return inst.items
}

func (inst *Stack[T]) Push(value T) {
	inst.items = append(inst.items, value)
}

// Swap replaces the top element and returns the previous top. Errors on
// an empty stack.
func (inst *Stack[T]) Swap(newValue T) (oldValue T, err error) {
	l := len(inst.items)
	if l == 0 {
		err = eh.Errorf("cannot swap last element of an empty stack")
		return
	}
	oldValue = inst.items[l-1]
	inst.items[l-1] = newValue
	return
}

// Pop removes and returns the top element. Errors on an empty stack;
// PopDefault is the non-error variant.
func (inst *Stack[T]) Pop() (retr T, err error) {
	n := len(inst.items)
	if n <= 0 {
		err = eh.Errorf("cannot pop an empty stack")
		return
	}
	retr = inst.items[n-1]
	var zero T
	inst.items[n-1] = zero
	inst.items = inst.items[:n-1]
	return
}

// PopDefault removes and returns the top element, or emptyValue when
// the stack is empty.
func (inst *Stack[T]) PopDefault(emptyValue T) (retr T) {
	n := len(inst.items)
	if n <= 0 {
		return emptyValue
	}
	retr = inst.items[n-1]
	var zero T
	inst.items[n-1] = zero
	inst.items = inst.items[:n-1]
	return
}

// Peek returns the top element without removing it. Errors on an empty
// stack; PeekDefault is the non-error variant.
func (inst *Stack[T]) Peek() (retr T, err error) {
	n := len(inst.items)
	if n <= 0 {
		err = eh.Errorf("cannot peek an empty stack")
		return
	}
	retr = inst.items[n-1]
	return
}

// PeekDefault returns the top element without removing it, or
// emptyValue when the stack is empty.
func (inst *Stack[T]) PeekDefault(emptyValue T) (retr T) {
	n := len(inst.items)
	if n <= 0 {
		return emptyValue
	}
	return inst.items[n-1]
}
