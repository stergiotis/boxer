//go:build llm_generated_opus47

package l9radiochanged_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l9radiochanged"
)

func TestL9Violations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l9radiochanged.Analyzer, "violator")
}

func TestL9Ignored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l9radiochanged.Analyzer, "ignored")
}

func TestL9Clean(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l9radiochanged.Analyzer, "clean")
}
