use crate::imzero2::app;
use crate::imzero2::appconfig;

pub fn run_main_loop(config: appconfig::AppConfig) -> eframe::Result {
    tracing::info!(args=?config,"up and running");

    // A `-backgroundColorRGBA` alpha below ff is a request for a see-through
    // window, so ask the windowing system for one; `App::clear_color` supplies
    // the colour. Without this the alpha is only advisory: unrequested, the
    // window gets whatever visual the platform hands it — measured as depth 24
    // (no alpha channel at all) on the Xwayland/wgpu path, depth 32 once this
    // flag is on — so a translucent clear colour may or may not do anything.
    // Requesting transparency unconditionally is the wrong default though: it
    // costs an ARGB visual and, on the wgpu host, a `CompositeAlphaMode` with
    // transparency, which egui-wgpu warns about when the surface lacks one.
    let transparent = config.background_color_rgba.is_some_and(|c| c.a() < u8::MAX);
    let mut native_options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([
                config.initial_main_window_width,
                config.initial_main_window_height,
            ])
            .with_min_inner_size([config.inner_min_size_width, config.inner_min_size_height])
            .with_fullscreen(config.fullscreen)
            .with_title(config.window_title.clone())
            .with_transparent(transparent)
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
