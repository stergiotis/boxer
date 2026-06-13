package l2color_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l2color"
)

func TestL2ColorViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l2color.Analyzer, "violator")
}

func TestL2ColorIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l2color.Analyzer, "ignored")
}

func TestL2ColorNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l2color.Analyzer, "clean")
}
