use crate::imzero2::appconfig;
use crate::imzero2::app;

pub fn run_main_loop(config: appconfig::AppConfig) -> eframe::Result {
    tracing::info!(args=?config,"up and running");

    let mut native_options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([config.initial_main_window_width, config.initial_main_window_height])
            .with_min_inner_size([config.inner_min_size_width, config.inner_min_size_height])
            .with_fullscreen(config.fullscreen)
            .with_title(config.window_title.clone())
            .with_resizable(true),
        ..Default::default()
    };
    native_options.vsync = config.vsync;
    eframe::run_native(
        &config.app_title,
        native_options,
        Box::new(|cc| {
            Ok(Box::new(app::App::new(cc,
                &config,
                std::io::stdin().lock(),
                std::io::stdout().lock(),
            )))
        })
    )
}