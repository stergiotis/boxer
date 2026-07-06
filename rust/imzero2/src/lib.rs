#![warn(clippy::all, rust_2018_idioms)]

#[cfg(all(
    not(feature = "desktop"),
    not(feature = "headless"),
    not(feature = "headless_svg")
))]
compile_error!(
    "imzero2 needs at least one host: enable the `desktop` (default), `headless`, or `headless_svg` feature"
);

pub mod cli;
mod fffi;
pub mod imzero2;
mod ipc;
pub mod runinfo;

#[cfg(feature = "desktop")]
pub use imzero2::entry::run_main_loop as run_imzero2_main_loop;
#[cfg(feature = "headless")]
pub use imzero2::headless::run_main_loop as run_imzero2_headless_loop;
#[cfg(feature = "headless_svg")]
pub use imzero2::headless_svg::run_main_loop as run_imzero2_svg_loop;
//pub use ipc::ipc06::run_main_loop as run_ipc06_main_loop;
pub use imzero2::appconfig;
pub use ipc::ipc06testharness::run_consumer as run_ipc06_testharness_consumer;
pub use ipc::ipc06testharness::run_producer as run_ipc06_testharness_producer;
