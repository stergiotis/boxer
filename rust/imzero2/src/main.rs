#![warn(clippy::all, rust_2018_idioms)]
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")] // hide console window on Windows in release

use pebble2_rust::cli::flags;

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
}

#[cfg(feature = "puffin")]
fn start_puffin_server() {
    puffin::set_scopes_on(true);

    match puffin_http::Server::new("127.0.0.1:8585") {
        Ok(puffin_server) => {
            tracing::info!("run: cargo install puffin_viewer && puffin_viewer --url 127.0.0.1:8585");
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

fn main() -> Result<(),Box<dyn std::error::Error>> {
    #[cfg(feature = "dhat-heap")]
    let _profiler = dhat::Profiler::new_heap();

    setup_tracing();
    // Enter the run-tagged root tracing span before any other event so
    // every subsequent log line carries the inherited run_id. The
    // guard binding must outlive every event we want tagged — kept in
    // scope for the duration of main. ADR-0026 §2026-05-12 follow-up
    // (b): cross-process audit attribution via PEBBLE2_RUN_ID.
    let _run_span = pebble2_rust::runinfo::enter_root_span();
    pebble2_rust::runinfo::log_bound_run();

    #[cfg(feature = "puffin")]
    start_puffin_server();

    let argc = std::env::args().len();
    let args = std::env::args().collect::<Vec<String>>();
    let bin_name = args[0].as_str();
    if argc < 2 {
        usage(&mut std::io::stderr(), bin_name, false)?;
    }
    let command = args[1].as_str();
    let rest_args : &[String] = &args[2..];
    let mut used = roaring::RoaringBitmap::new();
    if command == "-help" {
        usage(&mut std::io::stderr(), bin_name, true)?;
    }

    let r = match command {
        "imzero2" => {
            let mut cfg = pebble2_rust::appconfig::AppConfig::default();
            if pebble2_rust::cli::flags::find_flag_value_default_bool(rest_args.iter(), &mut used, "-help", false) {
                cfg.usage(&mut std::io::stderr()).expect("unable to display usage");
            } else {
                cfg.parse(&mut used, &rest_args);
            }
            flags::validate_all_args_used(rest_args, rest_args.len() as u32,&used);
            let r = pebble2_rust::run_imzero2_main_loop(cfg);
            if r.is_err() {
                let e = r.err().unwrap();
                Err(Box::<dyn std::error::Error>::from(e))
            } else {
                Ok(())
            }
        },
        "ipc" => {
            let shm_path = flags::find_flag_default(rest_args.iter(), &mut used, "-shm-path","".to_string());
            let data_size = flags::find_flag_value_default_parsable(rest_args.iter(), &mut used, "-data-size",0usize);
            let mode = flags::find_flag_default(rest_args.iter(), &mut used, "-mode","consumer".to_string());
            flags::validate_all_args_used(rest_args, rest_args.len() as u32,&used);
            tracing::info!(shm_path=shm_path,data_size=data_size,mode=mode,"running ipc test harness");
            match mode.as_str() {
                "consumer" => {
                    pebble2_rust::run_ipc06_testharness_consumer(shm_path.as_str(), data_size);
                },
                "producer" => {
                    pebble2_rust::run_ipc06_testharness_producer(shm_path.as_str(), data_size);
                }
                _ => {
                    tracing::error!(mode=mode,"unknown test harness mode");
                    panic!("unknown test harness mode");
                }
            }
            Ok(())
        },
        _ => {
            usage(&mut std::io::stderr(), bin_name, false)?;
            Ok(())
        }
    };
    return r;
}