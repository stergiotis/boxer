---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: decisions

A decision is a question the person settles with a button: approve what
an agent asks to do, accept a change a model proposes, confirm an export
they started. This page says how such a question is drawn and worded. It
does not say who may ask or what an answer authorises; that is the
host's and the grant's
([ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md),
[ADR-0324](../../adr/0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)).
The decision behind the pattern is
[ADR-0404](../../adr/0404-what-a-person-reads-in-a-window-a-stated-reader-one-way-to-draw-a-decision-and-the-chat-window-as-the-first-case.md)
§SD3 and §SD4; the widget is `widgets/decision`. None of it has been
tested with a person deciding under time pressure.

## Background

A decision is the one place in a window where a misread sentence has a
consequence outside the window: something is sent, kept, published, or
an agent's reach widens. Before the widget, each such question was drawn
where it arose, and the drawings disagreed on the things that matter:
whether the sentence stating the consequence wraps, how strong its type
is, and which button is filled.

**Foundational dependencies.**

- Button kinds ([ADR-0273](../../adr/0273-imzero2-ids-button-kinds.md)) — the emphasis rules below assign them.
- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Body` for what is asked and what follows; `Caption` is not used for a consequence.
- Colour ([ADR-0040](../../adr/0040-imzero2-design-system-palette-consolidated.md)) — a note takes a semantic role; the question itself takes none.
- Status conventions ([status-and-legends](./status-and-legends.md)) — the window's state chip says a decision is waiting.

## How it works

### Anatomy

Top to bottom, and nothing else:

| Part | Holds | Rule |
| --- | --- | --- |
| What is asked | one line, as the asker would say it to the person | names the asker when it is not the person ("Chat asks to call … in window 2") |
| The thing | what the answer applies to, with its size or extent | one line; identifiers only where the reader uses them |
| What follows | one sentence per consequence: what goes where, for how long, who can read it, what cannot be taken back | body strength, always wrapped, never in a tooltip alone |
| Notes | that the question went stale, or cannot be granted, and why | a semantic tone; a note that blocks an answer disables that option and says so beside it |
| Before answering | what the person has to read or fill in first: inputs, the question's own text, a call's arguments | before the options (the widget's `Form`) |
| Options | the answers | one row, the affirmative first |
| Detail | what supports the answer: a diff, a preview, the asker's stated reason | after the options, so the options do not move below it |

### The affirmative leads, and looks the same in every question

| The question is | Affirmative | Decline |
| --- | --- | --- |
| a **confirmation**: the person started the act | `Primary` | ordinary button |
| a **request**: a model, an agent or the harness asks for something it may not do alone | `Primary` | ordinary button |
| either, where saying yes destroys something | `Danger` | ordinary button |

Most requests are the expected next step of something the person asked
for, and a question with no focal point is slower to read. A caution
(the conversation has read untrusted content) is said by a note in a
warning tone above the options; it does not change the buttons, so
*Approve* is the same button in every dialog. An answer that cannot be
given (the question has gone stale) is disabled, with its reason beside
it. The widget draws the frame itself; a caller that
already frames the question (a card, a modal) passes `Unframed`.

### Words

- An option is the act in the person's words: *Approve*, *Decline*,
  *Export*, *Keep*. Not *OK*, not *Yes*.
- What follows is stated as what will happen, not as a description of
  the mechanism: "Kept for 30 days. Anyone who can read the store can
  read these files until then."
- What cannot be undone is said in those words.
- A count or a name in the question is the real one ("3 tables"), not a
  placeholder plural.

## Invariants

- A sentence stating a consequence is never cut and never set weaker
  than body text.
- The decline is never weaker than an ordinary button, and never further
  away than the affirmative.
- A question appears in one place at a time (ADR-0379 §SD1); where it
  can appear in a window or on a host card, both are the same drawing.
- A disabled answer stays visible, with its reason as a sentence.
- An app does not open a modal of its own for a decision (keelson-app A9).

## Trade-offs

- A primary on a request makes a routine approval quick and leans the
  person toward yes, also beside a warning. Taking the fill away there
  was tried: the warning is on most approvals, and the difference read
  as inconsistent styling.
- One widget fixes the order of parts. A question that needs another
  order is a sign it is two questions.

## Anti-patterns

- The consequence in the smallest type, under the buttons.
- *Approve* filled in one dialog and plain in the next.
- A column of filled `Danger` buttons, one per row; the armed state
  belongs to one item at a time (keelson-ux C2, C5).
- A decision that scrolls: options below a long diff.
- The same question in the transcript, in a side pane and on a card, each
  with its own buttons.

## Further reading

- [keelson-ux skill](../../skills/keelson-ux/SKILL.md) — where a decision sits among a window's other controls.
- [empty-states](./empty-states.md) — the sibling pattern for a pane with nothing to show.
