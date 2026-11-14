//! The audio lane's pipe pair to the Go host (ADR-0372 SD4, proposed).
//!
//! Sound is the Go host's: it decodes, resamples and keeps the position. The
//! device is a viewer's page. Between the two, this host is a relay that does
//! not read what it forwards. The Go host names two FIFOs in the environment;
//! lane messages cross each as a little-endian `u32` length and that many
//! bytes, without the wire prefix the carrier adds toward a viewer
//! ([`crate::imzero2::inputproto::PREFIX_AUDIO`]).
//!
//! Without the variables, or where a FIFO cannot be opened, nothing starts
//! and the session has no audio device, which the Go side reads as silence.

/// The FIFO the host writes and this process reads: messages toward the
/// device.
pub const TO_DEVICE_ENV: &str = "IMZERO2_AUDIO_LANE_TO_DEVICE";
/// The FIFO this process writes and the host reads: messages from the device.
pub const FROM_DEVICE_ENV: &str = "IMZERO2_AUDIO_LANE_FROM_DEVICE";

/// The largest message either direction carries. A chunk holds half a second
/// of audio at most, well under this; a length above it is a broken stream.
const MAX_MESSAGE: usize = 4 << 20;

/// The lane message a carrier writes itself: the device went away. Its layout
/// is `lanesink`'s device announcement, state absent, no rate.
pub const DEVICE_ABSENT: [u8; 6] = [0x81, 0x00, 0, 0, 0, 0];

/// Starts the lane when the host named both FIFOs. `to_device` is given each
/// message the host sends, on the reader's thread. The returned sender takes
/// messages for the host; it never blocks.
pub fn spawn_from_env(
    to_device: impl Fn(Vec<u8>) + Send + 'static,
) -> Option<std::sync::mpsc::Sender<Vec<u8>>> {
    let down = std::env::var(TO_DEVICE_ENV).ok().filter(|p| !p.is_empty())?;
    let up = std::env::var(FROM_DEVICE_ENV).ok().filter(|p| !p.is_empty())?;
    spawn(down, up, to_device)
}

fn spawn(
    down: String,
    up: String,
    to_device: impl Fn(Vec<u8>) + Send + 'static,
) -> Option<std::sync::mpsc::Sender<Vec<u8>>> {
    let reader = std::thread::Builder::new().name("imzero2-audio-down".into()).spawn(move || {
        // Opening a FIFO for reading waits for the host to open it for writing.
        let mut fifo = match std::fs::File::open(&down) {
            Ok(f) => f,
            Err(e) => {
                tracing::warn!(path = %down, error = %e, "audio lane: unable to open the FIFO from the host");
                return;
            }
        };
        loop {
            match read_message(&mut fifo) {
                Ok(Some(msg)) => to_device(msg),
                // The host closed its end.
                Ok(None) => return,
                Err(e) => {
                    tracing::warn!(error = %e, "audio lane: read from the host failed; the lane is closed");
                    return;
                }
            }
        }
    });
    if let Err(e) = reader {
        tracing::warn!(error = %e, "audio lane: unable to start the reader thread");
        return None;
    }
    let (tx, rx) = std::sync::mpsc::channel::<Vec<u8>>();
    let writer = std::thread::Builder::new().name("imzero2-audio-up".into()).spawn(move || {
        // Opening a FIFO for writing waits for the host to open it for reading.
        let mut fifo = match std::fs::OpenOptions::new().write(true).open(&up) {
            Ok(f) => f,
            Err(e) => {
                tracing::warn!(path = %up, error = %e, "audio lane: unable to open the FIFO to the host");
                return;
            }
        };
        while let Ok(msg) = rx.recv() {
            if let Err(e) = write_message(&mut fifo, &msg) {
                tracing::warn!(error = %e, "audio lane: write to the host failed; the lane is closed");
                return;
            }
        }
    });
    if let Err(e) = writer {
        tracing::warn!(error = %e, "audio lane: unable to start the writer thread");
        return None;
    }
    Some(tx)
}

/// Reads one length-prefixed message; `None` at a clean end of the stream.
fn read_message(r: &mut impl std::io::Read) -> std::io::Result<Option<Vec<u8>>> {
    let mut len = [0u8; 4];
    match r.read_exact(&mut len) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }
    let n = u32::from_le_bytes(len) as usize;
    if n > MAX_MESSAGE {
        return Err(std::io::Error::new(
            std::io::ErrorKind::InvalidData,
            format!("audio lane message of {n} bytes exceeds the limit"),
        ));
    }
    let mut msg = vec![0u8; n];
    r.read_exact(&mut msg)?;
    Ok(Some(msg))
}

fn write_message(w: &mut impl std::io::Write, msg: &[u8]) -> std::io::Result<()> {
    let len = u32::try_from(msg.len())
        .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidInput, e))?;
    // One write per message: a FIFO keeps a write of up to PIPE_BUF whole, and
    // what a device sends is a few bytes.
    let mut framed = Vec::with_capacity(4 + msg.len());
    framed.extend_from_slice(&len.to_le_bytes());
    framed.extend_from_slice(msg);
    w.write_all(&framed)?;
    w.flush()
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::sync::mpsc;
    use std::time::Duration;

    #[test]
    fn framing_round_trips_and_refuses_an_oversized_length() {
        let mut buf = Vec::new();
        write_message(&mut buf, &[1, 2, 3]).unwrap();
        write_message(&mut buf, &[]).unwrap();
        let mut r = std::io::Cursor::new(buf);
        assert_eq!(read_message(&mut r).unwrap(), Some(vec![1, 2, 3]));
        assert_eq!(read_message(&mut r).unwrap(), Some(vec![]));
        assert_eq!(read_message(&mut r).unwrap(), None);

        let mut big = std::io::Cursor::new(((MAX_MESSAGE + 1) as u32).to_le_bytes().to_vec());
        assert!(read_message(&mut big).is_err());
    }

    #[test]
    fn messages_cross_both_fifos() {
        let dir = std::env::temp_dir().join(format!("imzero2-audiolane-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let (down, up) = (dir.join("down"), dir.join("up"));
        for p in [&down, &up] {
            let _ = std::fs::remove_file(p);
            let made = std::process::Command::new("mkfifo").arg(p).status();
            if !made.is_ok_and(|s| s.success()) {
                eprintln!("mkfifo unavailable; skipped");
                return;
            }
        }
        let (seen_tx, seen_rx) = mpsc::channel();
        let to_host = spawn(
            down.to_string_lossy().into_owned(),
            up.to_string_lossy().into_owned(),
            move |msg| {
                let _ = seen_tx.send(msg);
            },
        )
        .expect("the lane starts");

        // The host's two ends.
        let mut host_w = std::fs::OpenOptions::new().write(true).open(&down).unwrap();
        let mut host_r = std::fs::File::open(&up).unwrap();

        write_message(&mut host_w, &[0x05]).unwrap();
        assert_eq!(
            seen_rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            vec![0x05]
        );

        to_host.send(DEVICE_ABSENT.to_vec()).unwrap();
        assert_eq!(
            read_message(&mut host_r).unwrap(),
            Some(DEVICE_ABSENT.to_vec())
        );
        let _ = std::fs::remove_dir_all(&dir);
    }
}
