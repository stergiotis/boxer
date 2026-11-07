---
type: reference
audience: end-user
status: draft
title: Operating fibscope as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating fibscope as an agent

Fibscope holds no model grant of its own. An agent reaches a window through
its operations catalog under a task grant the person approved (ADR-0269,
ADR-0318), sets the word under inspection or the advisor's magnitude, and
reads back the split, the SQL and the trade-off table; the person sees
every change in the window and in the command log, and the same handler
runs when they press **Example** or **Invalid**. `keelson('operations')`
lists the operations with their arguments; this page says what each
returns and refuses. Everything is a bit operation over one word, so every
operation answers at once and nothing leaves the window.

## Opening a window on an id

Another app opens fibscope on a word of its own through its launch config,
kind `fibscopeLaunch` (`apps/fibscope/launchcfg`): the 64-bit word, where
zero leaves the worked example. Any other word is accepted, one that is not
a tagged id included, since inspecting how such a word splits is one of
the things the window is for. A config that does not decode fails the open
rather than opening a plain window.

For scenes and headless runs, `BOXER_FIBSCOPE_ID` does the same from the
environment, as a decimal or `0x`-prefixed hexadecimal number. A value that
is neither leaves the worked example and says so on the Explore tab. A
launch config wins over the variable.

## The resources

- `id` — the word under inspection, an unsigned 64-bit integer. Written by
  `set_id`. While the person drags the tag or body scrubber or types in the
  id field the resource reads as being edited, and an agent's write lands as
  a conflict: the person wins a tie.
- `max_exp` — the ids expected in the busiest category, the Trade-offs
  tab's slider. Written by `set_max_exp`.

## get_split

A query returning how the word splits: the word in decimal, hexadecimal
and binary; whether it carries the `11` comma; and for a tagged id the tag
value, the code width, the code bits, the tag value as its Zeckendorf sum,
the body and the largest body the tag holds, and the first and last word
carrying the same tag. For a word that is not a tagged id `Valid` is false,
the tag and body fields are zero, as the identity package's split returns
them, and `Note` says what the word lacks.

## get_sql

A query returning the ClickHouse filter for the word's tag,
`LW_ID_HAS_TAG(id, <tag value>)`, and the statement it expands to, with the
tag folded into a constant range by the same expansion pass the SQL
pipeline runs. The expansion is golden-tested against ClickHouse in the
identity packages; this window does not run it. A word that is not a
tagged id is refused: *the word is not a tagged id; SQL decode applies to
tagged ids only*.

## get_tradeoff

A query returning the advisor's pick for `max_exp` — the code width, and
the first and last tag value of that width, or the advisor's refusal — and
the table of width classes, narrowest first: each width's tag values and
their count, the largest body, the encoding overhead, and the seconds to
fill one tag's body space at each of the five ingress rates from 100 Hz to
10 MHz. `Limit` bounds the rows, 64 by default and at most; `Truncated`
says the scheme has more classes than were returned.

## set_id

A command setting the word under inspection. It accepts any 64-bit value
and refuses none; whether the word is a tagged id is `get_split`'s to say.
It sets and never toggles, so the same word again changes nothing. The
person's gestures are the id field, and the **Example** and **Invalid**
buttons, which set the worked example and the word 42.

## set_max_exp

A command setting the ids expected in the busiest category. A value
outside 1 to 2^60-1 is refused naming the range (*max_exp is between 1 and
2^60-1 (1152921504606846975)*): at 2^60 ids the fitting tag would be
narrower than the two-bit minimum. The person's gesture is the slider.

## What is not an operation

The tag and body scrubbers recompose the word under a new tag or body;
an agent composes the word itself and sets it with `set_id`. The dock
layout, the plot and the table's scroll position are the person's view.
