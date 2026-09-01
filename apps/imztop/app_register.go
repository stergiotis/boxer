package imztop

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
)

// manifest is the static AppI descriptor every imztop instance
// returns. Kept package-level so the factory ctor can hand a fresh
// instance back without re-running Manifest validation.
var manifest = app.Manifest{
	Id:      "github.com/stergiotis/boxer/apps/imztop",
	Version: "0.1.0",
	Display: "imztop",
	Title:   "imztop",
	Summary: "Watch live processes with CPU and memory usage",
	// Phosphor gauge — the system-monitor metaphor; rendered from the
	// Phosphor icon font registered at carousel startup (ADR-0044).
	Icon:     icons.PhGauge,
	Topics:   []app.TopicT{app.TopicObservability},
	Keywords: []string{"top", "htop", "process", "processes", "cpu", "memory"},
	Surface:  app.SurfaceWindowed,
	// imztop consumes the system-metrics plane (ADR-0090): it subscribes to a
	// scraper's published bundles and holds no filesystem or system-state
	// capability of its own. The host mints its MountCtx.Bus() client gated on
	// these caps.
	//
	// It is no longer capability-free. ADR-0197 added replay, which reads stored
	// history from ClickHouse, so the second entry below is a real reach — made
	// only when a user enters replay, never at Mount. ADR-0090's property was
	// about /proc and the ADR-0085 sandbox, which an outbound database read does
	// not reopen; but "imztop holds no capability" is no longer true and should
	// not be repeated.
	Caps: []app.SubjectFilter{
		{
			Pattern:   sysmetricsbus.SubjectWildcard,
			Direction: app.CapDirectionSub,
			Reason:    "subscribe to system metrics (CPU/mem/disk/net/proc/...)",
		},
		// Replay (ADR-0197 §SD7). This entry names an outbound reach, not a bus
		// route: nothing publishes on `ch.server.read.*`, and the host mints a
		// client for it that imztop never uses. The only `ch.` subject family
		// that is routed is `ch.local.exec.*`, which fronts clickhouse-local
		// (ADR-0028), not the server holding `boxer.facts`.
		//
		// It is declared because it is true — entering replay dials ClickHouse
		// from this process — and not because a gate demanded it. ADR-0026 §SD10
		// attributes a capability to an app only when its own code calls the
		// classified sink, and imztop reaches the database through non-stdlib
		// hops, so the gate stays green either way. A reviewer asking "what does
		// imztop connect to" should find the answer here rather than in a call
		// graph.
		{
			Pattern:   "ch.server.read.boxer.facts",
			Direction: app.CapDirectionPub,
			Reason:    "read stored system-metrics history for replay (ADR-0197)",
		},
	},
}

func init() {
	// imztop registers an interactive per-window *App (its own selected
	// network interface, etc.). Screenshot capture is handled centrally by
	// the widgets TestDriver via the Demos registered in imztop_tour.go
	// (ADR-0057).
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, err error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("imztop: failed to register factory")
	}
}
