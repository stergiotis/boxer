use crate::fffi::io::ImZeroFffiIo;

pub struct DebugTools {
    last_update: Option<std::time::Instant>,
}

impl DebugTools {
    pub fn new() -> Self {
        return Self{
            last_update: None,
        }
    }
    pub fn render_debug_tools<R,W>(&mut self, io: &ImZeroFffiIo<R,W>, ctx: &egui::Context, ui: &mut egui::Ui)
    where
        R: std::io::BufRead,
        W: std::io::Write
    {
        ui.heading("performance measures");
        let fps_string;
        {
            let tnow = std::time::Instant::now();
            fps_string = if let Some(tprev) = self.last_update {
                let dt = (tnow - tprev).as_secs_f64();
                let fps = 1.0 / dt;
                fps.round().to_string()
            } else {
                "n/a".to_string()
            };
            self.last_update = Some(tnow);
        }
        egui::Grid::new("imerzo2 debug tools 0")
            .num_columns(3)
            .striped(true)
            .show(ui, |ui| {
                ui.add(egui::Label::new("Framerate"));
                ui.add(egui::Label::new(fps_string));
                ui.add(egui::Label::new("Hz"));
                ui.end_row();
            });

        ui.heading("egui context information");
        egui::Grid::new("imerzo2 debug tools 1")
            .num_columns(3)
            .striped(true)
            .show(ui, |ui| {
                ui.add(egui::Label::new("Render passes"));
                ui.add(egui::Label::new(format!("{}",ctx.cumulative_pass_nr())));
                ui.add(egui::Label::new("-"));
                ui.end_row();
                ui.add(egui::Label::new("Frames"));
                ui.add(egui::Label::new(format!("{}",ctx.cumulative_frame_nr())));
                ui.add(egui::Label::new("-"));
                ui.end_row();
            });
        ui.separator();
        ui.heading("last pass I/O statistics");
        egui::Grid::new("imerzo2 debug tools 2")
            .num_columns(3)
            .striped(true)
            .show(ui, |ui| {
                ui.add(egui::Label::new("io.write"));
                ui.add(egui::Label::new(format!("{}",io.written_bytes_count)));
                ui.add(egui::Label::new("Bytes"));
                ui.end_row();
                ui.add(egui::Label::new("io.read"));
                ui.add(egui::Label::new(format!("{}",io.read_bytes_count)));
                ui.add(egui::Label::new("Bytes"));
                ui.end_row();
                ui.add(egui::Label::new("io.flushes"));
                ui.add(egui::Label::new(format!("{}",io.flush_count)));
                ui.add(egui::Label::new("-"));
                ui.end_row();
            });
    }
}