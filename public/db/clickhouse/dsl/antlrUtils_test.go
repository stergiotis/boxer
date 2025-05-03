package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIterateAll(t *testing.T) {
	_, tree, err := parseSql("SELECT 1 FROM db.tbl;", nil, nil)
	require.NoError(t, err)
	i := 0
	for range IterateAll(tree) {
		i++
	}
	assert.Equal(t, 26, i)
	i = 0
	for range IterateAll(tree) {
		if i > 2 {
			break
		}
		i++
	}
	assert.Equal(t, 3, i)
}
