//! Stage 11 — command-line interface: `encode`, `decode`, `roundtrip`, `sweep`.

use std::path::PathBuf;

use clap::{Parser, Subcommand};

use rand::{RngExt, SeedableRng};
use watermark::codec::{ffmpeg_available, roundtrip, Codec};
use watermark::decode::{decode_at_origins, decode_frame, recover_words};
use watermark::fec::{encode_info, BITS_PER_WORD, N_WORDS};
use watermark::image_io::ImageFrame;
use watermark::render::encode_frame;
use watermark::{Error, LumaFrame, Payload, TileSpec};

#[derive(Parser)]
#[command(
    name = "watermark",
    version,
    about = "Tiled luminance-grid watermark (see EXPLANATION.md)"
)]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Embed a payload into a base PNG and write the watermarked PNG.
    Encode {
        /// Base image PNG. If omitted, a synthetic base of --size is used.
        #[arg(short, long)]
        input: Option<PathBuf>,
        /// Output watermarked PNG.
        #[arg(short, long)]
        output: PathBuf,
        /// 16-hex-digit payload. If omitted, a random payload is generated.
        #[arg(long)]
        payload: Option<String>,
        /// Luma delta (0..255 scale).
        #[arg(long, default_value_t = 8.0)]
        delta: f32,
        /// Synthetic base size WxH when --input is omitted.
        #[arg(long, default_value = "1280x720")]
        size: String,
    },
    /// Decode a payload from a watermarked PNG (or any 464×432+ crop of one).
    Decode {
        /// Watermarked PNG.
        #[arg(short, long)]
        input: PathBuf,
    },
    /// Render, run through ffmpeg codecs, and report BER + recovery.
    Roundtrip {
        /// Base image PNG. If omitted, a synthetic base of --size is used.
        #[arg(short, long)]
        input: Option<PathBuf>,
        /// Codec: h264, vp9, av1, or all.
        #[arg(long, default_value = "all")]
        codec: String,
        /// CRF; defaults to the per-codec recommended value.
        #[arg(long)]
        crf: Option<u32>,
        /// 16-hex-digit payload (random if omitted).
        #[arg(long)]
        payload: Option<String>,
        /// Luma delta.
        #[arg(long, default_value_t = 8.0)]
        delta: f32,
        #[arg(long, default_value = "1280x720")]
        size: String,
    },
    /// Sweep the luma delta and report the visibility/robustness trade-off.
    Sweep {
        #[arg(short, long)]
        input: Option<PathBuf>,
        #[arg(long, default_value = "all")]
        codec: String,
        #[arg(long, default_value = "1280x720")]
        size: String,
        /// Seed shared across contrast and codec arms.
        #[arg(long, default_value_t = 1)]
        seed: u64,
    },
}

/// Parse and run the CLI.
pub fn run() -> Result<(), Box<dyn std::error::Error>> {
    match Cli::parse().cmd {
        Cmd::Encode {
            input,
            output,
            payload,
            delta,
            size,
        } => {
            let spec = TileSpec::new(delta)?;
            let image = input.as_ref().map(ImageFrame::load_png).transpose()?;
            let base = match &image {
                Some(i) => i.luma()?,
                None => load_or_synth(None, &size)?,
            };
            let p = parse_or_random(payload.as_deref())?;
            let wm = encode_frame(&base, &p, &spec)?;
            match image {
                Some(i) => i.save_with_luma(&wm, &output)?,
                None => wm.save_png(&output)?,
            };
            println!("encoded payload {} -> {}", p.to_hex(), output.display());
            Ok(())
        }
        Cmd::Decode { input } => {
            let spec = TileSpec::default();
            let frame = LumaFrame::load_png(&input)?;
            match decode_frame(&frame, &spec) {
                Ok(p) => {
                    println!("{}", p.to_hex());
                    Ok(())
                }
                Err(Error::CrcMismatch) => Err("no valid payload (CRC mismatch)".into()),
                Err(e) => Err(Box::new(e)),
            }
        }
        Cmd::Roundtrip {
            input,
            codec,
            crf,
            payload,
            delta,
            size,
        } => {
            if !ffmpeg_available() {
                return Err("ffmpeg not found on PATH".into());
            }
            let spec = TileSpec::new(delta)?;
            let base = load_or_synth(input.as_ref(), &size)?;
            let p = parse_or_random(payload.as_deref())?;
            let wm = encode_frame(&base, &p, &spec)?;
            let truth = encode_info(&p.to_info_bits());

            println!("payload {}  ({}x{} frame)", p.to_hex(), base.w(), base.h());
            println!("codec  crf  pre_golay_BER  single_tile_OK  frame_OK  mean_abs_luma_change  max_abs_luma_change");
            let (mean, max) = distortion(&base, &wm);
            for c in codecs(&codec)? {
                let q = crf.unwrap_or_else(|| c.default_crf());
                let dec = roundtrip(&wm, c, q)?;
                let ber = single_tile_ber(&dec, &truth, &spec)?;
                let (single, ok) = recovery(&dec, &p, &spec);
                println!(
                    "{:5}  {q:3}  {ber:13.5}  {single}  {ok}  {mean:.3}  {max:.3}",
                    c.name()
                );
            }
            Ok(())
        }
        Cmd::Sweep {
            input,
            codec,
            size,
            seed,
        } => {
            if !ffmpeg_available() {
                return Err("ffmpeg not found on PATH".into());
            }
            let base = load_or_synth(input.as_ref(), &size)?;
            let mut rng = rand::rngs::StdRng::seed_from_u64(seed);
            let p = Payload(rng.random());
            println!(
                "seed {seed} payload {}; delta is target contrast, not distortion",
                p.to_hex()
            );
            println!("codec  crf  delta  pre_golay_BER  single_tile_OK  frame_OK  mean_abs_luma_change  max_abs_luma_change");
            for c in codecs(&codec)? {
                let q = c.default_crf();
                for delta in [2.0f32, 4.0, 6.0, 8.0, 10.0, 12.0, 16.0] {
                    let spec = TileSpec::new(delta)?;
                    let wm = encode_frame(&base, &p, &spec)?;
                    let truth = encode_info(&p.to_info_bits());
                    let dec = roundtrip(&wm, c, q)?;
                    let ber = single_tile_ber(&dec, &truth, &spec)?;
                    let (single, ok) = recovery(&dec, &p, &spec);
                    let (mean, max) = distortion(&base, &wm);
                    println!(
                        "{:5}  {q:3}  {delta:5.1}  {ber:13.5}  {single}  {ok}  {mean:.3}  {max:.3}",
                        c.name()
                    );
                }
            }
            Ok(())
        }
    }
}

fn single_tile_ber(dec: &LumaFrame, truth: &[u32; N_WORDS], spec: &TileSpec) -> Result<f64, Error> {
    let rec = recover_words(dec, &[(0, 0)], spec)?;
    let errs: u64 = rec
        .words
        .iter()
        .zip(truth.iter())
        .map(|(r, t)| (r ^ t).count_ones() as u64)
        .sum();
    Ok(errs as f64 / (N_WORDS * BITS_PER_WORD) as f64)
}

fn load_or_synth(
    input: Option<&PathBuf>,
    size: &str,
) -> Result<LumaFrame, Box<dyn std::error::Error>> {
    match input {
        Some(p) => Ok(LumaFrame::load_png(p)?),
        None => {
            let (w, h) = parse_size(size)?;
            Ok(LumaFrame::synthetic_natural(w, h, 1)?)
        }
    }
}

fn parse_or_random(payload: Option<&str>) -> Result<Payload, Box<dyn std::error::Error>> {
    match payload {
        Some(h) => Ok(Payload::from_hex(h)?),
        None => Ok(Payload(rand::random())),
    }
}

fn parse_size(s: &str) -> Result<(u32, u32), Box<dyn std::error::Error>> {
    let (w, h) = s
        .split_once(['x', 'X'])
        .ok_or("size must be WxH, e.g. 1280x720")?;
    Ok((w.trim().parse()?, h.trim().parse()?))
}

fn codecs(arg: &str) -> Result<Vec<Codec>, Box<dyn std::error::Error>> {
    if arg.eq_ignore_ascii_case("all") {
        Ok(Codec::all().to_vec())
    } else {
        Ok(vec![
            Codec::parse(arg).ok_or_else(|| format!("unknown codec '{arg}'"))?
        ])
    }
}

fn distortion(base: &LumaFrame, marked: &LumaFrame) -> (f64, f32) {
    let mut sum = 0.0;
    let mut max = 0.0f32;
    for (&a, &b) in base.pixels().iter().zip(marked.pixels()) {
        let d = (a - b).abs();
        sum += d as f64;
        max = max.max(d);
    }
    (sum / base.pixels().len() as f64, max)
}

fn recovery(frame: &LumaFrame, payload: &Payload, spec: &TileSpec) -> (bool, bool) {
    (
        decode_at_origins(frame, &[(0, 0)], spec).ok() == Some(*payload),
        decode_frame(frame, spec).ok() == Some(*payload),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn first_tile_failure_is_not_reported_as_single_tile_success() {
        let s = TileSpec::default();
        let p = Payload([9; 8]);
        let base = LumaFrame::filled(928, 864, 128.0).unwrap();
        let mut wm = encode_frame(&base, &p, &s).unwrap();
        // Flip four data bits in one word of the first tile only. The other
        // fifteen tiles still recover; single-tile correction must reject four.
        for cell in s.cells().iter().filter(|c| {
            matches!(
                c.kind,
                watermark::layout::CellKind::Data {
                    word: 0,
                    bit: 0..=3
                }
            )
        }) {
            let (x, y, n) = s.inner_rect(cell.col as u32, cell.row as u32);
            for dy in 0..n {
                for dx in 0..n {
                    wm.set(x + dx, y + dy, 256.0 - wm.at(x + dx, y + dy));
                }
            }
        }
        assert_eq!(recovery(&wm, &p, &s), (false, true));
    }
}
