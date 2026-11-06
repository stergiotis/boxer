//! Shell integration's command marks (OSC 133, FinalTerm's semantic prompts):
//! `C` where a command's output starts, at the cursor, `D ; status` where it
//! ends and how. A host reads them to know when a command is done, its
//! status and its output ([`Screen::commands`]). Every mark in the output
//! counts, whichever program sent it.

use super::Screen;
use crate::RowId;

/// A place in the retained rows, which scrolling carries.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Mark {
    /// The row.
    pub row: RowId,
    /// The column, from 0: where the next character goes, one past the
    /// cursor's while a wrap is pending (`Grid::next_column`).
    pub col: u16,
}

/// What the shell's marks say of its commands.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Commands {
    /// How many commands have begun (`C` with none running; a `C` while one
    /// runs moves its start and begins none).
    pub started: u64,
    /// How many commands have ended (`D`): a host waits for it to grow.
    pub finished: u64,
    /// The status the last command ended with, if the shell said it.
    pub last_status: Option<i32>,
    /// Where the last command's output began and ended.
    pub last_output: Option<(Mark, Mark)>,
    /// Where the running command's output began (`C`); `None` once it
    /// ended (`D`), and before any began.
    pub output_start: Option<Mark>,
}

impl Commands {
    /// Whether a command's output began (`C`) and has not ended (`D`).
    pub fn running(&self) -> bool {
        self.output_start.is_some()
    }

    /// Its marks: the running command's start, and the last one's start
    /// and end.
    pub(crate) fn marks(&self) -> [Option<Mark>; 3] {
        let (start, end) = self.last_output.unzip();
        [self.output_start, start, end]
    }

    /// These commands with their marks moved to `marks`, in `marks`'s
    /// order.
    pub(crate) fn moved(self, [output_start, start, end]: [Option<Mark>; 3]) -> Self {
        Self {
            output_start,
            last_output: start.zip(end),
            ..self
        }
    }
}

impl Screen {
    /// What the shell's marks say of its commands.
    pub fn commands(&self) -> Commands {
        self.commands
    }

    /// `C`, a command's output begins at the cursor, or `D [; status]`, it
    /// ended.
    pub(crate) fn command_mark(&mut self, command: &[u8], option: Option<&[u8]>) {
        let Some(here) = self.cursor_mark() else {
            return;
        };
        match command {
            b"C" => {
                if !self.commands.running() {
                    self.commands.started = self.commands.started.wrapping_add(1);
                }
                self.commands.output_start = Some(here);
            }
            b"D" => {
                let status = option
                    .and_then(|s| str::from_utf8(s).ok())
                    .and_then(|s| s.parse().ok());
                // A D with no C before it ends no command: a shell sends one
                // with every prompt, the first included.
                if let Some(start) = self.commands.output_start.take() {
                    self.commands.last_output = Some((start, here));
                    self.commands.last_status = status;
                    self.commands.finished = self.commands.finished.wrapping_add(1);
                }
            }
            _ => {}
        }
    }

    /// A prompt was drawn on the cursor's row.
    pub(crate) fn prompted(&mut self) {
        let (row, version) = (self.grid().cursor.row(), self.version);
        self.grid_mut().set_prompt(row, true, version);
    }

    pub(crate) fn cursor_mark(&self) -> Option<Mark> {
        let grid = self.grid();
        let row = grid.row_at(grid.cursor_index()?)?.id();
        Some(Mark {
            row,
            col: grid.next_column(),
        })
    }
}
