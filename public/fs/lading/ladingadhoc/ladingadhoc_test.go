package ladingadhoc_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

func TestMintedMountsCarryTheAdhocTagAndDiffer(t *testing.T) {
	seen := make(map[identifier.TaggedId]struct{}, 64)
	for i := 0; i < 64; i++ {
		mount, err := ladingadhoc.MintMount()
		require.NoError(t, err)
		assert.True(t, mount.IsValid(), "a minted id must carry a fibonacci comma")
		assert.True(t, ladingadhoc.IsAdhoc(mount))
		assert.NotZero(t, mount.RemoveTag(), "the zero body is reserved")
		_, dup := seen[mount]
		assert.False(t, dup, "minted the same mount twice")
		seen[mount] = struct{}{}
	}
}

func TestIsAdhocRejectsOtherMounts(t *testing.T) {
	// A recorded mount minted under another tag, and a comma-less id.
	assert.False(t, ladingadhoc.IsAdhoc(identifier.TaggedId(0xF5F5019800020001)))
	assert.False(t, ladingadhoc.IsAdhoc(identifier.TaggedId(0)))
	assert.False(t, ladingadhoc.IsAdhoc(identifier.TaggedId(1)))
}

func TestVisibilityAdmitsOnlyAdhocMounts(t *testing.T) {
	v := ladingadhoc.Visibility()
	mount, err := ladingadhoc.MintMount()
	require.NoError(t, err)
	assert.True(t, v.VisibleMount(mount))
	assert.False(t, v.VisibleMount(identifier.TaggedId(0xF5F5019800020001)))
	// A tag is a space of ids, not a list, so the scope is opaque and a
	// query under it must name its mount — `fs('*')` is refused at
	// expansion (ADR-0200 §SD6).
	scope, ids := v.EnumerateMounts()
	assert.Equal(t, ladingsql.MountScopeOpaque, scope)
	assert.Empty(t, ids)
}

func TestPublishRefusesWhatItCannotPublish(t *testing.T) {
	tree := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("a")}}

	_, err := ladingadhoc.Publish(t.Context(), nil, ladingadhoc.PublishInput{Publisher: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tree")

	_, err = ladingadhoc.Publish(t.Context(), nil, ladingadhoc.PublishInput{FS: tree})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publisher")

	_, err = ladingadhoc.Publish(t.Context(), nil, ladingadhoc.PublishInput{FS: tree, Publisher: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no executor")
}

// Republishing is how a caller avoids leaving one mount behind per press, so
// the target has to be a mount this package minted: writing scratch rows into
// a recorded mount is exactly what the check is for.
func TestPublishRefusesToRepublishIntoARecordedMount(t *testing.T) {
	tree := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("a")}}
	_, err := ladingadhoc.Publish(t.Context(), nil, ladingadhoc.PublishInput{
		FS:        tree,
		Publisher: "x",
		Mount:     identifier.TaggedId(0xF5F5019800020001),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an ad-hoc mount")
}
