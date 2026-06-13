// Package ssim implements the Structural Similarity Index Measure
// (Wang, Bovik, Sheikh, Simoncelli 2004) and its DSSIM variant.
//
// Used by the IDS Tier 2 review pipeline (ADR-0029 §SD9) as a
// deterministic pre-filter ahead of LLM grading: pairs of screenshots
// with SSIM ≥ 0.99 are functionally identical and don't merit an LLM
// call (cost + non-determinism); pairs with SSIM < 0.99 carry a
// material visual change worth semantic review.
//
// SSIM is *never a gate* in this pipeline — a low SSIM does not fail
// a build, it only triggers downstream semantic evaluation. A high
// SSIM bypasses that evaluation. The actual pass/fail decision still
// lives with the LLM rubric (or human review).
//
// Algorithm (per Wang 2004):
//   - Convert both images to a single luminance channel.
//   - For each non-overlapping K×K window (default K=8), compute:
//     μx, μy, σx², σy², σxy
//     SSIM_w = (2μxμy + c1)(2σxy + c2)
//              ────────────────────────────────
//              (μx² + μy² + c1)(σx² + σy² + c2)
//     with c1 = (k1·L)², c2 = (k2·L)², L = dynamic range,
//     k1 = 0.01, k2 = 0.03 (Wang 2004 reference constants).
//   - SSIM_image = mean over all windows.
//   - DSSIM = (1 − SSIM) / 2.
//
// The package operates on `image.Image` and is pure-Go with no deps.
package ssim

import (
	"errors"
	"image"
	"image/color"
)

// Reference constants from Wang et al. 2004.
const (
	K1            = 0.01
	K2            = 0.03
	DynamicRange  = 255.0 // 8-bit per channel
	DefaultWindow = 8     // K × K window size

	// ImperceptibleThreshold is the conventional "no visual change" floor.
	// Pairs with SSIM ≥ this value should be treated as identical.
	ImperceptibleThreshold = 0.99
)

// ErrSizeMismatch is returned when the two images do not share dimensions.
var ErrSizeMismatch = errors.New("ssim: image dimensions must match")

// ErrEmptyImage is returned when an image has zero area.
var ErrEmptyImage = errors.New("ssim: image is empty")

// Compute returns SSIM(a, b) over the entire image using `windowSize`
// non-overlapping K×K windows. windowSize=0 uses DefaultWindow.
//
// Per Wang 2004, SSIM is symmetric: Compute(a, b) == Compute(b, a).
// Two byte-identical images yield 1.0 exactly.
func Compute(a, b image.Image, windowSize int) (ssim float64, err error) {
	if windowSize <= 0 {
		windowSize = DefaultWindow
	}
	ab := a.Bounds()
	bb := b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		err = ErrSizeMismatch
		return
	}
	if ab.Dx() == 0 || ab.Dy() == 0 {
		err = ErrEmptyImage
		return
	}

	lumA := toLuminance(a)
	lumB := toLuminance(b)
	w := ab.Dx()
	h := ab.Dy()

	c1 := (K1 * DynamicRange) * (K1 * DynamicRange)
	c2 := (K2 * DynamicRange) * (K2 * DynamicRange)

	var sum float64
	var n int
	for y := 0; y+windowSize <= h; y += windowSize {
		for x := 0; x+windowSize <= w; x += windowSize {
			s := windowSSIM(lumA, lumB, w, x, y, windowSize, c1, c2)
			sum += s
			n++
		}
	}
	if n == 0 {
		// Image smaller than one window — fall back to whole-image.
		s := windowSSIM(lumA, lumB, w, 0, 0, min(w, h), c1, c2)
		sum = s
		n = 1
	}
	ssim = sum / float64(n)
	return
}

// DSSIM returns (1 − SSIM)/2. Higher means more different.
func DSSIM(a, b image.Image, windowSize int) (dssim float64, err error) {
	s, err := Compute(a, b, windowSize)
	if err != nil {
		return
	}
	dssim = (1.0 - s) / 2.0
	return
}

// toLuminance projects an image to a flat float64 luminance array
// using the Rec. 709 coefficients (0.2126R + 0.7152G + 0.0722B), the
// same coefficients used by APCA and WCAG-2.x for relative luminance.
// Operates on gamma-encoded sRGB values directly — Wang 2004 SSIM is
// defined over the displayed signal, not linear-light.
func toLuminance(img image.Image) (lum []float64) {
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	lum = make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.At(b.Min.X+x, b.Min.Y+y)
			r, g, bl := luminanceComponents(c)
			lum[y*w+x] = 0.2126*r + 0.7152*g + 0.0722*bl
		}
	}
	return
}

// luminanceComponents extracts 0-255 per-channel R/G/B from a color,
// downscaling color.Model's 16-bit alpha-premultiplied output.
func luminanceComponents(c color.Color) (r, g, b float64) {
	r16, g16, b16, _ := c.RGBA()
	r = float64(r16 >> 8)
	g = float64(g16 >> 8)
	b = float64(b16 >> 8)
	return
}

// windowSSIM computes SSIM for a single (x0, y0)-anchored K×K window.
func windowSSIM(a, b []float64, stride, x0, y0, k int, c1, c2 float64) (s float64) {
	n := float64(k * k)

	var sumA, sumB float64
	for dy := 0; dy < k; dy++ {
		row := (y0 + dy) * stride
		for dx := 0; dx < k; dx++ {
			sumA += a[row+x0+dx]
			sumB += b[row+x0+dx]
		}
	}
	muA := sumA / n
	muB := sumB / n

	var sigmaA2, sigmaB2, sigmaAB float64
	for dy := 0; dy < k; dy++ {
		row := (y0 + dy) * stride
		for dx := 0; dx < k; dx++ {
			da := a[row+x0+dx] - muA
			db := b[row+x0+dx] - muB
			sigmaA2 += da * da
			sigmaB2 += db * db
			sigmaAB += da * db
		}
	}
	sigmaA2 /= n
	sigmaB2 /= n
	sigmaAB /= n

	num := (2*muA*muB + c1) * (2*sigmaAB + c2)
	den := (muA*muA + muB*muB + c1) * (sigmaA2 + sigmaB2 + c2)
	s = num / den
	return
}

func min(a, b int) (r int) {
	if a < b {
		r = a
	} else {
		r = b
	}
	return
}
