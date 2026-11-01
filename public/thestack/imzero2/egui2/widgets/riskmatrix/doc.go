// Package riskmatrix is an immediate-mode widget (ADR-0267) that draws a risk
// register as a likelihood × impact matrix. It was promoted out of the play
// pane of the same name (ADR-0122, 2026-11-01 update), which builds its Model
// from a query result; any host with intervals per risk can draw it.
//
// Each risk carries likelihood per year and impact as intervals, residual and
// optionally inherent. The widget draws them two ways, switched by State.Mode:
//
//   - Banded: the familiar grid, as one binning of the stored intervals. Band
//     labels are numeric and geometric ("1 in 10–30 yr", "500k–2.5m"), cells
//     grow toward the high-risk corner, and a cell's shade is the expected
//     annual loss at its geometric centre on one sequential ramp. Nothing
//     multiplies ordinal scores: the shade is likelihood times impact in the
//     units the register stores.
//   - Continuous: log–log axes with each risk's range as a box and lines of
//     equal expected annual loss — the ranges the cells hide.
//
// With State.ShowInherent an arrow runs from each risk's inherent position to
// its residual one. Bands carries the edges, labels and shade boundaries. The
// zero value is a 5×5 on likelihood edges of 1%, 3%, 10% and 30% a year and
// impact edges in steps of five, anchored on the register's magnitude;
// DefaultBands is the same at a fixed 100k.
//
// Impact has no unit here: Input.FormatImpact and Input.ImpactUnit say what the
// amounts are, and without them they are plain numbers. The expected-loss
// shading multiplies a rate by an impact, which assumes the unit is a ratio
// scale.
//
//	res := riskmatrix.Render(riskmatrix.Input{
//	    Ids: ids, ScopeKey: "register", Model: &model, State: &st,
//	    Width: 900, Height: 470,
//	})
//	if res.Clicked >= 0 { /* risk res.Clicked was clicked */ }
//
// It is painted on the painter substrate into one canvas, every colour from
// the design system. The shade classes come from an IDS sequential palette
// (an IDS_ACCESSIBILITY or IDS_PALETTE_SEQUENTIAL selection when one is set,
// Oslo otherwise, see rampSequential), cut per theme by selectRamp: each class farther from
// the canvas than the last, as far apart as the palette allows under normal
// sight and simulated dichromacy, and 3:1 from the dots. The dots are the
// theme's extreme ink, which no status colour comes near (newPalette says why
// no qualitative slot does).
//
// Every risk dot and every banded cell is a hit region (ADR-0149's hit
// regions): the host hit-tests it, shows its tooltip at the pointer, and
// gives it an accessibility node named "R1 · name" for a dot and by its two
// bands for a cell, so a reader or a scene finds a risk by name. The dots are
// Tab stops; Enter or Space on a focused dot is its click. Result.Hovered and
// Result.Clicked are read back from the regions and lag the pointer by one
// frame.
package riskmatrix
