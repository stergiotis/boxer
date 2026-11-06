//! The colours a terminal reports of itself: its foreground and background
//! (`OSC 10`, `OSC 11`) and its colour scheme (`CSI ? 997 n`), and the
//! answers a program asking for them is given.

use crate::Rgb;

closed! {
    /// The terminal's colour scheme, as `CSI ? 997 ; 1|2 n` reports it
    /// (`references/modern/mode_2031_color_scheme_updates.md`).
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    pub enum Scheme {
        /// The report, which a terminal sends and fux-vt decodes.
        pub fn report(self) -> &'static [u8];
        /// A dark scheme: light text on a dark background.
        Dark => b"\x1b[?997;1n",
        /// A light scheme: dark text on a light background.
        Light => b"\x1b[?997;2n",
    }
}

/// What a terminal said of its colours, as far as it said it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Colours {
    /// The foreground colour (`OSC 10`), if the terminal said.
    pub foreground: Option<Rgb>,
    /// The background colour (`OSC 11`), if the terminal said.
    pub background: Option<Rgb>,
    /// The colour scheme (`CSI ? 997 n`), if the terminal said.
    pub scheme: Option<Scheme>,
}

impl Colours {
    /// The answer to a program's `OSC number ; ?`, if fux knows that colour.
    pub fn answer(&self, number: u8, bel: bool) -> Option<Vec<u8>> {
        let colour = match number {
            10 => self.foreground,
            11 => self.background,
            _ => None,
        };
        colour.map(|c| {
            crate::palette::answer(u16::from(number), None, c, bel)
                .as_bytes()
                .to_vec()
        })
    }
}
