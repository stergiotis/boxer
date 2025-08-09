package common

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizer(t *testing.T) {
	normalizer := NewTableNormalizer(DefaultNamingStyle)
	ops, err := NewTableOperations()
	require.NoError(t, err)
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for i := 0; i < 100; i++ {
		var tblDesc1, tblDesc2 TableDesc
		tblDesc1, err = GenerateSampleTableDesc(rnd, nil, nil)
		require.NoError(t, err)
		tblDesc2, err = ops.DeepCopy(&tblDesc1)
		require.NoError(t, err)
		_, _, _, err = normalizer.Normalize(&tblDesc2)
		require.NoError(t, err)
		for j := 0; j < 5; j++ {
			normalizer.Scramble(&tblDesc1, rnd)
			_, _, _, err = normalizer.Normalize(&tblDesc1)
			require.NoError(t, err)
			require.EqualValues(t, tblDesc2, tblDesc1)
		}
	}
}
