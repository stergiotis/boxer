package codelint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The exemption is the runtime tree by path prefix: the runtime package,
// every package below it, and nothing that merely shares the prefix text.
func TestCS016ExemptsTheRuntimeTreeByPrefix(t *testing.T) {
	assert.True(t, cs016Exempt(runtimePkgPath))
	assert.True(t, cs016Exempt(taskPkgPath))
	assert.True(t, cs016Exempt(hostbootPkgPath))
	assert.True(t, cs016Exempt(runtimePkgPath+"/bgjob"))
	assert.False(t, cs016Exempt(runtimePkgPath+"x/task"))
	assert.False(t, cs016Exempt("github.com/stergiotis/boxer/apps/tally"))
	assert.False(t, cs016Exempt("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"))
}
