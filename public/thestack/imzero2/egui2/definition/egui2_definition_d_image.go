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
//   - Rust caches `(TextureHandle, w, h, content_version)` keyed by widget id.
//     A non-empty pixel buffer with the same `(w, h, contentVersion)` triggers
//     re-upload (defensive — Go shouldn't send pixels when version matches);
//     an empty buffer with no cached entry is treated as "no draw".
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
    let (resp, hover_rc) = self.image_cache.show(
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
