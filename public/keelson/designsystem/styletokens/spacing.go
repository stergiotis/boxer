package styletokens

// PxTable is the IDS spacing magnitude ladder (ADR-0032 §SD2).
// PxTable[index][density] — column order matches DensityE discriminants
// (Tight=0, Standard=1, Roomy=2). All values are multiples of 2 px (the
// 2 px grid invariant from ADR-0029 §SD6).
//
// MIRROR INVARIANT: must equal Rust PX_TABLE in
// src/rust/imzero2_egui/src/style/tokens/spacing.rs. The drift test in
// styletokens_drift_test.go enforces this.
var PxTable = [8][3]float32{
	// Tight, Standard, Roomy
	{2, 2, 4},    // Px[0]
	{2, 4, 6},    // Px[1]
	{4, 6, 8},    // Px[2]
	{6, 8, 12},   // Px[3]
	{8, 12, 16},  // Px[4]
	{12, 16, 24}, // Px[5]
	{16, 24, 32}, // Px[6]
	{24, 32, 48}, // Px[7]
}

// Px is the generic ladder accessor. Most callers should use the
// purpose-named helpers below.
func Px(d DensityE, idx uint8) (v float32) {
	v = PxTable[idx][d]
	return
}

// ---- Padding (inside a widget / container) ----

// PaddingHair returns Px[0] — hairline padding (tight inline content).
func PaddingHair(d DensityE) (v float32) { v = Px(d, 0); return }

// PaddingInner returns Px[1] — inside small widgets (button text padding,
// badge interior).
func PaddingInner(d DensityE) (v float32) { v = Px(d, 1); return }

// PaddingTight returns Px[2] — tight container padding.
func PaddingTight(d DensityE) (v float32) { v = Px(d, 2); return }

// PaddingDefault returns Px[3] — default control padding, inline gaps.
func PaddingDefault(d DensityE) (v float32) { v = Px(d, 3); return }

// PaddingOuter returns Px[4] — panel inner padding, card content.
func PaddingOuter(d DensityE) (v float32) { v = Px(d, 4); return }

// PaddingLoose returns Px[5] — generous panel padding, dialog content.
func PaddingLoose(d DensityE) (v float32) { v = Px(d, 5); return }

// ---- Gap (between sibling items) ----

// GapInline returns Px[2] — between inline items (chip stacks, label clusters).
func GapInline(d DensityE) (v float32) { v = Px(d, 2); return }

// GapItems returns Px[3] — between list items (table rows, menu items).
func GapItems(d DensityE) (v float32) { v = Px(d, 3); return }

// GapSections returns Px[5] — between major sections within a panel.
func GapSections(d DensityE) (v float32) { v = Px(d, 5); return }

// GapPanels returns Px[6] — between panels at the layout level.
func GapPanels(d DensityE) (v float32) { v = Px(d, 6); return }

// ---- Margin (outside a panel) ----

// MarginFrame returns Px[6] — outside panel margins (panel-to-window-edge).
func MarginFrame(d DensityE) (v float32) { v = Px(d, 6); return }

// ---- Derived ----

// ButtonPaddingY returns the padding above and below the text of a button,
// combo box or selectable label under the active theme. A label placed
// beside such a control in a top-aligned row takes it as its own top and
// bottom margin, so that its text reads on the control's line.
func ButtonPaddingY(d DensityE) (v float32) { return buttonPaddingY(d, ActiveTheme()) }

// buttonPaddingY mirrors imzero2_egui's style: the IDS spacing sets the
// button padding to PaddingInner (tokens/mod.rs apply_spacing), and the
// fresh theme adds freshButtonPadding to it (fresh.rs).
func buttonPaddingY(d DensityE, th ThemeE) (v float32) {
	v = PaddingInner(d)
	if th == ThemeFresh {
		v += freshButtonPadding
	}
	return
}

// freshButtonPadding is what the fresh theme adds to the button padding.
const freshButtonPadding = 2
