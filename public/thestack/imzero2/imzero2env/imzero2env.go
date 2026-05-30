//go:build llm_generated_opus47

// Package imzero2env centralises the IMZERO2_* environment variables
// consumed across the imzero2 demo carousel, tours, and embedded
// applications. Each spec is registered with the boxer-wide registry
// (ADR-0058).
package imzero2env

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
)

// Render cadence values for [RenderCadence].
const (
	RenderCadenceContinuous = "continuous"
	RenderCadenceReactive   = "reactive"
)

var (
	// ScreenshotDir is the destination for per-window PNG captures
	// produced by the demo tour infrastructure. Empty means
	// "screenshot capture disabled".
	ScreenshotDir = env.NewPath(env.Spec{
		Name:        "IMZERO2_SCREENSHOT_DIR",
		Description: "destination directory for per-window PNG captures; empty disables capture",
		Category:    env.CategoryDev,
	})

	// ScreenshotDeterministic gates whether demos / tours with
	// non-deterministic content (time-of-day, live sysmetrics, randomised
	// initial state, etc.) participate in a screenshot capture run. Empty
	// = "include everything" (default — reviewers still see them).
	// Non-empty = "skip" — the widgets TestDriver omits demos tagged
	// with registry.DemoFlagNonDeterministic, and standalone tour
	// drivers (imztop, regex_explorer) return early so their PNGs aren't
	// produced. Used for byte-stable captures suitable for CI gating or
	// review diffs.
	ScreenshotDeterministic = env.NewString(env.Spec{
		Name:        "IMZERO2_SCREENSHOT_DETERMINISTIC",
		Description: "non-empty: skip non-deterministic demos / tours so captures are byte-stable across runs",
		Category:    env.CategoryDev,
	})

	// AllowNetwork gates the demo widgets that fetch external
	// resources (tiles, HTTP APIs). The legacy contract is "any
	// non-empty value other than \"1\" is treated as off".
	AllowNetwork = env.NewString(env.Spec{
		Name:        "IMZERO2_ALLOW_NETWORK",
		Description: "set to \"1\" to allow demo widgets to fetch external resources",
		Category:    env.CategoryDev,
	})

	// DebugMode selects an imzero2 launch profile: "memcheck",
	// "massif", "flamegraph", or "heaptrack". Empty means the default
	// launch path.
	DebugMode = env.NewString(env.Spec{
		Name:        "BOXER_IMZERO_DEBUG_MODE",
		Description: "imzero2 debug profile: memcheck|massif|flamegraph|heaptrack; empty uses the default launcher",
		Category:    env.CategoryDev,
	})

	// Density is the IDS density preset (tight | standard | roomy).
	// Case-insensitive; anything else is treated as "standard".
	Density = env.NewString(env.Spec{
		Name:        "IMZERO2_DENSITY",
		Description: "IDS density preset (tight|standard|roomy); empty defaults to standard",
		Category:    env.CategoryDev,
	})

	// ScreenshotSize is the canonical capture size override for tours.
	// Parsed as "WxH" (e.g. "1600x900"). When set, the widgets
	// TestDriver uses these dimensions as the stage rect for every
	// demo (overriding per-demo Demo.Stage values); standalone tours
	// (imztop, regex_explorer) switch from full-viewport capture to
	// rect-based capture at the same size. The launch wrapper
	// (src/rust/hmi.sh) widens the eframe viewport to fit so the
	// captured rect does not silently clip. ADR-0057 SD5.
	ScreenshotSize = env.NewString(env.Spec{
		Name:        "IMZERO2_SCREENSHOT_SIZE",
		Description: "tour capture size as WxH (e.g. 1600x900); empty uses per-demo defaults",
		Category:    env.CategoryDev,
	})

	// RenderCadence selects how the imzero2 frame loop schedules repaints
	// when the UI is idle (no input or animation):
	//   - "continuous" (default): request a repaint every pass, so the client
	//     paints at vsync rate. Most responsive; an occluded window is still
	//     throttled to the compositor's frame-callback rate for free, so this
	//     no longer floods the slow-frame log (that gate keys on real work,
	//     not wall-clock — see metrics.shouldWarnSlowFrame).
	//   - "reactive": after a short warmup, request only a slow idle heartbeat;
	//     egui still repaints immediately for input and animation, so
	//     interaction stays at vsync rate while a visible-but-idle window drops
	//     to a few fps, saving CPU/GPU.
	// Read by both the Go decorator (carousel.decorateRenderer) and the Rust
	// client (src/imzero2/app.rs), which inherits the variable as a child
	// process.
	RenderCadence = env.NewCategorialString(env.Spec{
		Name:        "IMZERO2_RENDER_CADENCE",
		Description: "frame-loop repaint cadence when idle: continuous (vsync rate) | reactive (idle heartbeat)",
		Category:    env.CategoryDev,
		Default:     RenderCadenceContinuous,
	}, []string{RenderCadenceContinuous, RenderCadenceReactive})
)

// ScreenshotSizeWH parses [ScreenshotSize] as "WxH". Returns (0,0,false)
// when the env var is unset, empty, or malformed; ok=true implies both
// dimensions are strictly positive. The 'x' separator is
// case-insensitive ("1600x900" and "1600X900" both parse).
func ScreenshotSizeWH() (w int32, h int32, ok bool) {
	raw := ScreenshotSize.Get()
	if raw == "" {
		return
	}
	idx := strings.IndexAny(raw, "xX")
	if idx <= 0 || idx == len(raw)-1 {
		return
	}
	wInt, wErr := strconv.ParseInt(raw[:idx], 10, 32)
	if wErr != nil || wInt <= 0 {
		return
	}
	hInt, hErr := strconv.ParseInt(raw[idx+1:], 10, 32)
	if hErr != nil || hInt <= 0 {
		return
	}
	w = int32(wInt)
	h = int32(hInt)
	ok = true
	return
}
