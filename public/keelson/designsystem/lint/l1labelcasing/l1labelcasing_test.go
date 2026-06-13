package l1labelcasing_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l1labelcasing"
)

func TestL1LabelCasingViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l1labelcasing.Analyzer, "violator")
}

func TestL1LabelCasingIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l1labelcasing.Analyzer, "ignored")
}

func TestL1LabelCasingNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l1labelcasing.Analyzer, "clean")
}
