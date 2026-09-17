use watermark::LumaFrame;

pub const CASES: &[&str] = &[
    "black",
    "white",
    "near-black",
    "near-white",
    "gradient",
    "text",
    "checker1",
    "checker8",
    "checker16",
    "stripes",
    "impulses",
    "noise",
];

/// Small bitmap glyphs and pixel-scale patterns need no font or image downloads.
pub fn fixture(name: &str, w: u32, h: u32) -> LumaFrame {
    let mut out = LumaFrame::filled(w, h, 128.0).unwrap();
    let mut state = 0x12345678u32;
    const GLYPHS: [[u8; 7]; 4] = [
        [14, 17, 17, 31, 17, 17, 17],
        [30, 17, 17, 30, 17, 17, 30],
        [14, 17, 16, 16, 16, 17, 14],
        [30, 17, 17, 17, 17, 17, 30],
    ];
    for y in 0..h {
        for x in 0..w {
            let v = match name {
                "black" => 0.0,
                "white" => 255.0,
                "near-black" => 1.0,
                "near-white" => 254.0,
                "gradient" => 255.0 * x as f32 / (w - 1) as f32,
                "text" => {
                    let gx = x % 14;
                    let gy = y % 20;
                    if gx < 10
                        && gy < 14
                        && GLYPHS[((x / 14 + y / 20) % 4) as usize][(gy / 2) as usize]
                            & (1 << (4 - gx / 2))
                            != 0
                    {
                        20.0
                    } else {
                        245.0
                    }
                }
                "checker1" | "checker8" | "checker16" => {
                    let pitch = match name {
                        "checker1" => 1,
                        "checker8" => 8,
                        _ => 16,
                    };
                    if ((x + 3) / pitch + (y + 5) / pitch) % 2 == 0 {
                        0.0
                    } else {
                        255.0
                    }
                }
                "stripes" => {
                    if (x + 2) % 16 < 3 || (y + 1) % 31 < 2 {
                        0.0
                    } else {
                        255.0
                    }
                }
                "impulses" => {
                    if (x + 3 * y) % 97 == 0 {
                        255.0
                    } else {
                        0.0
                    }
                }
                "noise" => {
                    state ^= state << 13;
                    state ^= state >> 17;
                    state ^= state << 5;
                    (state & 255) as f32
                }
                _ => panic!("unknown fixture"),
            };
            out.set(x, y, v);
        }
    }
    out
}
