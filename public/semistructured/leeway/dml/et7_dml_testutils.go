package dml

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/require"
)

func checkCodeInvariants(code []byte, t *testing.T) {
	codeS := unsafeperf.UnsafeBytesToString(code)
	listBuilderFieldRgx := regexp.MustCompile("[a-z0-9A-Z_]+ListBuilder[0-9]+")
	occ := listBuilderFieldRgx.FindAllString(codeS, -1)
	slices.Sort(occ)
	occ = slices.Compact(occ)
	for _, o := range occ {
		require.True(t, strings.Contains(codeS, o+".Append("), o)
	}
}
