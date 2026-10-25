package app

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPackageOfFunc(t *testing.T) {
	cases := map[string]string{
		"github.com/x/y/apps/a.init.0":                          "github.com/x/y/apps/a",
		"github.com/x/y/apps/a.init.0.func1":                    "github.com/x/y/apps/a",
		"github.com/x/y/apps/a.(*App).Mount":                    "github.com/x/y/apps/a",
		"github.com/x/y/pkg.F[github.com/a/b.T]":                "github.com/x/y/pkg",
		"gopkg.in/yaml%2ev3.Unmarshal":                          "gopkg.in/yaml.v3",
		"main.main":                                             "main",
		"main":                                                  "main",
		"github.com/x/y/runtime/app_test.TestOrigin_Registered": "github.com/x/y/runtime/app_test",
	}
	for in, want := range cases {
		assert.Equal(t, want, packageOfFunc(in), in)
	}
}

func TestOriginOfPackage(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/downstream", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "example.com/boxer", Version: "v0.1.0", Sum: "h1:boxer"},
			// A nested module is a longer prefix and must win over its parent.
			{Path: "example.com/boxer/nested", Version: "v0.2.0", Sum: "h1:nested"},
			{Path: "example.com/forked", Version: "v1.0.0", Sum: "h1:upstream",
				Replace: &debug.Module{Path: "example.com/fork", Version: "v1.0.1", Sum: "h1:fork"}},
			// A path prefix that is not a path-segment prefix must not match.
			{Path: "example.com/box", Version: "v9.9.9"},
		},
	}
	o := originOfPackage("example.com/downstream/apps/a", bi)
	assert.Equal(t, Origin{Package: "example.com/downstream/apps/a", Module: "example.com/downstream", ModuleVersion: "(devel)", Main: true}, o)

	o = originOfPackage("example.com/boxer/apps/play", bi)
	assert.Equal(t, "example.com/boxer", o.Module)
	assert.Equal(t, "v0.1.0", o.ModuleVersion)
	assert.Equal(t, "h1:boxer", o.ModuleSum)
	assert.False(t, o.Main)

	o = originOfPackage("example.com/boxer/nested/x", bi)
	assert.Equal(t, "example.com/boxer/nested", o.Module)

	o = originOfPackage("example.com/forked/x", bi)
	assert.Equal(t, "example.com/forked", o.Module)
	assert.Equal(t, "v1.0.0", o.ModuleVersion)
	assert.Equal(t, "example.com/fork@v1.0.1", o.Replace)
	assert.Equal(t, "h1:fork", o.ModuleSum)

	o = originOfPackage("main", bi)
	assert.Equal(t, "example.com/downstream", o.Module)
	assert.True(t, o.Main)

	o = originOfPackage("elsewhere.org/z", bi)
	assert.Equal(t, Origin{Package: "elsewhere.org/z"}, o)

	o = originOfPackage("example.com/boxer/apps/play", nil)
	assert.Equal(t, Origin{Package: "example.com/boxer/apps/play"}, o)
}
