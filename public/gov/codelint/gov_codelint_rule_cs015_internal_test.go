package codelint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The exemption names the broker's package and matches it exactly: a
// subpackage of the broker is a package like any other to the rule.
func TestCS015ExemptsTheBrokerExactly(t *testing.T) {
	assert.True(t, cs015Exempt(clipboardBrokerPkgPath))
	assert.False(t, cs015Exempt(clipboardBrokerPkgPath+"/sub"))
	assert.False(t, cs015Exempt(clipboardBrokerPkgPath+"x"))
	assert.False(t, cs015Exempt(hostbootPkgPath), "the host drains the broker; it does not request on it")
	assert.False(t, cs015Exempt("github.com/stergiotis/boxer/apps/chat"))
}
