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

func (stack *Stack[T]) Reset() {
	stack.Items = stack.Items[:0]
}

func (stack *Stack[T]) Depth() int {
	return len(stack.Items)
}

func (stack *Stack[T]) Push(value T) {
	stack.Items = append(stack.Items, value)
}

func (stack *Stack[T]) Swap(newValue T) (oldValue T, err error) {
	l := len(stack.Items)
	if l == 0 {
		err = eh.Errorf("cannot swap last element of an empty stack")
		return
	}
	oldValue = stack.Items[l-1]
	stack.Items[l-1] = newValue
	return
}

func (stack *Stack[T]) Pop() (retr T, err error) {
	n := len(stack.Items)
	if n <= 0 {
		err = eh.Errorf("cannot pop an empty stack")
		return
	}
	retr = stack.Items[n-1]
	stack.Items = stack.Items[:n-1]
	return
}

func (stack *Stack[T]) PopDefault(emptyValue T) (retr T) {
	n := len(stack.Items)
	if n <= 0 {
		return emptyValue
	}
	retr = stack.Items[n-1]
	stack.Items = stack.Items[:n-1]
	return
}

func (stack *Stack[T]) Peek() (retr T, err error) {
	n := len(stack.Items)
	if n <= 0 {
		err = eh.Errorf("cannot peek an empty stack")
		return
	}
	retr = stack.Items[n-1]
	return
}

func (stack *Stack[T]) PeekDefault(emptyValue T) (retr T) {
	n := len(stack.Items)
	if n <= 0 {
		return emptyValue
	}
	return stack.Items[n-1]
}
