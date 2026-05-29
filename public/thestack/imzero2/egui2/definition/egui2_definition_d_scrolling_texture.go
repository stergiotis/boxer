//go:build llm_generated_opus47

package definition

// =============================================================================
// scrollingTexture binding — see doc/adr/0009-imzero2-scrolling-texture-widget.md
// =============================================================================
//
// Purpose-built pixel-data widget: ring-buffer of RGBA columns, caller-owned
// head cursor, split-UV two-call draw. Colormap, intensity scaling, and
// bad/underflow/overflow substitution all live Go-side in the `colormap`
// package; this IDL carries only raw pre-packed RGBA (see ADR-0058 SD9 for
// why bulk pixel buffers bypass egui2.Color / ADR-0052).
//
// Opcodes:
//   - scrollingTexture        — write new columns at `head` + draw
//   - scrollingTextureRelease — drop the cache entry explicitly (LRU reaps otherwise)
//
// Milestone 2.5 wiring: the Rust apply code calls into the hand-written
// module `src/rust/src/imzero2/scrolling_texture.rs`, captures the returned
// `ScrollingTextureResponse`, and forwards `hover_rc` (packed row:col u64 or
// sentinel u64::MAX) into `r9_u64` plus `clicked` into `r10` keyed by the
// widget id — per SD11/SD12.
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func structScrollingTexture() ir.ConcreteType {
	return ir.NewConcreteType("scrollingTexture")
}

func definitionsScrollingTexture() (nodes []*ir.BuilderFactoryNode) {
	nodes = make([]*ir.BuilderFactoryNode, 0, 2)

	// scrollingTexture — per-frame write+draw for the ring-buffer texture.
	nodes = append(nodes, idl.NewBuilderFactoryNode("scrollingTexture").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("widthSlots", ctabb.U32).
			PlainArg("heightSlots", ctabb.U32).
			PlainArg("orientation", ctabb.U8).
			PlainArg("filter", ctabb.U8).
			PlainArg("head", ctabb.U32).
			PlainArg("newCount", ctabb.U32).
			PlainArg("newColumns", ctabb.U32h).
			PlainArg("displayWidthPx", ctabb.F32).
			PlainArg("displayHeightPx", ctabb.F32).
			Build()).
		WithConstructionCodeClientRust(rustClientCode(`0u8;`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let resp = self.scrolling_texture.push_and_draw(
        ui,
        c,
        {{Id}}.value(),
        width_slots,
        height_slots,
        orientation,
        filter,
        head,
        new_count,
        &new_columns,
        display_width_px,
        display_height_px,
    );
    self.r9_u64_push({{Id}}.value(), resp.hover_rc);
    self.r10_push({{Id}}.value(), resp.clicked);
}
`)).
		WithSettingImmediate(true).
		WithReturnType(structScrollingTexture()).
		Build())

	// scrollingTextureRelease — explicit LRU override for lifecycle-managed callers.
	nodes = append(nodes, idl.NewBuilderFactoryNode("scrollingTextureRelease").
		WithIdentityId(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.scrolling_texture.release({{Id}}.value());
`)).
		WithSettingImmediate(true).
		WithReturnType(structScrollingTexture()).
		Build())

	return
}
