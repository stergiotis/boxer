package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertNameStyle_HappyCase(t *testing.T) {
	require.Equal(t, "äV€ryTrickyCase1", ConvertNameStyle("äV€ryTrickyCase1", NamingStyleLowerCamelCase))
	require.Equal(t, "ÄV€ryTrickyCase1", ConvertNameStyle("äV€ryTrickyCase1", NamingStyleUpperCamelCase))
	require.Equal(t, "ä_v€ry_tricky_case1", ConvertNameStyle("äV€ryTrickyCase1", NamingStyleSnakeCase))
	require.Equal(t, "ä-v€ry-tricky-case1", ConvertNameStyle("äV€ryTrickyCase1", NamingStyleSpinalCase))
}
func TestJoinComponents(t *testing.T) {
	name, err := JoinComponents("ä", "Very", "tricky", "case")
	require.NoError(t, err)
	require.True(t, name.IsValid())
	require.True(t, name.IsUsingStyle(DefaultNamingStyle))
	require.NoError(t, name.Validate())

	comps := make([]StylableName, 0, 8)
	for comp := range name.IterateComponents() {
		comps = append(comps, comp)
	}
	require.Equal(t, 4, len(comps))
	require.Equal(t, "ä", comps[0].String())
	require.Equal(t, "very", comps[1].String())
	require.Equal(t, "tricky", comps[2].String())
	require.Equal(t, "case", comps[3].String())
}
