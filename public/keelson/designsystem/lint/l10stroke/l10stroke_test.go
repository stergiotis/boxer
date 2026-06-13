package l10stroke_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l10stroke"
)

func TestL10StrokeViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l10stroke.Analyzer, "violator")
}

func TestL10StrokeIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l10stroke.Analyzer, "ignored")
}

func TestL10StrokeNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l10stroke.Analyzer, "clean")
}
