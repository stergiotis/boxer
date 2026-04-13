//go:build llm_generated_opus46

package commitdigest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTiktokenCounter_Init(t *testing.T) {
	counter := &TiktokenCounter{Encoding: "cl100k_base"}
	err := counter.Init()
	require.NoError(t, err)
}

func TestTiktokenCounter_InitInvalidEncoding(t *testing.T) {
	counter := &TiktokenCounter{Encoding: "nonexistent_encoding"}
	err := counter.Init()
	assert.Error(t, err)
}

func TestTiktokenCounter_CountTokens(t *testing.T) {
	counter := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 1.0,
	}
	err := counter.Init()
	require.NoError(t, err)

	count := counter.CountTokens("hello world")
	assert.Greater(t, count, int64(0))
}

func TestTiktokenCounter_EmptyString(t *testing.T) {
	counter := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 1.0,
	}
	err := counter.Init()
	require.NoError(t, err)

	count := counter.CountTokens("")
	assert.Equal(t, int64(0), count)
}

func TestTiktokenCounter_CorrectionMultiplier(t *testing.T) {
	base := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 1.0,
	}
	err := base.Init()
	require.NoError(t, err)

	corrected := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 2.0,
	}
	err = corrected.Init()
	require.NoError(t, err)

	text := "This is a test sentence for token counting."
	baseCount := base.CountTokens(text)
	correctedCount := corrected.CountTokens(text)
	assert.Greater(t, correctedCount, baseCount)
}

func TestTiktokenCounter_DefaultCorrectionMultiplier(t *testing.T) {
	counter := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 0, // should fall back to 1.18
	}
	err := counter.Init()
	require.NoError(t, err)

	noCorrectionCounter := &TiktokenCounter{
		Encoding:             "cl100k_base",
		CorrectionMultiplier: 1.0,
	}
	err = noCorrectionCounter.Init()
	require.NoError(t, err)

	text := "This is a longer test sentence that should produce several tokens for comparison."
	defaultCount := counter.CountTokens(text)
	rawCount := noCorrectionCounter.CountTokens(text)
	assert.Greater(t, defaultCount, rawCount)
}
