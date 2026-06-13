package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsSpecial() (specials []ir.NodeI) {
	specials = make([]ir.NodeI, 0, 16)
	specials = append(specials, idl.NewProceduralNode("end").
		WithApplyCodeClientRust(rustClientCode("r = true;\n")).
		Build())
	specials = append(specials, idl.NewProceduralNode("requestRepaint").
		WithApplyCodeClientRust(rustClientCode("{{EguiContext}}.request_repaint();\n")).
		Build())
	specials = append(specials, idl.NewProceduralNode("showPuffinProfiler").
		WithApplyCodeClientRust(rustClientCode(`
//#[cfg(feature = "puffin")]
//puffin_egui::profiler_window({{EguiContext}}); // FIXME problem with egui version in puffin_egui crate
`)).
		Build())
	specials = append(specials, idl.NewProceduralNode("showDebugTools").
		WithApplyCodeClientRust(rustClientCode(`
				if {{EguiUiOptionalOuter}}.is_some() {
					self.render_debug_tools(c, {{EguiUiOptionalOuter}}.as_mut().unwrap());
                }
`)).
		Build())
	specials = append(specials, idl.NewProceduralNode("prepareNextFrame").
		WithApplyCodeClientRust(rustClientCode("self.prepare_next_frame();\n")).
		Build())
	specials = append(specials,
		idl.NewProceduralNode("passthrough").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("input", ctabb.U64).
				Build()).
			WithApplyCodeClientRust(rustClientCode("self.r9_u64_push({{Id}}.value(),input+1);\n")).
			Build())
	specials = append(specials,
		idl.NewProceduralNode("memoryResetAreas").
			WithApplyCodeClientRust(rustClientCode("{{EguiContext}}.memory_mut(|mem| mem.reset_areas());")).Build())
	// captureAvailableSize — snapshots ui.available_size() into r18 fields
	// so a Go-side widget can read the parent panel's available width/height
	// next frame via fetchR18AvailableSize and auto-fit. NaN sentinels when
	// no Ui is in scope (caller must invoke inside a panel / window body).
	specials = append(specials,
		idl.NewProceduralNode("captureAvailableSize").
			WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let s = ui.available_size();
    self.r18_avail_w = s.x;
    self.r18_avail_h = s.y;
} else {
    self.r18_avail_w = f32::NAN;
    self.r18_avail_h = f32::NAN;
}
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("guiZoomZoomMenuButtons").
			WithApplyCodeClientRust(rustClientCode(`
				if {{EguiUiOptionalOuter}}.is_some() {
					egui::gui_zoom::zoom_menu_buttons({{EguiUiOptionalOuter}}.as_mut().unwrap());
                }
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("widgetsGlobalThemePreferenceButtons").
			WithApplyCodeClientRust(rustClientCode(`
			if {{EguiUiOptionalOuter}}.is_some() {
				egui::widgets::global_theme_preference_buttons({{EguiUiOptionalOuter}}.as_mut().unwrap());
			}
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("contextSendViewPortCommandClose").
			WithApplyCodeClientRust(rustClientCode("{{EguiContext}}.send_viewport_cmd(egui::ViewportCommand::Close);")).Build())
	specials = append(specials,
		idl.NewProceduralNode("contextInspectionUi").
			WithApplyCodeClientRust(rustClientCode(`
			if {{EguiUiOptionalOuter}}.is_some() {
				{{EguiContext}}.inspection_ui({{EguiUiOptionalOuter}}.as_mut().unwrap());
			}
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("warnIfDebugBuild").
			WithApplyCodeClientRust(rustClientCode(`
			if {{EguiUiOptionalOuter}}.is_some() {
				egui::warn_if_debug_build({{EguiUiOptionalOuter}}.as_mut().unwrap());
			}
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("requestScreenshot").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("path", ctabb.S).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			{{EguiContext}}.send_viewport_cmd(egui::ViewportCommand::Screenshot(egui::UserData::new(path)));
`)).Build())
	// Vector SVG export. Queues `path` into ImZeroFffi::export_state; the
	// SvgExportPlugin (registered in App::new) drains the request in
	// on_end_pass — same pass as this opcode, after FFFI dispatch finishes
	// but before tessellation — and walks Context::graphics() into a
	// self-contained SVG file. Returns nothing on the FFFI wire; failures
	// land in tracing logs.
	//
	// `embedFonts=false` is the lightweight default: SVG references the
	// loaded face by name (`'Noto Sans', sans-serif`) and depends on the
	// viewer having a matching font installed (Tier 1).
	//
	// `embedFonts=true` runs a glyph-collection pre-pass, subsets each
	// used face to just the chars the frame paints, and base64-embeds the
	// subsetted TTFs as `@font-face`. The SVG becomes self-contained and
	// pixel-faithful at the cost of a one-time ~30–80 KB per used face.
	specials = append(specials,
		idl.NewProceduralNode("exportSvg").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("path", ctabb.S).
				PlainArg("embedFonts", ctabb.B).
				PlainArg("bgRgba", ctabb.U32).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let bg = if (bg_rgba & 0xff) == 0 {
				None
			} else {
				Some(egui::Color32::from_rgba_unmultiplied(
					((bg_rgba >> 24) & 0xff) as u8,
					((bg_rgba >> 16) & 0xff) as u8,
					((bg_rgba >> 8) & 0xff) as u8,
					(bg_rgba & 0xff) as u8,
				))
			};
			self.export_state
				.lock()
				.expect("svg_export state poisoned")
				.pending = Some(crate::imzero2::svgexport::ExportRequest {
				path: std::path::PathBuf::from(path),
				embed_fonts,
				scope: crate::imzero2::svgexport::ExportScope::Viewport,
				bg,
			});
`)).Build())
	// Single-window SVG export (M1 — companion to `exportSvg`, picks one
	// window's Middle-order layer by widget id and uses its area_rect as
	// the viewBox). Overlays spawned by that window (tooltips, combo
	// dropdowns, context menus) live on higher-order layers and are
	// intentionally excluded — the scope is the window-as-document, not
	// the user-visible composite. If the window has no recorded area_rect
	// this pass (collapsed, off-screen, never opened) the export fails
	// with a tracing error and no file is written.
	specials = append(specials,
		idl.NewProceduralNode("exportSvgWindow").
			WithIdentityIdReference().
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("path", ctabb.S).
				PlainArg("embedFonts", ctabb.B).
				PlainArg("mode", ctabb.U8).
				PlainArg("bgRgba", ctabb.U32).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let window_mode = if mode == 1 {
				crate::imzero2::svgexport::WindowMode::ContentOnly
			} else {
				crate::imzero2::svgexport::WindowMode::Faithful
			};
			let bg = if (bg_rgba & 0xff) == 0 {
				None
			} else {
				Some(egui::Color32::from_rgba_unmultiplied(
					((bg_rgba >> 24) & 0xff) as u8,
					((bg_rgba >> 16) & 0xff) as u8,
					((bg_rgba >> 8) & 0xff) as u8,
					(bg_rgba & 0xff) as u8,
				))
			};
			self.export_state
				.lock()
				.expect("svg_export state poisoned")
				.pending = Some(crate::imzero2::svgexport::ExportRequest {
				path: std::path::PathBuf::from(path),
				embed_fonts,
				scope: crate::imzero2::svgexport::ExportScope::Window {
					id: {{Id}},
					mode: window_mode,
				},
				bg,
			});
`)).Build())
	// Cropped-region screenshot. The rect is in logical points (pre-DPI); the
	// handler multiplies by pixels_per_point before slicing the ColorImage.
	// Used by the deterministic TestDriver to capture each demo's fixed stage
	// rect without neighbour chrome bleed.
	specials = append(specials,
		idl.NewProceduralNode("requestScreenshotRect").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("path", ctabb.S).
				PlainArg("rectX", ctabb.F32).
				PlainArg("rectY", ctabb.F32).
				PlainArg("rectW", ctabb.F32).
				PlainArg("rectH", ctabb.F32).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let req = crate::imzero2::interpreter::ScreenshotRequest {
				path,
				rect: Some(egui::Rect::from_min_size(
					egui::pos2(rect_x, rect_y),
					egui::vec2(rect_w, rect_h),
				)),
			};
			{{EguiContext}}.send_viewport_cmd(egui::ViewportCommand::Screenshot(egui::UserData::new(req)));
`)).Build())
	// Flip the interpreter-wide animation-freeze flag. When on, the three
	// animate_* opcodes below and the ProgressBar / ScrollArea animated
	// methods snap to their target value instead of tweening. TestDriver
	// sets this to true at startup for pixel-stable captures.
	specials = append(specials,
		idl.NewProceduralNode("setAnimationFreeze").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("freeze", ctabb.B).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			self.animation_freeze = freeze;
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("moveWindowToTop").
			WithIdentityIdReference().
			WithApplyCodeClientRust(rustClientCode(`
			{{EguiContext}}.move_to_top(egui::LayerId::new(egui::Order::Middle, {{Id}}));
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("setWindowCollapsed").
			WithIdentityIdReference().
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("collapsed", ctabb.B).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let collapsing_id = {{Id}}.with("collapsing");
			let mut state = egui::collapsing_header::CollapsingState::load_with_default_open({{EguiContext}}, collapsing_id, true);
			state.set_open(!collapsed);
			state.store({{EguiContext}});
`)).Build())
	// Animation primitives — see egui::Context::animate_*. Each tween is keyed by
	// `animId`; pass a stable id so egui's AnimationManager remembers from-value
	// across frames. Result (current 0..1 value) is pushed to r9_f64 keyed by
	// `animId`; the Go-side wrapper registers a databinding to read it next Sync.
	specials = append(specials,
		idl.NewProceduralNode("animateBoolWithTime").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("animId", ctabb.U64).
				PlainArg("target", ctabb.B).
				PlainArg("durSecs", ctabb.F32).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let aid = egui::Id::new(anim_id);
			let val = if self.animation_freeze {
				if target { 1.0f32 } else { 0.0f32 }
			} else {
				{{EguiContext}}.animate_bool_with_time(aid, target, dur_secs)
			};
			self.r9_f64_push(anim_id, val as f64);
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("animateBoolResponsive").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("animId", ctabb.U64).
				PlainArg("target", ctabb.B).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let aid = egui::Id::new(anim_id);
			let val = if self.animation_freeze {
				if target { 1.0f32 } else { 0.0f32 }
			} else {
				{{EguiContext}}.animate_bool_responsive(aid, target)
			};
			self.r9_f64_push(anim_id, val as f64);
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("animateValueWithTime").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("animId", ctabb.U64).
				PlainArg("target", ctabb.F32).
				PlainArg("durSecs", ctabb.F32).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let aid = egui::Id::new(anim_id);
			let val = if self.animation_freeze {
				target
			} else {
				{{EguiContext}}.animate_value_with_time(aid, target, dur_secs)
			};
			self.r9_f64_push(anim_id, val as f64);
`)).Build())
	specials = append(specials,
		idl.NewProceduralNode("requestRepaintAfter").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("durSecs", ctabb.F64).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			{{EguiContext}}.request_repaint_after(std::time::Duration::from_secs_f64(dur_secs));
`)).Build())
	// Text measurement — layout `text` in the given font and push the bounding
	// width to r9_f64 keyed by `measureId`. The Go-side wrapper registers a
	// databinding so callers receive the width one frame after the call.
	// Needed by widgets that position labels precisely (axis ticks, legends,
	// overlap-aware tick selection).
	specials = append(specials,
		idl.NewProceduralNode("measureText").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("measureId", ctabb.U64).
				PlainArg("text", ctabb.S).
				PlainArg("fontSize", ctabb.F32).
				PlainArg("monospace", ctabb.B).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let font_id = if monospace {
				egui::FontId::monospace(font_size)
			} else {
				egui::FontId::proportional(font_size)
			};
			// layout_no_wrap needs &mut FontsView (galley cache is mutable);
			// hence fonts_mut rather than fonts.
			let width = {{EguiContext}}.fonts_mut(|f| {
				f.layout_no_wrap(text, font_id, egui::Color32::WHITE).rect.width()
			});
			self.r9_f64_push(measure_id, width as f64);
`)).Build())
	// measureTextSize — measureText's two-extent sibling: one layout pass,
	// width pushed under widthMeasureId and height under heightMeasureId.
	// The height of a single non-wrapped line is the font's row height
	// (content-independent), which is what cell-sizing callers need (the
	// treemap label gates measure a short probe string once per text style).
	// Two explicit ids rather than a derived second id (measureId|1 etc.):
	// callers commonly hash-derive measure ids, where a bit-trick sibling id
	// is collision-prone; explicit ids are collision-free by construction.
	specials = append(specials,
		idl.NewProceduralNode("measureTextSize").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("widthMeasureId", ctabb.U64).
				PlainArg("heightMeasureId", ctabb.U64).
				PlainArg("text", ctabb.S).
				PlainArg("fontSize", ctabb.F32).
				PlainArg("monospace", ctabb.B).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
			let font_id = if monospace {
				egui::FontId::monospace(font_size)
			} else {
				egui::FontId::proportional(font_size)
			};
			// layout_no_wrap needs &mut FontsView (galley cache is mutable);
			// hence fonts_mut rather than fonts.
			let size = {{EguiContext}}.fonts_mut(|f| {
				f.layout_no_wrap(text, font_id, egui::Color32::WHITE).rect.size()
			});
			self.r9_f64_push(width_measure_id, size.x as f64);
			self.r9_f64_push(height_measure_id, size.y as f64);
`)).Build())
	// captureUiRect — snapshots the current ui.min_rect() into r21 parallel
	// vectors keyed by seq, so a Go-side caller can read multiple Ui rects
	// per frame via fetchR21UiRects. Used by the bezier-connector affordance
	// to thread an inspector window's viewport-absolute rect back to the
	// chip that drives it. No-op when no Ui is in scope.
	//
	// Caveats:
	//   - Captures ui.min_rect (bbox of placed widgets so far in the Ui),
	//     NOT ui.max_rect or any outer egui::Window response rect. Inside
	//     a c.Window body, this is the WINDOW'S CONTENT AREA — title bar
	//     and frame padding are NOT included.
	//   - Inside a wrapping Horizontal / FlexWrap, min_rect spans all
	//     rows the layout has produced; for a chip-row that's expected to
	//     stay on one line, callers should keep the row narrow enough not
	//     to wrap (otherwise the bezier endpoint emerges from the BOTTOM-
	//     RIGHT chip, not the rightmost chip of the first row).
	//   - Within one frame, every captureUiRect appends to r21 without
	//     dedup; duplicate seqs produce two rows and fetchR21UiRects
	//     returns both (Go-side last-write-wins in the StateManager map).
	specials = append(specials,
		idl.NewProceduralNode("captureUiRect").
			AddArguments(idl.NewArgumentsBuilder().
				PlainArg("seq", ctabb.U64).
				Build()).
			WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let r = ui.min_rect();
    self.r21_ui_rect_seqs.push(seq);
    self.r21_ui_rect_min_x.push(r.min.x);
    self.r21_ui_rect_min_y.push(r.min.y);
    self.r21_ui_rect_max_x.push(r.max.x);
    self.r21_ui_rect_max_y.push(r.max.y);
    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_x.len());
    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_y.len());
    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_x.len());
    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_y.len());
}
`)).Build())
	// paintAbsoluteOverlay — drains paint_cmds into an Order::Foreground
	// viewport-absolute painter that sits above every egui::Window. Use
	// when a connector / annotation / debug-overlay needs to cross window
	// boundaries; ordinary PaintCanvas allocates a Ui-scoped painter
	// clipped to its parent. The overlay's clip is the full screen_rect
	// so the bezier endpoint can land on any window anywhere on screen.
	//
	// Coordinates are viewport-absolute (origin = Pos2::ZERO when handed
	// to the shared drain helper). All paint-cmd variants supported by
	// PaintCanvas are handled here via the same helper; only SenseRegion
	// is skipped (interactive sensing requires a Ui scope which the
	// foreground overlay does not have) and logs a tracing warning.
	specials = append(specials,
		idl.NewProceduralNode("paintAbsoluteOverlay").
			WithApplyCodeClientRust(rustClientCode(`
{
    let screen = {{EguiContext}}.screen_rect();
    let layer_id = egui::LayerId::new(egui::Order::Foreground, egui::Id::new("imzero-absolute-overlay"));
    let painter = egui::Painter::new({{EguiContext}}.clone(), layer_id, screen);
    self.drain_paint_cmds_to_painter(&painter, egui::Pos2::ZERO, None);
}
`)).Build())
	return
}
