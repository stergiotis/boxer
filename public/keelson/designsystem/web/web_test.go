package web_test

import (
	"regexp"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/web"
	"github.com/stretchr/testify/require"
)

// TestStylesheet_AllVarsResolve guards against a typo'd token silently leaving
// an element unstyled: every var(--ids-*) referenced in the IDS stylesheet must
// have a matching custom-property definition (in ids-palette.css or ids.css).
func TestStylesheet_AllVarsResolve(t *testing.T) {
	css := web.Stylesheet()

	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--ids-[a-z0-9-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	require.NotEmpty(t, defined, "no --ids-* definitions found — stylesheet not loaded?")

	for _, m := range regexp.MustCompile(`var\((--ids-[a-z0-9-]+)`).FindAllStringSubmatch(css, -1) {
		require.Truef(t, defined[m[1]], "var %s used but never defined in the IDS stylesheet", m[1])
	}
}
