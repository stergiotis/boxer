//! The console widget: a terminal screen drawn from the bytes Go feeds it,
//! and the bytes for the program it hands back (ADR-0359).
//!
//! The VT core is the vendored `fux-vt`. Everything the widget asks of it goes
//! through [`Core`], so another core can stand in without the Go surface
//! changing (SD6). The widget never talks to a process: Go feeds output bytes
//! addressed by their offset in the stream (SD2) and receives, as rows tagged
//! by kind, what the program should be sent and what happened (SD1, SD3).

use std::collections::HashMap;

use egui::{Align2, Color32, FontId, Pos2, Rect, Sense, Stroke, Vec2};
use fux_vt::keys::{Direction, Key, KeyPress, Keystroke, Modifiers, encode};

/// What a row of the console register carries.
pub mod kind {
    /// Bytes a key produced, for the program.
    pub const KEY: u8 = 1;
    /// A paste, framed as the program asked, for the program.
    pub const PASTE: u8 = 2;
    /// A focus report (`CSI I` / `CSI O`), for the program.
    pub const FOCUS: u8 = 3;
    /// An answer the core produced to the program's query, for the program.
    pub const REPLY: u8 = 4;
    /// The grid's size changed: columns then rows, each a little-endian u16.
    pub const SIZE: u8 = 16;
    /// How many bytes of the stream the core has read: a little-endian u64.
    pub const ACK: u8 = 17;
    /// The window title the program set (OSC 0 or 2), UTF-8 as sent.
    pub const TITLE: u8 = 18;
    /// The program rang the bell.
    pub const BELL: u8 = 19;
    /// An OSC 52 set request: the selection parameter, a NUL, the data as sent.
    pub const CLIPBOARD: u8 = 20;
    /// Bytes were skipped: the core had read fewer than the offset Go sent
    /// (its state was dropped). From and to, each a little-endian u64.
    pub const GAP: u8 = 21;
    /// A desktop notification (OSC 9, 777 or 99), as sent.
    pub const NOTIFICATION: u8 = 22;
    /// A shell command began (OSC 133 `C`).
    pub const COMMAND_STARTED: u8 = 23;
    /// A shell command ended (OSC 133 `D`): its status as a little-endian i32,
    /// or nothing when the shell gave none.
    pub const COMMAND_ENDED: u8 = 24;
    /// The screen changed: the stream offset it shows (a little-endian u64),
    /// columns, rows, the cursor's column and row (each a little-endian u16),
    /// a flags byte (bit 0: the cursor is shown), then the screen's text as
    /// the accessibility node carries it. Sent at most every
    /// [`super::SCREEN_EVERY_S`] seconds, and only after a change.
    pub const SCREEN: u8 = 25;
}

/// The shortest time between two screen reports of one console, in seconds.
pub const SCREEN_EVERY_S: f64 = 0.1;

/// One row for Go: the widget it belongs to, what it is, its bytes.
pub struct ConsoleRow {
    pub id: u64,
    pub kind: u8,
    pub payload: Vec<u8>,
}

/// The per-frame settings Go sends with the feed.
pub struct ConsoleArgs<'a> {
    pub id: u64,
    pub offset: u64,
    pub feed: &'a [u8],
    pub history_bytes: u32,
    pub font_size: f32,
    /// The widget's size in points; zero on an axis takes what is left.
    pub size: Vec2,
    /// A fixed grid, columns then rows; zero on an axis follows the room.
    pub grid: (u16, u16),
}

// ---------------------------------------------------------------- the core seam

/// What the widget needs from a VT core (ADR-0359 SD6).
trait Core {
    /// Reads `bytes`, collecting what the core says into `out`.
    fn feed(&mut self, bytes: &[u8], out: &mut CoreOut);
    /// Resizes the grid; false if the core refused.
    fn resize(&mut self, cols: u16, rows: u16) -> bool;
    /// The visible screen, row by row.
    fn rows(&self) -> u16;
    fn cols(&self) -> u16;
    /// Calls `cell` for every glyph of row `row`: its column, text, whether it
    /// is two columns wide, and its colours and styles.
    fn row_cells(&self, row: u16, cell: &mut dyn FnMut(u16, &str, bool, Style));
    /// The cursor, if shown: row, column, shape.
    fn cursor(&self) -> Option<(u16, u16, CursorStyle)>;
    /// The bytes for a key press, in the mode the program asked for.
    fn key(&self, press: KeyPress, out: &mut Vec<u8>);
    /// The bytes for a paste, framed if the program asked for brackets.
    fn paste(&self, text: &str, out: &mut Vec<u8>);
    /// The focus report, if the program asked for them.
    fn focus_report(&self, focused: bool) -> Option<&'static [u8]>;
    /// A palette entry the program set, if it set one.
    fn palette(&self, index: u8) -> Option<Color32>;
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum CursorStyle {
    Block,
    Underline,
    Bar,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Ink {
    Default,
    Index(u8),
    Rgb(Color32),
}

#[derive(Clone, Copy)]
struct Style {
    fg: Ink,
    bg: Ink,
    bold: bool,
    dim: bool,
    inverse: bool,
    hidden: bool,
    underline: bool,
    strikeout: bool,
}

/// What the core said while it read: answers for the program and events.
#[derive(Default)]
struct CoreOut {
    replies: Vec<Vec<u8>>,
    events: Vec<(u8, Vec<u8>)>,
    /// Colour queries the host answers: the colour asked for and whether the
    /// query ended with BEL.
    color_queries: Vec<(u8, bool)>,
}

struct Fux {
    parser: fux_vt::Parser,
}

struct FuxSink<'a>(&'a mut CoreOut);

impl fux_vt::Sink for FuxSink<'_> {
    fn reply(&mut self, bytes: &[u8]) {
        self.0.replies.push(bytes.to_vec());
    }
    fn event(&mut self, event: fux_vt::Event<'_>) {
        use fux_vt::Event as E;
        match event {
            E::Title(kept) => self.0.events.push((kind::TITLE, kept.bytes.to_vec())),
            E::IconName(_) => {}
            E::Bell => self.0.events.push((kind::BELL, Vec::new())),
            E::Notification(kept) => self.0.events.push((kind::NOTIFICATION, kept.bytes.to_vec())),
            E::Clipboard { selection, data } => {
                let mut payload = selection.to_vec();
                payload.push(0);
                payload.extend_from_slice(data.bytes);
                self.0.events.push((kind::CLIPBOARD, payload));
            }
            E::ColorQuery { number, bel } => self.0.color_queries.push((number, bel)),
            E::CommandStarted => self.0.events.push((kind::COMMAND_STARTED, Vec::new())),
            E::CommandEnded(status) => self.0.events.push((
                kind::COMMAND_ENDED,
                status.map(|s| s.to_le_bytes().to_vec()).unwrap_or_default(),
            )),
        }
    }
}

impl Fux {
    fn new(cols: u16, rows: u16, history_bytes: usize) -> Option<Self> {
        use fux_vt::Feature::{
            Events, ExtendedReplies, Hyperlinks, KittyKeyboard, ModeReports, Palette, PromptMarks,
            Reflow, SettingReports, SizeReports,
        };
        let mut options = fux_vt::Options::new();
        for feature in [
            Events,
            ExtendedReplies,
            ModeReports,
            SizeReports,
            KittyKeyboard,
            Reflow,
            Hyperlinks,
            PromptMarks,
            SettingReports,
            Palette,
        ] {
            options = options.with(feature);
        }
        options = options.with_identity(Some(fux_vt::Identity {
            name: "imzero2",
            version: env!("CARGO_PKG_VERSION"),
        }));
        let size = fux_vt::Size::new(rows, cols).ok()?;
        let parser = fux_vt::Parser::with_options(size, history_bytes, options).ok()?;
        Some(Self { parser })
    }
}

fn ink(color: fux_vt::Color) -> Ink {
    match color {
        fux_vt::Color::Default => Ink::Default,
        fux_vt::Color::Idx(i) => Ink::Index(i),
        fux_vt::Color::Rgb(c) => Ink::Rgb(Color32::from_rgb(c.r, c.g, c.b)),
    }
}

impl Core for Fux {
    fn feed(&mut self, bytes: &[u8], out: &mut CoreOut) {
        // Every byte is read; a refusal loses only what it was for (a row of
        // history), and the screen is good to draw either way.
        let _refused = self.parser.process_all(bytes, &mut FuxSink(out));
    }
    fn resize(&mut self, cols: u16, rows: u16) -> bool {
        match fux_vt::Size::new(rows, cols) {
            Ok(size) => self.parser.resize(size).is_ok(),
            Err(_) => false,
        }
    }
    fn rows(&self) -> u16 {
        self.parser.screen().window().rows()
    }
    fn cols(&self) -> u16 {
        self.parser.screen().window().cols()
    }
    fn row_cells(&self, row: u16, cell: &mut dyn FnMut(u16, &str, bool, Style)) {
        let window = self.parser.screen().window();
        let Some(r) = window.row(row) else {
            return;
        };
        for col in 0..window.cols() {
            let Some(c) = r.cell(usize::from(col)) else {
                continue;
            };
            if c.is_wide_continuation() {
                continue;
            }
            let a = c.attributes();
            cell(
                col,
                c.contents(),
                c.is_wide(),
                Style {
                    fg: ink(a.foreground()),
                    bg: ink(a.background()),
                    bold: a.bold(),
                    dim: a.dim(),
                    inverse: a.inverse(),
                    hidden: a.hidden(),
                    underline: a.underline(),
                    strikeout: a.strikeout(),
                },
            );
        }
    }
    fn cursor(&self) -> Option<(u16, u16, CursorStyle)> {
        let screen = self.parser.screen();
        if !screen.mode(fux_vt::Mode::ShowCursor) {
            return None;
        }
        let (row, col) = screen.cursor_position();
        use fux_vt::CursorShape as S;
        let style = match screen.cursor_shape() {
            S::BlinkingUnderline | S::SteadyUnderline => CursorStyle::Underline,
            S::BlinkingBar | S::SteadyBar => CursorStyle::Bar,
            _ => CursorStyle::Block,
        };
        Some((row, col, style))
    }
    fn key(&self, press: KeyPress, out: &mut Vec<u8>) {
        let mode = self.parser.screen().key_mode();
        encode::key_bytes(Keystroke { press, kitty: None }, mode, out);
    }
    fn paste(&self, text: &str, out: &mut Vec<u8>) {
        let bracketed = self.parser.screen().mode(fux_vt::Mode::BracketedPaste);
        encode::paste(text, bracketed, out);
    }
    fn focus_report(&self, focused: bool) -> Option<&'static [u8]> {
        self.parser.screen().focus_report(focused)
    }
    fn palette(&self, index: u8) -> Option<Color32> {
        self.parser.screen().palette_color(index).map(|c| Color32::from_rgb(c.r, c.g, c.b))
    }
}

// ---------------------------------------------------------------- colours

/// The sixteen ANSI colours: xterm's defaults, with blue lifted so it reads
/// on a dark background.
const ANSI: [[u8; 3]; 16] = [
    [0x00, 0x00, 0x00],
    [0xcd, 0x00, 0x00],
    [0x00, 0xcd, 0x00],
    [0xcd, 0xcd, 0x00],
    [0x3b, 0x78, 0xff],
    [0xcd, 0x00, 0xcd],
    [0x00, 0xcd, 0xcd],
    [0xe5, 0xe5, 0xe5],
    [0x7f, 0x7f, 0x7f],
    [0xff, 0x00, 0x00],
    [0x00, 0xff, 0x00],
    [0xff, 0xff, 0x00],
    [0x5c, 0x5c, 0xff],
    [0xff, 0x00, 0xff],
    [0x00, 0xff, 0xff],
    [0xff, 0xff, 0xff],
];

/// xterm's 256-colour palette entry `i`.
fn xterm256(i: u8) -> Color32 {
    match i {
        0..=15 => {
            let [r, g, b] = ANSI[usize::from(i)];
            Color32::from_rgb(r, g, b)
        }
        16..=231 => {
            let n = i - 16;
            let level = |v: u8| if v == 0 { 0 } else { 55 + v * 40 };
            Color32::from_rgb(level(n / 36), level((n / 6) % 6), level(n % 6))
        }
        232..=255 => {
            let v = 8 + (i - 232) * 10;
            Color32::from_rgb(v, v, v)
        }
    }
}

struct Theme {
    fg: Color32,
    bg: Color32,
}

impl Theme {
    fn resolve(core: &dyn Core, ink: Ink, default: Color32) -> Color32 {
        match ink {
            Ink::Default => default,
            Ink::Index(i) => core.palette(i).unwrap_or_else(|| xterm256(i)),
            Ink::Rgb(c) => c,
        }
    }
    /// Foreground and background of a cell, styles applied.
    fn colors(&self, core: &dyn Core, s: Style) -> (Color32, Color32) {
        // Bold lifts the eight standard colours to their bright forms, as
        // xterm's boldColors does.
        let fg_ink = match s.fg {
            Ink::Index(i) if s.bold && i < 8 => Ink::Index(i + 8),
            other => other,
        };
        let mut fg = Self::resolve(core, fg_ink, self.fg);
        let mut bg = Self::resolve(core, s.bg, self.bg);
        if s.inverse {
            std::mem::swap(&mut fg, &mut bg);
        }
        if s.dim {
            fg = fg.gamma_multiply(0.6);
        }
        if s.hidden {
            fg = bg;
        }
        (fg, bg)
    }
}

fn rgb_report(c: Color32) -> String {
    let w = |v: u8| u16::from(v) * 0x101;
    format!("rgb:{:04x}/{:04x}/{:04x}", w(c.r()), w(c.g()), w(c.b()))
}

// ---------------------------------------------------------------- keys

/// The fux-vt key for an egui key that is not text: the named keys.
fn named_key(key: egui::Key) -> Option<Key> {
    use egui::Key as K;
    Some(match key {
        K::Enter => Key::Enter,
        K::Tab => Key::Tab,
        K::Escape => Key::Escape,
        K::Backspace => Key::Backspace,
        K::Delete => Key::Delete,
        K::Insert => Key::Insert,
        K::ArrowLeft => Key::Arrow(Direction::Left),
        K::ArrowRight => Key::Arrow(Direction::Right),
        K::ArrowUp => Key::Arrow(Direction::Up),
        K::ArrowDown => Key::Arrow(Direction::Down),
        K::Home => Key::Home,
        K::End => Key::End,
        K::PageUp => Key::PageUp,
        K::PageDown => Key::PageDown,
        _ => return function_key(key),
    })
}

/// F1 to F35, through the key's name, as fux-vt builds function keys only
/// from their names.
fn function_key(key: egui::Key) -> Option<Key> {
    let name = key.name();
    let n: u8 = name.strip_prefix('F')?.parse().ok()?;
    if !(1..=35).contains(&n) {
        return None;
    }
    name.parse::<KeyPress>().ok().map(|p| p.key)
}

/// The character an egui key types unshifted, for chords with Ctrl or Alt.
fn chord_char(key: egui::Key) -> Option<char> {
    use egui::Key as K;
    let name = key.name();
    if name.len() == 1 {
        let c = name.chars().next()?;
        if c.is_ascii_alphanumeric() {
            return Some(c.to_ascii_lowercase());
        }
    }
    Some(match key {
        K::Space => ' ',
        K::Minus => '-',
        K::Plus => '+',
        K::Equals => '=',
        K::Period => '.',
        K::Comma => ',',
        K::Colon => ':',
        K::Semicolon => ';',
        K::Slash => '/',
        K::Backslash => '\\',
        K::Pipe => '|',
        K::Questionmark => '?',
        K::Exclamationmark => '!',
        K::OpenBracket => '[',
        K::CloseBracket => ']',
        K::OpenCurlyBracket => '{',
        K::CloseCurlyBracket => '}',
        K::Backtick => '`',
        K::Quote => '\'',
        _ => return None,
    })
}

/// Ctrl alone.
const CTRL: Modifiers = Modifiers {
    ctrl: true,
    alt: false,
    shift: false,
};

fn modifiers(m: egui::Modifiers) -> Modifiers {
    Modifiers {
        ctrl: m.ctrl,
        alt: m.alt,
        shift: m.shift,
    }
}

// ---------------------------------------------------------------- state

struct Term {
    core: Box<dyn Core>,
    /// Bytes of the stream read so far (SD2).
    fed: u64,
    /// The size last reported to Go.
    reported: Option<(u16, u16)>,
    focused: bool,
    last_touched: u64,
    history_bytes: u32,
    /// The screen changed since it was last reported, and when that was.
    screen_dirty: bool,
    screen_at: f64,
}

/// The most columns or rows a console takes, whatever room it is given: an
/// unbounded parent (a scroll area) offers more than any screen holds.
const MAX_CELLS: i64 = 1000;

#[derive(Default)]
pub struct ConsoleCache {
    entries: HashMap<u64, Term>,
    frame: u64,
    /// Set while a capture replay runs (ADR-0281 §SD5): consoles are drawn
    /// as they stand, and nothing is fed, resized, focused or read.
    pub read_only: bool,
}

impl ConsoleCache {
    /// A console not drawn for this many frames is dropped: about ten minutes
    /// at 60 Hz. A terminal in a hidden tab keeps its screen far longer than a
    /// texture does, and a dropped one is reported as a gap when fed again.
    const MAX_AGE_FRAMES: u64 = 36_000;

    pub fn tick(&mut self) {
        self.frame = self.frame.wrapping_add(1);
        let frame = self.frame;
        self.entries.retain(|_, t| frame.saturating_sub(t.last_touched) < Self::MAX_AGE_FRAMES);
    }

    /// Drops the console `id` and its screen.
    pub fn release(&mut self, id: u64) {
        self.entries.remove(&id);
    }

    /// Feeds, draws and reads input for one console, appending its rows to
    /// `out`.
    pub fn show(&mut self, ui: &mut egui::Ui, args: ConsoleArgs<'_>, out: &mut Vec<ConsoleRow>) {
        let font = FontId::monospace(if args.font_size > 0.0 {
            args.font_size
        } else {
            ui.style().text_styles.get(&egui::TextStyle::Monospace).map_or(13.0, |f| f.size)
        });
        let (cell_w, cell_h) = ui.fonts_mut(|f| (f.glyph_width(&font, 'M'), f.row_height(&font)));
        let cell = Vec2::new(cell_w.max(1.0), cell_h.max(1.0));
        let left = ui.available_size();
        let avail = Vec2::new(
            if args.size.x > 0.0 {
                args.size.x
            } else {
                left.x
            },
            if args.size.y > 0.0 {
                args.size.y
            } else {
                left.y
            },
        );
        let fit = |given: u16, room: f32, cell: f32| -> u16 {
            if given > 0 {
                (i64::from(given)).clamp(1, MAX_CELLS) as u16
            } else {
                ((room / cell).floor() as i64).clamp(1, MAX_CELLS) as u16
            }
        };
        let cols = fit(args.grid.0, avail.x, cell.x);
        let rows = fit(args.grid.1, avail.y, cell.y);
        // The rect: the room given, up to a cell past the grid, so the
        // remainder of a pixel-sized pane is the console's background; a
        // fixed axis takes the grid's size, room or not.
        let side = |given: u16, room: f32, n: u16, cell: f32| -> f32 {
            if given > 0 {
                f32::from(n) * cell
            } else {
                room.min(f32::from(n + 1) * cell).max(cell)
            }
        };
        let area = Vec2::new(side(args.grid.0, avail.x, cols, cell.x), side(args.grid.1, avail.y, rows, cell.y));
        let history = if args.history_bytes > 0 {
            args.history_bytes
        } else {
            8 << 20
        };

        let frame = self.frame;
        let id = args.id;
        if self.read_only {
            if let Some(term) = self.entries.get(&id) {
                let theme = Theme {
                    fg: ui.visuals().text_color(),
                    bg: ui.visuals().extreme_bg_color,
                };
                let rect = Rect::from_min_size(ui.cursor().min, area);
                ui.allocate_rect(rect, Sense::hover());
                Self::draw(
                    ui,
                    term.core.as_ref(),
                    &theme,
                    rect,
                    cell,
                    &font,
                    term.focused,
                );
            }
            return;
        }
        let term = match self.entries.entry(id) {
            std::collections::hash_map::Entry::Occupied(e) => e.into_mut(),
            std::collections::hash_map::Entry::Vacant(v) => {
                let Some(core) = Fux::new(cols, rows, history as usize) else {
                    return;
                };
                v.insert(Term {
                    core: Box::new(core),
                    fed: 0,
                    reported: None,
                    focused: false,
                    last_touched: frame,
                    history_bytes: history,
                    screen_dirty: true,
                    screen_at: f64::NEG_INFINITY,
                })
            }
        };
        term.last_touched = frame;
        let _ = term.history_bytes;

        // Size first, so the bytes of this frame land on the grid they are
        // drawn on.
        if (term.core.cols(), term.core.rows()) != (cols, rows) {
            term.core.resize(cols, rows);
            term.screen_dirty = true;
        }
        let size = (term.core.cols(), term.core.rows());
        if term.reported != Some(size) {
            term.reported = Some(size);
            let mut payload = size.0.to_le_bytes().to_vec();
            payload.extend_from_slice(&size.1.to_le_bytes());
            out.push(ConsoleRow {
                id,
                kind: kind::SIZE,
                payload,
            });
        }

        // Feed (SD2): apply what lies past the bytes already read.
        let end = args.offset.saturating_add(args.feed.len() as u64);
        let mut core_out = CoreOut::default();
        if args.offset > term.fed {
            let mut payload = term.fed.to_le_bytes().to_vec();
            payload.extend_from_slice(&args.offset.to_le_bytes());
            out.push(ConsoleRow {
                id,
                kind: kind::GAP,
                payload,
            });
            term.core.feed(args.feed, &mut core_out);
            term.fed = end;
            term.screen_dirty = true;
        } else if end > term.fed {
            let skip = (term.fed - args.offset) as usize;
            term.core.feed(&args.feed[skip..], &mut core_out);
            term.fed = end;
            term.screen_dirty = true;
        }
        out.push(ConsoleRow {
            id,
            kind: kind::ACK,
            payload: term.fed.to_le_bytes().to_vec(),
        });
        if term.screen_dirty {
            let now = ui.input(|i| i.time);
            let wait = term.screen_at + SCREEN_EVERY_S - now;
            if wait <= 0.0 {
                term.screen_dirty = false;
                term.screen_at = now;
                out.push(ConsoleRow {
                    id,
                    kind: kind::SCREEN,
                    payload: screen_report(term.core.as_ref(), term.fed),
                });
            } else {
                ui.ctx().request_repaint_after(std::time::Duration::from_secs_f64(wait));
            }
        }

        let theme = Theme {
            fg: ui.visuals().text_color(),
            bg: ui.visuals().extreme_bg_color,
        };
        for reply in core_out.replies {
            out.push(ConsoleRow {
                id,
                kind: kind::REPLY,
                payload: reply,
            });
        }
        for (number, bel) in core_out.color_queries {
            let color = match number {
                10 | 12 => Some(theme.fg),
                11 => Some(theme.bg),
                _ => None,
            };
            if let Some(color) = color {
                let end: &str = if bel { "\x07" } else { "\x1b\\" };
                let reply = format!("\x1b]{number};{}{end}", rgb_report(color));
                out.push(ConsoleRow {
                    id,
                    kind: kind::REPLY,
                    payload: reply.into_bytes(),
                });
            }
        }
        for (k, payload) in core_out.events {
            out.push(ConsoleRow {
                id,
                kind: k,
                payload,
            });
        }

        // The widget's rect, its focus, and the keys while it has it (SD4).
        let rect = Rect::from_min_size(ui.cursor().min, area);
        let focus_id = egui::Id::new(id).with("imzero-focus");
        let resp = ui.interact(rect, focus_id, Sense::click());
        ui.allocate_rect(rect, Sense::hover());
        if resp.clicked() {
            resp.request_focus();
        }
        {
            // An accessibility node named "terminal" whose value is the
            // screen's text, built only when egui asks for it.
            let core = term.core.as_ref();
            resp.widget_info(|| {
                let mut info = egui::WidgetInfo::labeled(egui::WidgetType::Other, true, "terminal");
                info.current_text_value = Some(screen_text(core));
                info
            });
        }
        let focused = resp.has_focus();
        if focused != term.focused {
            term.focused = focused;
            if let Some(report) = term.core.focus_report(focused) {
                out.push(ConsoleRow {
                    id,
                    kind: kind::FOCUS,
                    payload: report.to_vec(),
                });
            }
        }
        if focused {
            ui.memory_mut(|m| {
                m.set_focus_lock_filter(
                    focus_id,
                    egui::EventFilter {
                        tab: true,
                        horizontal_arrows: true,
                        vertical_arrows: true,
                        escape: true,
                    },
                );
            });
            Self::read_keys(ui, term.core.as_ref(), id, out);
        }

        Self::draw(ui, term.core.as_ref(), &theme, rect, cell, &font, focused);
    }

    /// Encodes and consumes the focused console's key, text and paste events.
    fn read_keys(ui: &egui::Ui, core: &dyn Core, id: u64, out: &mut Vec<ConsoleRow>) {
        let mods = ui.input(|i| i.modifiers);
        // With Ctrl or Alt alone held, the key event is the chord and any text
        // event is the platform's rendering of it; with both (AltGr on some
        // platforms) or neither, the text is what was typed.
        let chord = mods.ctrl != mods.alt;
        let mut keys = Vec::new();
        let mut paste = Vec::new();
        ui.input_mut(|i| {
            i.events.retain(|e| match e {
                egui::Event::Key {
                    key,
                    pressed,
                    modifiers,
                    ..
                } => {
                    if let Some(k) = named_key(*key) {
                        if *pressed {
                            core.key(KeyPress::new(k, self::modifiers(*modifiers)), &mut keys);
                        }
                        return false;
                    }
                    if chord && let Some(c) = chord_char(*key) {
                        if *pressed {
                            core.key(
                                KeyPress::new(Key::Char(c), self::modifiers(*modifiers)),
                                &mut keys,
                            );
                        }
                        return false;
                    }
                    true
                }
                egui::Event::Text(text) => {
                    if !chord {
                        for c in text.chars() {
                            core.key(KeyPress::char(c), &mut keys);
                        }
                    }
                    false
                }
                // The host turns Ctrl-C and Ctrl-X into Copy and Cut. With no
                // selection to copy, they are the control characters.
                egui::Event::Copy => {
                    core.key(KeyPress::new(Key::Char('c'), CTRL), &mut keys);
                    false
                }
                egui::Event::Cut => {
                    core.key(KeyPress::new(Key::Char('x'), CTRL), &mut keys);
                    false
                }
                egui::Event::Paste(text) => {
                    core.paste(text, &mut paste);
                    false
                }
                _ => true,
            });
        });
        if !keys.is_empty() {
            out.push(ConsoleRow {
                id,
                kind: kind::KEY,
                payload: keys,
            });
        }
        if !paste.is_empty() {
            out.push(ConsoleRow {
                id,
                kind: kind::PASTE,
                payload: paste,
            });
        }
    }

    fn draw(
        ui: &egui::Ui,
        core: &dyn Core,
        theme: &Theme,
        rect: Rect,
        cell: Vec2,
        font: &FontId,
        focused: bool,
    ) {
        let painter = ui.painter_at(rect);
        painter.rect_filled(rect, 0.0, theme.bg);
        let origin = rect.min;
        let at = |row: u16, col: u16| {
            Pos2::new(
                origin.x + f32::from(col) * cell.x,
                origin.y + f32::from(row) * cell.y,
            )
        };
        let cursor = core.cursor();
        for row in 0..core.rows() {
            core.row_cells(row, &mut |col, text, wide, style| {
                let (fg, bg) = theme.colors(core, style);
                let width = if wide { 2.0 } else { 1.0 };
                let min = at(row, col);
                let cell_rect = Rect::from_min_size(min, Vec2::new(cell.x * width, cell.y));
                if bg != theme.bg {
                    painter.rect_filled(cell_rect, 0.0, bg);
                }
                if !text.is_empty() && text != " " {
                    painter.text(min, Align2::LEFT_TOP, text, font.clone(), fg);
                }
                if style.underline {
                    let y = cell_rect.bottom() - 1.0;
                    painter.hline(cell_rect.x_range(), y, Stroke::new(1.0, fg));
                }
                if style.strikeout {
                    painter.hline(
                        cell_rect.x_range(),
                        cell_rect.center().y,
                        Stroke::new(1.0, fg),
                    );
                }
            });
        }
        if let Some((row, col, style)) = cursor {
            let min = at(row, col);
            let r = Rect::from_min_size(min, cell);
            let color = theme.fg;
            match (style, focused) {
                (CursorStyle::Block, true) => {
                    painter.rect_filled(r, 0.0, color.gamma_multiply(0.7));
                }
                (CursorStyle::Underline, _) => {
                    painter.rect_filled(
                        Rect::from_min_max(Pos2::new(r.left(), r.bottom() - 2.0), r.max),
                        0.0,
                        color,
                    );
                }
                (CursorStyle::Bar, _) => {
                    painter.rect_filled(
                        Rect::from_min_max(r.min, Pos2::new(r.left() + 2.0, r.bottom())),
                        0.0,
                        color,
                    );
                }
                (CursorStyle::Block, false) => {
                    painter.rect_stroke(r, 0.0, Stroke::new(1.0, color), egui::StrokeKind::Inside);
                }
            }
        }
    }
}

/// The payload of a [`kind::SCREEN`] row.
fn screen_report(core: &dyn Core, fed: u64) -> Vec<u8> {
    let (cursor_row, cursor_col, shown) = match core.cursor() {
        Some((row, col, _)) => (row, col, true),
        None => (0, 0, false),
    };
    let text = screen_text(core);
    let mut payload = Vec::with_capacity(17 + text.len());
    payload.extend_from_slice(&fed.to_le_bytes());
    payload.extend_from_slice(&core.cols().to_le_bytes());
    payload.extend_from_slice(&core.rows().to_le_bytes());
    payload.extend_from_slice(&cursor_col.to_le_bytes());
    payload.extend_from_slice(&cursor_row.to_le_bytes());
    payload.push(u8::from(shown));
    payload.extend_from_slice(text.as_bytes());
    payload
}

/// The visible screen as text: rows trimmed on the right, a newline between.
fn screen_text(core: &dyn Core) -> String {
    let mut out = String::new();
    for row in 0..core.rows() {
        let mut line = String::new();
        let mut at = 0u16;
        core.row_cells(row, &mut |col, text, wide, _| {
            while at < col {
                line.push(' ');
                at += 1;
            }
            line.push_str(if text.is_empty() { " " } else { text });
            at = col + if wide { 2 } else { 1 };
        });
        if row > 0 {
            out.push('\n');
        }
        out.push_str(line.trim_end());
    }
    out.truncate(out.trim_end().len());
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fux(cols: u16, rows: u16) -> Fux {
        Fux::new(cols, rows, 1 << 16).expect("core")
    }

    fn text(core: &dyn Core, row: u16) -> String {
        screen_text(core).lines().nth(usize::from(row)).unwrap_or_default().to_owned()
    }

    #[test]
    fn a_screen_report_carries_the_offset_size_cursor_and_text() {
        let mut core = fux(20, 4);
        core.feed(b"hi\r\nthere", &mut CoreOut::default());
        let p = screen_report(&core, 9);
        assert_eq!(u64::from_le_bytes(p[0..8].try_into().unwrap()), 9);
        assert_eq!(u16::from_le_bytes([p[8], p[9]]), 20, "columns");
        assert_eq!(u16::from_le_bytes([p[10], p[11]]), 4, "rows");
        assert_eq!(u16::from_le_bytes([p[12], p[13]]), 5, "cursor column");
        assert_eq!(u16::from_le_bytes([p[14], p[15]]), 1, "cursor row");
        assert_eq!(p[16], 1, "the cursor is shown");
        assert_eq!(&p[17..], b"hi\nthere");
    }

    #[test]
    fn feeding_draws_text_and_replies_to_queries() {
        let mut core = fux(20, 4);
        let mut out = CoreOut::default();
        core.feed(b"hello\r\nworld\x1b[c", &mut out);
        assert_eq!(text(&core, 0), "hello");
        assert_eq!(text(&core, 1), "world");
        assert_eq!(out.replies.len(), 1, "DA1 is answered");
    }

    #[test]
    fn keys_follow_the_programs_cursor_mode() {
        let mut core = fux(20, 4);
        let mut bytes = Vec::new();
        core.key(KeyPress::plain(Key::Arrow(Direction::Up)), &mut bytes);
        assert_eq!(bytes, b"\x1b[A");
        core.feed(b"\x1b[?1h", &mut CoreOut::default());
        bytes.clear();
        core.key(KeyPress::plain(Key::Arrow(Direction::Up)), &mut bytes);
        assert_eq!(bytes, b"\x1bOA");
        bytes.clear();
        core.key(KeyPress::new(Key::Char('c'), CTRL), &mut bytes);
        assert_eq!(bytes, b"\x03");
    }

    #[test]
    fn a_paste_is_bracketed_when_asked() {
        let mut core = fux(20, 4);
        let mut bytes = Vec::new();
        core.paste("a\nb", &mut bytes);
        assert_eq!(bytes, b"a\rb");
        core.feed(b"\x1b[?2004h", &mut CoreOut::default());
        bytes.clear();
        core.paste("x", &mut bytes);
        assert_eq!(bytes, b"\x1b[200~x\x1b[201~");
    }

    #[test]
    fn function_keys_map_by_name() {
        assert!(function_key(egui::Key::F5).is_some());
        assert!(function_key(egui::Key::F35).is_some());
        assert!(function_key(egui::Key::A).is_none());
    }

    #[test]
    fn the_256_colour_cube_matches_xterm() {
        assert_eq!(xterm256(16), Color32::from_rgb(0, 0, 0));
        assert_eq!(xterm256(196), Color32::from_rgb(255, 0, 0));
        assert_eq!(xterm256(231), Color32::from_rgb(255, 255, 255));
        assert_eq!(xterm256(232), Color32::from_rgb(8, 8, 8));
    }
}
