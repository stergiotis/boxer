#![warn(clippy::all, rust_2018_idioms)]
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")] // hide console window on Windows in release

use imzero2::cli::flags;

#[cfg(feature = "dhat-heap")]
#[global_allocator]
static ALLOC: dhat::Alloc = dhat::Alloc;

#[cfg(not(feature = "dhat-heap"))]
#[global_allocator] // copied from egui_demo_app
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc; // Much faster allocator, can give 20% speedups: https://github.com/emilk/egui/pull/7029

fn setup_tracing() {
    let subscriber = tracing_subscriber::fmt()
        .with_writer(std::io::stderr)
        .with_max_level(tracing::Level::DEBUG)
        .with_file(true)
        .with_line_number(true)
        .with_thread_ids(false)
        .with_target(false)
        .compact()
        .finish();
    tracing::subscriber::set_global_default(subscriber).expect("setting tracing default failed");

    // Dependencies that report through the `log` crate rather than `tracing`
    // were being dropped entirely: nothing ever registered a `log` logger, and
    // `set_global_default` — unlike the builder's own `init()` — does not
    // install the bridge. walkers is the case that surfaced it, where a tile
    // server that is down, rate-limiting, or serving garbage produced a grey
    // map and complete silence.
    //
    // Bridged at Info, not Trace: walkers logs one `debug!` per tile decode and
    // one `trace!` per download, which at 20-40 tiles a screenful is noise on
    // the hot path, and every record below the cap is converted before it can
    // be filtered. Warnings and errors — the ones worth waking up for — arrive.
    if let Err(e) = tracing_log::LogTracer::builder().with_max_level(log::LevelFilter::Info).init()
    {
        tracing::warn!(error = %e, "could not bridge the log crate into tracing; dependency diagnostics will be dropped");
    }
}

/// Gate for the puffin profiler, read from `IMZERO2_PUFFIN`.
///
/// Accepts `1`, `true` and `on` (case-insensitively) so the spelling matches
/// both this file's existing `IMZERO2_HEADLESS` convention (`1` / `on`) and
/// the Go registry's `env.NewBool`, which parses via `strconv::ParseBool`
/// (`1` / `true`). Anything else, including unset, is off.
#[cfg(feature = "puffin")]
fn puffin_enabled_from_env() -> bool {
    std::env::var("IMZERO2_PUFFIN")
        .map(|v| v == "1" || v.eq_ignore_ascii_case("true") || v.eq_ignore_ascii_case("on"))
        .unwrap_or(false)
}

/// Turn on puffin scope collection and open the loopback profiler server.
///
/// Dormant unless `IMZERO2_PUFFIN` is truthy. The `puffin` feature ships in
/// every `build_rust.sh` build, so without this gate an ordinary run collected
/// scopes on every frame — measured 2026-08-18 at ~15.7k scopes and ~489 KiB
/// of scope data per frame — and held an unauthenticated port open, with no
/// off switch short of a rebuild.
///
/// Left off, `puffin::profile_scope!` expands to a single relaxed atomic load
/// (`are_scopes_on()`) and takes the `None` arm, so the compiled-in scopes
/// cost a predictable branch and nothing else. The runtime variable, not the
/// compile flag, is the switch — the same shape `EGUI_INSPECTION` uses for the
/// `inspection` feature, and for the same reason: the port is unauthenticated
/// and host-scoped. See ADR-0195 (2026-08-18 Update).
#[cfg(feature = "puffin")]
fn start_puffin_server() {
    if !puffin_enabled_from_env() {
        return;
    }
    puffin::set_scopes_on(true);

    match puffin_http::Server::new("127.0.0.1:8585") {
        Ok(puffin_server) => {
            tracing::info!(
                "puffin enabled by IMZERO2_PUFFIN; run: cargo install puffin_viewer && puffin_viewer --url 127.0.0.1:8585"
            );
            //std::process::Command::new("puffin_viewer")
            //    .arg("--url")
            //    .arg("127.0.0.1:8585")
            //    .spawn()
            //    .ok();

            // We can store the server if we want, but in this case we just want
            // it to keep running. Dropping it closes the server, so let's not drop it!
            #[expect(clippy::mem_forget)]
            std::mem::forget(puffin_server);
        }
        Err(err) => {
            tracing::error!("failed to start puffin server: {err}");
        }
    }
}

/// Select the host loop for the `imzero2` command (ADR-0024 SD1).
///
/// - Build with only `desktop` (the default) → eframe host.
/// - Build with only `headless` → headless render host.
/// - Build with only `headless_svg` → GPU-less SVG-only host.
/// - Build with both → desktop by default; `IMZERO2_HEADLESS=1` (or `on`)
///   selects the headless host at runtime, `IMZERO2_HEADLESS_SVG=1` the
///   SVG-only host.
fn run_imzero2(cfg: imzero2::appconfig::AppConfig) -> Result<(), Box<dyn std::error::Error>> {
    #[cfg(feature = "headless_svg")]
    {
        let env_pick = std::env::var("IMZERO2_HEADLESS_SVG")
            .map(|v| v == "1" || v.eq_ignore_ascii_case("on"))
            .unwrap_or(false);
        // Built alone (no other host), this is the only choice; built
        // alongside another host, an explicit env flag selects it.
        if env_pick || (!cfg!(feature = "desktop") && !cfg!(feature = "headless")) {
            return imzero2::run_imzero2_svg_loop(cfg).map_err(Into::into);
        }
    }
    #[cfg(feature = "headless")]
    {
        let env_pick = std::env::var("IMZERO2_HEADLESS")
            .map(|v| v == "1" || v.eq_ignore_ascii_case("on"))
            .unwrap_or(false);
        if env_pick || !cfg!(feature = "desktop") {
            return imzero2::run_imzero2_headless_loop(cfg).map_err(Into::into);
        }
    }
    #[cfg(feature = "desktop")]
    {
        return imzero2::run_imzero2_main_loop(cfg).map_err(Into::into);
    }
    #[cfg(not(feature = "desktop"))]
    {
        // With `headless` enabled (the only way to compile this far per the
        // lib.rs compile_error guard) the branch above always returns.
        unreachable!("no host feature matched");
    }
}

fn usage(w: &mut impl std::io::Write, bin_name: &str, regular: bool) -> std::io::Result<()> {
    write!(w, "usage:\n")?;
    write!(w, "{bin_name} imzero2\n")?;
    write!(w, "{bin_name} ipc\n")?;
    if !regular {
        std::process::exit(1);
    } else {
        std::process::exit(0);
    }
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    #[cfg(feature = "dhat-heap")]
    let _profiler = dhat::Profiler::new_heap();

    setup_tracing();
    // Enter the run-tagged root tracing span before any other event so
    // every subsequent log line carries the inherited run_id. The
    // guard binding must outlive every event we want tagged — kept in
    // scope for the duration of main. ADR-0026 §2026-05-12 follow-up
    // (b): cross-process audit attribution via PEBBLE2_RUN_ID.
    let _run_span = imzero2::runinfo::enter_root_span();
    imzero2::runinfo::log_bound_run();

    #[cfg(feature = "puffin")]
    start_puffin_server();

    let argc = std::env::args().len();
    let args = std::env::args().collect::<Vec<String>>();
    let bin_name = args[0].as_str();
    if argc < 2 {
        usage(&mut std::io::stderr(), bin_name, false)?;
    }
    let command = args[1].as_str();
    let rest_args: &[String] = &args[2..];
    let mut used = roaring::RoaringBitmap::new();
    if command == "-help" {
        usage(&mut std::io::stderr(), bin_name, true)?;
    }

    let r = match command {
        "imzero2" => {
            let mut cfg = imzero2::appconfig::AppConfig::default();
            if imzero2::cli::flags::find_flag_value_default_bool(
                rest_args.iter(),
                &mut used,
                "-help",
                false,
            ) {
                cfg.usage(&mut std::io::stderr()).expect("unable to display usage");
            } else {
                cfg.parse(&mut used, &rest_args);
            }
            flags::validate_all_args_used(rest_args, rest_args.len() as u32, &used);
            run_imzero2(cfg)
        }
        "ipc" => {
            let shm_path =
                flags::find_flag_default(rest_args.iter(), &mut used, "-shm-path", "".to_string());
            let data_size = flags::find_flag_value_default_parsable(
                rest_args.iter(),
                &mut used,
                "-data-size",
                0usize,
            );
            let mode = flags::find_flag_default(
                rest_args.iter(),
                &mut used,
                "-mode",
                "consumer".to_string(),
            );
            flags::validate_all_args_used(rest_args, rest_args.len() as u32, &used);
            tracing::info!(
                shm_path = shm_path,
                data_size = data_size,
                mode = mode,
                "running ipc test harness"
            );
            match mode.as_str() {
                "consumer" => {
                    imzero2::run_ipc06_testharness_consumer(shm_path.as_str(), data_size);
                }
                "producer" => {
                    imzero2::run_ipc06_testharness_producer(shm_path.as_str(), data_size);
                }
                _ => {
                    tracing::error!(mode = mode, "unknown test harness mode");
                    panic!("unknown test harness mode");
                }
            }
            Ok(())
        }
        _ => {
            usage(&mut std::io::stderr(), bin_name, false)?;
            Ok(())
        }
    };
    return r;
}
