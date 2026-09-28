package jackstay

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// AppId is the window's durable identity.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/jackstay"

// manifest declares the wizard of ADR-0259 §SD7. It talks to the two
// ClickHouse servers over HTTP directly, as the CLI does. Its plans and their
// journals live in its data area, which the fs broker owns; a plan enters or
// leaves it only through a file dialog the user answers. Its runtime state
// besides is the recent-plans list.
var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "jackstay",
	Title:    "jackstay — ClickHouse sync",
	Summary:  "Compare and sync tables between two ClickHouse servers, step by step",
	Icon:     icons.PhArrowsLeftRight,
	Topics:   []app.TopicT{app.TopicData},
	Keywords: []string{"clickhouse", "sync", "copy", "migrate", "diff", "replicate", "leeway"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1280,
		PreferredHeight: 820,
	},
	Caps: []app.SubjectFilter{
		{
			Pattern:   fsbroker.SubjectAppDataPrefix + ">",
			Direction: app.CapDirectionPub,
			Reason:    "jackstay: keep plans and their sync journals in the window's data area",
		},
		{
			Pattern:   fsbroker.SubjectDialogRead,
			Direction: app.CapDirectionPub,
			Reason:    "jackstay: Import — raise the file picker to copy in a plan the user chooses",
		},
		{
			Pattern:   fsbroker.SubjectDialogWrite,
			Direction: app.CapDirectionPub,
			Reason:    "jackstay: Export — raise the save picker to write a copy of the plan",
		},
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
