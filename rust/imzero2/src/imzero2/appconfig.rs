use crate::cli::flags;

#[derive(Debug, Clone)]
pub struct FontTweakConfig {
    pub scale: f32,
    pub y_offset_factor: f32,
    pub y_offset: f32,
}
impl Default for FontTweakConfig {
    fn default() -> Self {
        Self {
            scale: 1.0,
            y_offset_factor: 0.0,
            y_offset: 0.0,
        }
    }
}

#[derive(Debug)]
pub struct AppConfig {
    /// window size
    pub initial_main_window_width: f32,
    /// window size
    pub initial_main_window_height: f32,
    /// window size
    pub inner_min_size_width: f32,
    /// window size
    pub inner_min_size_height: f32,

    /// window title
    pub window_title: String,
    /// app name
    pub app_title: String,

    /// fullscreen
    pub fullscreen: bool,

    /// vsync
    pub vsync: bool,

    /// Colour the root viewport is cleared to each frame
    /// (`-backgroundColorRGBA`, hex `RRGGBB` or `RRGGBBAA`). `None` follows
    /// the active theme's `panel_fill`, which is what keeps the uncovered
    /// root indistinguishable from the panels painted over it.
    ///
    /// Leaving this to eframe's own default is what made the window
    /// semi-transparent under a compositing X11 WM: `eframe::App::clear_color`
    /// defaults to `rgba(12, 12, 12, 180)` — deliberately translucent, so
    /// that switching on `ViewportBuilder::with_transparent` shows an
    /// immediate effect — and whether that alpha then reaches the compositor
    /// is platform business, not something the app decided. The observable is
    /// the window's visual depth: `xwininfo` reports 24 (no alpha channel, so
    /// the alpha is dropped) on the Xwayland/wgpu path measured here, and 32
    /// once transparency is actually requested.
    ///
    /// Desktop (eframe) host only. The headless hosts keep their own opaque
    /// black clear, which is what makes the wgpu and CPU rasterizers
    /// byte-comparable on any pixel the UI does not cover (see
    /// `softraster::CLEAR`).
    pub background_color_rgba: Option<egui::Color32>,

    /// fffi interpreter
    pub fffi_interpreter: bool,

    /// path to main font TTF/OTF file (FontFamily::Proportional base)
    pub main_font_ttf: String,
    /// path to monospace font TTF/OTF file (FontFamily::Monospace base).
    /// Empty leaves Monospace at egui's built-in default (Hack).
    pub mono_font_ttf: String,
    /// path to Phosphor TTF file (ADR-0044 — icon font; fallback in
    /// both families)
    pub phosphor_font_ttf: String,
    /// path to fallback font TTF file (CJK/international coverage;
    /// fallback in both families)
    pub fallback_font_ttf: String,
    /// main font size in pixels
    pub main_font_size: f32,

    /// per-font tweaks (scale, y_offset_factor, y_offset)
    pub main_font_tweak: FontTweakConfig,
    pub mono_font_tweak: FontTweakConfig,
    pub phosphor_font_tweak: FontTweakConfig,
    pub fallback_font_tweak: FontTweakConfig,
}
impl Default for AppConfig {
    fn default() -> AppConfig {
        AppConfig {
            initial_main_window_width: 1024.0f32,
            initial_main_window_height: 796.0f32,
            inner_min_size_width: 400.0f32,
            inner_min_size_height: 300.0f32,
            window_title: "imzero2".to_string(),
            app_title: "imzero2".to_string(),
            fullscreen: false,
            vsync: true,
            background_color_rgba: None,
            fffi_interpreter: true,
            main_font_ttf: String::new(),
            mono_font_ttf: String::new(),
            phosphor_font_ttf: String::new(),
            fallback_font_ttf: String::new(),
            main_font_size: 14.0,
            main_font_tweak: FontTweakConfig::default(),
            mono_font_tweak: FontTweakConfig::default(),
            phosphor_font_tweak: FontTweakConfig::default(),
            fallback_font_tweak: FontTweakConfig::default(),
        }
    }
}

impl AppConfig {
    pub fn parse(&mut self, used: &mut roaring::RoaringBitmap, args: &[String]) {
        self.initial_main_window_width = flags::find_flag_value_default_parsable(
            args,
            used,
            "-initialMainWindowWidth",
            self.initial_main_window_width,
        );
        self.initial_main_window_height = flags::find_flag_value_default_parsable(
            args,
            used,
            "-initialMainWindowHeight",
            self.initial_main_window_height,
        );
        self.fullscreen =
            flags::find_flag_value_default_bool(args, used, "-fullscrreen", self.fullscreen);
        self.vsync = flags::find_flag_value_default_bool(args, used, "-vsync", self.vsync);

        self.window_title =
            flags::find_flag_default(args, used, "-windowTitle", self.window_title.clone())
                .to_owned();
        self.app_title =
            flags::find_flag_default(args, used, "-appTitle", self.window_title.clone()).to_owned();

        self.vsync = flags::find_flag_value_default_bool(args, used, "-vsync", self.vsync);
        self.background_color_rgba = parse_background_color(&flags::find_flag_default(
            args,
            used,
            "-backgroundColorRGBA",
            String::new(),
        ))
        .or(self.background_color_rgba);
        self.fffi_interpreter = flags::find_flag_value_default_bool(
            args,
            used,
            "-fffiInterpreter",
            self.fffi_interpreter,
        );

        self.main_font_ttf =
            flags::find_flag_default(args, used, "-mainFontTTF", self.main_font_ttf.clone())
                .to_owned();
        self.mono_font_ttf =
            flags::find_flag_default(args, used, "-monoFontTTF", self.mono_font_ttf.clone())
                .to_owned();
        self.phosphor_font_ttf = flags::find_flag_default(
            args,
            used,
            "-phosphorFontTTF",
            self.phosphor_font_ttf.clone(),
        )
        .to_owned();
        self.fallback_font_ttf = flags::find_flag_default(
            args,
            used,
            "-fallbackFontTTF",
            self.fallback_font_ttf.clone(),
        )
        .to_owned();
        self.main_font_size = flags::find_flag_value_default_parsable(
            args,
            used,
            "-mainFontSizeInPixels",
            self.main_font_size,
        );

        self.main_font_tweak.scale = flags::find_flag_value_default_parsable(
            args,
            used,
            "-mainFontScale",
            self.main_font_tweak.scale,
        );
        self.main_font_tweak.y_offset_factor = flags::find_flag_value_default_parsable(
            args,
            used,
            "-mainFontYOffsetFactor",
            self.main_font_tweak.y_offset_factor,
        );
        self.main_font_tweak.y_offset = flags::find_flag_value_default_parsable(
            args,
            used,
            "-mainFontYOffset",
            self.main_font_tweak.y_offset,
        );

        self.mono_font_tweak.scale = flags::find_flag_value_default_parsable(
            args,
            used,
            "-monoFontScale",
            self.mono_font_tweak.scale,
        );
        self.mono_font_tweak.y_offset_factor = flags::find_flag_value_default_parsable(
            args,
            used,
            "-monoFontYOffsetFactor",
            self.mono_font_tweak.y_offset_factor,
        );
        self.mono_font_tweak.y_offset = flags::find_flag_value_default_parsable(
            args,
            used,
            "-monoFontYOffset",
            self.mono_font_tweak.y_offset,
        );

        self.phosphor_font_tweak.scale = flags::find_flag_value_default_parsable(
            args,
            used,
            "-phosphorFontScale",
            self.phosphor_font_tweak.scale,
        );
        self.phosphor_font_tweak.y_offset_factor = flags::find_flag_value_default_parsable(
            args,
            used,
            "-phosphorFontYOffsetFactor",
            self.phosphor_font_tweak.y_offset_factor,
        );
        self.phosphor_font_tweak.y_offset = flags::find_flag_value_default_parsable(
            args,
            used,
            "-phosphorFontYOffset",
            self.phosphor_font_tweak.y_offset,
        );

        self.fallback_font_tweak.scale = flags::find_flag_value_default_parsable(
            args,
            used,
            "-fallbackFontScale",
            self.fallback_font_tweak.scale,
        );
        self.fallback_font_tweak.y_offset_factor = flags::find_flag_value_default_parsable(
            args,
            used,
            "-fallbackFontYOffsetFactor",
            self.fallback_font_tweak.y_offset_factor,
        );
        self.fallback_font_tweak.y_offset = flags::find_flag_value_default_parsable(
            args,
            used,
            "-fallbackFontYOffset",
            self.fallback_font_tweak.y_offset,
        );
    }
    pub fn usage(&mut self, w: &mut impl std::io::Write) -> std::io::Result<()> {
        write!(w, "usage:\n")?;
        write!(w, "info flags:\n")?;
        write!(w, "\t-help\n")?;

        write!(w, "general flags:\n")?;
        write!(
            w,
            "\t-initialMainWindowWidth [f32:{}]\n",
            self.inner_min_size_width
        )?;
        write!(
            w,
            "\t-initialMainWindowHeight [f32:{}]\n",
            self.inner_min_size_height
        )?;

        write!(w, "graphics flags:\n")?;
        write!(
            w,
            "\t-vsync [bool:{}]\n",
            if self.vsync { "on" } else { "off" }
        )?;
        write!(
            w,
            "\t-backgroundColorRGBA [hex RRGGBB|RRGGBBAA:{}]\n",
            match self.background_color_rgba {
                Some(c) => {
                    let [r, g, b, a] = c.to_srgba_unmultiplied();
                    format!("{r:02x}{g:02x}{b:02x}{a:02x}")
                }
                None => "theme panel fill".to_owned(),
            }
        )?;

        write!(w, "fffi flags:\n")?;
        write!(
            w,
            "\t-fffiInterpreter [bool:{}]\n",
            if self.fffi_interpreter { "on" } else { "off" }
        )?;

        write!(w, "font flags:\n")?;
        write!(w, "\t-mainFontTTF [string:{}]\n", self.main_font_ttf)?;
        write!(w, "\t-monoFontTTF [string:{}]\n", self.mono_font_ttf)?;
        write!(
            w,
            "\t-phosphorFontTTF [string:{}]\n",
            self.phosphor_font_ttf
        )?;
        write!(
            w,
            "\t-fallbackFontTTF [string:{}]\n",
            self.fallback_font_ttf
        )?;
        write!(w, "\t-mainFontSizeInPixels [f32:{}]\n", self.main_font_size)?;

        write!(w, "font tweak flags:\n")?;
        write!(w, "\t-mainFontScale [f32:{}]\n", self.main_font_tweak.scale)?;
        write!(
            w,
            "\t-mainFontYOffsetFactor [f32:{}]\n",
            self.main_font_tweak.y_offset_factor
        )?;
        write!(
            w,
            "\t-mainFontYOffset [f32:{}]\n",
            self.main_font_tweak.y_offset
        )?;
        write!(w, "\t-monoFontScale [f32:{}]\n", self.mono_font_tweak.scale)?;
        write!(
            w,
            "\t-monoFontYOffsetFactor [f32:{}]\n",
            self.mono_font_tweak.y_offset_factor
        )?;
        write!(
            w,
            "\t-monoFontYOffset [f32:{}]\n",
            self.mono_font_tweak.y_offset
        )?;
        write!(
            w,
            "\t-phosphorFontScale [f32:{}]\n",
            self.phosphor_font_tweak.scale
        )?;
        write!(
            w,
            "\t-phosphorFontYOffsetFactor [f32:{}]\n",
            self.phosphor_font_tweak.y_offset_factor
        )?;
        write!(
            w,
            "\t-phosphorFontYOffset [f32:{}]\n",
            self.phosphor_font_tweak.y_offset
        )?;
        write!(
            w,
            "\t-fallbackFontScale [f32:{}]\n",
            self.fallback_font_tweak.scale
        )?;
        write!(
            w,
            "\t-fallbackFontYOffsetFactor [f32:{}]\n",
            self.fallback_font_tweak.y_offset_factor
        )?;
        write!(
            w,
            "\t-fallbackFontYOffset [f32:{}]\n",
            self.fallback_font_tweak.y_offset
        )?;
        return Ok(());
    }
}

/// Parses `RRGGBB` / `RRGGBBAA` hex (a leading `#` is tolerated) into a
/// colour. Alpha is read as *straight*, not premultiplied, so `8f8f8fff` is
/// an opaque grey and `8f8f8f80` is the same grey at half coverage.
///
/// An unparsable value warns and yields `None` instead of aborting, unlike
/// the geometry and font flags, where `flags::` panics. The difference is
/// that a fallback exists here and is well defined — the theme's
/// `panel_fill` — so a typo'd colour is not worth refusing to start over.
fn parse_background_color(s: &str) -> Option<egui::Color32> {
    let h = s.strip_prefix('#').unwrap_or(s);
    if h.is_empty() {
        return None;
    }
    // The byte slicing below is not char-boundary safe, so establish that
    // every byte is a hex digit before indexing into it.
    if !matches!(h.len(), 6 | 8) || !h.bytes().all(|b| b.is_ascii_hexdigit()) {
        tracing::warn!(
            value = s,
            "ignoring malformed -backgroundColorRGBA, expected RRGGBB or RRGGBBAA hex"
        );
        return None;
    }
    let byte = |i: usize| {
        u8::from_str_radix(&h[i..i + 2], 16).expect("two validated hex digits parse as u8")
    };
    let (r, g, b) = (byte(0), byte(2), byte(4));
    Some(if h.len() == 8 {
        egui::Color32::from_rgba_unmultiplied(r, g, b, byte(6))
    } else {
        egui::Color32::from_rgb(r, g, b)
    })
}

#[cfg(test)]
mod tests {
    use super::parse_background_color as parse;

    #[test]
    fn six_digit_hex_is_opaque() {
        assert_eq!(
            parse("8f8f8f"),
            Some(egui::Color32::from_rgb(143, 143, 143))
        );
        assert_eq!(parse("#0b0e0f"), Some(egui::Color32::from_rgb(11, 14, 15)));
    }

    #[test]
    fn eight_digit_hex_carries_straight_alpha() {
        // An explicit ...ff must land on exactly the same colour as the
        // six-digit form — nothing may read it as near-opaque and drag the
        // window onto the transparency path.
        assert_eq!(parse("8f8f8fff"), parse("8f8f8f"));
        let half = parse("8f8f8f80").expect("well-formed");
        assert_eq!(half.to_srgba_unmultiplied(), [143, 143, 143, 128]);
    }

    #[test]
    fn absent_and_malformed_both_fall_back() {
        // "\u{e9}\u{e9}\u{e9}" is three chars but six *bytes*: it passes the
        // length check and would panic on a byte-indexed slice, so it is the
        // case the ascii-hexdigit guard exists for.
        for s in [
            "",
            "8f8f8",
            "8f8f8f0",
            "8f8f8f0ff",
            "not-a-colour",
            "8f8f8g",
            "\u{e9}\u{e9}\u{e9}",
        ] {
            assert_eq!(parse(s), None, "input {s:?}");
        }
    }
}
