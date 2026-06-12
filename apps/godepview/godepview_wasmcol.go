package godepview

import (
	"fmt"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/packageprops"
	"github.com/stergiotis/boxer/public/packageprops/proptable"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The WASM column joins the live dependency graph against the generated
// whole-repo PackageProps table (ADR-0080). godepview renders the full closure
// (stdlib + external + first-party), but only the surveyed first-party packages
// carry a TinyGo/wasm verdict; everything else renders "—". The Table is static
// data embedded via proptable, so this needs no survey run at render time.

var (
	wasmPropsOnce   sync.Once
	wasmPropsByPath map[string]packageprops.Props
)

// wasmProps returns the import-path → declared Props map, built once from the
// generated Table.
func wasmProps() (m map[string]packageprops.Props) {
	wasmPropsOnce.Do(func() {
		wasmPropsByPath = make(map[string]packageprops.Props, len(proptable.Table))
		for _, e := range proptable.Table {
			wasmPropsByPath[e.ImportPath] = e.Props
		}
	})
	return wasmPropsByPath
}

// wasmGlyph maps a state to a compact cell glyph.
func wasmGlyph(s packageprops.WASMState) (g string) {
	switch s {
	case packageprops.WASMCompiles:
		return "✓" // U+2713, present in Noto Sans
	case packageprops.WASMBlocked:
		return "×" // U+00D7 — not U+2717 ✗, which renders as a tofu box in Noto
	default:
		return "·"
	}
}

// wasmCompileCount is the number of targets (of 3) that compile — the sort key
// and the summary-color driver. -1 means "no verdict declared".
func wasmCompileCount(importPath string) (n int) {
	p, ok := wasmProps()[importPath]
	if !ok {
		return -1
	}
	for _, s := range [...]packageprops.WASMState{p.WASMWASI, p.WASMJS, p.WASMFreestanding} {
		if s == packageprops.WASMCompiles {
			n++
		}
	}
	return n
}

// wasmSummaryHex picks the cell tint from how many targets compile: all-green,
// all-red, or amber for a mixed (target-dependent) verdict.
func wasmSummaryHex(n int) (rgba uint32) {
	switch {
	case n >= 3:
		return styletokens.SuccessDefault.AsHex()
	case n <= 0:
		return styletokens.ErrorDefault.AsHex()
	default:
		return styletokens.WarningDefault.AsHex()
	}
}

// renderWasmCell draws the per-target compile verdict (wasi js freestanding) as
// three glyphs tinted by the overall outcome; weak "—" when the package carries
// no declared verdict.
func (inst *App) renderWasmCell(importPath string) {
	p, ok := wasmProps()[importPath]
	if !ok {
		for rt := range c.RichTextLabel("—") {
			rt.Weak()
		}
		return
	}
	text := wasmGlyph(p.WASMWASI) + wasmGlyph(p.WASMJS) + wasmGlyph(p.WASMFreestanding)
	tint := egcolor.Hex(wasmSummaryHex(wasmCompileCount(importPath)))
	for range c.RichTextLabelColored(tint, egcolor.Transparent, text) {
	}
}

// renderWasmDetail draws the focused package's wasm verdict line in the detail
// pane (full words, all three targets).
func (inst *App) renderWasmDetail(importPath string) {
	p, ok := wasmProps()[importPath]
	if !ok {
		for rt := range c.RichTextLabel("wasm (TinyGo): not surveyed") {
			rt.Weak()
		}
		return
	}
	c.Label(fmt.Sprintf("wasm (TinyGo): wasi %s · js %s · freestanding %s",
		p.WASMWASI, p.WASMJS, p.WASMFreestanding)).Send()
}
