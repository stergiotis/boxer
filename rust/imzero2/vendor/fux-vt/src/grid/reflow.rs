//! Reflow: the primary screen resized with its lines re-wrapped
//! (`Grid::reflowed`), laid out in one walk over the rows and copied in a
//! second, each through `Reflow`.
use super::*;
use crate::CommandMark;

impl Grid {
    /// The primary screen resized with reflow: every logical line, a run of
    /// soft-wrapped rows and the row that ends it, history included, is
    /// re-wrapped at `cols`, without splitting wide glyphs. The cursor stays
    /// on its character. When there are more rows than fit, blank lines
    /// below the cursor go first, then lines scroll into history, oldest
    /// dropped first past its budget; rows below the screen once the
    /// cursor's row is at its top are dropped. The scroll region is reset,
    /// as xterm does on resize. Replacement storage is built first, so
    /// allocation failure leaves this grid unchanged.
    ///
    /// Two walks over the rows, one to lay out and one to copy, so no line
    /// is gathered in memory of its own. The k-th row of a reflowed line
    /// keeps the identity of the line's k-th row before, if it had one.
    ///
    /// Also returns how many history rows, laid out as they were, the new
    /// grid takes unchanged (`adopt_history`), and `places` moved with their
    /// characters as the cursor is. A place in no retained row stays as it
    /// was; one on a row the resize drops goes to an identity no row has.
    pub fn reflowed(
        &self,
        size: Size,
        next: &mut u64,
        version: u64,
        places: [Option<CommandMark>; 3],
    ) -> Result<(Self, usize, [Option<CommandMark>; 3]), Error> {
        Self::check_size(size)?;
        let cols = size.cols();
        // The cursor and the saved cursor go with their characters; one
        // waiting to wrap is laid out one past its glyph.
        let at = |cursor: Cursor| {
            self.history_len()
                .checked_add(usize::from(cursor.row))
                .map(|row| (row, usize::from(past(cursor.col, cursor.pending_wrap))))
        };
        let [a, b, c] = places.map(|place| {
            place.and_then(|place| Some((self.index_of(place.row)?, usize::from(place.col))))
        });
        let marks = [at(self.cursor), at(self.saved.cursor), a, b, c];
        let layout = self.reflow(usize::from(cols), marks, &mut Layout)?;
        let screen = usize::from(size.rows());
        // A mark no line held goes to the start of the last row.
        let last = Mark::Blank(layout.rows.saturating_sub(1), 0);
        let [cursor, saved, laid @ ..] = layout.marks;
        let [cursor, saved] = [cursor, saved].map(|mark| mark.unwrap_or(last));
        // A place takes its column now, and its row's identity once the
        // copy gives the row one.
        let mut moved = [None; 3];
        for ((moved, laid), place) in moved.iter_mut().zip(laid).zip(places) {
            if let (Some(laid), Some(place)) = (laid, place) {
                let (row, col) = match laid {
                    Mark::Text(row, col) => (row, col),
                    Mark::Blank(row, col) => (row, col.min(usize::from(cols).saturating_sub(1))),
                };
                let col = u16::try_from(col).unwrap_or(cols);
                *moved = Some((row, CommandMark { col, ..place }));
            }
        }
        let (Mark::Text(cursor_row, _) | Mark::Blank(cursor_row, _)) = cursor;
        // Blank lines below the cursor go before any line scrolls into
        // history, but only those of the screen's own lines past the new
        // height: a resize never brings a history row back.
        let beyond = (layout.rows.saturating_sub(layout.screen_line)).saturating_sub(screen);
        let below = layout.rows.saturating_sub(cursor_row.saturating_add(1));
        let drop = layout.trailing_blank.min(beyond).min(below);
        let total = layout.rows.saturating_sub(drop);
        let live_top = total.saturating_sub(screen).min(cursor_row);
        let end = total.min(live_top.checked_add(screen).ok_or(Error::Capacity)?);
        let kept = layout.same.min(live_top);
        // Clamped to the screen; one past the text's last column is the
        // last column, waiting to wrap.
        let placed = |cursor: Cursor, mark: Mark| {
            let (row, col, pending_wrap) = match mark {
                Mark::Text(row, col) => (row, col, col >= usize::from(cols)),
                Mark::Blank(row, col) => (row, col, false),
            };
            let row = u16::try_from(row.saturating_sub(live_top)).unwrap_or(u16::MAX);
            let col = u16::try_from(col).unwrap_or(u16::MAX);
            Cursor {
                pending_wrap,
                ..cursor.placed((row, col), size)
            }
        };
        let mut replacement = Self {
            // The cursor's row is on screen: `live_top` is at most its row.
            cursor: placed(self.cursor, cursor),
            // So DECRC (as 1049 leaves the alternate screen) finds it.
            saved: Saved {
                cursor: placed(self.saved.cursor, saved),
                ..self.saved
            },
            ..self.successor(size)
        };
        replacement.reserve_screen()?;
        let mut copy = Fill {
            grid: &mut replacement,
            rows: kept..end,
            screen: live_top,
            next,
            version,
            cells: vec![BLANK; usize::from(cols)],
            written: 0,
            text: Text::default(),
            links: None,
            places: moved,
        };
        self.reflow(usize::from(cols), marks, &mut copy)?;
        let mut places = places;
        for (place, moved) in places.iter_mut().zip(copy.places) {
            if let Some((_, moved)) = moved {
                *place = Some(moved);
            }
        }
        while replacement.order.len() < screen {
            let meta = Meta::new(next_id(next), version, false, 0);
            replacement.push_screen_row(meta, &[], None, None);
        }
        replacement.fit_cursor();
        Ok((replacement, kept, places))
    }

    /// Lays every retained row out again at `width` columns, giving each run
    /// of cells and each finished row to `target`. `marks` are the cursor's,
    /// the saved cursor's and the places' retained row and column.
    fn reflow(
        &self,
        width: usize,
        marks: [Option<(usize, usize)>; MARKS],
        target: &mut impl Reflow,
    ) -> Result<Reflowed, Error> {
        let retained = self.retained_len();
        let mut pass = Pass {
            width,
            marks,
            out: Reflowed::default(),
            target,
        };
        // Renditions are looked up only if any row may be double width.
        let halved = self.doubled() || self.history.has_renditions();
        let double = |index: usize| halved && self.rendition_at(index).is_double();
        let mut start = 0;
        while start < retained {
            // A line's length leaves out its blank tail, which can run back
            // over several rows. A double-width row is a line of its own,
            // never joined to the rows around it nor split.
            let (mut end, mut before, mut length) = (start, 0usize, 0);
            loop {
                let (wrapped, width, text) = self.facts_at(end);
                if text > 0 {
                    length = before.saturating_add(text);
                }
                before = before.saturating_add(width);
                let next = end.saturating_add(1);
                if !wrapped || next >= retained || double(end) || double(next) {
                    break;
                }
                end = next;
            }
            if (start..=end).contains(&self.history_len()) {
                pass.out.screen_line = pass.out.rows;
            }
            let rendition = if double(start) {
                self.rendition_at(start)
            } else {
                LineRendition::Single
            };
            let first = pass.out.rows;
            self.lay_out_runs((start, end), (length, rendition), &mut pass)?;
            // A history line laid out exactly as it was, after lines that
            // all were, is kept unchanged.
            let next = end.saturating_add(1);
            let rows = pass.out.rows.saturating_sub(first);
            if pass.out.same == start
                && first == start
                && rows == next.saturating_sub(start)
                && next <= self.history_len()
                && (start..next).all(|i| self.facts_at(i).1 == width)
            {
                pass.out.same = next;
            }
            start = next;
        }
        Ok(pass.out)
    }

    fn rendition_at(&self, index: usize) -> LineRendition {
        self.row_at(index)
            .map_or(LineRendition::Single, |row| row.rendition())
    }

    /// Lays out the line of rows `start` through `end`, `length` cells of
    /// text, a run of cells at a time, so a line costs its rows, not its
    /// cells. A wide glyph that would end in the last column moves to the
    /// next row; spacers are skipped; at a width under two, wide glyphs are
    /// left out. A double-width line keeps as much as fits in half the
    /// width and loses the rest, as DECDWL does (DEC STD 070, DECDWL, notes
    /// 1 and 2).
    fn lay_out_runs(
        &self,
        (start, end): (usize, usize),
        (length, rendition): (usize, LineRendition),
        pass: &mut Pass<'_, impl Reflow>,
    ) -> Result<(), Error> {
        let length = if rendition.is_double() {
            length.min((pass.width / 2).max(1))
        } else {
            length
        };
        let mut line = Run {
            used: 0,
            next: start,
            end,
            prompt: false,
            pending: false,
            offsets: [None; MARKS],
        };
        let first = pass.out.rows;
        let mut base = 0usize;
        for i in start..=end {
            let Some(row) = self.row_at(i) else {
                continue;
            };
            let len = row.width;
            // The row the row's first cell goes to is where the prompt
            // starts after, or the line's last, if no cell goes after it.
            line.pending |= row.starts_prompt();
            for (offset, mark) in line.offsets.iter_mut().zip(pass.marks) {
                if let Some((row_index, col)) = mark
                    && row_index == i
                {
                    *offset = base.checked_add(col);
                }
            }
            let mut taken = len.min(length.saturating_sub(base));
            if i != end && row.ends_in_spacer() {
                // A cursor on a spacer goes with the glyph after it.
                let spacer = base.saturating_add(len).saturating_sub(1);
                for offset in &mut line.offsets {
                    if *offset == Some(spacer) {
                        *offset = spacer.checked_add(1);
                    }
                }
                taken = taken.min(len.saturating_sub(1));
            }
            // At one column, only the runs between wide glyphs.
            let mut from = 0;
            loop {
                let to = if pass.width < 2 {
                    (from..taken)
                        .find(|&c| {
                            row.stored(c)
                                .is_some_and(|c| c.is_wide() || c.is_wide_continuation())
                        })
                        .unwrap_or(taken)
                } else {
                    taken
                };
                self.lay_out_run(row, base, from..to, &mut line, pass)?;
                if to >= taken {
                    break;
                }
                // A place on a cell left out goes with the cell after it;
                // the cursors (the first two marks) keep the line's end.
                let left_out = base.saturating_add(to);
                for offset in line.offsets.iter_mut().skip(2) {
                    if *offset == Some(left_out) {
                        *offset = left_out.checked_add(1);
                    }
                }
                from = to.saturating_add(1);
            }
            base = base.saturating_add(len);
        }
        line.prompt |= line.pending;
        let out = &mut pass.out;
        for (offset, mark) in line.offsets.iter_mut().zip(&mut out.marks) {
            if let Some(offset) = offset.take() {
                // At the end of the line's text, after its last glyph; or
                // as far past it on the last row, in its blanks.
                *mark = Some(match offset.saturating_sub(length) {
                    0 => Mark::Text(out.rows, line.used),
                    past => Mark::Blank(out.rows, line.used.saturating_add(past)),
                });
            }
        }
        let id = self.id_at(line.next, end);
        pass.target
            .row(out.rows, line.used, (false, rendition), id, line.prompt)?;
        out.rows = out.rows.saturating_add(1);
        out.trailing_blank = if length == 0 {
            out.trailing_blank
                .saturating_add(out.rows.saturating_sub(first))
        } else {
            0
        };
        Ok(())
    }

    /// Lays out cells `cells` of `row`, which starts `base` cells into its
    /// line.
    fn lay_out_run(
        &self,
        row: Row<'_>,
        base: usize,
        cells: Range<usize>,
        line: &mut Run,
        pass: &mut Pass<'_, impl Reflow>,
    ) -> Result<(), Error> {
        let (width, taken) = (pass.width, cells.end);
        let mut at = cells.start;
        while at < taken {
            if line.used >= width {
                self.end_row(line, pass)?;
            }
            let room = width.saturating_sub(line.used);
            let take = room.min(taken.saturating_sub(at));
            let next = at.saturating_add(take);
            // A wide glyph ending the run in the last column goes to the
            // next row, leaving that column blank.
            let last = next.saturating_sub(1);
            let padded = take == room && row.stored(last).is_some_and(Compact::is_wide);
            if padded {
                line.place(row, base, at..last, pass);
                self.end_row(line, pass)?;
                line.place(row, base, last..next, pass);
            } else {
                line.place(row, base, at..next, pass);
            }
            at = next;
        }
        Ok(())
    }

    fn end_row(&self, line: &mut Run, pass: &mut Pass<'_, impl Reflow>) -> Result<(), Error> {
        let id = self.id_at(line.next, line.end);
        line.next = line.next.saturating_add(1);
        let wrapped = (true, LineRendition::Single);
        (pass.target).row(pass.out.rows, line.used, wrapped, id, line.prompt)?;
        pass.out.rows = pass.out.rows.saturating_add(1);
        line.used = 0;
        line.prompt = false;
        Ok(())
    }

    /// The identity of retained row `index`, if it is at most `end`: the
    /// line's row whose place a row laid out takes.
    fn id_at(&self, index: usize, end: usize) -> Option<RowId> {
        if index > end {
            return None;
        }
        // As `row_at` counts it, without making the row.
        crate::budget::spend(1);
        match index.checked_sub(self.history.len()) {
            None => self.history.kept(index).map(|kept| kept.id),
            Some(row) => self
                .order
                .get(row)
                .and_then(|&slot| Some(self.meta.get(slot)?.id)),
        }
    }

    /// Retained row `index`'s soft wrap, width, and how many of its cells
    /// come before its blank tail. A history row keeps no blank tail; a
    /// screen row's is blank past its `used` mark.
    fn facts_at(&self, index: usize) -> (bool, usize, usize) {
        let Some(row) = index.checked_sub(self.history.len()) else {
            let kept = self.history.kept(index);
            let goes_on = self.history.continues(index);
            return kept.map_or((false, 0, 0), |k| {
                (goes_on, usize::from(k.width()), k.len())
            });
        };
        let width = usize::from(self.size.cols());
        let Some((slot, m)) =
            (self.order.get(row)).and_then(|&slot| Some((slot, self.meta.get(slot)?)))
        else {
            return (false, width, 0);
        };
        let cells = self.slice(slot);
        let used = cells.get(..usize::from(m.used)).unwrap_or(cells);
        (m.continues(), width, trimmed(used).len())
    }
}

/// The links of a run of cells: their row's, if it has any, and where the
/// cells are in it.
type RunLinks<'a> = Option<(Runs<'a>, Range<usize>)>;

/// Where a reflow's rows go: `Layout` only counts them; `Fill` writes the
/// ones kept into the replacement grid.
trait Reflow {
    /// `cells`, whose clusters are in `text`, go at `col` of reflowed row
    /// `row`, where they fit; cells of the run the row does not keep are
    /// blank.
    fn run(&mut self, row: usize, col: usize, cells: &[Compact], text: &Text, links: RunLinks<'_>);
    /// Reflowed row `row` is finished, its cells from `used` on blank; `id`
    /// is that of its line's row in the same place before, if any.
    fn row(
        &mut self,
        row: usize,
        used: usize,
        wrapped: (bool, LineRendition),
        id: Option<RowId>,
        prompt: bool,
    ) -> Result<(), Error>;
}

/// How many marks a reflow carries: the cursor, the saved cursor and three
/// places.
const MARKS: usize = 5;

struct Pass<'a, T> {
    width: usize,
    marks: [Option<(usize, usize)>; MARKS],
    out: Reflowed,
    target: &'a mut T,
}

/// A line being laid out a run at a time (`Grid::lay_out_runs`).
#[derive(Debug)]
struct Run {
    /// The cells laid out in the row being laid out.
    used: usize,
    /// The line's row whose identity the next row laid out takes, and its
    /// last row.
    next: usize,
    end: usize,
    /// Whether a prompt starts on the row being laid out, or on the row
    /// the next cell placed goes to.
    prompt: bool,
    pending: bool,
    /// Where the marks are in the line, each taken once placed.
    offsets: [Option<usize>; MARKS],
}

impl Run {
    /// Lays out cells `cells` of `row`, which starts `base` cells into the
    /// line, in the row being laid out: they fit.
    fn place(
        &mut self,
        row: Row<'_>,
        base: usize,
        cells: Range<usize>,
        pass: &mut Pass<'_, impl Reflow>,
    ) {
        if cells.is_empty() || cells.end > row.width {
            return;
        }
        // The cells the row keeps of the run; the rest are blank.
        let kept = row.cells.len();
        let run = row
            .cells
            .get(cells.start.min(kept)..cells.end.min(kept))
            .unwrap_or_default();
        let start = base.saturating_add(cells.start);
        let end = base.saturating_add(cells.end);
        for (offset, mark) in self.offsets.iter_mut().zip(&mut pass.out.marks) {
            if let Some(offset) = offset.take_if(|offset| (start..end).contains(offset)) {
                *mark = Some(Mark::Text(
                    pass.out.rows,
                    self.used.saturating_add(offset.saturating_sub(start)),
                ));
            }
        }
        self.prompt |= self.pending;
        self.pending = false;
        let links = row.links.map(|links| (links, cells.clone()));
        pass.target
            .run(pass.out.rows, self.used, run, row.text, links);
        self.used = self.used.saturating_add(cells.len());
    }
}

/// Where a mark, the cursor, the saved cursor or a place, is laid out: a
/// reflowed row and a column in it.
#[derive(Clone, Copy)]
enum Mark {
    /// On a glyph of its line's text, or just after the last: one past the
    /// last column waits to wrap.
    Text(usize, usize),
    /// In the blanks past the end of its line's text, as far past it as
    /// before: in the last column at most, never waiting to wrap, as
    /// resizing without reflow keeps it.
    Blank(usize, usize),
}

/// The shape of a reflow.
#[derive(Default)]
struct Reflowed {
    rows: usize,
    /// `None` for a mark no line held.
    marks: [Option<Mark>; MARKS],
    trailing_blank: usize,
    /// The laid-out row that the line holding the screen's first row
    /// begins on.
    screen_line: usize,
    /// How many rows of history, from its oldest, are laid out as they were.
    same: usize,
}

struct Layout;
impl Reflow for Layout {
    fn run(&mut self, _: usize, _: usize, _: &[Compact], _: &Text, _: RunLinks<'_>) {}
    fn row(
        &mut self,
        _: usize,
        _: usize,
        _: (bool, LineRendition),
        _: Option<RowId>,
        _: bool,
    ) -> Result<(), Error> {
        Ok(())
    }
}

/// Writes reflowed rows `rows` into `grid`, which has none yet: into its
/// history above `screen`, else onto its screen.
struct Fill<'a> {
    grid: &'a mut Grid,
    rows: Range<usize>,
    screen: usize,
    next: &'a mut u64,
    version: u64,
    cells: Vec<Compact>,
    /// How far into `cells` the row being laid out was written; past it,
    /// they are blank.
    written: usize,
    text: Text,
    links: Option<RowLinks>,
    /// The places, each with the reflowed row it is on, which gives it its
    /// identity.
    places: [Option<(usize, CommandMark)>; 3],
}
impl Reflow for Fill<'_> {
    fn run(&mut self, row: usize, col: usize, cells: &[Compact], text: &Text, links: RunLinks<'_>) {
        if !self.rows.contains(&row) {
            return;
        }
        let Some(dst) = col
            .checked_add(cells.len())
            .and_then(|end| self.cells.get_mut(col..end))
        else {
            return;
        };
        self.written = self.written.max(col.saturating_add(cells.len()));
        crate::budget::spend(cells.len());
        crate::copy_from(dst, cells);
        // Clusters held in the old row's text are stored again in the new.
        if !text.is_empty() {
            let spilled = (cells.iter().enumerate())
                .filter(|(_, cell)| cell.is_spilled())
                .map(|(i, cell)| (col.saturating_add(i), *cell));
            let mut line = Line {
                cells: &mut self.cells,
                text: &mut self.text,
            };
            line.store_again(spilled, text);
        }
        if let Some((links, from)) = links {
            for (at, link) in links.within(from) {
                let at = col.saturating_add(at.start)..col.saturating_add(at.end);
                self.links.get_or_insert_default().set(at, Some(link));
            }
        }
    }
    fn row(
        &mut self,
        row: usize,
        used: usize,
        (wrapped, rendition): (bool, LineRendition),
        id: Option<RowId>,
        prompt: bool,
    ) -> Result<(), Error> {
        let keep = self.rows.contains(&row);
        let mut places = (self.places.iter_mut().flatten())
            .filter(|(at, _)| *at == row)
            .peekable();
        if !keep && places.peek().is_none() {
            return Ok(());
        }
        let id = id.unwrap_or_else(|| next_id(self.next));
        places.for_each(|(_, place)| place.row = id);
        if !keep {
            return Ok(());
        }
        let cols = self.grid.size.cols();
        // The next reflow finds the row's text, and recycling clears it,
        // from its `used` mark.
        let used = u16::try_from(used).map_or(cols, |used| used.min(cols));
        // Past the mark, every cell is blank: no half of a wide glyph.
        repair_wide(self.cells.get_mut(..usize::from(used)).unwrap_or_default());
        let text = std::mem::take(&mut self.text);
        let links = self.links.take();
        let written = std::mem::take(&mut self.written);
        let mut meta = Meta::new(id, self.version, wrapped, used).with_rendition(rendition);
        meta.set(PROMPT, prompt);
        let history = row < self.screen;
        let cells = (self.cells.as_slice(), written);
        (self.grid).keep_row(meta, cells, (Some(text), links), history)?;
        crate::budget::spend(written);
        self.cells
            .get_mut(..written)
            .unwrap_or_default()
            .fill(BLANK);
        Ok(())
    }
}
