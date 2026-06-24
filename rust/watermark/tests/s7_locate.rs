//! Stage 7 acceptance — phase recovery within ±1 px and correct complete-tile
//! enumeration over 200 random integer offsets (including single-tile offsets).

use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

use watermark::decode::decode_at_origins;
use watermark::locate::locate;
use watermark::render::encode_frame;
use watermark::{LumaFrame, Payload, TileSpec};

fn circ_dist(a: f32, b: f32, period: f32) -> f32 {
    let d = (a - b).rem_euclid(period);
    d.min(period - d)
}

/// True crop-local tile origins for a window of `cw×ch` taken at `(ox,oy)` from a
/// frame whose tiles are aligned at the origin.
fn true_origins(ox: u32, oy: u32, cw: u32, ch: u32, spec: &TileSpec) -> Vec<(u32, u32)> {
    let mut v = Vec::new();
    let mut ky = 0u32;
    while ky * spec.tile_h < oy + ch {
        let fy = ky * spec.tile_h;
        if fy >= oy && fy + spec.tile_h <= oy + ch {
            let mut kx = 0u32;
            while kx * spec.tile_w < ox + cw {
                let fx = kx * spec.tile_w;
                if fx >= ox && fx + spec.tile_w <= ox + cw {
                    v.push((fx - ox, fy - oy));
                }
                kx += 1;
            }
        }
        ky += 1;
    }
    v
}

fn run(seed: u64, natural: bool) {
    let spec = TileSpec::default();
    let (fw, fh) = (3 * spec.tile_w, 3 * spec.tile_h);
    let base = if natural {
        LumaFrame::synthetic_natural(fw, fh, seed)
    } else {
        LumaFrame::filled(fw, fh, 120.0)
    };
    let payload = Payload([0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88]);
    let full = encode_frame(&base, &payload, &spec);

    let (cw, ch) = (spec.window_w(), spec.window_h()); // 464×432
    let mut rng = StdRng::seed_from_u64(seed);
    let mut singles = 0;

    for _ in 0..200 {
        let ox = rng.gen_range(0..spec.tile_w);
        let oy = rng.gen_range(0..spec.tile_h);
        let crop = full.crop(ox, oy, cw, ch);

        let loc = locate(&crop, &spec);
        let exp_x = (spec.tile_w - ox % spec.tile_w) % spec.tile_w;
        let exp_y = (spec.tile_h - oy % spec.tile_h) % spec.tile_h;
        assert!(
            circ_dist(loc.phase_x, exp_x as f32, spec.tile_w as f32) <= 1.0,
            "phase_x off: got {} want {exp_x} (ox={ox})",
            loc.phase_x
        );
        assert!(
            circ_dist(loc.phase_y, exp_y as f32, spec.tile_h as f32) <= 1.0,
            "phase_y off: got {} want {exp_y} (oy={oy})",
            loc.phase_y
        );

        let got = spec.complete_tile_origins(cw, ch, loc.phase_x, loc.phase_y);
        let mut got_sorted = got.clone();
        got_sorted.sort_unstable();
        let mut want = true_origins(ox, oy, cw, ch, &spec);
        want.sort_unstable();
        assert_eq!(got_sorted, want, "origin set mismatch at ox={ox} oy={oy}");

        if want.len() == 1 {
            singles += 1;
        }

        // End-to-end on clean data: locate then decode the crop.
        let decoded = decode_at_origins(&crop, &got, &spec).expect("locate→decode");
        assert_eq!(decoded, payload);
    }
    assert!(singles > 0, "expected some single-tile worst-case offsets");
    eprintln!("natural={natural}: {singles}/200 single-tile offsets, all located+decoded");
}

#[test]
fn locate_flat_base() {
    run(0xF1A7, false);
}

#[test]
fn locate_natural_base() {
    run(0x9A7, true);
}
