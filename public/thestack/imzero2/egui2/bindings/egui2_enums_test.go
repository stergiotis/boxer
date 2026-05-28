package bindings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseFlagsE_Iterate(t *testing.T) {
	require.EqualValues(t, 0, NilResponseFlags.Count())
	for _, f := range AllResponseFlags {
		require.EqualValues(t, false, NilResponseFlags.Has(f))
		require.EqualValues(t, true, NilResponseFlags.Set(f).Has(f))
		require.EqualValues(t, false, NilResponseFlags.Set(f).Clear(f).Has(f))
		for _, g := range AllResponseFlags {
			require.EqualValues(t, f == g, NilResponseFlags.Set(f).Has(g))
		}
	}
	{
		u := NilResponseFlags
		for i, f := range AllResponseFlags {
			u = u.Set(f)
			require.EqualValues(t, i+1, u.Count())
		}
	}
}
