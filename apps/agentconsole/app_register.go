// Package agentconsole is a coordinator driven by hand (ADR-0269): a window
// that asks for a task grant over another window and calls that window's
// operations through the host's runtime.agent services, showing every
// outcome. It is the developer's way to exercise an app's catalog — and the
// window host's frame, queue and dispatcher — without a model; the chat app
// becomes the coordinator a model drives.
//
// A grant needs the person's approval in host chrome. Where the host shows
// none, the console works only on the headless host with
// BOXER_AGENT_TEST_GRANTS set, which scenes do.
package agentconsole

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// AppId is the console's manifest id.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/agentconsole"

var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "Agent console",
	Summary:  "Call another window's operations by hand under a task grant",
	Icon:     icons.PhTerminalWindow,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"agent", "operations", "grant", "dispatcher", "adr-0269"},
	Kind:     app.KindDemo,
	Surface:  app.SurfaceWindowed,
	Caps: append(agent.ClientCaps("agentconsole: call operations by hand"),
		app.SubjectFilter{Pattern: windowhost.OpenSubject, Direction: app.CapDirectionPub,
			Reason: "agentconsole: open a window to work in"}),
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  640,
		PreferredHeight: 560,
	},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("agentconsole: failed to register factory")
	}
}
