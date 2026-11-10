package chatlet

import (
	"strings"

	"github.com/stergiotis/boxer/apps/chat"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A chatlet embeds as a sqlapplet does (ADR-0368, proposed): a host draws
// the chat its document makes inside its own window, with the chat's
// capabilities on its own manifest (chat.EmbedCaps). A document of a
// registered book is listed in the launcher too; a host that wants one
// only embedded parses its own document with ParseDocSource and passes the
// preset to chat.NewEmbedded.

// Slug is the slug a minted preset's app id carries, empty for a preset
// this package did not mint.
func Slug(p chat.Preset) (slug string) {
	slug, _ = strings.CutPrefix(string(p.Manifest.Id), idPrefix)
	if slug == string(p.Manifest.Id) {
		return ""
	}
	return
}

// Find is the preset of the registered document whose slug is slug. It
// parses the registered books on each call; a host calls it once, when it
// builds its pane.
func Find(slug string) (p chat.Preset, err error) {
	for _, b := range snapshotBooks() {
		presets, _ := parseBook(b)
		for _, q := range presets {
			if Slug(q) == slug {
				return q, nil
			}
		}
	}
	return p, eb.Build().Str("slug", slug).Errorf("chatlet: no registered document has this slug")
}

// Embed is a pane of the registered chatlet slug inside the host cfg names.
// An empty cfg.Key takes the slug.
func Embed(slug string, cfg chat.EmbedConfig) (pane *chat.Pane, err error) {
	p, err := Find(slug)
	if err != nil {
		return
	}
	if cfg.Key == "" {
		cfg.Key = slug
	}
	return chat.NewEmbedded(p, cfg)
}
