package common

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

// TestScrambleThenNormalizeRoundTrips pins the two properties Scramble must
// have, both of which it lacked while its body was empty: it must actually
// perturb the table (an identity Scramble makes every Scramble-then-Normalize
// assertion vacuous), and what it produces must stay valid, since Normalize
// validates before it re-styles and would otherwise reject the input rather
// than canonicalize it. The seed is fixed so "did it perturb" cannot flake.
func TestScrambleThenNormalizeRoundTrips(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	normalizer := NewTableNormalizer(naming.DefaultNamingStyle)
	validator := NewTableValidator()
	rnd := rand.New(rand.NewPCG(1, 2))

	canonical := buildTable(t, "geoTable", loadGeoTable)
	_, _, _, err = normalizer.Normalize(&canonical)
	require.NoError(t, err)

	perturbed := false
	for range 20 {
		var tbl TableDesc
		tbl, err = ops.DeepCopy(&canonical)
		require.NoError(t, err)

		normalizer.Scramble(&tbl, rnd)
		validator.Reset()
		require.NoError(t, validator.ValidateTable(&tbl), "a scrambled table must stay valid")
		if !reflect.DeepEqual(tbl, canonical) {
			perturbed = true
		}

		_, _, _, err = normalizer.Normalize(&tbl)
		require.NoError(t, err)
		require.EqualValues(t, canonical, tbl, "Normalize must undo Scramble")
	}
	require.True(t, perturbed, "Scramble must not be an identity")
}

func TestNormalizer(t *testing.T) {
	normalizer := NewTableNormalizer(naming.DefaultNamingStyle)
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
