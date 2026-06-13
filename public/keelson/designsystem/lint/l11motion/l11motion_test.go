package l11motion_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l11motion"
)

func TestL11MotionViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l11motion.Analyzer, "violator")
}

func TestL11MotionIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l11motion.Analyzer, "ignored")
}

func TestL11MotionNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l11motion.Analyzer, "clean")
}
