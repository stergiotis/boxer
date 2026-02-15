//go:build llm_generated_gemini3pro

package finddivisions

import (
	"fmt"
	"math"
	"strings"
)

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

// LabelStrategy defines how to format a number and score that format's inherent "niceness".
type LabelStrategy interface {
	// Format converts a float to a string representation.
	Format(val float64) string
	// Score returns the legibility score (0.0 to 1.0) for this specific value in this format.
	Score(val float64) float64
}

// 1. Decimal Strategy
// Score: 1.0 if 10^-4 < |n| < 10^6, else 0.0 (Paper Table 3)
// Note: 0 is always score 1.0
type DecimalStrategy struct{}

func (d DecimalStrategy) Format(val float64) string {
	s := fmt.Sprintf("%.4f", val)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

func (d DecimalStrategy) Score(val float64) float64 {
	if val == 0 {
		return 1.0
	}
	abs := math.Abs(val)
	if abs > 1e-4 && abs < 1e6 {
		return 1.0
	}
	return 0.0 // C# implementation returns 0.5 or 0 based on implementation, paper says check bounds.
}

// 2. Scientific Strategy
// Score: 0.25 constant.
type ScientificStrategy struct{}

func (s ScientificStrategy) Format(val float64) string {
	if val == 0 {
		return "0"
	}
	// "5x10^6" is hard to render in plain text, using standard "5e+06"
	return fmt.Sprintf("%.2e", val)
}

func (s ScientificStrategy) Score(val float64) float64 {
	if val == 0 {
		return 1.0
	}
	return 0.25
}

// 3. K-Suffix Strategy (Thousands)
// Score: 0.75 if 10^3 <= |n| < 10^6
type KSuffixStrategy struct{}

func (k KSuffixStrategy) Format(val float64) string {
	if val == 0 {
		return "0"
	}
	// 5000 -> 5K
	return fmt.Sprintf("%gK", val/1000.0)
}

func (k KSuffixStrategy) Score(val float64) float64 {
	if val == 0 {
		return 1.0
	}
	abs := math.Abs(val)
	if abs >= 1e3 && abs < 1e6 {
		return 0.75
	}
	return 0.0 // Penalty for using K notation on small numbers
}

// 4. M-Suffix Strategy (Millions)
// Score: 0.75 if 10^6 <= |n| < 10^9
type MSuffixStrategy struct{}

func (m MSuffixStrategy) Format(val float64) string {
	if val == 0 {
		return "0"
	}
	return fmt.Sprintf("%gM", val/1e6)
}

func (m MSuffixStrategy) Score(val float64) float64 {
	if val == 0 {
		return 1.0
	}
	abs := math.Abs(val)
	if abs >= 1e6 && abs < 1e9 {
		return 0.75
	}
	return 0.0
}

type ExhaustiveTypesettingScorer struct {
	textMeasurer    TextMeasurerI
	uniformDecimals bool
	fontSizePt      float64
	dpi             float64
	axisLenPx       float64
	horizontal      bool
	strategies      []LabelStrategy
}

func NewExhaustiveScorer(fontSizePt, dpi, axisLengthPx float64, uniformDecimals bool, textMeasurer TextMeasurerI) *ExhaustiveTypesettingScorer {
	return &ExhaustiveTypesettingScorer{
		textMeasurer:    textMeasurer,
		uniformDecimals: uniformDecimals,
		fontSizePt:      fontSizePt,
		dpi:             dpi,
		axisLenPx:       axisLengthPx,
		horizontal:      true,
		// Register the formatting strategies from the paper
		strategies: []LabelStrategy{
			DecimalStrategy{},
			ScientificStrategy{},
			KSuffixStrategy{},
			MSuffixStrategy{},
		},
	}
}

// calculateBestConfiguration finds the best strategy and its score for the given ticks
func (inst *ExhaustiveTypesettingScorer) calculateBestConfiguration(lmin, lmax, lstep, dmin, dmax float64) (bestLabels []string, bestScore float64) {
	ticks := GenerateTicks(lmin, lmax, lstep)
	if len(ticks) < 2 {
		// Degenerate case
		return []string{fmt.Sprintf("%g", lmin)}, -1.0
	}

	bestScore = math.Inf(-1)

	// Pre-calculate invariant scores
	// 1. Font Size (C# logic: 0.2 weight, penalty if < 7pt)
	fsScore := 1.0
	if inst.fontSizePt < 7.0 {
		// Hard constraint in paper
		fsScore = math.Inf(-1)
	} else {
		// Normalized score logic from C#
		// ((data.fontSize - fsmin + 1) / (options.fontSize - fsmin))
		// Simplified here to 1.0 assuming we are using the target font size.
		fsScore = 1.0
	}

	// 2. Orientation (C# logic: 1.0 or -0.5)
	orientScore := 1.0
	if !inst.horizontal {
		orientScore = -0.5
	}

	// Convert EM to pixels for overlap calculation
	emPx := inst.fontSizePt * inst.dpi / 72.0

	// Iterate over ALL strategies (Decimal, Scientific, K, M)
	for _, strat := range inst.strategies {

		currentLabels := make([]string, len(ticks))
		sumFormatScore := 0.0

		// Generate labels and sum up the format-specific scores
		for i, t := range ticks {
			currentLabels[i] = strat.Format(t)
			sumFormatScore += strat.Score(t)
		}

		// Average format score for this set
		avgFormatScore := sumFormatScore / float64(len(ticks))

		// If the format is invalid for these numbers (e.g. K suffix for 0.005),
		// strat.Score returns 0. If average is low, this strategy is bad.
		// However, C# doesn't prune early, it just averages it in.

		// Calculate Overlap
		overlapScore := inst.calculateOverlap(ticks, currentLabels, dmin, dmax, emPx)

		// Final Weighted Average: (format + font + orient + overlap) / 4
		totalScore := (avgFormatScore + fsScore + orientScore + overlapScore) / 4.0

		if totalScore > bestScore {
			bestScore = totalScore
			bestLabels = currentLabels
		}
	}

	return bestLabels, bestScore
}

// Score satisfies LegibilityScorer.
func (inst *ExhaustiveTypesettingScorer) Score(lmin, lmax, lstep, dmin, dmax float64) float64 {
	// We discard the labels here, we just want the score
	_, score := inst.calculateBestConfiguration(lmin, lmax, lstep, dmin, dmax)
	return score
}

func (inst *ExhaustiveTypesettingScorer) Format(lmin, lmax, lstep, dmin, dmax float64) []string {
	if inst.uniformDecimals {
		return inst.formatUniform(lmin, lmax, lstep, dmin, dmax)
	}
	return inst.formatMaybeNonuniform(lmin, lmax, lstep, dmin, dmax)
}
func (inst *ExhaustiveTypesettingScorer) formatMaybeNonuniform(lmin, lmax, lstep, dmin, dmax float64) []string {
	labels, _ := inst.calculateBestConfiguration(lmin, lmax, lstep, dmin, dmax)
	return labels
}

// formatUniform ensures all labels share the same decimal precision.
func (inst *ExhaustiveTypesettingScorer) formatUniform(lmin, lmax, lstep, dmin, dmax float64) []string {
	// 1. Get the raw "best" strategy result
	rawLabels := inst.formatMaybeNonuniform(lmin, lmax, lstep, dmin, dmax)

	// 2. Check if the winner was Decimal Strategy
	// (If it's Scientific or K-suffix, we usually leave it alone or handle differently)
	// We can infer this by checking if they look like standard floats.

	maxDecimals := 0
	isDecimal := true

	for _, lbl := range rawLabels {
		if strings.ContainsAny(lbl, "eEkKM") {
			isDecimal = false
			break
		}

		// Count decimals
		if idx := strings.Index(lbl, "."); idx != -1 {
			decimals := len(lbl) - idx - 1
			if decimals > maxDecimals {
				maxDecimals = decimals
			}
		}
	}

	if !isDecimal {
		return rawLabels
	}

	// 3. Re-format with uniform precision
	ticks := GenerateTicksRobust(lmin, lmax, lstep) // Use robust generator
	uniformLabels := make([]string, len(ticks))

	formatStr := fmt.Sprintf("%%.%df", maxDecimals) // e.g., "%.2f"

	for i, t := range ticks {
		uniformLabels[i] = fmt.Sprintf(formatStr, t)
	}

	return uniformLabels
}

// Helper to calculate overlap
func (inst *ExhaustiveTypesettingScorer) calculateOverlap(ticks []float64, labels []string, dmin, dmax, emPx float64) float64 {
	widths := make([]float64, len(labels))
	for i, txt := range labels {
		widths[i] = inst.measureString(txt)
	}

	dataRange := dmax - dmin
	if dataRange == 0 {
		dataRange = 1.0
	}
	// Scale: how many pixels per unit of data
	scale := inst.axisLenPx / dataRange

	return calculateOverlap(ticks, widths, dmin, dmax, scale, emPx, inst.horizontal)
}

func (inst *ExhaustiveTypesettingScorer) measureString(text string) float64 {
	return inst.textMeasurer.MeasureSingleLine(text, inst.fontSizePt, inst.dpi)
}
