package registry

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stretchr/testify/require"
)

func TestNewNaturalKeyRegistry(t *testing.T) {
	reg, err := NewNaturalKeyRegistry[*contract.VcsManagedContract](identifier.TagValue(4), 8, naming.LowerSnakeCase, 0, contract.NewVcsManagedContract())
	require.NoError(t, err)
	nkV1 := reg.MustRegister("nkV1").SetVirtual()
	nkV2 := reg.MustRegister("nkV2").SetVirtual()
	require.Equal(t, "nk_v1", nkV1.GetNaturalKey().String())
	require.Equal(t, true, nkV1.GetFlags().HasVirtual())
	require.Equal(t, false, nkV1.GetFlags().HasFinal())
	require.Equal(t, false, nkV1.GetFlags().HasDeprecated())
	require.Equal(t, "nk_v2", nkV2.GetNaturalKey().String())
	require.Equal(t, true, nkV2.GetFlags().HasVirtual())
	require.Equal(t, false, nkV2.GetFlags().HasFinal())
	require.Equal(t, false, nkV2.GetFlags().HasDeprecated())
	nk1 := reg.MustRegister("nk1").MustAddParentsVirtual(nkV1, nkV2)
	require.Equal(t, "nk1", nk1.GetNaturalKey().String())
	require.Equal(t, false, nk1.GetFlags().HasVirtual())
	require.Equal(t, false, nk1.GetFlags().HasFinal())
	require.Equal(t, false, nk1.GetFlags().HasDeprecated())
	nkF1 := reg.MustRegister("nkF1").MustAddParentsVirtual(nkV1, nkV2).SetFinal()
	require.Equal(t, "nk_f1", nkF1.GetNaturalKey().String())
	require.Equal(t, false, nkF1.GetFlags().HasVirtual())
	require.Equal(t, true, nkF1.GetFlags().HasFinal())
	require.Equal(t, false, nkF1.GetFlags().HasDeprecated())
	s := slices.Collect(functional.IterLeftOnly(nkF1.IterateParentsVirtual()))
	e := []identifier.TaggedId{nkV1.w.id, nkV2.w.id}
	slices.Sort(s)
	slices.Sort(e)
	require.Equal(t, s, e)
}
