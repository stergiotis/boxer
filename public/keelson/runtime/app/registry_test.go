//go:build llm_generated_opus47

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestApp(t *testing.T, id AppIdT) (a AppI) {
	t.Helper()
	app, err := NewLegacyFuncApp(testManifest(id), func() (err error) { return })
	require.NoError(t, err)
	a = app
	return
}

func testManifest(id AppIdT) (m Manifest) {
	m = Manifest{
		Id:      id,
		Version: "0.1.0",
		Display: string(id),
		Surface: SurfaceWindowed,
	}
	return
}

func TestRegistry_Register_Lookup(t *testing.T) {
	reg := NewRegistry()
	a := newTestApp(t, "org.test.foo")
	err := reg.Register(a)
	require.NoError(t, err)

	got, ok := reg.Lookup("org.test.foo")
	require.True(t, ok)
	assert.Equal(t, AppIdT("org.test.foo"), got.Manifest().Id)
}

func TestRegistry_Lookup_Missing(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Lookup("org.test.absent")
	assert.False(t, ok)
}

func TestRegistry_Register_DuplicateId(t *testing.T) {
	reg := NewRegistry()
	a1 := newTestApp(t, "org.test.dup")
	a2 := newTestApp(t, "org.test.dup")

	err := reg.Register(a1)
	require.NoError(t, err)
	err = reg.Register(a2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate Id")
}

func TestRegistry_Register_NilApp(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(nil)
	require.Error(t, err)
}

func TestRegistry_Register_InvalidManifest(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&brokenApp{})
	require.Error(t, err)
}

func TestRegistry_All_SortedById(t *testing.T) {
	reg := NewRegistry()
	for _, id := range []AppIdT{"org.test.c", "org.test.a", "org.test.b"} {
		err := reg.Register(newTestApp(t, id))
		require.NoError(t, err)
	}
	all := reg.All()
	require.Len(t, all, 3)
	assert.Equal(t, AppIdT("org.test.a"), all[0].Manifest().Id)
	assert.Equal(t, AppIdT("org.test.b"), all[1].Manifest().Id)
	assert.Equal(t, AppIdT("org.test.c"), all[2].Manifest().Id)
}

func TestRegistry_Len(t *testing.T) {
	reg := NewRegistry()
	assert.Equal(t, 0, reg.Len())
	err := reg.Register(newTestApp(t, "org.test.one"))
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Len())
}

// brokenApp is an AppI whose Manifest fails Validate. Used to verify that
// the Registry rejects malformed apps even when the caller has bypassed
// NewLegacyFuncApp.
type brokenApp struct{}

func (inst *brokenApp) Manifest() (m Manifest)                { return }
func (inst *brokenApp) Mount(ctx MountContextI) (err error)   { return }
func (inst *brokenApp) Frame(ctx FrameContextI) (err error)   { return }
func (inst *brokenApp) Unmount(ctx MountContextI) (err error) { return }

// Factory-API tests below cover RegisterFactory, Open, LookupManifest,
// AllManifests, and the singleton-vs-factory dispatch contract.

func TestRegistry_RegisterFactory_Open_FreshInstancePerCall(t *testing.T) {
	reg := NewRegistry()
	var ctorCalls int
	ctor := func() (a AppI, err error) {
		ctorCalls++
		a, err = NewLegacyFuncApp(testManifest("org.test.factory"), func() (e error) { return })
		return
	}
	err := reg.RegisterFactory(testManifest("org.test.factory"), ctor)
	require.NoError(t, err)

	a1, err := reg.Open("org.test.factory")
	require.NoError(t, err)
	a2, err := reg.Open("org.test.factory")
	require.NoError(t, err)

	assert.Equal(t, 2, ctorCalls, "factory ctor must run once per Open")
	assert.NotSame(t, a1, a2, "factory must yield distinct AppI instances")
}

func TestRegistry_Register_Singleton_OpenReturnsSameInstance(t *testing.T) {
	reg := NewRegistry()
	a := newTestApp(t, "org.test.singleton")
	err := reg.Register(a)
	require.NoError(t, err)

	a1, err := reg.Open("org.test.singleton")
	require.NoError(t, err)
	a2, err := reg.Open("org.test.singleton")
	require.NoError(t, err)

	assert.Same(t, a, a1, "singleton ctor must return the originally registered instance")
	assert.Same(t, a1, a2, "singleton Open must be idempotent")
}

func TestRegistry_Open_MissingId(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Open("org.test.absent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_RegisterFactory_DuplicateId(t *testing.T) {
	reg := NewRegistry()
	m := testManifest("org.test.dup")
	err := reg.RegisterFactory(m, func() (a AppI, ctorErr error) {
		a, ctorErr = NewLegacyFuncApp(m, func() (e error) { return })
		return
	})
	require.NoError(t, err)
	err = reg.RegisterFactory(m, func() (a AppI, ctorErr error) {
		a, ctorErr = NewLegacyFuncApp(m, func() (e error) { return })
		return
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate Id")
}

func TestRegistry_RegisterFactory_NilCtor(t *testing.T) {
	reg := NewRegistry()
	err := reg.RegisterFactory(testManifest("org.test.x"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil ctor")
}

func TestRegistry_RegisterFactory_InvalidManifest(t *testing.T) {
	reg := NewRegistry()
	err := reg.RegisterFactory(Manifest{}, func() (a AppI, ctorErr error) { return })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid manifest")
}

func TestRegistry_Open_CtorError(t *testing.T) {
	reg := NewRegistry()
	err := reg.RegisterFactory(testManifest("org.test.bad"), func() (a AppI, ctorErr error) {
		ctorErr = assert.AnError
		return
	})
	require.NoError(t, err)
	_, err = reg.Open("org.test.bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ctor failed")
}

func TestRegistry_Open_CtorReturnsNil(t *testing.T) {
	reg := NewRegistry()
	err := reg.RegisterFactory(testManifest("org.test.nil"), func() (a AppI, ctorErr error) { return })
	require.NoError(t, err)
	_, err = reg.Open("org.test.nil")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil AppI")
}

func TestRegistry_LookupManifest_ReturnsStatic(t *testing.T) {
	reg := NewRegistry()
	m := testManifest("org.test.meta")
	m.Display = "custom display"
	a, err := NewLegacyFuncApp(m, func() (e error) { return })
	require.NoError(t, err)
	err = reg.Register(a)
	require.NoError(t, err)

	got, ok := reg.LookupManifest("org.test.meta")
	require.True(t, ok)
	assert.Equal(t, "custom display", got.Display)
}

func TestRegistry_LookupManifest_Missing(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.LookupManifest("org.test.absent")
	assert.False(t, ok)
}

func TestRegistry_AllManifests_SortedById(t *testing.T) {
	reg := NewRegistry()
	for _, id := range []AppIdT{"org.test.c", "org.test.a", "org.test.b"} {
		err := reg.Register(newTestApp(t, id))
		require.NoError(t, err)
	}
	manifests := reg.AllManifests()
	require.Len(t, manifests, 3)
	assert.Equal(t, AppIdT("org.test.a"), manifests[0].Id)
	assert.Equal(t, AppIdT("org.test.b"), manifests[1].Id)
	assert.Equal(t, AppIdT("org.test.c"), manifests[2].Id)
}

func TestRegistry_AllManifests_DoesNotInstantiate(t *testing.T) {
	reg := NewRegistry()
	var ctorCalls int
	m := testManifest("org.test.lazy")
	err := reg.RegisterFactory(m, func() (a AppI, ctorErr error) {
		ctorCalls++
		a, ctorErr = NewLegacyFuncApp(m, func() (e error) { return })
		return
	})
	require.NoError(t, err)

	_ = reg.AllManifests()
	_ = reg.AllManifests()
	assert.Equal(t, 0, ctorCalls, "AllManifests must not invoke ctors")
}

func TestRegistry_All_BackwardCompat_FactoryAppsInstantiate(t *testing.T) {
	// All() is the deprecated shim; documents that it does invoke ctors,
	// once per call, for factory entries — wasteful but matches the
	// historical AppI-returning shape until C3 strips remaining callers.
	reg := NewRegistry()
	var ctorCalls int
	m := testManifest("org.test.eager")
	err := reg.RegisterFactory(m, func() (a AppI, ctorErr error) {
		ctorCalls++
		a, ctorErr = NewLegacyFuncApp(m, func() (e error) { return })
		return
	})
	require.NoError(t, err)

	_ = reg.All()
	_ = reg.All()
	assert.Equal(t, 2, ctorCalls)
}
