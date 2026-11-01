package servicedemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// ManifestId is the app id, for a predicate or a seed to name.
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/servicedemo"

// manifest declares the service: headless, with the one persist key the
// host turns into a `runtime.persist` cap at mount.
var manifest = app.Manifest{
	Id:            ManifestId,
	Version:       "0.1.0",
	Display:       "Tick service",
	Summary:       "Count ticks in the background and keep the count",
	Topics:        []app.TopicT{app.TopicRuntime},
	Keywords:      []string{"service", "headless", "background", "tick"},
	Kind:          app.KindDemo,
	Surface:       app.SurfaceHeadless,
	PersistedKeys: []string{TickKey},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = New(0)
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("servicedemo: failed to register factory")
	}
}
