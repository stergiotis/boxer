package containers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStack_LIFO(t *testing.T) {
	st := NewStack[int]()
	require.Equal(t, 0, st.Depth())

	st.Push(1)
	st.Push(2)
	st.Push(3)
	require.Equal(t, 3, st.Depth())

	v, err := st.Pop()
	require.NoError(t, err)
	require.Equal(t, 3, v)
	v, err = st.Pop()
	require.NoError(t, err)
	require.Equal(t, 2, v)
	require.Equal(t, 1, st.Depth())
}

func TestStack_EmptyErrors(t *testing.T) {
	st := NewStack[string]()
	_, err := st.Pop()
	require.Error(t, err)
	_, err = st.Peek()
	require.Error(t, err)
	_, err = st.Swap("x")
	require.Error(t, err)
	require.Equal(t, "dflt", st.PopDefault("dflt"))
	require.Equal(t, "dflt", st.PeekDefault("dflt"))
}

func TestStack_PeekAndDefaults(t *testing.T) {
	st := NewStackSized[int](2)
	st.Push(7)

	v, err := st.Peek()
	require.NoError(t, err)
	require.Equal(t, 7, v)
	require.Equal(t, 1, st.Depth(), "Peek does not remove")

	require.Equal(t, 7, st.PeekDefault(-1))
	require.Equal(t, 7, st.PopDefault(-1))
	require.Equal(t, 0, st.Depth())
}

func TestStack_Swap(t *testing.T) {
	st := NewStack[int]()
	st.Push(1)
	st.Push(2)
	old, err := st.Swap(9)
	require.NoError(t, err)
	require.Equal(t, 2, old)
	require.Equal(t, 2, st.Depth())
	v, _ := st.Pop()
	require.Equal(t, 9, v)
}

func TestStack_ItemsView(t *testing.T) {
	st := NewStack[int]()
	st.Push(1)
	st.Push(2)
	require.Equal(t, []int{1, 2}, st.Items(), "bottom of the stack first")
	require.Empty(t, NewStack[int]().Items())
}

func TestStack_ZeroValueUsable(t *testing.T) {
	var st Stack[int]
	st.Push(1)
	v, err := st.Pop()
	require.NoError(t, err)
	require.Equal(t, 1, v)
}

func TestStack_Reset(t *testing.T) {
	st := NewStack[int]()
	st.Push(1)
	st.Push(2)
	st.Reset()
	require.Equal(t, 0, st.Depth())
	st.Push(3)
	require.Equal(t, 3, st.PeekDefault(-1))
}

// Vacated slots must not keep pointer referents reachable through the
// backing array (containers review 2026-07-05; pattern of
// TestReset_ZeroesPointerValuedSlots for the KV container).
func TestStack_PopClearsSlot(t *testing.T) {
	st := NewStack[*int]()
	a, b := 1, 2
	st.Push(&a)
	st.Push(&b)

	popped, err := st.Pop()
	require.NoError(t, err)
	require.Equal(t, &b, popped)
	tail := st.items[:cap(st.items)]
	require.Nil(t, tail[1], "Pop left the vacated slot populated")

	require.Equal(t, &a, st.PopDefault(nil))
	require.Nil(t, tail[0], "PopDefault left the vacated slot populated")
}

func TestStack_ResetClearsSlots(t *testing.T) {
	st := NewStack[*int]()
	a, b := 1, 2
	st.Push(&a)
	st.Push(&b)
	st.Reset()
	tail := st.items[:cap(st.items)]
	for i, p := range tail {
		require.Nil(t, p, "items[%d] not cleared by Reset", i)
	}
}
