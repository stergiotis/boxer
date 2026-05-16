package containers

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

type Stack[T any] struct {
	Items []T
}

func NewStack[T any]() *Stack[T] {
	return &Stack[T]{Items: make([]T, 0, 16)}
}

func NewStackSized[T any](n int) *Stack[T] {
	return &Stack[T]{Items: make([]T, 0, n)}
}

func (inst *Stack[T]) Reset() {
	inst.Items = inst.Items[:0]
}

func (inst *Stack[T]) Depth() int {
	return len(inst.Items)
}

func (inst *Stack[T]) Push(value T) {
	inst.Items = append(inst.Items, value)
}

func (inst *Stack[T]) Swap(newValue T) (oldValue T, err error) {
	l := len(inst.Items)
	if l == 0 {
		err = eh.Errorf("cannot swap last element of an empty stack")
		return
	}
	oldValue = inst.Items[l-1]
	inst.Items[l-1] = newValue
	return
}

func (inst *Stack[T]) Pop() (retr T, err error) {
	n := len(inst.Items)
	if n <= 0 {
		err = eh.Errorf("cannot pop an empty stack")
		return
	}
	retr = inst.Items[n-1]
	inst.Items = inst.Items[:n-1]
	return
}

func (inst *Stack[T]) PopDefault(emptyValue T) (retr T) {
	n := len(inst.Items)
	if n <= 0 {
		return emptyValue
	}
	retr = inst.Items[n-1]
	inst.Items = inst.Items[:n-1]
	return
}

func (inst *Stack[T]) Peek() (retr T, err error) {
	n := len(inst.Items)
	if n <= 0 {
		err = eh.Errorf("cannot peek an empty stack")
		return
	}
	retr = inst.Items[n-1]
	return
}

func (inst *Stack[T]) PeekDefault(emptyValue T) (retr T) {
	n := len(inst.Items)
	if n <= 0 {
		return emptyValue
	}
	return inst.Items[n-1]
}
