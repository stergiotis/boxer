//go:build llm_generated_gemini3pro

package ragged

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIterate2(t *testing.T) {
	// Helper struct to collect results from the iterator for easy assertion
	type Pair[A, B any] struct {
		Val1 A
		Val2 B
	}

	t.Run("Equal Lengths", func(t *testing.T) {
		s1 := []int{1, 2, 3}
		s2 := []string{"one", "two", "three"}

		var got []Pair[int, string]

		// Using the standard range loop over the iterator (Go 1.23+)
		for a, b := range Iterate2(s1, s2) {
			got = append(got, Pair[int, string]{Val1: a, Val2: b})
		}

		expected := []Pair[int, string]{
			{1, "one"},
			{2, "two"},
			{3, "three"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("S1 Shorter than S2", func(t *testing.T) {
		s1 := []int{10, 20}
		s2 := []string{"a", "b", "c", "d"}

		var got []Pair[int, string]

		for a, b := range Iterate2(s1, s2) {
			got = append(got, Pair[int, string]{a, b})
		}

		// Should stop after s1 runs out
		expected := []Pair[int, string]{
			{10, "a"},
			{20, "b"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("S2 Shorter than S1", func(t *testing.T) {
		s1 := []int{1, 2, 3, 4}
		s2 := []string{"x", "y"}

		var got []Pair[int, string]

		for a, b := range Iterate2(s1, s2) {
			got = append(got, Pair[int, string]{a, b})
		}

		// Should stop after s2 runs out
		expected := []Pair[int, string]{
			{1, "x"},
			{2, "y"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("Empty Slices", func(t *testing.T) {
		s1 := []int{}
		s2 := []string{"exist"}

		count := 0
		for range Iterate2(s1, s2) {
			count++
		}

		assert.Zero(t, count, "Iterator should yield nothing if one slice is empty")
	})

	t.Run("Supports Different Types", func(t *testing.T) {
		// Testing generic flexibility (e.g., bool and float)
		s1 := []bool{true, false}
		s2 := []float64{3.14, 1.59}

		var got []Pair[bool, float64]
		for a, b := range Iterate2(s1, s2) {
			got = append(got, Pair[bool, float64]{a, b})
		}

		expected := []Pair[bool, float64]{
			{true, 3.14},
			{false, 1.59},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("Handles Early Break (Yield returns false)", func(t *testing.T) {
		// This tests the `if !yield(...) return` logic in your function.
		s1 := []int{1, 2, 3, 4}
		s2 := []int{1, 2, 3, 4}

		itemsVisited := 0

		for a, _ := range Iterate2(s1, s2) {
			itemsVisited++
			// Simulate a "break" statement in a loop
			if a == 2 {
				break
			}
		}

		// We expected to visit 1, then 2, then break. Total 2 visits.
		// If the iterator ignored the break signal, it would have visited all 4.
		assert.Equal(t, 2, itemsVisited)
	})
}
