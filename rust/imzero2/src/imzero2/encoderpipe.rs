//! ffmpeg encoder subprocess (ADR-0024 SD3, Phase 2).
//!
//! Feeds tightly-packed BGRA frames to ffmpeg's stdin (`-f rawvideo
//! -pix_fmt bgra`) and drains the raw Annex-B H.264 byte stream from its
//! stdout on a separate thread, into one of two targets:
//!
//! - [`EncoderTarget::File`] — append the raw elementary stream to a file
//!   (Phase 2 verification; `IMZERO2_HEADLESS_H264_OUT`).
//! - [`EncoderTarget::Channel`] — split the stream into access units
//!   (ffmpeg is told to insert Access Unit Delimiters and repeat SPS/PPS
//!   on key frames), wrap each AU in the ADR-0024 SD4 protobuf envelope
//!   with the 0x01 prefix, and push it into the WebSocket carrier's
//!   bounded channel. A full channel blocks the drain thread, which backs
//!   up ffmpeg and ultimately the render loop — encoded frames are never
//!   dropped (the acceptance-review backpressure rule; the SD9 ring
//!   refines this at Phase 5).
//!
//! The subprocess is supervised per SD3: on a write failure the child is
//! reaped, logged, and respawned. Each respawn restarts the elementary
//! stream at SPS/PPS + IDR, which also satisfies the SD4 (re)connect rule.

use crate::imzero2::framesink::FrameSink;
use crate::imzero2::inputproto as pb;
use prost::Message as _;

pub enum EncoderTarget {
    File(std::path::PathBuf),
    /// Pre-framed WebSocket payloads (0x01 + VideoChunk) for the carrier.
    Channel(tokio::sync::mpsc::Sender<Vec<u8>>),
}

pub struct EncoderSink {
    child: Option<std::process::Child>,
    stdin: Option<std::process::ChildStdin>,
    drain: Option<std::thread::JoinHandle<()>>,
    width: u32,
    height: u32,
    fps: f32,
    encoder_args: Vec<String>,
    target: EncoderTarget,
    restarts: u32,
}

impl EncoderSink {
    pub fn new(
        width: u32,
        height: u32,
        fps: f32,
        encoder_args: Vec<String>,
        target: EncoderTarget,
    ) -> std::io::Result<Self> {
        let mut sink = Self {
            child: None,
            stdin: None,
            drain: None,
            width,
            height,
            fps,
            encoder_args,
            target,
            restarts: 0,
        };
        sink.spawn(false)?;
        Ok(sink)
    }

    fn spawn(&mut self, restart: bool) -> std::io::Result<()> {
        let mut cmd = std::process::Command::new("ffmpeg");
        cmd.arg("-hide_banner")
            .arg("-loglevel")
            .arg("warning")
            .arg("-f")
            .arg("rawvideo")
            .arg("-pix_fmt")
            .arg("bgra")
            .arg("-video_size")
            .arg(format!("{}x{}", self.width, self.height))
            .arg("-framerate")
            .arg(format!("{}", self.fps))
            .arg("-i")
            .arg("pipe:0")
            .args(&self.encoder_args);
        if matches!(self.target, EncoderTarget::Channel(_)) {
            // AU-accurate framing for the live carrier: AUDs delimit access
            // units without slice-header parsing, and SPS/PPS repeat on
            // every key frame so a (re)joining viewer can configure its
            // decoder from the stream alone (ADR-0024 SD4).
            cmd.arg("-bsf:v")
                .arg("h264_metadata=aud=insert,dump_extra=freq=keyframe");
        }
        cmd.arg("-f")
            .arg("h264")
            .arg("pipe:1")
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            // ffmpeg diagnostics join our stderr; this binary's stdout is the
            // FFFI2 data channel and stays untouched.
            .stderr(std::process::Stdio::inherit());
        tracing::info!(args=?cmd.get_args().collect::<Vec<_>>(), restart, "spawning ffmpeg encoder");
        let mut child = cmd.spawn()?;
        let stdin = child.stdin.take();
        let stdout = child.stdout.take();
        let drain = match &self.target {
            EncoderTarget::File(path) => {
                let path = path.clone();
                std::thread::Builder::new()
                    .name("imzero2-h264-drain".to_owned())
                    .spawn(move || drain_to_file(stdout, &path, restart))?
            }
            EncoderTarget::Channel(tx) => {
                let tx = tx.clone();
                std::thread::Builder::new()
                    .name("imzero2-h264-drain".to_owned())
                    .spawn(move || drain_to_channel(stdout, &tx))?
            }
        };
        self.child = Some(child);
        self.stdin = stdin;
        self.drain = Some(drain);
        Ok(())
    }

    fn reap(&mut self) {
        self.stdin = None; // closes the pipe → ffmpeg flushes and exits
        if let Some(mut child) = self.child.take() {
            match child.wait() {
                Ok(status) => tracing::info!(%status, "ffmpeg encoder exited"),
                Err(e) => tracing::error!(error=%e, "failed to reap ffmpeg encoder"),
            }
        }
        if let Some(drain) = self.drain.take() {
            let _ = drain.join();
        }
    }
}

impl FrameSink for EncoderSink {
    fn on_frame(&mut self, bgra: &[u8], _width: u32, _height: u32, _frame_idx: u64) {
        let write_failed = match self.stdin.as_mut() {
            Some(stdin) => std::io::Write::write_all(stdin, bgra).is_err(),
            None => true,
        };
        if write_failed {
            // SD3 supervision: log, reap, respawn. The new process restarts
            // the elementary stream at SPS/PPS + IDR, so downstream
            // consumers can resynchronize.
            self.restarts += 1;
            tracing::error!(restarts = self.restarts, "ffmpeg encoder write failed — restarting encoder");
            self.reap();
            std::thread::sleep(std::time::Duration::from_millis(500));
            if let Err(e) = self.spawn(true) {
                tracing::error!(error=%e, "ffmpeg encoder respawn failed; will retry on next frame");
            }
        }
    }
}

impl Drop for EncoderSink {
    fn drop(&mut self) {
        self.reap();
    }
}

fn drain_to_file(stdout: Option<std::process::ChildStdout>, path: &std::path::Path, append: bool) {
    let file = std::fs::OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(!append)
        .append(append)
        .open(path);
    let mut file = match file {
        Ok(f) => std::io::BufWriter::new(f),
        Err(e) => {
            tracing::error!(path=%path.display(), error=%e, "cannot open h264 output file; encoder output discarded");
            if let Some(mut so) = stdout {
                let _ = std::io::copy(&mut so, &mut std::io::sink());
            }
            return;
        }
    };
    if let Some(mut so) = stdout {
        match std::io::copy(&mut so, &mut file) {
            Ok(n) => tracing::info!(bytes = n, path=%path.display(), "h264 drain finished"),
            Err(e) => tracing::error!(error=%e, "h264 drain failed"),
        }
    }
    let _ = std::io::Write::flush(&mut file);
}

/// NAL unit type of the Annex-B unit starting at `idx` (the position of
/// the 00 00 01 prefix).
fn nal_type_at(buf: &[u8], idx: usize) -> Option<u8> {
    buf.get(idx + 3).map(|b| b & 0x1f)
}

/// Positions of all 00-00-01 start codes in `buf` whose NAL type is AUD
/// (9). A leading 00 of a 4-byte start code stays with the preceding AU,
/// which decoders ignore.
fn find_aud_positions(buf: &[u8]) -> Vec<usize> {
    let mut out = Vec::new();
    let mut i = 0usize;
    while i + 3 < buf.len() {
        if buf.get(i) == Some(&0) && buf.get(i + 1) == Some(&0) && buf.get(i + 2) == Some(&1) {
            if nal_type_at(buf, i) == Some(9) {
                out.push(i);
            }
            i += 3;
        } else {
            i += 1;
        }
    }
    out
}

/// True when the access unit contains an IDR slice (NAL type 5).
fn au_has_idr(au: &[u8]) -> bool {
    let mut i = 0usize;
    while i + 3 < au.len() {
        if au.get(i) == Some(&0) && au.get(i + 1) == Some(&0) && au.get(i + 2) == Some(&1) {
            if nal_type_at(au, i) == Some(5) {
                return true;
            }
            i += 3;
        } else {
            i += 1;
        }
    }
    false
}

/// Incrementally split ffmpeg's Annex-B output into access units (AUD to
/// AUD), wrap each in the SD4 envelope, and push framed payloads into the
/// carrier channel. Returns when ffmpeg's stdout closes or the channel is
/// dropped (viewer disconnected).
fn drain_to_channel(
    stdout: Option<std::process::ChildStdout>,
    tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
) {
    use std::io::Read as _;
    let Some(mut so) = stdout else { return };
    let started = std::time::Instant::now();
    let mut pending: Vec<u8> = Vec::with_capacity(256 * 1024);
    let mut chunk = vec![0u8; 64 * 1024];
    let mut frame_index: u64 = 0;
    let mut sent_bytes: u64 = 0;
    loop {
        let n = match so.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => n,
            Err(e) => {
                tracing::error!(error=%e, "h264 drain read failed");
                break;
            }
        };
        pending.extend_from_slice(chunk.get(..n).unwrap_or_default());
        let auds = find_aud_positions(&pending);
        // Emit every complete AU (between consecutive AUDs); keep the tail
        // from the last AUD onwards — it is still accumulating.
        if auds.len() >= 2 {
            let last = *auds.last().unwrap_or(&0);
            for pair in auds.windows(2) {
                if let &[a, b] = pair {
                    let au = pending.get(a..b).unwrap_or_default();
                    let envelope = pb::VideoChunk {
                        frame_index,
                        timestamp_micros: started.elapsed().as_micros() as u64,
                        keyframe: au_has_idr(au),
                        data: au.to_vec(),
                    };
                    frame_index += 1;
                    let mut framed = Vec::with_capacity(1 + envelope.encoded_len());
                    framed.push(pb::PREFIX_VIDEO);
                    if envelope.encode(&mut framed).is_err() {
                        continue; // Vec encode cannot fail in practice
                    }
                    sent_bytes += framed.len() as u64;
                    if tx.blocking_send(framed).is_err() {
                        tracing::info!(frames = frame_index, "viewer channel closed — stopping h264 drain");
                        // Keep consuming so ffmpeg can exit cleanly once our
                        // stdin side closes.
                        let _ = std::io::copy(&mut so, &mut std::io::sink());
                        return;
                    }
                }
            }
            pending.drain(..last);
        }
    }
    tracing::info!(frames = frame_index, bytes = sent_bytes, "h264 drain finished (encoder eof)");
}
