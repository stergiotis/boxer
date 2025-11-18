package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_getModuleInfo(t *testing.T) {
	m := getModuleInfo(0)
	require.Equal(t, "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry", m)
}
