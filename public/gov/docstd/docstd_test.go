//go:build llm_generated_opus48

package docstd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateFrontmatter_Conformant(t *testing.T) {
	cases := []struct {
		docType string
		status  string
		allow   bool
	}{
		{TypeExplanation, StatusStable, true},
		{TypeExplanation, StatusStable, false},
		{TypeHowTo, StatusDraft, false},
		{TypeReference, StatusDeprecated, false},
		{TypeTutorial, StatusSuperseded, false},
		{TypeADR, StatusAccepted, true},
		{TypeADR, StatusDeprecated, true}, // deprecated shared with descriptive set
	}
	for _, c := range cases {
		t.Run(c.docType+"/"+c.status, func(t *testing.T) {
			require.Empty(t, ValidateFrontmatter(c.docType, c.status, c.allow))
		})
	}
}

func TestValidateFrontmatter_MissingFields(t *testing.T) {
	// Missing type only.
	vs := ValidateFrontmatter("", StatusDraft, true)
	require.Len(t, vs, 1)
	require.Equal(t, "type", vs[0].Field)
	require.Contains(t, vs[0].Message, "missing required field 'type'")

	// Missing status only.
	vs = ValidateFrontmatter(TypeHowTo, "", true)
	require.Len(t, vs, 1)
	require.Equal(t, "status", vs[0].Field)
	require.Contains(t, vs[0].Message, "missing required field 'status'")

	// Both missing — type first, then status.
	vs = ValidateFrontmatter("", "", true)
	require.Len(t, vs, 2)
	require.Equal(t, "type", vs[0].Field)
	require.Equal(t, "status", vs[1].Field)
}

func TestValidateFrontmatter_InvalidType(t *testing.T) {
	vs := ValidateFrontmatter("bogus", StatusDraft, true)
	require.Len(t, vs, 1)
	require.Equal(t, "type", vs[0].Field)
	require.Equal(t, "bogus", vs[0].Value)
	// allowADR=true lists all five types.
	require.Contains(t, vs[0].Message, "adr")
}

func TestValidateFrontmatter_InvalidStatusKeyedOnType(t *testing.T) {
	// 'accepted' is ADR-only — invalid for a descriptive type.
	vs := ValidateFrontmatter(TypeExplanation, StatusAccepted, true)
	require.Len(t, vs, 1)
	require.Equal(t, "status", vs[0].Field)
	require.Contains(t, vs[0].Message, "not valid for type 'explanation'")

	// 'draft' is descriptive-only — invalid for an ADR.
	vs = ValidateFrontmatter(TypeADR, StatusDraft, true)
	require.Len(t, vs, 1)
	require.Equal(t, "status", vs[0].Field)
}

func TestValidateFrontmatter_HelpRejectsADR(t *testing.T) {
	// allowADR=false: 'adr' is not a valid type, and the message omits it.
	vs := ValidateFrontmatter(TypeADR, StatusAccepted, false)
	require.Len(t, vs, 1, "only the type is flagged; status 'accepted' is internally consistent with adr")
	require.Equal(t, "type", vs[0].Field)
	// The allowed-types list omits adr (the rejected value is still quoted
	// in the message, so inspect only the "is not one of: …" portion).
	_, list, found := strings.Cut(vs[0].Message, "is not one of: ")
	require.True(t, found)
	require.NotContains(t, list, TypeADR)
	for _, ct := range []string{TypeReference, TypeHowTo, TypeExplanation, TypeTutorial} {
		require.Contains(t, list, ct)
	}
}

func TestTypeListVariesByAllowADR(t *testing.T) {
	require.True(t, strings.HasSuffix(typeList(true), TypeADR))
	require.NotContains(t, typeList(false), TypeADR)
}

func TestMembership(t *testing.T) {
	require.True(t, IsContentType(TypeReference))
	require.True(t, IsContentType(TypeTutorial))
	require.False(t, IsContentType(TypeADR))
	require.False(t, IsContentType(""))

	require.True(t, IsType(TypeADR))
	require.True(t, IsType(TypeExplanation))
	require.False(t, IsType("bogus"))

	require.True(t, IsStatusForType(TypeADR, StatusAccepted))
	require.False(t, IsStatusForType(TypeADR, StatusDraft))
	require.True(t, IsStatusForType(TypeExplanation, StatusDraft))
	require.False(t, IsStatusForType(TypeExplanation, StatusAccepted))
	// Empty/unknown type falls back to the descriptive set.
	require.True(t, IsStatusForType("", StatusDraft))
}
