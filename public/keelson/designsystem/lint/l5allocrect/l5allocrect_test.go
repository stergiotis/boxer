//go:build llm_generated_opus47

package l5allocrect_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l5allocrect"
)

func TestL5Violations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l5allocrect.Analyzer, "violator")
}

func TestL5Ignored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l5allocrect.Analyzer, "ignored")
}

func TestL5Clean(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l5allocrect.Analyzer, "clean")
}
