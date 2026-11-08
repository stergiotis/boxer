//! A wake from the Go host outside the lock-stepped frame (ADR-0363 §M4,
//! proposed).
//!
//! Go builds a frame only once a pass has started and the client reads its
//! stream, so under the reactive cadence data that reaches Go between passes
//! waits for the next heartbeat or input. The host names a FIFO in
//! `IMZERO2_WAKE_FIFO`; it writes a byte there whenever the window should run
//! a pass now, and a thread here turns each read into a repaint request, which
//! egui accepts from any thread. Without the variable, or where the path
//! cannot be opened, nothing changes. The wake asks for one pass; what the
//! pass draws is still Go's.

use std::io::Read as _;

/// The variable the host sets for its client.
pub const WAKE_FIFO_ENV: &str = "IMZERO2_WAKE_FIFO";

/// Starts the wake thread when the host named a FIFO. `wake` runs once per
/// read, however many bytes the read returned: the host coalesces, and one
/// pass serves every wake that arrived before it.
pub fn spawn_from_env(wake: impl Fn() + Send + 'static) {
    if let Ok(path) = std::env::var(WAKE_FIFO_ENV)
        && !path.is_empty()
    {
        spawn(path, wake);
    }
}

fn spawn(path: String, wake: impl Fn() + Send + 'static) {
    let spawned = std::thread::Builder::new()
        .name("imzero2-wake".into())
        .spawn(move || {
            // Opening a FIFO for reading blocks until the writer opens it.
            let mut fifo = match std::fs::File::open(&path) {
                Ok(f) => f,
                Err(e) => {
                    tracing::warn!(path = %path, error = %e, "wake fifo: unable to open; passes keep their cadence");
                    return;
                }
            };
            let mut buf = [0u8; 64];
            loop {
                match fifo.read(&mut buf) {
                    // The host closed its end: it has exited or stopped waking.
                    Ok(0) => return,
                    Ok(_) => wake(),
                    Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {}
                    Err(e) => {
                        tracing::warn!(error = %e, "wake fifo: read failed; passes keep their cadence");
                        return;
                    }
                }
            }
        });
    if let Err(e) = spawned {
        tracing::warn!(error = %e, "wake fifo: unable to start the thread");
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::io::Write as _;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::{Arc, mpsc};
    use std::time::Duration;

    #[test]
    fn each_write_wakes_and_closing_ends_the_thread() {
        let dir = std::env::temp_dir().join(format!("imzero2-wake-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("wake");
        let _ = std::fs::remove_file(&path);
        let made = std::process::Command::new("mkfifo").arg(&path).status();
        if !made.is_ok_and(|s| s.success()) {
            eprintln!("mkfifo unavailable; skipped");
            return;
        }
        let count = Arc::new(AtomicUsize::new(0));
        let (tx, rx) = mpsc::channel();
        let c = Arc::clone(&count);
        spawn(path.to_string_lossy().into_owned(), move || {
            c.fetch_add(1, Ordering::SeqCst);
            let _ = tx.send(());
        });
        let mut w = std::fs::OpenOptions::new().write(true).open(&path).unwrap();
        w.write_all(&[1]).unwrap();
        rx.recv_timeout(Duration::from_secs(5)).expect("first wake");
        w.write_all(&[1]).unwrap();
        rx.recv_timeout(Duration::from_secs(5)).expect("second wake");
        drop(w);
        assert_eq!(count.load(Ordering::SeqCst), 2);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
