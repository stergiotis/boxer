package terrainscope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCenterRayIndex(t *testing.T) {
	tests := []struct {
		name   string
		angles []float64
		want   int
	}{
		{"symmetric-odd", []float64{-2, -1, 0, 1, 2}, 2},
		{"single", []float64{0}, 0},
		{"three", []float64{-1, 0, 1}, 1},
		{"asymmetric-nearest-zero", []float64{-0.4, 0.1, 0.6}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, centerRayIndex(tc.angles))
		})
	}
}

func TestF32sToF64(t *testing.T) {
	assert.Equal(t, []float64{}, f32sToF64([]float32{}))
	got := f32sToF64([]float32{1.5, -2.25, 0})
	assert.Equal(t, []float64{1.5, -2.25, 0}, got)
}
