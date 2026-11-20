// Package hello is the smallest windowed app: one window, one value kept
// across runs, and a help page. It is the code the first-app tutorial
// builds (doc/tutorials/first-app.md) and has no purpose beyond that.
package hello

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// helpFS embeds the help book (apps/hello/help/*.md) the help host shows
// on F1.
//
//go:embed help/*.md
var helpFS embed.FS

// AppId is the app's manifest id.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/hello"

// GreetingsKey is the persist key the greeting count is kept under.
const GreetingsKey = "greetings"

var manifest = app.Manifest{
	Id:            AppId,
	Version:       "0.1.0",
	Display:       "Hello",
	Summary:       "Greet someone and count the greetings across runs",
	Topics:        []app.TopicT{app.TopicRuntime},
	Keywords:      []string{"hello", "tutorial", "example"},
	Kind:          app.KindDemo,
	Surface:       app.SurfaceWindowed,
	PersistedKeys: []string{GreetingsKey},
	Help:          help.MustSub(helpFS, "help"),
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  360,
		PreferredHeight: 200,
	},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("hello: failed to register factory")
	}
}
