package l3spacing_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l3spacing"
)

func TestL3SpacingViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l3spacing.Analyzer, "violator")
}

func TestL3SpacingIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l3spacing.Analyzer, "ignored")
}

func TestL3SpacingNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l3spacing.Analyzer, "clean")
}
