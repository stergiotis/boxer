package canonicalTypes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypes(t *testing.T) {
	var a AstNodeI
	a = &GroupAstNode{}
	_, castable := (a).(PrimitiveAstNodeI)
	require.False(t, castable, "group ast node is a primitive type")
}
