package keelsonfield

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInts(t *testing.T) {
	got, err := parseInts("[0, -180,360]")
	require.NoError(t, err)
	assert.Equal(t, []int64{0, -180, 360}, got)
	_, err = parseInts("0,1")
	assert.Error(t, err)
}
