---
type: reference
audience: end-user
status: draft
title: ADR timeline
icon: "🗓"
endpoint: introspection
tabs: [timeline, table, detail]
topics: [about]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ADR timeline

The decision corpus on a calendar axis: one bar per ADR, running from the day
it was proposed to the day it stopped being under consideration. A decision
still marked `proposed` has no such day yet, so its bar runs to today — those
are the ones reaching the right edge.

**Bar length is elapsed calendar time, not effort.** The corpus records dates,
never durations; a long bar says a decision sat unreviewed for that long, which
is a different claim from anyone having worked on it throughout.

**Colour is code evidence** — the count of §-pinned citations `keelson('adr')`
reports, log-scaled against the largest count in the corpus. Read it as a lower
bound in both directions: the scan finds code that names a decision, so code
implementing one without citing it is invisible here, and a citation is
evidence that something was built, not that it was finished. The scale is
pinned to the whole corpus before the status filter, so narrowing the filter
does not silently rescale the colours.

**Vertical position means nothing.** Rows are packing, not a category: the
panel drops each bar into the first row where it does not overlap its
neighbours. Two bars in the same row are unrelated except in not having
overlapped.

**The green day-marks behind the bars are review days** — days on which at
least three decisions were reviewed at once. Review is bursty where proposal is
continuous, so bars tend to end *on* those marks rather than between them; the
label on each says how many landed that day.

**The knobs.** `span` picks what the bar measures: `review` is proposal → the
day the decision settled (its `reviewed-date`, or `withdrawn-date` for a
withdrawn one, or today while it is still proposed). `activity` is proposal →
the last date written anywhere in the document, which tracks how long the text
kept being edited rather than how long the decision was open. `status` narrows
the corpus to one lifecycle state — `all`, or one of `proposed`, `accepted`,
`deferred`, `superseded`, `withdrawn`.

```md preamble
Bar length is time from proposal; colour is §-pinned code citations, log-scaled.
Row position is packing, not meaning. Click a bar for its row in **Detail**.
```

```sql
SET param_span = 'review';
SET param_status = 'all';

-- span:   `review` draws proposal → the day the decision settled;
--         `activity` draws proposal → the last dated edit to the document.
-- status: one lifecycle state, or `all`.
WITH
  scanned AS (
    SELECT
      num, title, status, superseded_by, path, impl_evidence,
      code_refs, subtasks_done, subtasks_total, update_count,
      toDateOrNull(date)           AS proposed,
      toDateOrNull(reviewed_date)  AS reviewed,
      toDateOrNull(withdrawn_date) AS withdrawn,
      toDateOrNull(last_date)      AS touched,
      -- Taken before the status filter below, so the colour scale means the
      -- same thing whichever subset is drawn.
      max(code_refs) OVER ()       AS corpus_refs_max
    FROM keelson('adr')
  ),
  spans AS (
    SELECT
      num, title, status, superseded_by, path, impl_evidence,
      code_refs, subtasks_done, subtasks_total, update_count,
      corpus_refs_max, proposed,
      ifNull(touched, proposed) AS last_touch,
      -- The day it stopped being under consideration. A withdrawal ends it
      -- whatever the review said; anything still proposed has not ended, so
      -- it runs to today; a decision reviewed without a recorded date falls
      -- back to its last dated edit.
      multiIf(withdrawn IS NOT NULL, withdrawn,
              status = 'proposed',   toDate(now('UTC')),
              reviewed IS NOT NULL,  reviewed,
              ifNull(touched, proposed)) AS settled
    FROM scanned
    -- A decision with no parseable date cannot be placed on an axis at all.
    WHERE proposed IS NOT NULL
      AND ({status:String} = 'all' OR status = {status:String})
  ),
  drawn AS (
    SELECT
      num, title, status, superseded_by, path, impl_evidence,
      code_refs, subtasks_done, subtasks_total, update_count,
      corpus_refs_max, proposed,
      -- greatest(): a document carrying a date earlier than its own
      -- frontmatter would otherwise invert the interval and be dropped.
      greatest(if({span:String} = 'activity', last_touch, settled), proposed) AS until
    FROM spans
  )
SELECT
  toDateTime64(proposed, 3, 'UTC')                                      AS _tl_time,
  toDateTime64(until, 3, 'UTC')                                         AS _tl_time_end,
  least(1., log(1 + code_refs) / log(1 + greatest(corpus_refs_max, 1))) AS _tl_intensity,
  concat('ADR-', leftPad(toString(num), 4, '0'))                        AS adr,
  title,
  status,
  proposed                          AS proposed_on,
  until                             AS until_on,
  dateDiff('day', proposed, until)  AS days,
  code_refs,
  impl_evidence,
  subtasks_done,
  subtasks_total,
  update_count,
  superseded_by,
  path
FROM drawn
ORDER BY _tl_time ASC, num ASC
```

The bands behind the bars are their own query, on the Timeline tab's
**Background bands** lane. It counts reviews per day and keeps the days where
several landed together:

```sql bands
SELECT
  toDateTime64(toDateOrNull(reviewed_date), 3, 'UTC')             AS _tl_band_from,
  toDateTime64(addDays(toDateOrNull(reviewed_date), 1), 3, 'UTC') AS _tl_band_to,
  'success.default'                                               AS _tl_band_color,
  concat(toString(count()), ' reviewed')                          AS _tl_band_label
FROM keelson('adr')
WHERE reviewed_date != ''
GROUP BY reviewed_date
HAVING count() >= 3
ORDER BY _tl_band_from ASC
```

## Reading it honestly

- **A same-day bar has no width.** Proposal and review land on the same date
  for a large part of the corpus, and such a bar is drawn at the panel's
  minimum width — a tick, not a span. It is there; it is not a one-day
  decision process, and it is not a rendering fault.
- **`until` is today for anything still proposed**, so those bars grow by a day
  every day and the `days` column is an age, not a duration. Everything else's
  `days` is settled and will not move again.
- **Three is the band threshold**, chosen to separate batch review from the
  ordinary case rather than measured. Days with one or two reviews are not
  marked and are not therefore different in kind.
- **`impl_evidence` and `code_refs` describe citations, not implementations**,
  and `subtasks_done` is the author's own ✓ — nobody else's verdict. The two
  overlap rather than partition: a sub-item can be both cited and unmarked, or
  marked and uncited.
- **Every run re-reads the corpus from disk.** `keelson('adr')` is a live table
  over markdown files, so this is never stale and never free; the buffer and
  the bands query share one read when they run together.
- **An empty timeline usually means no corpus, not no decisions.** These tables
  read the repository the process was started in — the nearest `doc/adr` at or
  above the working directory — so a binary launched from elsewhere finds
  nothing and says so as zero rows. `BOXER_ADR_DIR` points it at one.
- **The whole corpus packs into roughly 35 rows**, which is taller than the
  pane at the default window size: the time axis sits under the bars and needs
  a scroll (or a bigger window). Narrowing `status` shortens the stack.
