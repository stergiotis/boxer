package colormap

// Named perceptually-uniform palettes, suitable as the Palette field of a
// Config. All entries are 0xRRGGBBAA with alpha 0xff (fully opaque). The
// sequential family (Viridis, Inferno, Magma, Plasma, Turbo, Cividis,
// Greys) walks from dark/cool at stop 0 to bright/warm at the last stop.
// The diverging family (RdBu) walks from blue at stop 0 through white at
// the midpoint to red at the last stop; use with data that has a natural
// zero (e.g. deviation from a baseline), with DataMin = -k, DataMax = +k.
//
// These are 8- or 9-stop samplings of the matplotlib/bokeh reference
// tables. Linear interpolation in Map produces a smooth gradient. Callers
// needing higher-fidelity lookup can supply their own longer palette;
// length does not affect Map's per-sample cost.

// Viridis8 — dark purple → teal → yellow. The default sequential palette.
// Perceptually uniform; safe for most colour-vision deficiencies.
var Viridis8 = []uint32{
	0x440154ff, 0x472d7bff, 0x3e4989ff, 0x31688eff,
	0x26838eff, 0x1f9d89ff, 0x6cce58ff, 0xfde725ff,
}

// Inferno8 — black → purple → red → yellow. Higher contrast at the dark
// end than Viridis; good on dark backgrounds.
var Inferno8 = []uint32{
	0x000004ff, 0x1b0c41ff, 0x4a0c6bff, 0x781c6dff,
	0xa52c60ff, 0xcf4446ff, 0xed6925ff, 0xfcfea4ff,
}

// Magma8 — black → magenta → orange → pale yellow. Warmer version of
// Inferno with a softer mid-range.
var Magma8 = []uint32{
	0x000004ff, 0x180f3dff, 0x440f76ff, 0x721f81ff,
	0x9e2f7fff, 0xcd4071ff, 0xf1605dff, 0xfcfdbfff,
}

// Plasma8 — deep purple → pink → orange → yellow. Higher saturation
// across the whole range than Viridis; eye-catching for comparison plots.
var Plasma8 = []uint32{
	0x0d0887ff, 0x5c02a6ff, 0x9c179eff, 0xcb4779ff,
	0xed7953ff, 0xfb9f3aff, 0xfdca26ff, 0xf0f921ff,
}

// Turbo8 — dark blue → cyan → yellow → orange → dark red. Google's
// rainbow-style palette; visually distinct but not strictly perceptually
// uniform. Use when the extra local contrast helps pattern-finding and
// absolute-value reading is not critical.
var Turbo8 = []uint32{
	0x30123bff, 0x4668e2ff, 0x1abafeff, 0x2fe5a3ff,
	0xa2fc3cff, 0xfabc2aff, 0xf56934ff, 0x7a0403ff,
}

// Cividis8 — dark blue → gold. Designed for protanopic/deuteranopic
// viewers specifically; safe for almost any colour-vision type.
var Cividis8 = []uint32{
	0x002051ff, 0x0e3573ff, 0x36486bff, 0x575c6dff,
	0x767469ff, 0x988e5fff, 0xbeac4cff, 0xfde725ff,
}

// Greys8 — linear grayscale, black at 0 to white at 1. Print-safe;
// useful for publications and grayscale export paths.
var Greys8 = []uint32{
	0x000000ff, 0x242424ff, 0x494949ff, 0x6d6d6dff,
	0x929292ff, 0xb6b6b6ff, 0xdbdbdbff, 0xffffffff,
}

// RdBu9 — diverging red → white → blue at 9 stops (matplotlib RdBu_r).
// Stop 4 is the white midpoint: a Config with DataMin = -k, DataMax = +k
// places zero exactly on white. Use for signed data where the sign of
// the deviation matters (e.g. residuals, correlation differences).
var RdBu9 = []uint32{
	0x67001fff, 0xb2182bff, 0xd6604dff, 0xf4a582ff,
	0xf7f7f7ff, 0x92c5deff, 0x4393c3ff, 0x2166acff,
	0x053061ff,
}
