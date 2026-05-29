//go:build llm_generated_opus47

package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyFuncApp_NilRender(t *testing.T) {
	_, err := NewLegacyFuncApp(testManifest("org.test.x"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil render")
}

func TestLegacyFuncApp_InvalidManifest(t *testing.T) {
	_, err := NewLegacyFuncApp(Manifest{}, func() (err error) { return })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid manifest")
}

func TestLegacyFuncApp_Frame_CallsRender(t *testing.T) {
	var calls int
	a, err := NewLegacyFuncApp(testManifest("org.test.frame"), func() (err error) {
		calls++
		return
	})
	require.NoError(t, err)

	err = a.Mount(nil)
	require.NoError(t, err)
	err = a.Frame(nil)
	require.NoError(t, err)
	err = a.Frame(nil)
	require.NoError(t, err)
	err = a.Unmount(nil)
	require.NoError(t, err)

	assert.Equal(t, 2, calls)
}

func TestLegacyFuncApp_Frame_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	a, err := NewLegacyFuncApp(testManifest("org.test.err"), func() (err error) {
		err = sentinel
		return
	})
	require.NoError(t, err)
	err = a.Frame(nil)
	assert.ErrorIs(t, err, sentinel)
}

func TestLegacyFuncApp_Manifest_Returns(t *testing.T) {
	m := testManifest("org.test.m")
	m.Category = "tools"
	a, err := NewLegacyFuncApp(m, func() (err error) { return })
	require.NoError(t, err)
	got := a.Manifest()
	assert.Equal(t, m.Id, got.Id)
	assert.Equal(t, m.Category, got.Category)
}
