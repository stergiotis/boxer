package chatlet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/chat"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestEmbed_aRegisteredChatletInAHostsWindow(t *testing.T) {
	p, err := Find("audit-trail")
	require.NoError(t, err)
	assert.Equal(t, "audit-trail", Slug(p))

	pane, err := Embed("audit-trail", chat.EmbedConfig{Host: "github.com/example/host", Layout: chat.EmbedLayoutCompact})
	require.NoError(t, err)
	assert.Equal(t, "github.com/example/host#audit-trail", pane.Identity(), "the key defaults to the slug")

	_, err = Embed("no-such-chatlet", chat.EmbedConfig{Host: "github.com/example/host"})
	assert.Error(t, err)
	assert.Empty(t, Slug(chat.Preset{}), "a preset this package did not mint has no slug")
}

// A host's own document embeds without a listing: parsed, not registered.
func TestEmbed_anUnlistedDocument(t *testing.T) {
	src := []byte("---\ntitle: Notes\nsummary: Talk about the notes beside it\nallow: read\nchanges: ask\nreach: host\n---\n\nYou talk about the notes.\n")
	p, err := ParseDocSource("example-host", "notes.md", src, []app.TopicT{app.TopicData})
	require.NoError(t, err)
	pane, err := chat.NewEmbedded(p, chat.EmbedConfig{Host: "github.com/example/host", Key: Slug(p)})
	require.NoError(t, err)
	assert.Equal(t, "github.com/example/host#notes", pane.Identity())
	_, err = Find("notes")
	assert.Error(t, err, "it is in no registered book")
}
