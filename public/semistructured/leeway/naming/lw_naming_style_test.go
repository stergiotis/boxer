package naming

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertNameStyle_HappyCase(t *testing.T) {
	require.Equal(t, "äV€ryTrickyCase1", ConvertNameStyle("äV€ryTrickyCase1", LowerCamelCase))
	require.Equal(t, "ÄV€ryTrickyCase1", ConvertNameStyle("äV€ryTrickyCase1", UpperCamelCase))
	require.Equal(t, "ä_v€ry_tricky_case1", ConvertNameStyle("äV€ryTrickyCase1", LowerSnakeCase))
	require.Equal(t, "Ä_V€RY_TRICKY_CASE1", ConvertNameStyle("äV€ryTrickyCase1", UpperSnakeCase))
	require.Equal(t, "ä-v€ry-tricky-case1", ConvertNameStyle("äV€ryTrickyCase1", LowerSpinalCase))
	require.Equal(t, "Ä-V€RY-TRICKY-CASE1", ConvertNameStyle("äV€ryTrickyCase1", UpperSpinalCase))
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

// Regression for review C-1: ValidateNameComponent ranged over a []rune with a
// single loop variable, so it compared the *index* against the separator rune
// values — separators inside a component were never detected and any component
// of length ≥46 was bogusly rejected (index 45 == '-'). Verify both directions.
func TestValidateNameComponentDetectsSeparators(t *testing.T) {
	require.Error(t, ValidateNameComponent("foo-bar"), "spinal-case separator inside a component must be rejected")
	require.Error(t, ValidateNameComponent("foo_bar"), "snake-case separator inside a component must be rejected")

	// A long single-token component (≥46 runes) must be accepted — the old
	// index-based loop falsely flagged it as containing the '-' separator.
	long := StylableName("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") // 51×'a'
	require.NoError(t, ValidateNameComponent(long), "a long separator-free component must validate")

	// The uniqueness property on StylableName depends on this: a hyphenated
	// component must not join-collide with two separate components.
	joined, err := JoinComponents("foo", "bar")
	require.NoError(t, err)
	_, err = JoinComponents("foo-bar")
	require.Error(t, err, "a component carrying the join separator must be rejected, preserving JoinComponents injectivity")
	require.Equal(t, "foo-bar", joined.String())
}

// ConvertNameStyle is not idempotent for every (name, style) pair, and cannot
// be under this component model: component boundaries are recovered from the
// spelling, and upper-casing manufactures one at a digit. "foo2bar" is ONE
// component while lower-cased — nothing separates "2" from "b" — but its
// upper-cased spelling reads as TWO, because "2" then "B" IS a boundary.
//
// The consequence is what matters: the once-converted name is then spelled in
// NO supported style, so TableValidator rejects it. This pins the shape of the
// limitation so a change to the underlying splitter is noticed here rather
// than as an intermittent failure in a randomized schema test.
func TestConvertNameStyle_NotIdempotentAcrossDigitBoundary(t *testing.T) {
	const name = "foo2bar"
	for _, style := range []NamingStyleE{UpperSnakeCase, UpperSpinalCase} {
		once := ConvertNameStyle(name, style)
		require.Equal(t, "FOO2BAR", string(once), "%s", style)
		require.NotEqual(t, once, ConvertNameStyle(once, style),
			"%s: upper-casing invents a component boundary at the digit", style)

		matched := false
		for _, s2 := range AllNamingStyles {
			if ConvertNameStyle(once, s2) == once {
				matched = true
			}
		}
		require.False(t, matched, "%s: the converted name matches no style — the state validation rejects", style)
	}
}

// IsStyleStable is the guard callers use to avoid landing there.
func TestIsStyleStable(t *testing.T) {
	// Lossy exactly where the upper styles meet the digit boundary.
	require.False(t, IsStyleStable("foo2bar", UpperSnakeCase))
	require.False(t, IsStyleStable("foo2bar", UpperSpinalCase))
	// The lower styles keep the single component intact.
	require.True(t, IsStyleStable("foo2bar", LowerSnakeCase))
	require.True(t, IsStyleStable("foo2bar", LowerSpinalCase))
	require.True(t, IsStyleStable("foo2bar", LowerCamelCase))
	// A name that spells its boundary with a capital — every name in the tree
	// looks like this — is stable in every style.
	for _, style := range AllNamingStyles {
		require.Truef(t, IsStyleStable("u64Array", style), "u64Array must survive %s", style)
	}
}
