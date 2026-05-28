//go:build llm_generated_opus47

// Package imzero2env centralises the IMZERO2_* environment variables
// consumed across the imzero2 demo carousel, tours, and embedded
// applications. Each spec is registered with the boxer-wide registry
// (ADR-0009).
package imzero2env

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
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
	// captured rect does not silently clip. ADR-0008 SD5.
	ScreenshotSize = env.NewString(env.Spec{
		Name:        "IMZERO2_SCREENSHOT_SIZE",
		Description: "tour capture size as WxH (e.g. 1600x900); empty uses per-demo defaults",
		Category:    env.CategoryDev,
	})
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
