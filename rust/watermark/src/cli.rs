//! Stage 11 — command-line interface: `encode`, `decode`, `roundtrip`, `sweep`.

use std::path::PathBuf;

use clap::{Parser, Subcommand};

use watermark::codec::{ffmpeg_available, roundtrip, Codec};
use watermark::decode::{decode_frame, recover_words};
use watermark::fec::{encode_info, BITS_PER_WORD, N_WORDS};
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
            let spec = TileSpec::new(delta);
            let base = load_or_synth(input.as_ref(), &size)?;
            let p = parse_or_random(payload.as_deref())?;
            let wm = encode_frame(&base, &p, &spec);
            wm.save_png(&output)?;
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
            let spec = TileSpec::new(delta);
            let base = load_or_synth(input.as_ref(), &size)?;
            let p = parse_or_random(payload.as_deref())?;
            let wm = encode_frame(&base, &p, &spec);
            let truth = encode_info(&p.to_info_bits());

            println!("payload {}  ({}x{} frame)", p.to_hex(), base.w, base.h);
            println!("codec  crf  pre_golay_BER  single_tile_OK");
            for c in codecs(&codec)? {
                let q = crf.unwrap_or_else(|| c.default_crf());
                let dec = roundtrip(&wm, c, q)?;
                let ber = single_tile_ber(&dec, &truth, &spec);
                let ok = decode_frame(&dec, &spec).ok() == Some(p);
                println!("{:5}  {q:3}  {ber:13.5}  {ok}", c.name());
            }
            Ok(())
        }
        Cmd::Sweep { input, codec, size } => {
            if !ffmpeg_available() {
                return Err("ffmpeg not found on PATH".into());
            }
            let base = load_or_synth(input.as_ref(), &size)?;
            println!("delta is the visibility knob; BER is single-tile pre-Golay.");
            println!("codec  crf  delta  pre_golay_BER  OK");
            for c in codecs(&codec)? {
                let q = c.default_crf();
                for delta in [2.0f32, 4.0, 6.0, 8.0, 10.0, 12.0, 16.0] {
                    let spec = TileSpec::new(delta);
                    let p = Payload(rand::random());
                    let wm = encode_frame(&base, &p, &spec);
                    let truth = encode_info(&p.to_info_bits());
                    let dec = roundtrip(&wm, c, q)?;
                    let ber = single_tile_ber(&dec, &truth, &spec);
                    let ok = decode_frame(&dec, &spec).ok() == Some(p);
                    println!("{:5}  {q:3}  {delta:5.1}  {ber:13.5}  {ok}", c.name());
                }
            }
            Ok(())
        }
    }
}

fn single_tile_ber(dec: &LumaFrame, truth: &[u32; N_WORDS], spec: &TileSpec) -> f64 {
    let rec = recover_words(dec, &[(0, 0)], spec);
    let errs: u64 = rec
        .words
        .iter()
        .zip(truth.iter())
        .map(|(r, t)| (r ^ t).count_ones() as u64)
        .sum();
    errs as f64 / (N_WORDS * BITS_PER_WORD) as f64
}

fn load_or_synth(
    input: Option<&PathBuf>,
    size: &str,
) -> Result<LumaFrame, Box<dyn std::error::Error>> {
    match input {
        Some(p) => Ok(LumaFrame::load_png(p)?),
        None => {
            let (w, h) = parse_size(size)?;
            Ok(LumaFrame::synthetic_natural(w, h, 1))
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
