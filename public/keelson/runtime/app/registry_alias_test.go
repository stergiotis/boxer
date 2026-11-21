package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func factoryOf(m Manifest) (ctor AppCtor) {
	return func() (AppI, error) { return &plainApp{m: m}, nil }
}

func TestAnUnsetAliasIsTheDerivedOne(t *testing.T) {
	m := testManifest("github.com/x/apps/play")
	assert.Equal(t, "play", m.SubjectAlias())
	m.Alias = "acme_play"
	assert.Equal(t, "acme_play", m.SubjectAlias())
}

func TestADuplicateAliasNamesBothIds(t *testing.T) {
	reg := NewRegistry()
	a := testManifest("github.com/a/apps/play")
	b := testManifest("github.com/b/apps/play")
	require.NoError(t, reg.RegisterFactory(a, factoryOf(a)))
	err := reg.RegisterFactory(b, factoryOf(b))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.com/a/apps/play")
	assert.Contains(t, err.Error(), "github.com/b/apps/play")
	b.Alias = "b_play"
	require.NoError(t, reg.RegisterFactory(b, factoryOf(b)))
	assert.Equal(t, "b_play", reg.AliasOf(b.Id))
	assert.Equal(t, "play", reg.AliasOf(a.Id))
	assert.Equal(t, "other", reg.AliasOf("github.com/c/other"), "an unregistered id derives its alias")
}

func TestAnExplicitAliasCollidesWithADerivedOne(t *testing.T) {
	reg := NewRegistry()
	a := testManifest("github.com/a/apps/play")
	b := testManifest("github.com/b/apps/tally")
	b.Alias = "play"
	require.NoError(t, reg.RegisterFactory(a, factoryOf(a)))
	require.Error(t, reg.RegisterFactory(b, factoryOf(b)))
}

func TestAMalformedAliasIsRefused(t *testing.T) {
	for _, alias := range []string{"a.b", "a b", "a>", "*"} {
		m := testManifest("github.com/a/apps/x")
		m.Alias = alias
		assert.Error(t, m.Validate(), alias)
	}
}
