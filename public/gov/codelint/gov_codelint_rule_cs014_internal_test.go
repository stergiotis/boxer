package codelint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The exemption names three packages and matches them exactly (ADR-0317
// §SD2): a subpackage of the host, the broker or the bindings is a package
// like any other to the rule.
func TestCS014ExemptsThreePackagesExactly(t *testing.T) {
	for _, p := range []string{clipboardBrokerPkgPath, hostbootPkgPath, bindingsPkgPath} {
		assert.True(t, cs014Exempt(p), p)
		assert.False(t, cs014Exempt(p+"/sub"), "a subpackage is not exempt: %s", p)
		assert.False(t, cs014Exempt(p+"x"), p)
	}
	assert.False(t, cs014Exempt("github.com/stergiotis/boxer/apps/chat"))
}
