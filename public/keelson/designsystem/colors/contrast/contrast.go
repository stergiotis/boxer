//go:build llm_generated_opus47

// Package contrast implements WCAG 2.1 contrast ratios for IDS palette
// pair verification (ADR-0031 §SD5).
//
// AA gates the build (4.5:1 body, 3:1 large text + UI). AAA misses warn.
package contrast

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
)

// PairKind selects the WCAG threshold.
type PairKind string

const (
	KindBody  PairKind = "body"  // ≥ 4.5:1 (AA), ≥ 7:1 (AAA)
	KindLarge PairKind = "large" // ≥ 3:1 (AA), ≥ 4.5:1 (AAA); also: UI components
	KindUI    PairKind = "ui"    // ≥ 3:1 (AA); AAA undefined for UI
)

// AAFloor returns the required ratio for AA pass.
func AAFloor(kind PairKind) (r float64) {
	switch kind {
	case KindBody:
		r = 4.5
	case KindLarge, KindUI:
		r = 3.0
	default:
		r = 4.5
	}
	return
}

// AAAFloor returns the required ratio for AAA aspirational pass.
// Returns 0 for kinds where AAA does not apply.
func AAAFloor(kind PairKind) (r float64) {
	switch kind {
	case KindBody:
		r = 7.0
	case KindLarge:
		r = 4.5
	default:
		r = 0.0
	}
	return
}

// RelativeLuminance — WCAG 2.1 definition. Inputs are sRGB [0, 255] uint8s.
func RelativeLuminance(r, g, b uint8) (l float64) {
	rl := oklab.SrgbToLinear(float64(r) / 255.0)
	gl := oklab.SrgbToLinear(float64(g) / 255.0)
	bl := oklab.SrgbToLinear(float64(b) / 255.0)
	l = 0.2126*rl + 0.7152*gl + 0.0722*bl
	return
}

// Ratio returns the WCAG contrast ratio of two sRGB triples; ≥ 1.0 always.
func Ratio(fgR, fgG, fgB, bgR, bgG, bgB uint8) (r float64) {
	l1 := RelativeLuminance(fgR, fgG, fgB)
	l2 := RelativeLuminance(bgR, bgG, bgB)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	r = (l1 + 0.05) / (l2 + 0.05)
	return
}

// Result is the per-pair grading.
type Result struct {
	Name     string
	Kind     PairKind
	Ratio    float64
	AAPass   bool
	AAAPass  bool
	FgR, FgG, FgB uint8
	BgR, BgG, BgB uint8
}

// Format returns a human-readable single-line result.
func (inst Result) Format() (s string) {
	aa := "AA-fail"
	if inst.AAPass {
		aa = "AA-pass"
	}
	aaa := "AAA-miss"
	if inst.AAAPass {
		aaa = "AAA-pass"
	}
	s = fmt.Sprintf("%-32s %-5s %.2f:1  %s  %s",
		inst.Name, inst.Kind, inst.Ratio, aa, aaa)
	return
}
