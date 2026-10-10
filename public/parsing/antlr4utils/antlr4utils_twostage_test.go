package antlr4utils

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
)

// No statement in the repository's corpus reaches the LL fallback once the
// grammar's LL islands are in place (ADR-0305), so the fallback is
// checked here directly rather than through a parse.

func TestTwoStageReturnsStageOneWhenClean(t *testing.T) {
	var modes []int
	result, ok, fellBack := TwoStage(func(mode int) (string, bool) {
		modes = append(modes, mode)
		return "sll", true
	})
	assert.Equal(t, []int{antlr.PredictionModeSLL}, modes)
	assert.Equal(t, "sll", result)
	assert.True(t, ok)
	assert.False(t, fellBack)
}

func TestTwoStageFallsBackToLL(t *testing.T) {
	var modes []int
	result, ok, fellBack := TwoStage(func(mode int) (string, bool) {
		modes = append(modes, mode)
		return map[int]string{antlr.PredictionModeSLL: "sll", antlr.PredictionModeLL: "ll"}[mode], mode == antlr.PredictionModeLL
	})
	assert.Equal(t, []int{antlr.PredictionModeSLL, antlr.PredictionModeLL}, modes)
	assert.Equal(t, "ll", result, "the LL attempt's result must replace stage one's")
	assert.True(t, ok)
	assert.True(t, fellBack)
}

func TestTwoStageReportsLLVerdictOnGenuineError(t *testing.T) {
	result, ok, fellBack := TwoStage(func(mode int) (string, bool) {
		return map[int]string{antlr.PredictionModeSLL: "sll", antlr.PredictionModeLL: "ll"}[mode], false
	})
	assert.Equal(t, "ll", result, "diagnostics must come from the LL attempt")
	assert.False(t, ok)
	assert.True(t, fellBack)
}
