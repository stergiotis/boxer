package widgets

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// =============================================================================
// U64Edit — exact 64-bit integer input, next to the widgets that can't do it.
//
// A tagged id / hash / bitmask is always > 2^53. egui's DragValue and Slider
// are f64 scrubbers by construction — they funnel every value through
// Numeric::to_f64/from_f64 (lossy above 2^53) and their hex formatter casts the
// f64 to i64 (saturates at i64::MAX = 0x7fffffffffffffff). c.U64Edit is
// TextEdit-backed and parses/formats the value exactly across the whole range.
//
// This scene shows one bit pattern in all three widgets so the corruption is
// visible side by side; the U64Edit fields stay exact and editable, the two
// scrubbers clamp. "Reset all" re-seeds every field — U64Edit re-seeds exactly
// (and drops the frontend's cached buffer via the Stubborn-Text override),
// while the scrubbers re-clamp.
// =============================================================================

// u64demoSeed is the wide bit pattern the scene demonstrates: > 2^63, so both
// scrubbers round it AND their hex path saturates.
const u64demoSeed uint64 = 0xDEADBEEFCAFEF00D

// Per-widget values. Separate vars because each widget's SendRespVal writes its
// own (possibly rounded) value back — sharing one var would let the scrubbers
// corrupt the exact fields. Package-level: this stateless demo shares state
// across gallery windows, which is fine for a read-mostly showcase.
var (
	u64demoHex  = u64demoSeed
	u64demoDec  = u64demoSeed
	u64demoDrag = u64demoSeed
)

func init() {
	registry.Register(registry.Demo{
		Name:        "u64-edit",
		Category:    "Layout & widgets",
		Title:       "u64 edit — exact 64-bit integer",
		Stage:       [2]float32{860, 460},
		Kind:        registry.DemoKindMixed,
		Description: "c.U64Edit: an exact 64-bit integer field (decimal or 0x-hex, TextEdit-backed) for ids/hashes/bitmasks, shown against DragValueU64 and SliderU64 — both f64 scrubbers that round the value and clamp the hex to i64::MAX.",
		Render:      demoU64Edit,
	})
}

func demoU64Edit(ids *c.WidgetIdStack) {
	stdSection("the same 64-bit value in three widgets",
		"seed = 0xDEADBEEFCAFEF00D (> 2^63). Edit the U64Edit fields — decimal or 0x-hex, parsed exactly. The DragValue below cannot represent it (SliderU64 shares the identical f64 core).")

	// U64Edit — hex display. Exact across the full uint64 range.
	for range c.Horizontal().KeepIter() {
		c.Label("U64Edit (hex)   ").Send()
		c.U64Edit(ids.PrepareStr("u64-hex"), u64demoHex).
			Hex().DesiredWidth(240).HintText("id — decimal or 0x-hex").
			SendRespVal(&u64demoHex)
		c.Label(fmt.Sprintf("→ 0x%016X = %d  ✓ exact", u64demoHex, u64demoHex)).Send()
	}

	// U64Edit — decimal display. Same exactness, different rendering.
	for range c.Horizontal().KeepIter() {
		c.Label("U64Edit (dec)   ").Send()
		c.U64Edit(ids.PrepareStr("u64-dec"), u64demoDec).
			DesiredWidth(240).HintText("id — decimal or 0x-hex").
			SendRespVal(&u64demoDec)
		c.Label(fmt.Sprintf("→ 0x%016X = %d  ✓ exact", u64demoDec, u64demoDec)).Send()
	}

	c.Separator().Send()

	// DragValueU64 — f64-backed. Its hex formatter saturates at i64::MAX.
	for range c.Horizontal().KeepIter() {
		c.Label("DragValueU64    ").Send()
		c.DragValueU64(ids.PrepareStr("u64-drag"), u64demoDrag).
			Hexadecimal(16, false, false).Speed(0).
			SendRespVal(&u64demoDrag)
		c.Label("✗ f64→i64 clamp: shows 7fffffffffffffff, not …f00d").Send()
	}

	c.Separator().Send()

	if c.Button(ids.PrepareStr("u64-reset"), c.Atoms().Text("Reset all").Keep()).
		SendResp().HasPrimaryClicked() {
		// External write to the U64Edit values: they re-seed exactly and the
		// frontend's cached text is dropped by U64Edit's OverrideDatabindingSPtr.
		u64demoHex = u64demoSeed
		u64demoDec = u64demoSeed
		u64demoDrag = u64demoSeed
	}
}
