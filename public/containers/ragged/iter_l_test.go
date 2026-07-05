package ragged

import (
	"iter"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// countingSeq wraps a slice in an iter.Seq that records how many
// elements were pulled. slices.Values is stateless, so only a counting
// source can observe spurious consumption by the zip adapters.
func countingSeq[T any](items []T, pulled *int) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range items {
			*pulled++
			if !yield(v) {
				return
			}
		}
	}
}

// Pull-count contract of the lazy zips (containers review 2026-07-05):
// the sequence side is ALWAYS invoked, and when it is strictly longer
// than the slice side (including an empty slice) exactly one extra
// element is pulled and discarded; at equal lengths nothing extra is
// pulled. The extra pull is deliberate — fffi2 stream-backed sequences
// perform mandatory reads when invoked and self-drain on early
// termination, so the zips must enter the sequence rather than skip it
// (see the Zip2L doc comment). These tests pin that contract; do not
// "optimize" the pulls away without auditing the egui2 statemanagement
// FFI call sites.
func TestZipConsumption(t *testing.T) {
	t.Run("Zip2L empty slice pulls exactly one", func(t *testing.T) {
		pulled := 0
		for range Zip2L(countingSeq([]int{1, 2, 3}, &pulled), []string{}) {
			t.Fatal("must not yield")
		}
		assert.Equal(t, 1, pulled)
	})
	t.Run("Zip2L slice exhaustion pulls pair count plus one", func(t *testing.T) {
		pulled := 0
		n := 0
		for range Zip2L(countingSeq([]int{1, 2, 3, 4}, &pulled), []string{"a", "b"}) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 3, pulled)
	})
	t.Run("Zip2L equal lengths pulls exactly the pair count", func(t *testing.T) {
		pulled := 0
		n := 0
		for range Zip2L(countingSeq([]int{1, 2}, &pulled), []string{"a", "b"}) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 2, pulled)
	})
	t.Run("Zip2R empty slice pulls exactly one", func(t *testing.T) {
		pulled := 0
		for range Zip2R([]string{}, countingSeq([]int{1, 2, 3}, &pulled)) {
			t.Fatal("must not yield")
		}
		assert.Equal(t, 1, pulled)
	})
	t.Run("Zip2R slice exhaustion pulls pair count plus one", func(t *testing.T) {
		pulled := 0
		n := 0
		for range Zip2R([]string{"a", "b"}, countingSeq([]int{1, 2, 3, 4}, &pulled)) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 3, pulled)
	})
	t.Run("Zip2R equal lengths pulls exactly the pair count", func(t *testing.T) {
		pulled := 0
		n := 0
		for range Zip2R([]string{"a", "b"}, countingSeq([]int{1, 2}, &pulled)) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 2, pulled)
	})
	t.Run("Zip2LR right exhaustion pulls one extra from left", func(t *testing.T) {
		pulledL := 0
		pulledR := 0
		n := 0
		for range Zip2LR(countingSeq([]int{1, 2, 3, 4}, &pulledL), countingSeq([]string{"a", "b"}, &pulledR)) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 3, pulledL, "documented: one extra pull from s1 discovers s2's end")
		assert.Equal(t, 2, pulledR)
	})
	t.Run("Zip2LR left exhaustion pulls nothing extra from right", func(t *testing.T) {
		pulledL := 0
		pulledR := 0
		n := 0
		for range Zip2LR(countingSeq([]int{1, 2}, &pulledL), countingSeq([]string{"a", "b", "c", "d"}, &pulledR)) {
			n++
		}
		assert.Equal(t, 2, n)
		assert.Equal(t, 2, pulledL)
		assert.Equal(t, 2, pulledR)
	})
}

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
func TestIterate2LR(t *testing.T) {
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
			seq2 := Zip2LR(s1, s2)

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
