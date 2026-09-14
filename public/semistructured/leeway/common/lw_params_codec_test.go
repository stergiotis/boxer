package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

func TestParamsCodecDeclaration(t *testing.T) {
	require.Equal(t, ParamsCodecUndeclared, DeclaredParamsCodec(useaspects.EmptyAspectSet))
	require.Equal(t, ParamsCodecUndeclared, DeclaredParamsCodec(useaspects.EncodeAspectsMustValidate(useaspects.AspectSpatial)))
	for _, c := range AllParamsCodecs {
		a, ok := c.Aspect()
		require.True(t, ok, c)
		back, ok := GetParamsCodecByAspect(a)
		require.True(t, ok)
		require.Equal(t, c, back)
		require.Equal(t, c, DeclaredParamsCodec(useaspects.EncodeAspectsMustValidate(a)))
	}
	_, ok := ParamsCodecUndeclared.Aspect()
	require.False(t, ok)
}

func TestParamsCodecMergerAndValidator(t *testing.T) {
	m, err := NewTableManipulator()
	require.NoError(t, err)
	m.SetTableName("t")
	sec := m.TaggedValueSection("s").
		AddSectionUseAspects(useaspects.AspectSpatial).
		AddSectionMembership(MembershipSpecLowCardVerbatim)
	sec.TaggedValueColumn("v", ctabb.F32)
	// Declared on a section without a params-bearing channel: the build's
	// validation rejects it.
	sec.AddSectionParamsCodec(ParamsCodecFixedWidthHex)
	_, err = m.BuildTableDesc()
	require.ErrorContains(t, err, "params-codec declaration without a params-bearing membership channel")
	// With the channel: accepted, the other aspects are kept, and declaring
	// twice is idempotent.
	sec.AddSectionMembership(MembershipSpecMixedLowCardVerbatimHighCardParameters)
	sec.AddSectionParamsCodec(ParamsCodecFixedWidthHex)
	td, err := m.BuildTableDesc()
	require.NoError(t, err)
	s := td.TaggedValuesSections[0]
	require.Equal(t, ParamsCodecFixedWidthHex, DeclaredParamsCodec(s.UseAspects))
	require.True(t, s.UseAspects.Contains(useaspects.AspectSpatial), "declaring keeps the other aspects")
	n, err := s.UseAspects.CountEncodedAspects()
	require.NoError(t, err)
	require.Equal(t, 2, n)
	// A view of the same section that drops the channel is a schema error,
	// not a codec re-interpretation.
	s.MembershipSpec = MembershipSpecLowCardVerbatim
	require.Error(t, NewTableValidator().ValidateSection(s))
}
