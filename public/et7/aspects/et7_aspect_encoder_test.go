package aspects

import (
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalEt7AspectEnum(t *testing.T) {
	require.EqualValues(t, len(AllDataAspects), MaxDataAspectExcl)
	m := make([]string, 0, len(AllDataAspects))
	for i := DataAspectE(0); i < MaxDataAspectExcl; i++ {
		require.False(t, slices.Contains(m, i.String()))
		m = append(m, i.String())
	}
}
func TestCanonicalEt7AspectCoder(t *testing.T) {
	encoder := NewCanonicalEt7AspectCoder()
	rgx := regexp.MustCompile("[^a-zA-Z0-9]")
	{
		_, err := encoder.Encode(MaxDataAspectExcl)
		require.Error(t, err)
	}
	for i := DataAspectE(0); i < MaxDataAspectExcl; i++ {
		enc, err := encoder.Encode(i)
		require.NoError(t, err)
		for k, a := range encoder.IterateAspects(enc) {
			require.Equal(t, k, 0)
			require.Equal(t, a, i)
		}
		require.EqualValues(t, i, slices.Index(AllDataAspects, i))
		require.False(t, encoder.IsEmpty(enc))

		var n int
		n, err = encoder.CountEncodedAspects(enc)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
	}
	for i := DataAspectE(0); i < MaxDataAspectExcl; i++ {
		for j := DataAspectE(0); j < MaxDataAspectExcl; j++ {
			enc, err := encoder.Encode(i, j)
			require.NoError(t, err)
			outer := false
			inner := false
			for k, a := range encoder.IterateAspects(enc) {
				require.Less(t, k, 2)
				if a == i {
					require.False(t, outer)
					outer = true
				}
				if a == j {
					require.False(t, inner)
					inner = true
				}
			}
			require.False(t, encoder.IsEmpty(enc))

			var me DataAspectE
			me, err = encoder.MaxEncodedAspect(enc)
			require.NoError(t, err)
			require.EqualValues(t, max(i, j), me)

			require.False(t, rgx.MatchString(enc.String()))

			var n int
			n, err = encoder.CountEncodedAspects(enc)
			require.NoError(t, err)
			if i != j {
				require.EqualValues(t, 2, n)
			} else {
				require.EqualValues(t, 1, n)
			}

			{
				var enc1, enc2, encU EncodedEt7AspectSet
				enc1, err = encoder.Encode(i)
				require.NoError(t, err)
				enc2, err = encoder.Encode(j)
				require.NoError(t, err)
				encU, err = encoder.UnionAspects(enc1, enc2)
				require.NoError(t, err)
				require.EqualValues(t, enc, encU)
			}
		}
	}
	require.True(t, encoder.IsEmpty(EmptyAspectSet))
	{
		n, err := encoder.CountEncodedAspects(EmptyAspectSet)
		require.NoError(t, err)
		require.EqualValues(t, 0, n)
	}
}
