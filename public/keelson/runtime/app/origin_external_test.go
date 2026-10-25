package app_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The registering package is the first frame outside package app, so the
// test has to register from outside it. Both entry points are covered: the
// package-level helper adds a frame inside package app that the walk skips.
func TestOrigin_Registered(t *testing.T) {
	reg := app.NewRegistry()
	m := app.Manifest{
		Id:      "org.test.origin",
		Display: "origin",
		Topics:  []app.TopicT{app.TopicRuntime},
		Summary: "fixture summary",
		Surface: app.SurfaceWindowed,
	}
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) { return }))

	o, ok := reg.Origin("org.test.origin")
	require.True(t, ok)
	assert.Equal(t, "github.com/stergiotis/boxer/public/keelson/runtime/app_test", o.Package)
	assert.Equal(t, "github.com/stergiotis/boxer", o.Module)

	regs := reg.Registrations()
	require.Len(t, regs, 1)
	assert.Equal(t, o, regs[0].Origin)

	_, ok = reg.Origin("org.test.absent")
	assert.False(t, ok)
}
