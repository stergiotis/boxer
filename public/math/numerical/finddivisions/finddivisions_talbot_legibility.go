//go:build llm_generated_gemini3pro

package finddivisions

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language" // Required for Script
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// SimpleLegibilityScorer implements the "Original" algorithm behavior.
// It ignores font size/overlap (always returns score 1.0) and uses standard formatting.
type SimpleLegibilityScorer struct{}

func (s SimpleLegibilityScorer) Score(lmin, lmax, lstep, dmin, dmax float64) float64 {
	// The original algorithm does not penalize based on legibility.
	return 1.0
}

func (s SimpleLegibilityScorer) Format(lmin, lmax, lstep, dmin, dmax float64) []string {
	ticks := GenerateTicks(lmin, lmax, lstep)
	labels := make([]string, len(ticks))

	// Basic heuristic: check magnitude to decide format
	useSci := false
	maxVal := 0.0
	for _, t := range ticks {
		absT := math.Abs(t)
		if absT > maxVal {
			maxVal = absT
		}
	}
	// Use scientific for very large or very small numbers
	if maxVal > 0 && (maxVal >= 1e7 || maxVal <= 1e-5) {
		useSci = true
	}

	for i, t := range ticks {
		if useSci {
			labels[i] = strconv.FormatFloat(t, 'e', 2, 64)
		} else {
			// standard formatting, remove trailing zeros
			s := strconv.FormatFloat(t, 'f', -1, 64)
			labels[i] = s
		}
	}
	return labels
}

/* see https://github.com/jtalbot/Labeling/blob/master/Layout/Formatters/QuantitativeFormatter.cs for the original implementation
The Labeling code is released under the BSD 2-clause license.

Copyright (c) 2012, Stanford University
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

// TypesettingScorer implements LegibilityScorer using the typesetting engine.
type TypesettingScorer struct {
	face       *font.Face
	shaper     shaping.HarfbuzzShaper
	fontSize   float64 // Points (pt)
	dpi        float64
	axisLenPx  float64
	horizontal bool
}

// NewTypesettingScorer creates a scorer with a loaded font.
func NewTypesettingScorer(fontData []byte, fontSizePt, dpi, axisLengthPx float64) (*TypesettingScorer, error) {
	// ParseTTF parses the font data
	f, err := font.ParseTTF(bytes.NewReader(fontData))
	if err != nil {
		return nil, err
	}

	return &TypesettingScorer{
		face:       f,
		shaper:     shaping.HarfbuzzShaper{},
		fontSize:   fontSizePt,
		dpi:        dpi,
		axisLenPx:  axisLengthPx,
		horizontal: true,
	}, nil
}

// Score calculates the composite legibility score.
func (inst *TypesettingScorer) Score(lmin, lmax, lstep, dmin, dmax float64) float64 {
	ticks := GenerateTicks(lmin, lmax, lstep)
	if len(ticks) < 2 {
		return 1.0
	}

	// 1. Get Labels using the internal shared logic
	labels, fmtScore := inst.generateLabelsAndScore(ticks)

	// 2. Font Size Check
	fsScore := 1.0
	if inst.fontSize < 7.0 {
		return math.Inf(-1)
	}

	// 3. Orientation Check
	orientScore := 1.0
	if !inst.horizontal {
		orientScore = -0.5
	}

	// 4. Overlap Check
	overlapScore := inst.calculateOverlap(ticks, labels, dmin, dmax)

	return (fmtScore + fsScore + orientScore + overlapScore) / 4.0
}

// Format satisfies the interface. It generates the labels exactly as Score did.
func (inst *TypesettingScorer) Format(lmin, lmax, lstep, dmin, dmax float64) []string {
	ticks := GenerateTicks(lmin, lmax, lstep)
	labels, _ := inst.generateLabelsAndScore(ticks)
	return labels
}

// generateLabelsAndScore is the internal logic shared by Score and Format
func (inst *TypesettingScorer) generateLabelsAndScore(ticks []float64) ([]string, float64) {
	labels := make([]string, len(ticks))
	useScientific := false
	maxVal := 0.0

	for _, t := range ticks {
		if math.Abs(t) > maxVal {
			maxVal = math.Abs(t)
		}
	}

	// Heuristic thresholds
	if maxVal > 1e6 || (maxVal > 0 && maxVal < 1e-4) {
		useScientific = true
	}

	for i, t := range ticks {
		if useScientific {
			labels[i] = fmt.Sprintf("%.2e", t)
		} else {
			s := fmt.Sprintf("%.4f", t)
			if strings.Contains(s, ".") {
				s = strings.TrimRight(s, "0")
				s = strings.TrimRight(s, ".")
			}
			labels[i] = s
		}
	}

	// Penalty for scientific notation (from C# paper implementation)
	score := 1.0
	if useScientific {
		score = 0.25
	}
	return labels, score
}

func calculateOverlap(ticks []float64, widths []float64, dmin, dmax, scale, emPx float64, horizontal bool) float64 {
	dataRange := dmax - dmin
	if dataRange == 0 {
		dataRange = 1.0
	}

	positions := make([]float64, len(ticks))
	for i, t := range ticks {
		positions[i] = (t - dmin) * scale
	}
	// Determine font height (approximate from EM size)
	// In many fonts, line height is ~1.2 * EM
	fontHeightPx := emPx * 1.2

	minOverlapScore := 1.0

	for i := 0; i < len(ticks)-1; i++ {
		score := 1.0
		dist := 0.0
		if horizontal {
			// Center alignment assumption
			rightEdgeI := positions[i] + widths[i]/2.0
			leftEdgeJ := positions[i+1] - widths[i+1]/2.0

			dist = leftEdgeJ - rightEdgeI
			safeDist := math.Max(0, dist)

			if safeDist < 1e-5 {
				score = math.Inf(-1)
			} else {
				// C# Formula: Min(1, 2 - (1.5 * em) / distance)
				score = math.Min(1.0, 2.0-(1.5*emPx)/safeDist)
			}
		} else {
			// Y-AXIS Logic (Vertical collision)
			// Here, "positions" represents the vertical Y coordinate (usually bottom-up).
			// We compare the Top of I vs Bottom of J (or vice versa depending on coord system).

			// Assuming positions[i] is lower on screen (higher value) than positions[i+1]??
			// Actually, let's just look at the absolute distance between tick centers.
			centerDist := math.Abs(positions[i] - positions[i+1])

			// If centers are closer than font height, they overlap.
			// We want at least 1.5 EM padding? Or just non-overlap?
			// The paper uses "1.5em" as the generic padding metric.

			// Available space between centers needs to accommodate the text height.
			// Space = centerDist
			// Required = fontHeightPx

			// Effectively, the "whitespace" is:
			verticalWhitespace := centerDist - fontHeightPx

			if verticalWhitespace < 0 {
				score = math.Inf(-1)
			} else {
				// Use paper's formula but applied to vertical gap
				score = math.Min(1.0, 2.0-(1.5*emPx)/centerDist)
			}
		}
		if score < minOverlapScore {
			minOverlapScore = score
		}
	}

	return minOverlapScore
}
func (inst *TypesettingScorer) calculateOverlap(ticks []float64, labels []string, dmin, dmax float64) float64 {
	// Calculate EM size in pixels: Points * (DPI / 72)
	emPx := inst.fontSize * inst.dpi / 72.0

	widths := make([]float64, len(labels))
	for i, txt := range labels {
		widths[i] = inst.measureString(txt)
	}

	dataRange := dmax - dmin
	if dataRange == 0 {
		dataRange = 1.0
	}
	scale := inst.axisLenPx / dataRange
	return calculateOverlap(ticks, widths, dmin, dmax, scale, emPx, inst.horizontal)
}

func (inst *TypesettingScorer) measureString(text string) float64 {
	runes := []rune(text)

	// FIX: Explicitly convert font size to Fixed 26.6 format
	// 1 unit = 1/64th of a point.
	fixedSize := fixed.Int26_6(inst.fontSize * 64)

	input := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      inst.face,
		Size:      fixedSize,
		Script:    language.Latin,
	}

	output := inst.shaper.Shape(input)

	var totalAdvance fixed.Int26_6
	for _, glyph := range output.Glyphs {
		totalAdvance += glyph.Advance
	}

	// Convert 26.6 fixed point back to float pixels
	// (Value / 64) gives points, then scale by DPI
	widthPts := float64(totalAdvance) / 64.0
	widthPx := widthPts * inst.dpi / 72.0

	return widthPx
}
