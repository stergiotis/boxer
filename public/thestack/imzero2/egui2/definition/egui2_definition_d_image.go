package definition

// =============================================================================
// image binding — RGBA8 pixel-data widget with Go-controlled content version
// and Rust-side texture cache. Sibling to scrollingTexture (ADR-0058) but
// without the ring-buffer; one upload per (id, contentVersion) shape.
// =============================================================================
//
// Wire contract (option C + send-side skip):
//
//   - Go ships full pixels only when content changed. When `contentVersion`
//     matches the version Go last sent for this widget id, Go ships
//     `pixels=[]uint32{}` (empty, NOT nil — see FFFI2 nil-sentinel asymmetry)
//     to mean "draw the cached texture, don't re-upload".
//   - Rust caches `(TextureHandle, w, h, content_version)` keyed by widget id
//     and decides on the CACHE KEY ALONE: it re-uploads when there is no
//     entry, when `contentVersion` moved, or when `(w, h)` changed. Matching
//     key ⇒ the cached texture is drawn and the buffer is ignored, whether or
//     not Go shipped pixels. An empty buffer with no cached entry is "no
//     draw" and reports starvation so the sender re-ships.
//
// The cost of shipping pixels Go did not have to ship is therefore WIRE
// BANDWIDTH — one memcpy each way — and NOT a GPU re-upload. A caller
// pinning `contentVersion` and re-sending every frame (the markdown widget
// does exactly this, deliberately, so one Doc can render under several id
// scopes) pays that memcpy per frame: negligible for icons, the wrong cost
// model for full-page screenshots. This comment used to claim the opposite —
// that a non-empty buffer always forces a re-upload — which taught two
// downstream comments a cost model the interpreter never had.
//
// Hover readout (per SD11 in ADR-0058): packed as `(row << 32) | col` in
// image-pixel space (NOT screen pixels — caller doesn't have to invert the
// fit math). Sentinel `u64::MAX` = pointer outside widget rect. Forwarded to
// `r9_u64` keyed by the widget id.
//
// Click+hover bits flow through the standard r7 ResponseFlags pipeline (the
// apply code calls `apply_response_to_r7` rather than `apply_widget`, since
// `egui::Image` doesn't fit `apply_widget`'s `Widget` bound for our
// allocate-then-paint draw path).
//
// Opcodes:
//   - image        — draw at the current ui cursor; upload pixels iff changed
//   - imageRelease — drop the cache entry explicitly (LRU reaps otherwise)
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsImage() (nodes []*ir.BuilderFactoryNode) {
	nodes = make([]*ir.BuilderFactoryNode, 0, 2)

	// image — show RGBA pixels, conditional re-upload by contentVersion.
	nodes = append(nodes, idl.NewBuilderFactoryNode("image").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("widthPx", ctabb.U32).
			PlainArg("heightPx", ctabb.U32).
			PlainArg("contentVersion", ctabb.U64).
			PlainArg("fit", ctabb.U8).
			PlainArg("fixedW", ctabb.U32).
			PlainArg("fixedH", ctabb.U32).
			PlainArg("filter", ctabb.U8).
			PlainArg("tintRgba", ctabb.U32).
			PlainArg("pixels", ctabb.U32h).
			Build()).
		WithConstructionCodeClientRust(rustClientCode(`0u8;`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let (resp, hover_rc, starved) = self.image_cache.show(
        ui,
        c,
        {{Id}}.value(),
        width_px,
        height_px,
        content_version,
        fit,
        fixed_w,
        fixed_h,
        filter,
        tint_rgba,
        &pixels,
    );
    if starved {
        // Interpreted with no pixels and no cache entry — report so the
        // Go-side sender re-arms and re-ships (fetchR22StarvedTextures).
        self.r22_starved_texture_ids.push({{Id}}.value());
    }
    if self.r8_response_flags_filter.match_response_any(&resp) {
        let mut res = ResponseFlags::empty();
        res.populate(&resp);
        self.r7_push({{Id}}.value(), res);
    }
    self.r9_u64_push({{Id}}.value(), hover_rc);
}
`)).
		WithSettingImmediate(true).
		WithReturnType(structImage()).
		Build())

	// imageRelease — explicit LRU override for lifecycle-managed callers.
	nodes = append(nodes, idl.NewBuilderFactoryNode("imageRelease").
		WithIdentityId(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.image_cache.release({{Id}}.value());
`)).
		WithSettingImmediate(true).
		WithReturnType(structImage()).
		Build())

	return
}
