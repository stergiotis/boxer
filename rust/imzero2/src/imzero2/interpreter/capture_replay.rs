//! Capture replay (ADR-0281 §SD5): re-run recorded opcode bytes into a
//! separate, input-less egui context without disturbing the live frame.
//!
//! The interpreter is shared with the live frame, so a replay is fenced:
//! - writes toward the server fail (`ImZeroFffiIo::capture_replay`), and the
//!   apply code of host-effect nodes and fetchers is skipped by the generated
//!   guard on `capture_replay`;
//! - the image and scrolling-texture caches upload nothing and change no
//!   live entry;
//! - the per-frame registers start empty and the live ones are put back
//!   afterwards; state that outlives a frame (dock layouts, column-width
//!   epochs, window open bindings) is seen by the replay and restored after
//!   it, so the replay cannot move it.

use super::{ImZeroFffi, InterpretResult};
use super::{PaintCmd, TableCell, WindowGeomRow};
use crate::imzero2::code_view;
use crate::imzero2::fenums::ResponseFlags;
use crate::imzero2::svgexport::LinkZone;
use crate::imzero2::text_edit_highlight;

/// The live interpreter state a replay must not leave changed.
struct SavedLiveState<'a> {
    r0_atoms: egui::Atoms<'a>,
    r1_widget_text: egui::WidgetText,
    r5_id_set: roaring::RoaringTreemap,
    r7_ids: Vec<u64>,
    r7_responses: Vec<ResponseFlags>,
    r9_u64_ids: Vec<u64>,
    r9_u64_values: Vec<u64>,
    r9_f64_ids: Vec<u64>,
    r9_f64_values: Vec<f64>,
    r9_i64_ids: Vec<u64>,
    r9_i64_values: Vec<i64>,
    r9_s_ids: Vec<u64>,
    r9_s_values: Vec<String>,
    r22_starved_texture_ids: Vec<u64>,
    text_edit_pending_insert: Option<String>,
    text_edit_pending_highlight: Option<code_view::CodeViewJobData>,
    text_edit_pending_styled: Option<Vec<text_edit_highlight::StyledSection>>,
    text_edit_pending_set_cursor: Option<(u64, bool)>,
    video_pipeline_request: Option<u8>,
    video_cap_ids: Vec<u64>,
    video_cap_flags: Vec<u32>,
    video_stream_info: Vec<u64>,
    r9_et_prefetch_ids: Vec<u64>,
    r9_et_prefetch_values: Vec<u64>,
    r10_true_ids: Vec<u64>,
    r10_false_ids: Vec<u64>,
    pending_window_place: std::collections::HashMap<u64, egui::Rect>,
    r27_windows: Vec<WindowGeomRow>,
    table_columns: Vec<egui_extras::Column>,
    table_header_texts: Vec<String>,
    table_cells: Vec<TableCell>,
    et_columns: Vec<egui_table::Column>,
    et_header_texts: Vec<String>,
    et_row_heights: Vec<f32>,
    new_table_columns: Vec<egui_extras::Column>,
    new_table_row_heights: Vec<f32>,
    paint_cmds: Vec<PaintCmd>,
    r12_code_view_job: code_view::CodeViewJobData,
    r24_styled_sections: Vec<text_edit_highlight::StyledSection>,
    r23_canvas_wheel_ids: Vec<u64>,
    r23_canvas_wheel_scroll_x: Vec<f32>,
    r23_canvas_wheel_scroll_y: Vec<f32>,
    r23_canvas_wheel_zoom: Vec<f32>,
    r23_canvas_wheel_hover_x: Vec<f32>,
    r23_canvas_wheel_hover_y: Vec<f32>,
    r24_canvas_pointer_ids: Vec<u64>,
    r24_canvas_pointer_origin_x: Vec<f32>,
    r24_canvas_pointer_origin_y: Vec<f32>,
    r24_canvas_pointer_pos_x: Vec<f32>,
    r24_canvas_pointer_pos_y: Vec<f32>,
    r24_canvas_pointer_mods: Vec<u8>,
    r26_key_capture_ids: Vec<u64>,
    r26_key_capture_codes: Vec<u8>,
    r26_key_capture_mods: Vec<u8>,
    r26_key_capture_edges: Vec<u8>,
    r21_ui_rect_seqs: Vec<u64>,
    r21_ui_rect_min_x: Vec<f32>,
    r21_ui_rect_min_y: Vec<f32>,
    r21_ui_rect_max_x: Vec<f32>,
    r21_ui_rect_max_y: Vec<f32>,
    r25_et_colwidth_ids: Vec<u64>,
    r25_et_colwidth_counts: Vec<u64>,
    r25_et_colwidth_values: Vec<f32>,
    message_offsets: Vec<usize>,
    message_lengths: Vec<u32>,
    message_func_proc_ids_raw: Vec<u32>,
    text_edit_pending_no_wrap: bool,
    text_edit_pending_report_cursor: bool,
    text_edit_pending_capture_tab: bool,
    text_edit_pending_capture_keys: u64,
    scratch_open_binding_id: u64,
    scratch_window_maximized: bool,
    r27_work_rect: egui::Rect,
    r11_color32: egui::Color32,
    r8_response_flags_filter: ResponseFlags,
    r18_avail_w: f32,
    r18_avail_h: f32,
    last_good_func_proc_id_raw: Option<u32>,
    last_good_msg_len: Option<u32>,
    last_good_byte_offset: Option<usize>,
    frame_trail: [(u32, u32, usize); 16],
    frame_trail_head: usize,
    frame_trail_count: u64,
    animation_freeze: bool,
    last_interpret_us: u32,
    last_pass_nr: u64,
    read_blocked_ns: u64,
    window_open_bindings: std::collections::HashMap<u64, bool>,
    width_epochs: std::collections::HashMap<u64, u32>,
    dock_states: std::collections::HashMap<u64, egui_dock::DockState<u64>>,
    link_zones: Vec<LinkZone>,
}

/// What a capture replay did besides drawing.
#[derive(Debug, Default)]
pub struct CaptureReplayReport {
    /// Texture uploads the caches refused; each is a texture the live
    /// context did not hold, so the capture shows a hole there.
    pub refused_uploads: u64,
    /// Hyperlink zones the replay painted, for an SVG of the capture.
    pub link_zones: Vec<LinkZone>,
}

/// An egui context to replay into, initialised from the live one. `Memory`
/// goes first: it also holds the options and any pending font definitions,
/// so copying it after them would discard both. The live context must have
/// run a pass: before its first, it holds no fonts to copy.
pub fn capture_context(live: &egui::Context) -> egui::Context {
    let ctx = egui::Context::default();
    ctx.memory_mut(|m| *m = live.memory(|l| l.clone()));
    ctx.options_mut(|o| *o = live.options(|l| l.clone()));
    ctx.set_fonts(live.fonts(|f| f.definitions().clone()));
    ctx
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    fn save_live_state(&mut self) -> SavedLiveState<'a> {
        let link_zones =
            self.link_zones.lock().map(|mut z| std::mem::take(&mut *z)).unwrap_or_default();
        SavedLiveState {
            r0_atoms: std::mem::take(&mut self.r0_atoms),
            r1_widget_text: std::mem::take(&mut self.r1_widget_text),
            r5_id_set: std::mem::take(&mut self.r5_id_set),
            r7_ids: std::mem::take(&mut self.r7_ids),
            r7_responses: std::mem::take(&mut self.r7_responses),
            r9_u64_ids: std::mem::take(&mut self.r9_u64_ids),
            r9_u64_values: std::mem::take(&mut self.r9_u64_values),
            r9_f64_ids: std::mem::take(&mut self.r9_f64_ids),
            r9_f64_values: std::mem::take(&mut self.r9_f64_values),
            r9_i64_ids: std::mem::take(&mut self.r9_i64_ids),
            r9_i64_values: std::mem::take(&mut self.r9_i64_values),
            r9_s_ids: std::mem::take(&mut self.r9_s_ids),
            r9_s_values: std::mem::take(&mut self.r9_s_values),
            r22_starved_texture_ids: std::mem::take(&mut self.r22_starved_texture_ids),
            text_edit_pending_insert: std::mem::take(&mut self.text_edit_pending_insert),
            text_edit_pending_highlight: std::mem::take(&mut self.text_edit_pending_highlight),
            text_edit_pending_styled: std::mem::take(&mut self.text_edit_pending_styled),
            text_edit_pending_set_cursor: std::mem::take(&mut self.text_edit_pending_set_cursor),
            video_pipeline_request: std::mem::take(&mut self.video_pipeline_request),
            video_cap_ids: std::mem::take(&mut self.video_cap_ids),
            video_cap_flags: std::mem::take(&mut self.video_cap_flags),
            video_stream_info: std::mem::take(&mut self.video_stream_info),
            r9_et_prefetch_ids: std::mem::take(&mut self.r9_et_prefetch_ids),
            r9_et_prefetch_values: std::mem::take(&mut self.r9_et_prefetch_values),
            r10_true_ids: std::mem::take(&mut self.r10_true_ids),
            r10_false_ids: std::mem::take(&mut self.r10_false_ids),
            pending_window_place: std::mem::take(&mut self.pending_window_place),
            r27_windows: std::mem::take(&mut self.r27_windows),
            table_columns: std::mem::take(&mut self.table_columns),
            table_header_texts: std::mem::take(&mut self.table_header_texts),
            table_cells: std::mem::take(&mut self.table_cells),
            et_columns: std::mem::take(&mut self.et_columns),
            et_header_texts: std::mem::take(&mut self.et_header_texts),
            et_row_heights: std::mem::take(&mut self.et_row_heights),
            new_table_columns: std::mem::take(&mut self.new_table_columns),
            new_table_row_heights: std::mem::take(&mut self.new_table_row_heights),
            paint_cmds: std::mem::take(&mut self.paint_cmds),
            r12_code_view_job: std::mem::take(&mut self.r12_code_view_job),
            r24_styled_sections: std::mem::take(&mut self.r24_styled_sections),
            r23_canvas_wheel_ids: std::mem::take(&mut self.r23_canvas_wheel_ids),
            r23_canvas_wheel_scroll_x: std::mem::take(&mut self.r23_canvas_wheel_scroll_x),
            r23_canvas_wheel_scroll_y: std::mem::take(&mut self.r23_canvas_wheel_scroll_y),
            r23_canvas_wheel_zoom: std::mem::take(&mut self.r23_canvas_wheel_zoom),
            r23_canvas_wheel_hover_x: std::mem::take(&mut self.r23_canvas_wheel_hover_x),
            r23_canvas_wheel_hover_y: std::mem::take(&mut self.r23_canvas_wheel_hover_y),
            r24_canvas_pointer_ids: std::mem::take(&mut self.r24_canvas_pointer_ids),
            r24_canvas_pointer_origin_x: std::mem::take(&mut self.r24_canvas_pointer_origin_x),
            r24_canvas_pointer_origin_y: std::mem::take(&mut self.r24_canvas_pointer_origin_y),
            r24_canvas_pointer_pos_x: std::mem::take(&mut self.r24_canvas_pointer_pos_x),
            r24_canvas_pointer_pos_y: std::mem::take(&mut self.r24_canvas_pointer_pos_y),
            r24_canvas_pointer_mods: std::mem::take(&mut self.r24_canvas_pointer_mods),
            r26_key_capture_ids: std::mem::take(&mut self.r26_key_capture_ids),
            r26_key_capture_codes: std::mem::take(&mut self.r26_key_capture_codes),
            r26_key_capture_mods: std::mem::take(&mut self.r26_key_capture_mods),
            r26_key_capture_edges: std::mem::take(&mut self.r26_key_capture_edges),
            r21_ui_rect_seqs: std::mem::take(&mut self.r21_ui_rect_seqs),
            r21_ui_rect_min_x: std::mem::take(&mut self.r21_ui_rect_min_x),
            r21_ui_rect_min_y: std::mem::take(&mut self.r21_ui_rect_min_y),
            r21_ui_rect_max_x: std::mem::take(&mut self.r21_ui_rect_max_x),
            r21_ui_rect_max_y: std::mem::take(&mut self.r21_ui_rect_max_y),
            r25_et_colwidth_ids: std::mem::take(&mut self.r25_et_colwidth_ids),
            r25_et_colwidth_counts: std::mem::take(&mut self.r25_et_colwidth_counts),
            r25_et_colwidth_values: std::mem::take(&mut self.r25_et_colwidth_values),
            message_offsets: std::mem::take(&mut self.message_offsets),
            message_lengths: std::mem::take(&mut self.message_lengths),
            message_func_proc_ids_raw: std::mem::take(&mut self.message_func_proc_ids_raw),
            text_edit_pending_no_wrap: self.text_edit_pending_no_wrap,
            text_edit_pending_report_cursor: self.text_edit_pending_report_cursor,
            text_edit_pending_capture_tab: self.text_edit_pending_capture_tab,
            text_edit_pending_capture_keys: self.text_edit_pending_capture_keys,
            scratch_open_binding_id: self.scratch_open_binding_id,
            scratch_window_maximized: self.scratch_window_maximized,
            r27_work_rect: self.r27_work_rect,
            r11_color32: self.r11_color32,
            r8_response_flags_filter: self.r8_response_flags_filter,
            r18_avail_w: self.r18_avail_w,
            r18_avail_h: self.r18_avail_h,
            last_good_func_proc_id_raw: self.last_good_func_proc_id_raw,
            last_good_msg_len: self.last_good_msg_len,
            last_good_byte_offset: self.last_good_byte_offset,
            frame_trail: self.frame_trail,
            frame_trail_head: self.frame_trail_head,
            frame_trail_count: self.frame_trail_count,
            animation_freeze: self.animation_freeze,
            last_interpret_us: self.last_interpret_us,
            last_pass_nr: self.last_pass_nr,
            read_blocked_ns: self.read_blocked_ns,
            window_open_bindings: self.window_open_bindings.clone(),
            width_epochs: self.width_epochs.clone(),
            dock_states: self.dock_states.clone(),
            link_zones,
        }
    }

    /// Puts the live state back and returns the link zones the replay left.
    fn restore_live_state(&mut self, s: SavedLiveState<'a>) -> Vec<LinkZone> {
        self.r0_atoms = s.r0_atoms;
        self.r1_widget_text = s.r1_widget_text;
        self.r5_id_set = s.r5_id_set;
        self.r7_ids = s.r7_ids;
        self.r7_responses = s.r7_responses;
        self.r9_u64_ids = s.r9_u64_ids;
        self.r9_u64_values = s.r9_u64_values;
        self.r9_f64_ids = s.r9_f64_ids;
        self.r9_f64_values = s.r9_f64_values;
        self.r9_i64_ids = s.r9_i64_ids;
        self.r9_i64_values = s.r9_i64_values;
        self.r9_s_ids = s.r9_s_ids;
        self.r9_s_values = s.r9_s_values;
        self.r22_starved_texture_ids = s.r22_starved_texture_ids;
        self.text_edit_pending_insert = s.text_edit_pending_insert;
        self.text_edit_pending_highlight = s.text_edit_pending_highlight;
        self.text_edit_pending_styled = s.text_edit_pending_styled;
        self.text_edit_pending_set_cursor = s.text_edit_pending_set_cursor;
        self.video_pipeline_request = s.video_pipeline_request;
        self.video_cap_ids = s.video_cap_ids;
        self.video_cap_flags = s.video_cap_flags;
        self.video_stream_info = s.video_stream_info;
        self.r9_et_prefetch_ids = s.r9_et_prefetch_ids;
        self.r9_et_prefetch_values = s.r9_et_prefetch_values;
        self.r10_true_ids = s.r10_true_ids;
        self.r10_false_ids = s.r10_false_ids;
        self.pending_window_place = s.pending_window_place;
        self.r27_windows = s.r27_windows;
        self.table_columns = s.table_columns;
        self.table_header_texts = s.table_header_texts;
        self.table_cells = s.table_cells;
        self.et_columns = s.et_columns;
        self.et_header_texts = s.et_header_texts;
        self.et_row_heights = s.et_row_heights;
        self.new_table_columns = s.new_table_columns;
        self.new_table_row_heights = s.new_table_row_heights;
        self.paint_cmds = s.paint_cmds;
        self.r12_code_view_job = s.r12_code_view_job;
        self.r24_styled_sections = s.r24_styled_sections;
        self.r23_canvas_wheel_ids = s.r23_canvas_wheel_ids;
        self.r23_canvas_wheel_scroll_x = s.r23_canvas_wheel_scroll_x;
        self.r23_canvas_wheel_scroll_y = s.r23_canvas_wheel_scroll_y;
        self.r23_canvas_wheel_zoom = s.r23_canvas_wheel_zoom;
        self.r23_canvas_wheel_hover_x = s.r23_canvas_wheel_hover_x;
        self.r23_canvas_wheel_hover_y = s.r23_canvas_wheel_hover_y;
        self.r24_canvas_pointer_ids = s.r24_canvas_pointer_ids;
        self.r24_canvas_pointer_origin_x = s.r24_canvas_pointer_origin_x;
        self.r24_canvas_pointer_origin_y = s.r24_canvas_pointer_origin_y;
        self.r24_canvas_pointer_pos_x = s.r24_canvas_pointer_pos_x;
        self.r24_canvas_pointer_pos_y = s.r24_canvas_pointer_pos_y;
        self.r24_canvas_pointer_mods = s.r24_canvas_pointer_mods;
        self.r26_key_capture_ids = s.r26_key_capture_ids;
        self.r26_key_capture_codes = s.r26_key_capture_codes;
        self.r26_key_capture_mods = s.r26_key_capture_mods;
        self.r26_key_capture_edges = s.r26_key_capture_edges;
        self.r21_ui_rect_seqs = s.r21_ui_rect_seqs;
        self.r21_ui_rect_min_x = s.r21_ui_rect_min_x;
        self.r21_ui_rect_min_y = s.r21_ui_rect_min_y;
        self.r21_ui_rect_max_x = s.r21_ui_rect_max_x;
        self.r21_ui_rect_max_y = s.r21_ui_rect_max_y;
        self.r25_et_colwidth_ids = s.r25_et_colwidth_ids;
        self.r25_et_colwidth_counts = s.r25_et_colwidth_counts;
        self.r25_et_colwidth_values = s.r25_et_colwidth_values;
        self.message_offsets = s.message_offsets;
        self.message_lengths = s.message_lengths;
        self.message_func_proc_ids_raw = s.message_func_proc_ids_raw;
        self.text_edit_pending_no_wrap = s.text_edit_pending_no_wrap;
        self.text_edit_pending_report_cursor = s.text_edit_pending_report_cursor;
        self.text_edit_pending_capture_tab = s.text_edit_pending_capture_tab;
        self.text_edit_pending_capture_keys = s.text_edit_pending_capture_keys;
        self.scratch_open_binding_id = s.scratch_open_binding_id;
        self.scratch_window_maximized = s.scratch_window_maximized;
        self.r27_work_rect = s.r27_work_rect;
        self.r11_color32 = s.r11_color32;
        self.r8_response_flags_filter = s.r8_response_flags_filter;
        self.r18_avail_w = s.r18_avail_w;
        self.r18_avail_h = s.r18_avail_h;
        self.last_good_func_proc_id_raw = s.last_good_func_proc_id_raw;
        self.last_good_msg_len = s.last_good_msg_len;
        self.last_good_byte_offset = s.last_good_byte_offset;
        self.frame_trail = s.frame_trail;
        self.frame_trail_head = s.frame_trail_head;
        self.frame_trail_count = s.frame_trail_count;
        self.animation_freeze = s.animation_freeze;
        self.last_interpret_us = s.last_interpret_us;
        self.last_pass_nr = s.last_pass_nr;
        self.read_blocked_ns = s.read_blocked_ns;
        self.window_open_bindings = s.window_open_bindings;
        self.width_epochs = s.width_epochs;
        self.dock_states = s.dock_states;
        self.link_zones
            .lock()
            .map(|mut z| std::mem::replace(&mut *z, s.link_zones))
            .unwrap_or_default()
    }

    /// Replays `spans` — whole messages of one recorded frame, in order —
    /// against `ctx`, a capture context from [`capture_context`], inside its
    /// pass. Call it from that context's `run_ui`.
    pub fn replay_for_capture(
        &mut self,
        ctx: &egui::Context,
        spans: &[&[u8]],
    ) -> (InterpretResult<()>, CaptureReplayReport) {
        let saved = self.save_live_state();
        let refused_before = self.image_cache.refused_uploads
            + self.paint_image_cache.refused_uploads
            + self.scrolling_texture.refused_uploads;
        self.capture_replay = true;
        self.io.capture_replay = true;
        self.image_cache.read_only = true;
        self.paint_image_cache.read_only = true;
        self.scrolling_texture.read_only = true;

        let mut root_ui = egui::Ui::new(
            ctx.clone(),
            egui::Id::new((ctx.viewport_id(), "__imzero2_root")),
            egui::UiBuilder::new()
                .layer_id(egui::LayerId::background())
                .max_rect(ctx.viewport_rect()),
        );
        let mut result = Ok(());
        for span in spans {
            self.io.begin_replay(span);
            let mut root = Some(&mut root_ui);
            // A span holds whole messages; the loop ends when they are read,
            // or when a pass makes no progress.
            while self.io.replay_remaining() > 0 {
                let before = self.io.replay_remaining();
                result = self.interpret_outer(ctx, &mut root);
                if result.is_err() || self.io.replay_remaining() == before {
                    break;
                }
            }
            self.io.end_replay();
            if result.is_err() {
                break;
            }
        }

        self.capture_replay = false;
        self.io.capture_replay = false;
        self.image_cache.read_only = false;
        self.paint_image_cache.read_only = false;
        self.scrolling_texture.read_only = false;
        let refused_after = self.image_cache.refused_uploads
            + self.paint_image_cache.refused_uploads
            + self.scrolling_texture.refused_uploads;
        let link_zones = self.restore_live_state(saved);
        (
            result,
            CaptureReplayReport {
                refused_uploads: refused_after - refused_before,
                link_zones,
            },
        )
    }
}

/// Rasterizes a capture's tessellated shapes into tightly packed RGBA. A
/// host installs one with [`ImZeroFffi::set_capture_raster`]; without one, a
/// capture is answered as unsupported (ADR-0281 §SD5).
pub trait CaptureRasterI {
    fn rasterize(
        &mut self,
        clipped: &[egui::ClippedPrimitive],
        textures: &egui::TexturesDelta,
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
    ) -> Result<Vec<u8>, String>;
}

/// `captureReplay`'s formats.
pub const CAPTURE_FORMAT_PNG: u8 = 0;
pub const CAPTURE_FORMAT_SVG: u8 = 1;

/// `CaptureResult::status` values, as `fetchCaptureResult` reports them.
pub const CAPTURE_COMPLETED: u8 = 1;
pub const CAPTURE_FAILED: u8 = 2;
pub const CAPTURE_UNSUPPORTED: u8 = 3;

/// The outcome of one `captureReplay`, held until `fetchCaptureResult`.
#[derive(Debug, Default)]
pub struct CaptureResult {
    pub request_id: u64,
    pub status: u8,
    pub width: u32,
    pub height: u32,
    pub reason: String,
    /// A pixel capture's tightly packed RGBA, `width` × `height`, top-left
    /// origin; an SVG capture's document.
    pub data: Vec<u8>,
    pub refused_uploads: u64,
    /// Meshes whose texture neither the live mirror nor the capture context
    /// held; the rasterizer skips them, so each is a hole.
    pub unknown_textures: u64,
}

impl<R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'_, R, W> {
    /// Keeps the fonts an SVG capture embeds.
    pub fn set_capture_fonts(
        &mut self,
        fonts: std::sync::Arc<crate::imzero2::svgexport::FontResolver>,
    ) {
        self.capture_fonts = Some(fonts);
    }

    /// Installs the host's rasterizer for captures.
    pub fn set_capture_raster(&mut self, raster: Box<dyn CaptureRasterI>) {
        self.capture_raster = Some(raster);
    }

    /// Replays `stream` — whole messages of a recorded frame, the granted
    /// windows' spans — into a capture context built from `live`, and keeps
    /// the pixels for `fetchCaptureResult`. Runs inside the live pass.
    pub fn capture_render(
        &mut self,
        live: &egui::Context,
        request_id: u64,
        format: u8,
        stream: &[u8],
    ) {
        let mut r = self.capture_render_inner(live, format, stream);
        r.request_id = request_id;
        self.capture_result = Some(r);
    }

    fn capture_render_inner(
        &mut self,
        live: &egui::Context,
        format: u8,
        stream: &[u8],
    ) -> CaptureResult {
        let failed = |status: u8, reason: String| CaptureResult {
            status,
            reason,
            ..Default::default()
        };
        if format == CAPTURE_FORMAT_PNG && self.capture_raster.is_none() {
            return failed(
                CAPTURE_UNSUPPORTED,
                "this host has no rasterizer for captures".into(),
            );
        }
        if format != CAPTURE_FORMAT_PNG && format != CAPTURE_FORMAT_SVG {
            return failed(CAPTURE_FAILED, format!("unknown capture format {format}"));
        }
        let ppp = live.pixels_per_point();
        let screen = live.viewport_rect();
        let width_px = (screen.width() * ppp).round().max(1.0) as u32;
        let height_px = (screen.height() * ppp).round().max(1.0) as u32;
        let mut raw = egui::RawInput {
            screen_rect: Some(screen),
            max_texture_side: Some(live.input(|i| i.max_texture_side)),
            time: Some(live.input(|i| i.time)),
            predicted_dt: live.input(|i| i.predicted_dt),
            focused: false,
            ..Default::default()
        };
        raw.viewports.entry(egui::ViewportId::ROOT).or_default().native_pixels_per_point =
            Some(ppp);

        let ctx = capture_context(live);
        let mut replay = None;
        let mut svg = None;
        let out = ctx.run_ui(raw, |ui| {
            let r = self.replay_for_capture(ui.ctx(), &[stream]);
            if format == CAPTURE_FORMAT_SVG && r.0.is_ok() {
                // Inside the pass, while the capture context's graphics
                // still hold what the replay drew: the exporter reads them
                // there, with the live images from the mirror. Nothing is
                // written to a file (ADR-0281 §SD5).
                let fonts = self.capture_fonts.as_deref().cloned().unwrap_or_default();
                let links = std::sync::Arc::new(std::sync::Mutex::new(r.1.link_zones.clone()));
                svg = Some(crate::imzero2::svgexport::render_svg_from_context(
                    ui.ctx(),
                    &fonts,
                    &self.texture_cache,
                    &links,
                    true,
                    Some(egui::Color32::BLACK),
                ));
            }
            replay = Some(r);
        });
        let Some((result, report)) = replay else {
            return failed(CAPTURE_FAILED, "the capture pass did not run".into());
        };
        if let Err(e) = result {
            return failed(CAPTURE_FAILED, format!("replay: {e}"));
        }
        if let Some(svg) = svg {
            return CaptureResult {
                status: CAPTURE_COMPLETED,
                width: width_px,
                height: height_px,
                data: svg.into_bytes(),
                refused_uploads: report.refused_uploads,
                ..Default::default()
            };
        }
        let clipped = ctx.tessellate(out.shapes, out.pixels_per_point);

        // The capture context's own textures — its font atlas — and the live
        // context's images from the CPU mirror. The caches upload nothing
        // during a replay, so the two sets do not overlap.
        self.scrolling_texture.sync_export_mirror();
        let mut textures = egui::TexturesDelta::default();
        let mut known: std::collections::HashSet<egui::TextureId> =
            out.textures_delta.set.iter().map(|(id, _)| *id).collect();
        if let Ok(mirror) = self.texture_cache.lock() {
            for (id, t) in mirror.iter() {
                if known.contains(id) {
                    continue;
                }
                let image = egui::ColorImage::from_rgba_unmultiplied(
                    [t.width as usize, t.height as usize],
                    &t.rgba,
                );
                let options = if t.nearest {
                    egui::TextureOptions::NEAREST
                } else {
                    egui::TextureOptions::LINEAR
                };
                textures.set.push((*id, egui::epaint::ImageDelta::full(image, options)));
                known.insert(*id);
            }
        }
        textures.set.extend(out.textures_delta.set);
        let unknown_textures = clipped
            .iter()
            .filter(|p| matches!(&p.primitive, egui::epaint::Primitive::Mesh(m) if !known.contains(&m.texture_id)))
            .count() as u64;

        let raster = self.capture_raster.as_mut().expect("checked above");
        match raster.rasterize(
            &clipped,
            &textures,
            width_px,
            height_px,
            out.pixels_per_point,
        ) {
            Ok(rgba) => CaptureResult {
                status: CAPTURE_COMPLETED,
                width: width_px,
                height: height_px,
                data: rgba,
                refused_uploads: report.refused_uploads,
                unknown_textures,
                ..Default::default()
            },
            Err(e) => failed(CAPTURE_FAILED, format!("raster: {e}")),
        }
    }

    /// Answers `fetchCaptureResult`: the held result, or status 0 when there
    /// is none. The result is taken.
    pub fn write_capture_result(&mut self) -> crate::fffi::common::FffiResult<()> {
        let r = self.capture_result.take().unwrap_or_default();
        self.io.write_plain_u64(r.request_id)?;
        self.io.write_plain_u8(r.status)?;
        self.io.write_plain_u32(r.width)?;
        self.io.write_plain_u32(r.height)?;
        self.io.write_plain_s(r.reason)?;
        self.io.write_plain_u8_slice(&r.data)?;
        self.io.write_plain_u64(r.refused_uploads)?;
        self.io.write_plain_u64(r.unknown_textures)?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::imzero2::enums_out::FuncProcId;

    type Interp = ImZeroFffi<'static, std::io::Cursor<Vec<u8>>, Vec<u8>>;

    fn interp() -> Interp {
        ImZeroFffi::new(std::io::Cursor::new(Vec::new()), Vec::new())
    }

    /// One top-level message: its length (opcode + body), the opcode, the body.
    fn message(op: FuncProcId, body: &[u8]) -> Vec<u8> {
        let mut m = ((4 + body.len()) as u32).to_le_bytes().to_vec();
        m.extend_from_slice(&(op as u32).to_le_bytes());
        m.extend_from_slice(body);
        m
    }

    fn export_svg(path: &str) -> Vec<u8> {
        let mut body = (path.len() as u32).to_le_bytes().to_vec();
        body.extend_from_slice(path.as_bytes());
        body.push(0); // embedFonts
        body.extend_from_slice(&0u32.to_le_bytes()); // bgRgba
        message(FuncProcId::ExportSvg, &body)
    }

    fn passthrough(id: u64, input: u64) -> Vec<u8> {
        let mut body = id.to_le_bytes().to_vec();
        body.extend_from_slice(&input.to_le_bytes());
        message(FuncProcId::Passthrough, &body)
    }

    /// Interprets `bytes` as the live frame would, outside a capture replay.
    fn interpret_live(i: &mut Interp, ctx: &egui::Context, bytes: &[u8]) {
        let _ = ctx.run_ui(egui::RawInput::default(), |ui| {
            i.io.begin_replay(bytes);
            i.interpret_outer(ui.ctx(), &mut None).expect("live interpretation");
            i.io.end_replay();
        });
    }

    fn replay(i: &mut Interp, bytes: &[u8]) -> (InterpretResult<()>, CaptureReplayReport) {
        let live = egui::Context::default();
        let _ = live.run_ui(egui::RawInput::default(), |_| {});
        let ctx = capture_context(&live);
        let mut out = None;
        let _ = ctx.run_ui(egui::RawInput::default(), |ui| {
            out = Some(i.replay_for_capture(ui.ctx(), &[bytes]));
        });
        out.expect("the pass ran")
    }

    #[test]
    fn a_host_effect_opcode_runs_live_and_not_in_a_replay() {
        let bytes = export_svg("/nonexistent/x.svg");
        let mut live = interp();
        interpret_live(&mut live, &egui::Context::default(), &bytes);
        assert!(live.export_state.lock().unwrap().pending.is_some());

        let mut i = interp();
        let (r, _) = replay(&mut i, &bytes);
        r.expect("replay");
        assert!(i.export_state.lock().unwrap().pending.is_none());
    }

    #[test]
    fn a_fetcher_answers_live_and_writes_nothing_in_a_replay() {
        let bytes = message(FuncProcId::FetchF1KeyPressed, &[]);
        let mut live = interp();
        interpret_live(&mut live, &egui::Context::default(), &bytes);
        assert!(!live.io.w.is_empty());

        let mut i = interp();
        let (r, _) = replay(&mut i, &bytes);
        r.expect("replay");
        assert!(i.io.w.is_empty());
    }

    #[test]
    fn a_write_during_a_replay_is_an_error() {
        let mut i = interp();
        i.io.capture_replay = true;
        assert!(matches!(
            i.io.write_plain_u32(1),
            Err(crate::fffi::common::FffiError::WriteDuringCaptureReplay(4))
        ));
        assert!(i.io.flush().is_err());
        assert!(i.io.w.is_empty());
    }

    #[test]
    fn the_live_registers_survive_a_replay() {
        let mut i = interp();
        interpret_live(&mut i, &egui::Context::default(), &passthrough(7, 1));
        assert_eq!(i.r9_u64_ids, vec![7]);

        let (r, _) = replay(&mut i, &passthrough(9, 1));
        r.expect("replay");
        assert_eq!(i.r9_u64_ids, vec![7]);
        assert_eq!(i.r9_u64_values, vec![2]);
        assert!(!i.capture_replay && !i.io.capture_replay);
    }

    #[test]
    fn the_capture_context_takes_the_live_fonts_options_and_memory() {
        let live = egui::Context::default();
        let mut fonts = egui::FontDefinitions::default();
        fonts.families.insert(
            egui::FontFamily::Name("capture-test".into()),
            vec!["Hack".to_owned()],
        );
        live.set_fonts(fonts);
        live.options_mut(|o| o.max_passes = std::num::NonZeroUsize::new(1).unwrap());
        let _ = live.run_ui(egui::RawInput::default(), |ui| {
            ui.ctx().memory_mut(|m| m.data.insert_temp(egui::Id::new("k"), 5u32));
        });
        let ctx = capture_context(&live);
        let _ = ctx.run_ui(egui::RawInput::default(), |_| {});
        assert_eq!(ctx.options(|o| o.max_passes.get()), 1);
        assert_eq!(
            ctx.memory(|m| m.data.get_temp::<u32>(egui::Id::new("k"))),
            Some(5)
        );
        assert!(ctx.fonts(|f| {
            f.definitions().families.contains_key(&egui::FontFamily::Name("capture-test".into()))
        }));
    }

    #[test]
    fn a_read_only_image_cache_refuses_an_upload() {
        let ctx = egui::Context::default();
        let mut cache = crate::imzero2::image::ImageCache::new();
        cache.read_only = true;
        let id = cache.ensure(
            &ctx,
            1,
            1,
            1,
            1,
            egui::TextureOptions::default(),
            &[0xff00_00ff],
        );
        assert!(id.is_none());
        assert_eq!(cache.refused_uploads, 1);
    }
}
