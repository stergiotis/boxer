package tally

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

//go:embed help
var helpFS embed.FS

// ManifestId is the durable app id (ADR-0200 §SD1). Renaming it is a
// deprecation event.
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/tally"

var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "tally",
	Title:    "tally — lading browser",
	Summary:  "Browse lading file-tree snapshots and open files from them",
	Icon:     icons.PhFolderOpen,
	Topics:   []app.TopicT{app.TopicData},
	Keywords: []string{"lading", "snapshot", "browser", "files", "fs", "mount"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1200,
		PreferredHeight: 760,
	},
	Caps: append([]app.SubjectFilter{
		{
			Pattern:   windowhost.OpenSubject,
			Reason:    "open the selection in play as a query over the store",
			Direction: app.CapDirectionPub,
		},
		{
			Pattern:   clipboardbroker.SubjectWrite,
			Reason:    "copy a snapshot path or an rclone mount command",
			Direction: app.CapDirectionPub,
		},
		// A recording's peaks build is a background job the task monitor
		// should list and be able to cancel (ADR-0208, ADR-0038).
	}, task.ProducerCaps()...),
	LaunchKind: launchcfg.Kind,
	Workingset: true,
	Help:       help.MustSub(helpFS, "help"),
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("tally: failed to register factory")
	}
}
