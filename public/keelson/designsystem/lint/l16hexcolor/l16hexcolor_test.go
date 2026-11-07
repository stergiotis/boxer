package l16hexcolor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l16hexcolor"
)

func TestL16HexColorViolations(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l16hexcolor.Analyzer, "violator")
}

func TestL16HexColorIgnored(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l16hexcolor.Analyzer, "ignored")
}

func TestL16HexColorNoFalsePositives(t *testing.T) {
	dir := analysistest.TestData()
	analysistest.Run(t, dir, l16hexcolor.Analyzer, "clean")
}

func TestL16IsColourName(t *testing.T) {
	for _, name := range []string{"bandColor", "colInvalid_colour", "ColorAccent", "weekendHex", "ToneWarn", "availabilityBandColor"} {
		assert.True(t, l16hexcolor.IsColourName(name), name)
	}
	for _, name := range []string{"maxRows", "stone", "hexdump", "milestoneCount", "mask"} {
		assert.False(t, l16hexcolor.IsColourName(name), name)
	}
}
