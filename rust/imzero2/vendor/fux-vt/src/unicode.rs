//! Extended grapheme cluster boundaries (UAX #29), decided one character at
//! a time, and the columns characters and clusters take (fux-vt's width
//! rule).
//!
//! [`Cluster`] is the state of the cluster being printed: a few bits, enough
//! for every rule that looks further back than one character (emoji ZWJ
//! sequences, regional indicator pairs, Indic conjuncts). Boundaries so
//! never depend on the text a cell stores, which may be cut short, and each
//! character costs one table lookup. The tables are generated from the
//! Unicode Character Database by `fux-vt/gen`, with each character's width
//! beside its segmentation properties.

#[rustfmt::skip]
mod tables;
mod width;

pub(crate) use width::cluster_width;
pub use width::{char_width, clusters, str_width};

/// Whether `c`, printed right after the grapheme cluster `cluster`, would
/// continue it rather than start the next one, as fux-vt joins what is
/// printed (UAX #29): an emoji modifier after an emoji, a combining mark
/// after anything, anything but a control after a Prepend character but
/// one of no width, which fux-vt joins nothing of width to (README). A
/// host that paints cells side by side to a terminal (fux) uses it to tell
/// where the terminal may join a cell to the one before it.
pub fn continues_cluster(cluster: &str, c: char) -> bool {
    // No rule joins a character of these kinds to what comes before it,
    // but for a Prepend character: every printable ASCII character (they
    // are all Other), and most of what is not ASCII (box drawing, CJK,
    // letters). Unless the cluster ends in a Prepend character, that needs
    // no look at the cluster.
    let next = Properties::of(c);
    let last = cluster.chars().next_back().map(Properties::of);
    if last.is_some_and(|last| last.is_prepend() && last.width() == Some(0))
        && next.width() != Some(0)
    {
        return false;
    }
    if matches!(
        next.kind(),
        Break::Other | Break::Prepend | Break::Control | Break::Cr
    ) && !next.pictographic()
        && next.conjunct() != Conjunct::Consonant
        && last.is_none_or(|last| last.kind() != Break::Prepend)
    {
        return false;
    }
    Cluster::of(cluster).push(c)
}

closed! {
    /// Grapheme_Cluster_Break, numbered as the generated tables store it:
    /// in the order declared, which is `fux-vt/gen`'s `BREAKS`.
    #[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
    enum Break {
        #[default]
        Other,
        Cr,
        Lf,
        Control,
        Extend,
        Zwj,
        RegionalIndicator,
        Prepend,
        SpacingMark,
        L,
        V,
        T,
        Lv,
        Lvt,
    }
}

impl Break {
    /// The kind the low four bits of `bits` number; a number past the last
    /// is `Other`.
    fn of(bits: u16) -> Self {
        let kind = Self::ALL.get(usize::from(bits & 0x0f));
        kind.copied().unwrap_or_default()
    }
}

/// Indic_Conjunct_Break.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Conjunct {
    None,
    Consonant,
    Extend,
    Linker,
}

/// A character's properties from the generated table, all from one
/// lookup: its segmentation, and the columns it takes alone. Its bits as
/// the table holds them, each read when asked for.
#[derive(Clone, Copy, Debug)]
pub(crate) struct Properties {
    bits: u16,
}

/// `c`'s sixteen bits in the generated table (`tables.rs` says what each
/// holds).
#[inline]
fn bits(c: char) -> u16 {
    let code = u32::from(c);
    let low = code & ((1 << tables::SHIFT) - 1);
    code.checked_shr(tables::SHIFT)
        .and_then(|block| usize::try_from(block).ok())
        .and_then(|block| tables::BLOCK.get(block))
        .and_then(|&block| {
            let start = usize::from(block).checked_shl(tables::SHIFT)?;
            start.checked_add(usize::try_from(low).ok()?)
        })
        .and_then(|at| tables::PROPERTIES.get(at))
        .copied()
        .unwrap_or(0)
}

impl Properties {
    #[inline]
    pub(crate) fn of(c: char) -> Self {
        Self { bits: bits(c) }
    }

    /// Grapheme_Cluster_Break.
    #[inline]
    fn kind(self) -> Break {
        Break::of(self.bits)
    }

    /// Whether the character is a Prepend character (UAX #29).
    #[inline]
    pub(crate) fn is_prepend(self) -> bool {
        self.kind() == Break::Prepend
    }

    /// Extended_Pictographic.
    #[inline]
    fn pictographic(self) -> bool {
        self.bits & 0x10 != 0
    }

    /// Indic_Conjunct_Break.
    #[inline]
    fn conjunct(self) -> Conjunct {
        match (self.bits >> 5) & 3 {
            1 => Conjunct::Consonant,
            2 => Conjunct::Extend,
            3 => Conjunct::Linker,
            _ => Conjunct::None,
        }
    }

    /// The columns the character takes alone, `None` for a control: what
    /// [`char_width`] says.
    #[inline]
    pub(crate) fn width(self) -> Option<usize> {
        width::of_bits(self.bits)
    }

    /// Whether it is a regional indicator.
    #[inline]
    pub(crate) fn regional_indicator(self) -> bool {
        self.kind() == Break::RegionalIndicator
    }

    /// Whether the cluster width rule looks at the character, before or
    /// after it: text with none such takes its characters' columns summed
    /// (`cluster_width`).
    #[inline]
    pub(crate) fn in_context(self) -> bool {
        self.bits & width::CONTEXT != 0
    }
}

/// How far an emoji ZWJ sequence has come (GB11: `ExtPict Extend* ZWJ ×
/// ExtPict`).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Emoji {
    #[default]
    None,
    /// An Extended_Pictographic character and any Extend after it.
    Pictograph,
    /// ... and a zero width joiner: another pictograph joins.
    Joined,
}

/// How far an Indic conjunct has come (GB9c: `Consonant [Extend Linker]*
/// Linker [Extend Linker]* × Consonant`).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Indic {
    #[default]
    None,
    /// A consonant, and extenders after it but no linker.
    Consonant,
    /// ... and a linker: another consonant joins.
    Linked,
}

/// The state of a grapheme cluster, after its characters so far.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Cluster {
    last: Break,
    emoji: Emoji,
    indic: Indic,
    /// The run of regional indicators ending the cluster is odd: the next
    /// one completes a flag.
    odd_indicators: bool,
}

impl Cluster {
    /// A cluster that starts with `c`. Printable ASCII, which the run path
    /// starts every cell with, is Other, no pictograph and no conjunct's
    /// part, with no look in the table.
    #[inline]
    pub(crate) fn start(c: char) -> Self {
        if matches!(c, ' '..='~') {
            return Self::default();
        }
        Self::starting(Properties::of(c))
    }

    /// A cluster that starts with a character whose properties are `next`.
    #[inline]
    pub(crate) fn starting(next: Properties) -> Self {
        Self::default().after(next, false)
    }

    /// A cluster nothing joins, as one a control ends (GB4): the state
    /// after a Prepend character of no width, which joins nothing after it
    /// (README), so that what follows is segmented afresh.
    pub(crate) fn ended() -> Self {
        Self {
            last: Break::Control,
            ..Self::default()
        }
    }

    /// The state of the cluster whose text is `text`, as if its characters
    /// had been printed one after another.
    pub(crate) fn of(text: &str) -> Self {
        let mut chars = text.chars();
        let mut cluster = chars.next().map(Self::start).unwrap_or_default();
        for c in chars {
            cluster.push(c);
        }
        cluster
    }

    /// Whether `c` continues the cluster, rather than starting the next one.
    /// Either way the state moves on to include `c`.
    pub(crate) fn push(&mut self, c: char) -> bool {
        self.push_properties(Properties::of(c))
    }

    /// `push` for a character whose properties are `next`.
    #[inline]
    pub(crate) fn push_properties(&mut self, next: Properties) -> bool {
        let joins = self.joins(next);
        *self = self.after(next, joins);
        joins
    }

    /// Whether no rule joins a character whose properties are `next` to
    /// the cluster, by the shortcut that needs none of the rules: it is of
    /// kind Other and no conjunct's consonant, the cluster does not end
    /// with a Prepend character, and it is no pictograph after an emoji's
    /// zero width joiner. A character it does not answer for may still not
    /// join.
    #[inline]
    pub(crate) fn never_joins(self, next: Properties) -> bool {
        // Kind Other is 0 in the low four bits, a consonant 1 in bits 5-6.
        next.bits & 0x0f == 0
            && next.bits & 0x60 != 0x20
            && self.last != Break::Prepend
            && !(next.pictographic() && self.emoji == Emoji::Joined)
    }

    /// Whether the cluster ends with a Prepend character, which joins
    /// what follows it but a control (GB9b): printable ASCII too.
    pub(crate) fn prepended(self) -> bool {
        self.last == Break::Prepend
    }

    fn joins(&self, next: Properties) -> bool {
        use Break::{
            Control, Cr, Extend, L, Lf, Lv, Lvt, Prepend, RegionalIndicator, SpacingMark, T, V, Zwj,
        };
        match (self.last, next.kind()) {
            // GB3, GB4, GB5.
            (Cr, Lf) => true,
            (Control | Cr | Lf, _) | (_, Control | Cr | Lf) => false,
            // GB6, GB7, GB8: Hangul syllables.
            (L, L | V | Lv | Lvt) | (Lv | V, V | T) | (Lvt | T, T) => true,
            // GB9, GB9a.
            (_, Extend | Zwj | SpacingMark) => true,
            // GB9b.
            (Prepend, _) => true,
            // GB9c.
            _ if self.indic == Indic::Linked && next.conjunct() == Conjunct::Consonant => true,
            // GB11.
            _ if self.emoji == Emoji::Joined && next.pictographic() => true,
            // GB12, GB13.
            (RegionalIndicator, RegionalIndicator) => self.odd_indicators,
            // GB999.
            _ => false,
        }
    }

    /// The state after `next`, which `joins` the cluster or starts one.
    fn after(self, next: Properties, joins: bool) -> Self {
        // A rule's progress carries on only within one cluster.
        fn within<T: Default>(joins: bool, state: T) -> T {
            if joins { state } else { T::default() }
        }
        let emoji = if next.pictographic() {
            Emoji::Pictograph
        } else {
            match (within(joins, self.emoji), next.kind()) {
                (Emoji::Pictograph, Break::Extend) => Emoji::Pictograph,
                (Emoji::Pictograph, Break::Zwj) => Emoji::Joined,
                _ => Emoji::None,
            }
        };
        let indic = match (within(joins, self.indic), next.conjunct()) {
            (_, Conjunct::Consonant) => Indic::Consonant,
            (Indic::Consonant | Indic::Linked, Conjunct::Linker) => Indic::Linked,
            (state, Conjunct::Extend) => state,
            _ => Indic::None,
        };
        let odd_indicators = next.kind() == Break::RegionalIndicator
            && !(joins && self.last == Break::RegionalIndicator && self.odd_indicators);
        Self {
            last: next.kind(),
            emoji,
            indic,
            odd_indicators,
        }
    }
}

#[cfg(test)]
mod tests;
