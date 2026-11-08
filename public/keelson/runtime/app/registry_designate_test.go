package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTaker registers an app declaring kind as its LaunchKind.
func newTaker(t *testing.T, reg *Registry, id AppIdT, kind string) {
	t.Helper()
	m := testManifest(id)
	m.LaunchKind = kind
	a, err := NewLegacyFuncApp(m, func() (err error) { return })
	require.NoError(t, err)
	require.NoError(t, reg.Register(a))
}

func TestDesignateForKind_RecordsTheDesignation(t *testing.T) {
	reg := NewRegistry()
	newTaker(t, reg, "org.test.a", "someKind")
	newTaker(t, reg, "org.test.b", "someKind")
	require.NoError(t, reg.DesignateForKind("someKind", "org.test.a"))

	id, ok := reg.Designation("someKind")
	require.True(t, ok)
	assert.Equal(t, AppIdT("org.test.a"), id)
	_, ok = reg.Designation("otherKind")
	assert.False(t, ok)
}

func TestDesignateForKind_RefusesAnUnregisteredApp(t *testing.T) {
	reg := NewRegistry()
	err := reg.DesignateForKind("someKind", "org.test.absent")
	assert.Error(t, err)
	_, ok := reg.Designation("someKind")
	assert.False(t, ok)
}

func TestDesignateForKind_RefusesAnAppThatDoesNotDeclareTheKind(t *testing.T) {
	reg := NewRegistry()
	newTaker(t, reg, "org.test.a", "otherKind")
	assert.Error(t, reg.DesignateForKind("someKind", "org.test.a"))
}

func TestDesignateForKind_RefusesWhenRepeatedForTheKind(t *testing.T) {
	reg := NewRegistry()
	newTaker(t, reg, "org.test.a", "someKind")
	newTaker(t, reg, "org.test.b", "someKind")
	require.NoError(t, reg.DesignateForKind("someKind", "org.test.a"))
	assert.Error(t, reg.DesignateForKind("someKind", "org.test.b"), "a second designation is refused, not resolved by call order")
	assert.Error(t, reg.DesignateForKind("someKind", "org.test.a"), "repeating the same designation is refused too")
	id, _ := reg.Designation("someKind")
	assert.Equal(t, AppIdT("org.test.a"), id)
}

func TestDesignateForKind_RefusesAnEmptyKind(t *testing.T) {
	reg := NewRegistry()
	newTaker(t, reg, "org.test.a", "someKind")
	assert.Error(t, reg.DesignateForKind("", "org.test.a"))
}
