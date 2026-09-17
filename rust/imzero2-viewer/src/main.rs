#![cfg_attr(windows, windows_subsystem = "windows")]

fn main() {
    #[cfg(windows)]
    if let Err(error) = imzero2_viewer::win::run() {
        if std::env::args().any(|arg| arg == "--timeout") {
            eprintln!("{error:#}");
        } else {
            imzero2_viewer::win::show_error(&format!("{error:#}"));
        }
        std::process::exit(1);
    }
    #[cfg(not(windows))]
    {
        eprintln!("imzero2-viewer is a Windows application. Build with --target x86_64-pc-windows-gnu; core tests run natively.");
        std::process::exit(1);
    }
}
