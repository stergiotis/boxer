package styletokens

// Surface size archetypes (ADR-0065). Values are egui LOGICAL POINTS.
//
// Go-only — no Rust mirror, no drift test. Unlike spacing/rounding/stroke/
// motion/typography, these never reach egui::Style: keelson windows are
// created host-side from Go (windowhost passes the size to c.Window), so
// window sizing is a Go/windowhost concern, not a Rust styling one.
//
// Why points and not pixels: egui's pixels_per_point already maps logical
// points to the monitor's physical DPI, so these sizes must NOT scale with
// screen resolution — doing so would double-apply DPI. The "make the whole
// UI bigger/smaller" axis is density (Tight/Standard/Roomy), a user
// preference; surface sizes are deliberately density-independent in this
// first cut (see ADR-0065, which parks the density-aware option per the
// ADR-0032 §Q3 rationale).
//
// Why preferred and not max: egui clamps every window's size and position
// to the host window, so an archetype is a *preferred* open size with the
// host as the hard ceiling. They are role-based presets / guidance, not a
// straitjacket — an app with a genuinely content-driven size may still set
// SurfaceHints literally. ADR-0065 records the one deliberate exception
// (logdemo — a 720×280 log strip no archetype fits) and the host-relative
// + dimension-scale options that were considered and parked.
//
// Wire into a Manifest:
//
//	SurfaceHints: app.SurfaceHints{
//	    PreferredWidth:  styletokens.SurfaceWorkspace.W,
//	    PreferredHeight: styletokens.SurfaceWorkspace.H,
//	}

// SurfaceSize is a preferred window open-size in egui logical points,
// matching the units of app.SurfaceHints.PreferredWidth/Height.
type SurfaceSize struct {
	W uint16
	H uint16
}

var (
	// SurfaceInspector — a compact, accessory surface that tethers to a
	// caller widget (property inspectors, a regex explorer opened as an
	// inspector). Kept small so it stays near its anchor rather than
	// covering the host window. No current app is this small; it exists for
	// the tethered-inspector role that motivated ADR-0065.
	SurfaceInspector = SurfaceSize{W: 420, H: 560}
	// SurfaceTool — a focused, single-purpose window (config inspector,
	// small forms). The default for "one job, moderate content".
	SurfaceTool = SurfaceSize{W: 720, H: 600}
	// SurfaceApp — a medium application window sitting between Tool and
	// Workspace (help centers, catalogue/showcase surfaces). Added to cover
	// the ~860–960 pt middle band that neither Tool nor Workspace fits once
	// every app is migrated; the host-side windowDefaultSize fallback
	// resolves here too.
	SurfaceApp = SurfaceSize{W: 900, H: 640}
	// SurfaceWorkspace — a wide, data-dense workspace (tables, plots,
	// multi-pane explorers). The widest archetype; egui still clamps it to
	// the host window.
	SurfaceWorkspace = SurfaceSize{W: 1100, H: 720}
)
