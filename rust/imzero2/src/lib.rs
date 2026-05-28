#![warn(clippy::all, rust_2018_idioms)]

pub mod imzero2;
mod fffi;
pub mod cli;
mod ipc;
pub mod runinfo;

pub use imzero2::entry::run_main_loop as run_imzero2_main_loop;
//pub use ipc::ipc06::run_main_loop as run_ipc06_main_loop;
pub use ipc::ipc06testharness::run_producer as run_ipc06_testharness_producer;
pub use ipc::ipc06testharness::run_consumer as run_ipc06_testharness_consumer;
pub use imzero2::appconfig;