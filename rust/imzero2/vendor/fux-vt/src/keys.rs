//! Keys, their modifiers, and their names: tmux's names (`C-x`, `M-x`,
//! `S-Left`, `Enter`, `BTab`, …) plus any single character.
pub mod colour;
pub mod encode;
pub mod mouse;

use std::fmt;

closed! {
    #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
    /// An arrow key's direction.
    pub enum Direction {
        /// The direction's name, lower case (`left`, `right`, `up`, `down`),
        /// as messages write it; key names are `Left` and so on (`NAMED`).
        pub fn name(self) -> &'static str;
        /// The left arrow.
        Left => "left",
        /// The right arrow.
        Right => "right",
        /// The up arrow.
        Up => "up",
        /// The down arrow.
        Down => "down",
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
/// A key, without its modifiers.
pub enum Key {
    /// A key that types a character.
    Char(char),
    /// Enter (Return).
    Enter,
    /// Tab.
    Tab,
    /// Escape.
    Escape,
    /// Backspace.
    Backspace,
    /// Delete (forward delete).
    Delete,
    /// Insert.
    Insert,
    /// An arrow key.
    Arrow(Direction),
    /// Home.
    Home,
    /// End.
    End,
    /// Page Up.
    PageUp,
    /// Page Down.
    PageDown,
    /// A function key.
    F(Function),
}

/// A function key's number, F1 to F35: the keys kitty's protocol numbers
/// (its "Functional key definitions", F13 from 57376 to F35 at 57398),
/// F1 to F12 of them xterm's too.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Function(u8);

impl Function {
    /// The last function key.
    pub(crate) const LAST: u8 = 35;
    /// F`n`, if there is one (`1..=35`).
    pub(crate) fn new(n: u8) -> Option<Self> {
        (1..=Self::LAST).contains(&n).then_some(Self(n))
    }
    /// Its number, 1 to 35.
    pub fn number(self) -> u8 {
        self.0
    }
    /// F1 to F`last`, in order.
    pub(crate) fn up_to(last: u8) -> impl Iterator<Item = Key> {
        (1..=last).filter_map(Self::new).map(Key::F)
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
/// The modifiers fux and legacy terminals tell apart.
pub struct Modifiers {
    /// Control.
    pub ctrl: bool,
    /// Alt (Meta in the legacy sense: an Escape prefix).
    pub alt: bool,
    /// Shift.
    pub shift: bool,
}

// The modifier bits of xterm's and kitty's protocols, one less than their
// modifier parameter, as `Kitty::mods` holds them.
pub(crate) const SHIFT_BIT: u8 = 1;
pub(crate) const ALT_BIT: u8 = 2;
pub(crate) const CTRL_BIT: u8 = 4;
pub(crate) const CAPS_LOCK_BIT: u8 = 64;
pub(crate) const NUM_LOCK_BIT: u8 = 128;

/// The kitty keyboard protocol's five progressive enhancement flags, 1 to
/// 16 ("The progressive enhancement flags"): the bits of a flags number
/// read; the rest mean nothing.
pub(crate) const KITTY_FLAGS: u8 = 0b1_1111;

/// Disambiguate escape codes (1): Escape, and keys with Ctrl or Alt, sent
/// as `CSI … u`.
pub(crate) const DISAMBIGUATE: u8 = 1;

/// The flags of those a screen keeps and reports: all but report event
/// types (2), as fux sends no repeats or releases. The spec's "Detection of
/// support": "applications can detect such implementations by first
/// setting the desired progressive enhancements and then querying".
pub(crate) const KITTY_FLAGS_KEPT: u8 = KITTY_FLAGS & !0b10;

impl Modifiers {
    /// As xterm's and kitty's modifier bits.
    pub(crate) fn bits(self) -> u8 {
        let bit = |on: bool, bit: u8| if on { bit } else { 0 };
        bit(self.shift, SHIFT_BIT) | bit(self.alt, ALT_BIT) | bit(self.ctrl, CTRL_BIT)
    }
    /// No modifier.
    pub const NONE: Self = Self {
        ctrl: false,
        alt: false,
        shift: false,
    };
    /// Control alone.
    pub(crate) const CTRL: Self = Self {
        ctrl: true,
        ..Self::NONE
    };
    /// Alt alone.
    #[cfg(test)]
    pub(crate) const ALT: Self = Self {
        alt: true,
        ..Self::NONE
    };
    /// Shift alone.
    pub(crate) const SHIFT: Self = Self {
        shift: true,
        ..Self::NONE
    };
    /// Whether no modifier is held.
    pub(crate) fn is_empty(self) -> bool {
        self == Self::NONE
    }
}

/// A key with its modifiers, normalized so that equal key presses compare
/// equal: a character's shift is dropped, as the character says it already
/// (Shift and `a` arrive as `A`, and the name `S-a` is read as `A`), and a
/// control letter is lower case (`C-b`), as terminals cannot tell the two
/// apart.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct KeyPress {
    /// The key.
    pub key: Key,
    /// The modifiers held with it.
    pub mods: Modifiers,
}

impl KeyPress {
    /// `key` with `mods` held.
    pub fn new(key: Key, mods: Modifiers) -> Self {
        let mut press = Self { key, mods };
        if let Key::Char(c) = key {
            // A letter's shift is its upper case, as it arrives typed (`S-a`
            // is `A`); a control letter is lower case all the same.
            press.mods.shift = false;
            if press.mods.ctrl {
                press.key = Key::Char(c.to_ascii_lowercase());
            } else if mods.shift {
                press.key = Key::Char(c.to_ascii_uppercase());
            }
        }
        press
    }
    /// `key` with no modifier.
    pub fn plain(key: Key) -> Self {
        Self::new(key, Modifiers::NONE)
    }
    /// The key that types `c`, with no modifier.
    pub fn char(c: char) -> Self {
        Self::plain(Key::Char(c))
    }
    /// The decoded key, unless Ctrl or Alt is held.
    pub fn plain_key(self) -> Option<Key> {
        (!self.mods.ctrl && !self.mods.alt).then_some(self.key)
    }
}

/// A key as the client's terminal sent it: the press fux matches (bindings,
/// overlays, copy mode), and, from a terminal speaking the kitty keyboard
/// protocol, what it said beyond the press, which a pane whose program
/// speaks the protocol is given back (`encode::key_bytes`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Keystroke {
    /// The press a legacy terminal would have sent for the key.
    pub press: KeyPress,
    /// What a terminal speaking the kitty protocol said beyond the press, if it did.
    pub kitty: Option<Kitty>,
}

/// What a terminal speaking the kitty keyboard protocol said of a key
/// beyond its `KeyPress` (`references/modern/kitty_keyboard_protocol.html`).
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash)]
pub struct Kitty {
    /// The number of a key sent as `CSI number u`: its unshifted code
    /// point, or a functional key's (keypad keys from 57399); none for a
    /// key sent as `CSI 1 ; m A` or `CSI n ~`.
    pub code: Option<u32>,
    /// The shifted key, as reported (alternate keys).
    pub shifted: Option<u32>,
    /// The base-layout key, as reported (alternate keys).
    pub base: Option<u32>,
    /// The modifier bits: Shift 1, Alt 2, Ctrl 4, Super 8, Hyper 16, Meta
    /// 32, Caps Lock 64, Num Lock 128. A `KeyPress` keeps the first three.
    pub mods: u8,
}

impl From<KeyPress> for Keystroke {
    fn from(press: KeyPress) -> Self {
        Self { press, kitty: None }
    }
}

const NAMED: &[(&str, Key)] = &[
    ("Enter", Key::Enter),
    ("Tab", Key::Tab),
    ("Escape", Key::Escape),
    ("Space", Key::Char(' ')),
    ("BSpace", Key::Backspace),
    ("Up", Key::Arrow(Direction::Up)),
    ("Down", Key::Arrow(Direction::Down)),
    ("Left", Key::Arrow(Direction::Left)),
    ("Right", Key::Arrow(Direction::Right)),
    ("Home", Key::Home),
    ("End", Key::End),
    ("PageUp", Key::PageUp),
    ("PageDown", Key::PageDown),
    ("Insert", Key::Insert),
    ("Delete", Key::Delete),
];

/// Accepted as well as the names above; never printed.
const ALIASES: &[(&str, Key)] = &[
    ("PgUp", Key::PageUp),
    ("PPage", Key::PageUp),
    ("PgDn", Key::PageDown),
    ("NPage", Key::PageDown),
    ("IC", Key::Insert),
    ("DC", Key::Delete),
    ("Esc", Key::Escape),
    ("Backspace", Key::Backspace),
    ("Return", Key::Enter),
    ("Del", Key::Delete),
    ("Ins", Key::Insert),
];

fn key_name(key: Key) -> String {
    if let Some((name, _)) = NAMED.iter().find(|(_, k)| *k == key) {
        return (*name).to_owned();
    }
    // Every key but a character and a function key is named above.
    if let Key::F(n) = key {
        return format!("F{}", n.number());
    }
    if let Key::Char(c) = key {
        return c.to_string();
    }
    String::new()
}

impl fmt::Display for KeyPress {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.key == Key::Tab && self.mods.shift && !self.mods.ctrl && !self.mods.alt {
            return f.write_str("BTab");
        }
        if self.mods.ctrl {
            f.write_str("C-")?;
        }
        if self.mods.alt {
            f.write_str("M-")?;
        }
        if self.mods.shift {
            f.write_str("S-")?;
        }
        f.write_str(&key_name(self.key))
    }
}

/// A name that is no key's.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error {
    /// The name that was not recognised.
    pub(crate) name: String,
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "unknown key {:?}", self.name)
    }
}

impl std::error::Error for Error {}

impl std::str::FromStr for KeyPress {
    type Err = Error;
    fn from_str(name: &str) -> Result<Self, Error> {
        let mut mods = Modifiers::NONE;
        let mut rest = name;
        let unknown = || Error {
            name: name.to_owned(),
        };
        // `C-` and friends are prefixes only while something follows them,
        // so `-` and `C--` name the minus key.
        while let Some((prefix, tail)) = rest.split_at_checked(2) {
            if tail.is_empty() {
                break;
            }
            let held = match prefix {
                "C-" | "c-" => &mut mods.ctrl,
                "M-" | "m-" => &mut mods.alt,
                "S-" | "s-" => &mut mods.shift,
                _ => break,
            };
            // A modifier given twice (`M-M-x`) names no key.
            if *held {
                return Err(unknown());
            }
            *held = true;
            rest = tail;
        }
        let key = if rest.eq_ignore_ascii_case("BTab") {
            mods.shift = true;
            Key::Tab
        } else if let Some((_, key)) = NAMED
            .iter()
            .chain(ALIASES)
            .find(|(n, _)| n.eq_ignore_ascii_case(rest))
        {
            *key
        } else if let Some(n) = rest
            .strip_prefix(['F', 'f'])
            .and_then(|n| n.parse::<u8>().ok())
            .and_then(Function::new)
        {
            Key::F(n)
        } else {
            let mut chars = rest.chars();
            match (chars.next(), chars.next()) {
                (Some(c), None) if !c.is_control() => Key::Char(c),
                _ => return Err(unknown()),
            }
        };
        Ok(KeyPress::new(key, mods))
    }
}

/// Every key name, as a host lists them.
pub fn all_names() -> Vec<String> {
    let mut names: Vec<String> = NAMED.iter().map(|(n, _)| (*n).to_owned()).collect();
    names.push("BTab".into());
    names.extend(Function::up_to(Function::LAST).map(key_name));
    names
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(name: &str) -> Option<KeyPress> {
        name.parse().ok()
    }

    #[test]
    fn tmux_names_parse_and_print_canonically() {
        for (name, canonical) in [
            ("C-b", "C-b"),
            ("C-B", "C-b"),
            ("c-b", "C-b"),
            ("M-x", "M-x"),
            ("C-M-Left", "C-M-Left"),
            ("S-Up", "S-Up"),
            ("BTab", "BTab"),
            ("btab", "BTab"),
            ("BTAB", "BTab"),
            ("S-Tab", "BTab"),
            ("Enter", "Enter"),
            ("enter", "Enter"),
            ("Space", "Space"),
            (" ", "Space"),
            ("BSpace", "BSpace"),
            ("PgUp", "PageUp"),
            ("NPage", "PageDown"),
            ("IC", "Insert"),
            ("F1", "F1"),
            ("F12", "F12"),
            ("-", "-"),
            ("C--", "C--"),
            ("T", "T"),
            ("S-t", "T"),
            ("s-M-v", "M-V"),
            ("S-1", "1"),
            ("C-S-a", "C-a"),
            ("界", "界"),
            (":", ":"),
        ] {
            assert_eq!(
                parse(name).map(|k| k.to_string()),
                Some(canonical.into()),
                "{name}"
            );
        }
        // F1 to F35, as kitty's protocol numbers them; no F0 or F36.
        for name in ["F13", "F35", "C-F20"] {
            assert_eq!(parse(name).map(|k| k.to_string()), Some(name.into()));
        }
        for bad in ["", "F0", "F36", "F255", "Nope", "C-", "ab"] {
            assert!(parse(bad).is_none(), "{bad}");
        }
        assert_eq!(
            "Nope".parse::<KeyPress>().map_err(|e| e.to_string()),
            Err("unknown key \"Nope\"".to_owned())
        );
        for name in all_names() {
            assert_eq!(parse(&name).map(|k| k.to_string()), Some(name.clone()));
        }
    }

    /// One key, one value: Shift and `a` built is the `A` that `S-a` names.
    #[test]
    fn a_press_built_is_the_press_named() {
        let shifted = KeyPress::new(Key::Char('a'), Modifiers::SHIFT);
        assert_eq!(Some(shifted), parse("S-a"));
        assert_eq!(shifted, KeyPress::char('A'));
    }
}
