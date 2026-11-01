package trailviewer

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

//go:embed help
var helpFS embed.FS

// manifest is the static descriptor every window returns.
var manifest = runtimeapp.Manifest{
	Id:       launchcfg.AppId,
	Version:  "0.1.0",
	Display:  "Trail viewer",
	Title:    "Trail Viewer",
	Summary:  "Read the audit trail by case and time window, with the evidence verdict per run",
	Icon:     icons.PhFootprints,
	Topics:   []runtimeapp.TopicT{runtimeapp.TopicRuntime, runtimeapp.TopicObservability},
	Keywords: []string{"trail", "audit", "evidence", "seal", "timeline", "review"},
	Surface:  runtimeapp.SurfaceWindowed,
	// The time range picker evaluates expressions it cannot read itself
	// through the chlocal pool (ADR-0016 Phase 4).
	Caps: []runtimeapp.SubjectFilter{{
		Pattern:   chlocalbroker.SubjectExecPrefix + timerangepicker.PoolName,
		Direction: runtimeapp.CapDirectionPub,
		Reason:    "evaluate the time range picker's expressions (ADR-0016 Phase 4)",
	}, {
		Pattern:   adhocdata.SubjectPublish,
		Direction: runtimeapp.CapDirectionPub,
		Reason:    "Send to play: publish the table's rows as an ad-hoc dataset (ADR-0350 §SD9, ADR-0134)",
	}, {
		Pattern:   windowhost.OpenSubject,
		Direction: runtimeapp.CapDirectionPub,
		Reason:    "Send to play: open a play window on the published rows (ADR-0135)",
	}},
	// A launch config opens the window on a case (launchcfg); chat's Open
	// trail sends one for a conversation.
	LaunchKind: launchcfg.Kind,
	// The saved views, one small JSON document under the app id (views.go).
	PersistedKeys: []string{viewsKey},
	// The operations catalog (ADR-0350 §SD8), in trailviewer_ops.go.
	Operations: ops.Catalog(),
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  styletokens.SurfaceWorkspace.W,
		PreferredHeight: styletokens.SurfaceWorkspace.H,
	},
	Help: help.MustSub(helpFS, "help"),
}

func init() {
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, ctorErr error) {
		a = newInstance(manifest)
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("trailviewer: failed to register factory")
	}
}
