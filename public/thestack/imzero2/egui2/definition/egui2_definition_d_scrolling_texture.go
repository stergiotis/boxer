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
		// ADR-0140 (Update 2026-08-28): the second hover-scoped wheel-capture
		// site. Same contract as paintCanvas — captureZoom is a pure per-id read,
		// captureScroll additionally zeroes the global smooth_scroll_delta so an
		// enclosing ScrollArea does not also scroll — read back per widget id via
		// StateManager.GetCanvasWheel. The widget already senses hover+click.
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("captureZoom").
			CodeClientRust(rustClientCode("capture_zoom = true;\n")).EndMethod().
			BeginMethod("captureScroll").
			CodeClientRust(rustClientCode("capture_scroll = true;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut capture_zoom = false;
let mut capture_scroll = false;
`)).
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
    if resp.fresh_texture {
        // The ring texture was (re)created this frame: any columns the
        // sender pushed while this widget went uninterpreted (hidden dock
        // tab) or while the idle LRU held its entry are gone. Report so
        // the Go side can reset its head honestly instead of desyncing
        // (fetchR22StarvedTextures).
        self.r22_starved_texture_ids.push({{Id}}.value());
    }
    self.r9_u64_push({{Id}}.value(), resp.hover_rc);
    self.r10_push({{Id}}.value(), resp.clicked);
    // ADR-0140 hover-scoped wheel capture, keyed by this widget's id: own the
    // wheel only while the pointer is over the texture rect; scroll is
    // consumed so the enclosing ScrollArea does not also scroll.
    if (capture_zoom || capture_scroll) && resp.contains_pointer {
        let mut wheel_scroll_x = 0.0f32;
        let mut wheel_scroll_y = 0.0f32;
        let mut wheel_zoom = 1.0f32;
        if capture_zoom {
            wheel_zoom = ui.input(|inp| inp.zoom_delta());
        }
        if capture_scroll {
            let sd = ui.input(|inp| inp.smooth_scroll_delta);
            wheel_scroll_x = sd.x;
            wheel_scroll_y = sd.y;
            ui.input_mut(|inp| inp.smooth_scroll_delta = egui::Vec2::ZERO);
        }
        self.r23_canvas_wheel_push({{Id}}.value(), wheel_scroll_x, wheel_scroll_y, wheel_zoom, resp.hover_x, resp.hover_y);
    }
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
