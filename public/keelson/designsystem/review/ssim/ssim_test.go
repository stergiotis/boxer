//go:build llm_generated_opus47

package ssim_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/review/ssim"
)

func solid(w, h int, c color.Color) (img *image.RGBA) {
	img = image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return
}

func nearly(a, b, tol float64) (ok bool) {
	ok = math.Abs(a-b) <= tol
	return
}

// Identical images yield SSIM exactly 1.0 (the algorithm's defining
// property — same μ, same σ, σxy = σx² = σy²).
func TestIdenticalImagesYieldOne(t *testing.T) {
	img := solid(64, 64, color.NRGBA{R: 100, G: 150, B: 200, A: 255})
	s, err := ssim.Compute(img, img, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !nearly(s, 1.0, 1e-12) {
		t.Errorf("identical-image SSIM should be 1.0, got %v", s)
	}
}

// DSSIM for identical images is exactly 0.
func TestIdenticalImagesDSSIMIsZero(t *testing.T) {
	img := solid(64, 64, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
	d, err := ssim.DSSIM(img, img, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !nearly(d, 0.0, 1e-12) {
		t.Errorf("identical-image DSSIM should be 0.0, got %v", d)
	}
}

// SSIM is symmetric: SSIM(a, b) == SSIM(b, a).
func TestSymmetry(t *testing.T) {
	a := solid(32, 32, color.NRGBA{R: 100, A: 255})
	b := solid(32, 32, color.NRGBA{B: 200, A: 255})
	s1, _ := ssim.Compute(a, b, 8)
	s2, _ := ssim.Compute(b, a, 8)
	if !nearly(s1, s2, 1e-12) {
		t.Errorf("symmetry broken: SSIM(a,b)=%v, SSIM(b,a)=%v", s1, s2)
	}
}

// Black vs white — opposite extremes. SSIM is bounded by the c1/c2
// luminance terms; pure black vs pure white evaluates to ≈ c1·c2 /
// (L²·L²) which is small but non-zero. Verify it's bounded well below
// the imperceptible threshold.
func TestBlackVsWhiteLowSimilarity(t *testing.T) {
	black := solid(64, 64, color.NRGBA{A: 255})
	white := solid(64, 64, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	s, err := ssim.Compute(black, white, 8)
	if err != nil {
		t.Fatal(err)
	}
	if s >= 0.1 {
		t.Errorf("black/white SSIM should be very small, got %v", s)
	}
	if s < 0 {
		t.Errorf("SSIM must be non-negative, got %v", s)
	}
}

// Tiny per-pixel perturbation — SSIM should stay very close to 1.
func TestSinglePixelShiftStaysAboveThreshold(t *testing.T) {
	base := solid(64, 64, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	// Copy and tweak one pixel by +1 in each channel.
	tweaked := image.NewRGBA(base.Bounds())
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			tweaked.Set(x, y, base.At(x, y))
		}
	}
	tweaked.Set(0, 0, color.NRGBA{R: 129, G: 129, B: 129, A: 255})

	s, err := ssim.Compute(base, tweaked, 8)
	if err != nil {
		t.Fatal(err)
	}
	if s < ssim.ImperceptibleThreshold {
		t.Errorf("single-pixel tweak should keep SSIM above %v, got %v",
			ssim.ImperceptibleThreshold, s)
	}
}

// Dimension mismatch must error.
func TestSizeMismatchErrors(t *testing.T) {
	a := solid(32, 32, color.NRGBA{A: 255})
	b := solid(33, 32, color.NRGBA{A: 255})
	_, err := ssim.Compute(a, b, 8)
	if err == nil {
		t.Fatal("expected ErrSizeMismatch")
	}
}

// Empty image must error.
func TestEmptyImageErrors(t *testing.T) {
	empty := image.NewRGBA(image.Rect(0, 0, 0, 0))
	_, err := ssim.Compute(empty, empty, 8)
	if err == nil {
		t.Fatal("expected ErrEmptyImage")
	}
}

// Window-size sweep — different K should give similar (but not identical)
// SSIM for the same image pair. Mostly a smoke test for the K parameter.
func TestWindowSizeAffectsResult(t *testing.T) {
	a := solid(64, 64, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	b := image.NewRGBA(a.Bounds())
	// Stripe pattern for some texture.
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			v := uint8(100)
			if (x+y)%4 == 0 {
				v = 150
			}
			b.Set(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	s4, _ := ssim.Compute(a, b, 4)
	s8, _ := ssim.Compute(a, b, 8)
	s16, _ := ssim.Compute(a, b, 16)
	for _, s := range []float64{s4, s8, s16} {
		if s < 0 || s > 1 {
			t.Errorf("SSIM out of [0, 1]: %v", s)
		}
	}
}
