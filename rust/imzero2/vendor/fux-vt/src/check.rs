//! The invariant checker (THREATS.md section 6), with the `checks` feature:
//! [`crate::Parser::check`] asserts every invariant that can be read from
//! the state alone. Invariants that compare two states live in the tests
//! (`tests/corpus/invariants.rs`). The checker's own reads are not counted
//! as work. Each check is shown a state with its one fault by a test
//! beside it (`mod faults`), so a check that stops checking fails there.

use std::{cell::Cell, fmt};

thread_local! {
    /// How many more of `crate::reserve`'s allocations on this thread are
    /// refused.
    static REFUSE: Cell<u32> = const { Cell::new(0) };
}

/// Refuses the next `n` allocations fux-vt makes on this thread, as if
/// memory had run out, to test what a refusal leaves (THREATS.md B44).
pub fn refuse_next(n: u32) {
    REFUSE.set(n);
}

/// Whether this allocation is one `refuse_next` refuses; it counts one.
pub(crate) fn refusing() -> bool {
    let n = REFUSE.get();
    REFUSE.set(n.saturating_sub(1));
    n > 0
}

/// What a check found wrong.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Fault {
    /// The check's name.
    pub(crate) check: &'static str,
    /// What was wrong, and where.
    pub(crate) detail: String,
}

/// What the checker did: the checks that ran, the faults found, and the
/// invariants it cannot check yet.
#[derive(Clone, Debug, Default)]
pub struct Checked {
    ran: Vec<&'static str>,
    faults: Vec<Fault>,
    waiting: Vec<(&'static str, &'static str)>,
}

impl Checked {
    /// Records that `check` ran, and its fault, if it found one; `which`
    /// names the part checked (a screen), for the fault's detail.
    pub(crate) fn record(&mut self, check: &'static str, which: &str, found: Result<(), String>) {
        if !self.ran.contains(&check) {
            self.ran.push(check);
        }
        if let Err(detail) = found {
            let detail = if which.is_empty() {
                detail
            } else {
                format!("{which}: {detail}")
            };
            self.faults.push(Fault { check, detail });
        }
    }
    pub(crate) fn wait(&mut self, check: &'static str, on: &'static str) {
        self.waiting.push((check, on));
    }
    pub(crate) fn merge(&mut self, other: Self) {
        for check in other.ran {
            if !self.ran.contains(&check) {
                self.ran.push(check);
            }
        }
        self.faults.extend(other.faults);
        self.waiting.extend(other.waiting);
    }
    /// Whether no check found a fault.
    pub fn is_clean(&self) -> bool {
        self.faults.is_empty()
    }
    /// The faults found by `check`.
    pub fn faults_of(&self, check: &str) -> impl Iterator<Item = &Fault> {
        self.faults.iter().filter(move |f| f.check == check)
    }
}

/// What the checker's own tests read.
#[cfg(test)]
impl Checked {
    /// The checks that ran, by name.
    pub(crate) fn ran(&self) -> &[&'static str] {
        &self.ran
    }
    /// The faults found.
    pub(crate) fn faults(&self) -> &[Fault] {
        &self.faults
    }
    /// The invariants not checked yet, each with what it waits on.
    pub(crate) fn waiting(&self) -> &[(&'static str, &'static str)] {
        &self.waiting
    }
}

impl fmt::Display for Checked {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} checks ran", self.ran.len())?;
        for fault in &self.faults {
            write!(f, "\n  {}: {}", fault.check, fault.detail)?;
        }
        Ok(())
    }
}

/// `Err(detail)` unless `holds`: one check's finding.
pub(crate) fn ensure(holds: bool, detail: impl FnOnce() -> String) -> Result<(), String> {
    if holds { Ok(()) } else { Err(detail()) }
}

/// Runs `check` without counting its work.
pub(crate) fn unseen<T>(check: impl FnOnce() -> T) -> T {
    let before = crate::budget::spent();
    let result = check();
    crate::budget::rewind(before);
    result
}
