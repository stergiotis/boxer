//go:build llm_generated_gemini3pro

package slices

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type MyFloat float64
type MyString string
type MyComplex complex128

type Animal interface {
	Speak() string
}

type Dog struct{ Name string }

func (d Dog) Speak() string { return "Woof" }

type Cat struct{ Name string }

func (c Cat) Speak() string { return "Meow" }

func TestCopySliceFloat(t *testing.T) {
	t.Run("Float32 to Float64", func(t *testing.T) {
		src := []float32{1.1, 2.2, 3.3}
		dest := []float64{}

		got := CopySliceFloat(src, dest)

		// Note: Floating point comparison can be tricky due to precision,
		// but assert.Equal handles slice values reasonably well.
		// For strict precision, you might check element-wise with assert.InDelta.
		expected := []float64{1.100000023841858, 2.200000047683716, 3.299999952316284} // float32 precision artifacts
		assert.Equal(t, expected, got)
	})

	t.Run("Float64 to MyFloat (Custom Type)", func(t *testing.T) {
		src := []float64{10.0, 20.0}
		dest := []MyFloat{}

		got := CopySliceFloat(src, dest)

		assert.Equal(t, []MyFloat{10.0, 20.0}, got)
	})

	t.Run("Append to existing slice", func(t *testing.T) {
		src := []float64{3.0, 4.0}
		dest := []float64{1.0, 2.0}

		got := CopySliceFloat(src, dest)

		assert.Equal(t, []float64{1.0, 2.0, 3.0, 4.0}, got)
	})
}

func TestCopySliceComplex(t *testing.T) {
	t.Run("Complex64 to Complex128", func(t *testing.T) {
		src := []complex64{1 + 2i, 3 + 4i}
		dest := []complex128{}

		got := CopySliceComplex(src, dest)

		assert.Equal(t, []complex128{1 + 2i, 3 + 4i}, got)
	})

	t.Run("Complex128 to MyComplex", func(t *testing.T) {
		src := []complex128{5 + 5i}
		dest := []MyComplex{}

		got := CopySliceComplex(src, dest)

		assert.Equal(t, []MyComplex{5 + 5i}, got)
	})
}

func TestCopySliceString(t *testing.T) {
	t.Run("String to String", func(t *testing.T) {
		src := []string{"hello", "world"}
		dest := []string{}

		got := CopySliceString(src, dest)

		assert.Equal(t, src, got)
	})

	t.Run("String to MyString", func(t *testing.T) {
		src := []string{"foo", "bar"}
		dest := []MyString{}

		got := CopySliceString(src, dest)

		assert.Equal(t, []MyString{"foo", "bar"}, got)
	})

	t.Run("Empty Source", func(t *testing.T) {
		src := []string{}
		dest := []string{"original"}

		got := CopySliceString(src, dest)

		assert.Equal(t, []string{"original"}, got)
	})
}

func TestCopySliceInterfaceCastable(t *testing.T) {
	// Scenario 1: Filter []any -> []string
	// The function should skip integers and bools, only keeping strings.
	t.Run("Filter Any to String", func(t *testing.T) {
		src := []any{"keep", 1, 2.5, "keep2", true}
		dest := []string{}

		got := CopySliceInterfaceCastable[any, string](src, dest)

		assert.Equal(t, []string{"keep", "keep2"}, got)
	})

	// Scenario 2: Concrete Structs -> Interface
	// Dog implements Animal, so all elements should be copied.
	t.Run("Struct to Interface", func(t *testing.T) {
		src := []Dog{{Name: "Rex"}, {Name: "Spot"}}
		dest := []Animal{}

		got := CopySliceInterfaceCastable[Dog, Animal](src, dest)

		assert.Len(t, got, 2)
		assert.Equal(t, "Woof", got[0].Speak())
		assert.IsType(t, Dog{}, got[0])
	})

	// Scenario 3: Mixed []any -> []Interface
	// Should keep Dogs and Cats, but reject strings (which don't implement Animal).
	t.Run("Any to Interface Filtering", func(t *testing.T) {
		src := []any{
			Dog{Name: "Rex"},
			"Not an animal",
			Cat{Name: "Luna"},
		}
		dest := []Animal{}

		got := CopySliceInterfaceCastable[any, Animal](src, dest)

		assert.Len(t, got, 2)
		assert.IsType(t, Dog{}, got[0])
		assert.IsType(t, Cat{}, got[1])
		assert.Equal(t, "Woof", got[0].Speak())
		assert.Equal(t, "Meow", got[1].Speak())
	})
}
