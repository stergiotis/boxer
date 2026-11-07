package codelint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The scope is the apps tree by path prefix: the tree's root, every package
// below it, and nothing that merely shares the prefix text.
func TestCS017CoversTheAppsTreeByPrefix(t *testing.T) {
	r := NewRuleCS017()
	assert.True(t, r.inScope(appsTreePkgPath))
	assert.True(t, r.inScope(appsTreePkgPath+"/play"))
	assert.True(t, r.inScope(appsTreePkgPath+"/mdedit/mdeditlink"))
	assert.False(t, r.inScope(appsTreePkgPath+"x/play"))
	assert.False(t, r.inScope(runtimePkgPath+"/bgjob"))
	assert.False(t, r.inScope("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"))
}

func TestCS017LifetimeAnnotationNeedsAReason(t *testing.T) {
	assert.True(t, lifetimeAnnotationPattern.MatchString("// lifetime: process-wide sampler"))
	assert.True(t, lifetimeAnnotationPattern.MatchString("//lifetime: once per process"))
	assert.False(t, lifetimeAnnotationPattern.MatchString("// lifetime:"))
	assert.False(t, lifetimeAnnotationPattern.MatchString("// lifetime:   "))
	assert.False(t, lifetimeAnnotationPattern.MatchString("// the lifetime: of the window"))
}
