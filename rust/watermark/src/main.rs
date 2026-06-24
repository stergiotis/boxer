//! CLI entry point. The watermark library lives in `lib.rs`; this binary is a
//! thin wrapper around [`cli::run`].

mod cli;

fn main() {
    if let Err(e) = cli::run() {
        eprintln!("error: {e}");
        std::process::exit(1);
    }
}
