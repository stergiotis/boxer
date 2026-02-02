//go:build llm_generated_gemini3pro

package ragged

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIterate2L(t *testing.T) {
	// Pair is a helper struct to capture the output of iter.Seq2 for comparison
	type Pair struct {
		ValA int
		ValB string
	}

	tests := []struct {
		name       string
		inputSeq   []int    // Will be converted to iter.Seq[int]
		inputSlice []string // Passed directly
		stopAfter  int      // If > 0, simulates the caller breaking early (yield returns false)
		expected   []Pair
	}{
		{
			name:       "Equal lengths",
			inputSeq:   []int{1, 2, 3},
			inputSlice: []string{"a", "b", "c"},
			expected:   []Pair{{1, "a"}, {2, "b"}, {3, "c"}},
		},
		{
			name:       "Iterator (s1) is shorter",
			inputSeq:   []int{1, 2},
			inputSlice: []string{"a", "b", "c", "d"},
			expected:   []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:       "Slice (s2) is shorter",
			inputSeq:   []int{1, 2, 3, 4},
			inputSlice: []string{"a", "b"},
			expected:   []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:       "Empty iterator",
			inputSeq:   []int{},
			inputSlice: []string{"a", "b"},
			expected:   []Pair(nil),
		},
		{
			name:       "Empty slice",
			inputSeq:   []int{1, 2},
			inputSlice: []string{},
			expected:   []Pair(nil),
		},
		{
			name:       "Both empty",
			inputSeq:   []int{},
			inputSlice: []string{},
			expected:   []Pair(nil),
		},
		{
			name:       "Caller breaks early (Stop after 1)",
			inputSeq:   []int{1, 2, 3},
			inputSlice: []string{"a", "b", "c"},
			stopAfter:  1,
			expected:   []Pair{{1, "a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert slice to iterator for the first argument
			s1 := slices.Values(tt.inputSeq)

			// Get the resulting iterator
			seq2 := Zip2L(s1, tt.inputSlice)

			// Collect results
			var result []Pair
			count := 0

			// Iterate over the result
			seq2(func(a int, b string) bool {
				result = append(result, Pair{ValA: a, ValB: b})
				count++

				// Handle early break simulation
				if tt.stopAfter > 0 && count >= tt.stopAfter {
					return false
				}
				return true
			})

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIterate2R(t *testing.T) {
	type Pair struct {
		ValA int
		ValB string
	}

	tests := []struct {
		name       string
		inputSlice []int    // Passed directly as first arg
		inputSeq   []string // Will be converted to iter.Seq[string] as second arg
		stopAfter  int      // If > 0, simulates the caller breaking early
		expected   []Pair
	}{
		{
			name:       "Equal lengths",
			inputSlice: []int{1, 2, 3},
			inputSeq:   []string{"a", "b", "c"},
			expected:   []Pair{{1, "a"}, {2, "b"}, {3, "c"}},
		},
		{
			name:       "Slice (s1) is shorter",
			inputSlice: []int{1, 2},
			inputSeq:   []string{"a", "b", "c", "d"},
			expected:   []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:       "Iterator (s2) is shorter",
			inputSlice: []int{1, 2, 3, 4},
			inputSeq:   []string{"a", "b"},
			expected:   []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:       "Empty slice",
			inputSlice: []int{},
			inputSeq:   []string{"a", "b"},
			expected:   []Pair(nil),
		},
		{
			name:       "Empty iterator",
			inputSlice: []int{1, 2},
			inputSeq:   []string{},
			expected:   []Pair(nil),
		},
		{
			name:       "Both empty",
			inputSlice: []int{},
			inputSeq:   []string{},
			expected:   []Pair(nil),
		},
		{
			name:       "Caller breaks early (Stop after 1)",
			inputSlice: []int{1, 2, 3},
			inputSeq:   []string{"a", "b", "c"},
			stopAfter:  1,
			expected:   []Pair{{1, "a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert slice to iterator for the second argument
			s2 := slices.Values(tt.inputSeq)

			// Get the resulting iterator: Slice is 1st arg, Iterator is 2nd arg
			seq2 := Zip2R(tt.inputSlice, s2)

			// Collect results
			var result []Pair
			count := 0

			seq2(func(a int, b string) bool {
				result = append(result, Pair{ValA: a, ValB: b})
				count++

				if tt.stopAfter > 0 && count >= tt.stopAfter {
					return false
				}
				return true
			})

			assert.Equal(t, tt.expected, result)
		})
	}
}
func TestIterate2RL(t *testing.T) {
	type Pair struct {
		ValA int
		ValB string
	}

	tests := []struct {
		name      string
		seq1      []int    // Will be converted to iter.Seq[int]
		seq2      []string // Will be converted to iter.Seq[string]
		stopAfter int      // If > 0, simulates the caller breaking early
		expected  []Pair
	}{
		{
			name:     "Equal lengths",
			seq1:     []int{1, 2, 3},
			seq2:     []string{"a", "b", "c"},
			expected: []Pair{{1, "a"}, {2, "b"}, {3, "c"}},
		},
		{
			name:     "Seq1 is shorter",
			seq1:     []int{1, 2},
			seq2:     []string{"a", "b", "c", "d"},
			expected: []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:     "Seq2 is shorter",
			seq1:     []int{1, 2, 3, 4},
			seq2:     []string{"a", "b"},
			expected: []Pair{{1, "a"}, {2, "b"}},
		},
		{
			name:     "Seq1 empty",
			seq1:     []int{},
			seq2:     []string{"a", "b"},
			expected: []Pair(nil),
		},
		{
			name:     "Seq2 empty",
			seq1:     []int{1, 2},
			seq2:     []string{},
			expected: []Pair(nil),
		},
		{
			name:     "Both empty",
			seq1:     []int{},
			seq2:     []string{},
			expected: []Pair(nil),
		},
		{
			name:      "Caller breaks early (Stop after 1)",
			seq1:      []int{1, 2, 3},
			seq2:      []string{"a", "b", "c"},
			stopAfter: 1,
			expected:  []Pair{{1, "a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert slices to iterators
			s1 := slices.Values(tt.seq1)
			s2 := slices.Values(tt.seq2)

			// Get the combined iterator
			seq2 := Zip2RL(s1, s2)

			var result []Pair
			count := 0

			seq2(func(a int, b string) bool {
				result = append(result, Pair{ValA: a, ValB: b})
				count++

				if tt.stopAfter > 0 && count >= tt.stopAfter {
					return false
				}
				return true
			})

			assert.Equal(t, tt.expected, result)
		})
	}
}
