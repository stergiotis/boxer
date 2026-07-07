use crate::imzero2::app;
use crate::imzero2::appconfig;

pub fn run_main_loop(config: appconfig::AppConfig) -> eframe::Result {
    tracing::info!(args=?config,"up and running");

    let mut native_options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([
                config.initial_main_window_width,
                config.initial_main_window_height,
            ])
            .with_min_inner_size([config.inner_min_size_width, config.inner_min_size_height])
            .with_fullscreen(config.fullscreen)
            .with_title(config.window_title.clone())
            .with_resizable(true),
        ..Default::default()
    };
    // eframe 0.35 removed `NativeOptions::vsync`; on the wgpu backend vsync is
    // the swapchain present mode (default `AutoVsync`). Map the `-vsync` flag
    // onto it so the config option keeps its effect.
    native_options.wgpu_options.surface.present_mode = if config.vsync {
        egui_wgpu::wgpu::PresentMode::AutoVsync
    } else {
        egui_wgpu::wgpu::PresentMode::AutoNoVsync
    };
    eframe::run_native(
        &config.app_title,
        native_options,
        Box::new(|cc| {
            Ok(Box::new(app::App::new(
                cc,
                &config,
                std::io::stdin().lock(),
                std::io::stdout().lock(),
            )))
        }),
    )
}
