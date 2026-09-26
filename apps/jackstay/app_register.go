package jackstay

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// AppId is the window's durable identity.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/jackstay"

// manifest declares the wizard of ADR-0259 §SD7. It talks to the two
// ClickHouse servers over HTTP directly, as the CLI does, and publishes on no
// bus subject; its only runtime state is the recent-plans list.
var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "jackstay",
	Title:    "jackstay — ClickHouse sync",
	Summary:  "Compare and sync tables from one ClickHouse server to another, step by step",
	Icon:     icons.PhArrowsLeftRight,
	Topics:   []app.TopicT{app.TopicData},
	Keywords: []string{"clickhouse", "sync", "copy", "migrate", "diff", "replicate", "leeway"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1280,
		PreferredHeight: 820,
	},
	// The plans this window used, for the first page's resume list.
	PersistedKeys: []string{recentKey},
}

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("jackstay app: failed to register factory")
	}
}
