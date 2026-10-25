package launcher

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stretchr/testify/assert"
)

func TestOriginLabel(t *testing.T) {
	assert.Equal(t, "example.com/dep@v1.2.3",
		originLabel(app.Origin{Package: "example.com/dep/apps/b", Module: "example.com/dep", ModuleVersion: "v1.2.3"}))
	assert.Equal(t, "example.com/main",
		originLabel(app.Origin{Package: "example.com/main/apps/a", Module: "example.com/main", ModuleVersion: "(devel)", Main: true}),
		"a local build has no version to show")
	assert.Equal(t, "example.com/dep@v1.2.3 → ../dep",
		originLabel(app.Origin{Module: "example.com/dep", ModuleVersion: "v1.2.3", Replace: "../dep"}))
	assert.Equal(t, "example.com/x/pkg", originLabel(app.Origin{Package: "example.com/x/pkg"}),
		"without build information the package is the answer")
	assert.Equal(t, "", originLabel(app.Origin{}))
}
