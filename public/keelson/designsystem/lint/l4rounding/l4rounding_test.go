package l4rounding_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l4rounding"
)

func TestL4RoundingViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l4rounding.Analyzer, "violator")
}

func TestL4RoundingIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l4rounding.Analyzer, "ignored")
}

func TestL4RoundingNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l4rounding.Analyzer, "clean")
}
