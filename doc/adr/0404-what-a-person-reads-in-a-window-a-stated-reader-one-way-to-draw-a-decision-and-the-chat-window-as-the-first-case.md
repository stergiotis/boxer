---
type: adr
status: proposed
date: 2026-11-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0404: What a person reads in a window — a stated reader, one way to draw a decision, and the chat window as the first case

## Context

A reading of scene captures on 2026-11-22, each judged cold against one
rubric, separated the app windows along a line no rule names. The windows
that read well are organised around a job with a first step and use the
words of the person doing it. The windows that read badly are organised
around their mechanism and use the builder's words. No app states who it
is for, so the builder is the reader by default. The captures were
scripted, not recordings of people at work, and nothing below has been
tested with a person of the role it names.

Three findings from that reading are properties of the toolkit and the
host, not of one app:

- **A decision asked of the person is drawn by hand wherever it occurs.**
  The chat window draws a proposed change, an export and a screenshot
  consent each with its own code; the host draws its queue cards and the
  agent's request with theirs. They disagree on what matters most. The
  host wraps its sentences and fills the affirmative button. The chat sets
  the sentences that state the consequence in its smallest, weakest type,
  does not wrap them, and fills nothing. In one capture the sentence
  saying what an export contains and what confirming endorses is cut at
  the pane's edge.
- **A segmented choice cannot be told from a row of commands.**
  `selector.StyleSegmented` draws each option as a separate framed button.
- **No layout invariant sees text cut by an edge.** `clipped` finds text
  the host elided with "…", and `outside` checks interactive nodes only
  ([ADR-0363](./0363-porting-from-an-end-user-egui-application.md) M1).

The chat window is the case these are tried on. It serves three readers
at once: the person conversing, the person supervising a model that works
in their windows, and the builder of the harness. At its default size the
bar above the transcript runs to five rows, and the messages take
between a tenth and a sixth of the window (estimated from captures, not
measured).
[ADR-0379](./0379-the-chat-window-one-place-for-a-decision-ids-unique-per-conversation-and-what-the-person-reads.md)
§SD5 kept the full bar as the default and the dock's split unchanged, by
the person's choice on 2026-11-17; the same person reopened both on
2026-11-22.

## Decision

### SD1 — An app states its reader and its job

The first paragraph of an app's help overview is its brief: the role it
is for, the job, what triggers it, what the reader knows and does not.
`Manifest.Summary` is the brief's short form. The
[keelson-ux skill](../skills/keelson-ux/SKILL.md) holds the rules that
follow from a brief. Nothing checks the brief mechanically (Deferred).

### SD2 — One reader by default; the others are one step away, not a mode

A window is drawn for its primary reader. What a second reader needs sits
behind one step: a fold, a tethered inspector, a menu, the playground. A
setting that swaps the whole window between a plain and a full rendering
is two windows to maintain and test, and leaves the choice of reader to
the person least able to make it, the one who has just arrived.

### SD3 — A decision is drawn by one widget, in the window and on the card

`widgets/decision` draws every question the person is asked to settle
with a button: what is asked, the thing it is asked about, what follows
from each answer, any note that the question has gone stale or cannot be
granted, and the options. The sentences that state what follows are body
text and always wrap. What the person has to read or fill in before
answering (a form, the question's own text, a call's arguments) comes
before the options; detail that supports the answer (a diff, a preview,
the asker's stated reason) comes after them, so the options do not move
below a long body. An app's in-window form and
the host's card for the same question
([ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md),
ADR-0379 §SD1) are the same drawing.

The widget draws. Who may ask, what an answer authorises and where it is
recorded stay with the host and the grant
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).

### SD4 — The affirmative leads, unless the question cautions

- The affirmative is the primary, where the person started the act and
  is asked whether to go through with it, and where a model, an agent or
  the harness asks the person for something it may not do on its own.
- **A request that carries a note of caution has no primary.** Where the
  question itself says, in a warning or an error tone, that there is a
  reason to hesitate (the conversation has read untrusted content, the
  question has gone stale), the options have equal weight. A
  confirmation of the person's own act keeps its primary.
- An affirmative that destroys is `Danger` in every case.

The option that declines is never drawn weaker than a plain button. The
widget assigns the kinds from who started the act, each option's role
and the notes' tones; a caller does not choose them.

The first form of this decision, on 2026-11-22, gave no request a
primary at all, so that the surface would not lean the person toward
widening what an asker may do. The person testing it found a dialog
whose buttons all look the same, which is the flat hierarchy this ADR's
context names as a defect; the fill came back the same day, and the
equal weight was kept for the one case where the surface has itself
raised a caution.

### SD5 — A segmented choice reads as one control

`selector.StyleSegmented` draws its options under one outline, a
hairline apart, the selected option filled and ringed in the accent; an
option under the pointer takes a neutral fill and no ring, and no option
changes size when it is hovered or selected. The
outline's rounding is the active theme's rounding for a button plus the
inset. At rest it is not mistakable for a row of command buttons.

### SD6 — A layout invariant for cut text

A scene capture gains a fifth invariant, `cut`: a text node that reaches
past the right edge of the window it is in. It starts advisory, as
`overlap` is, and becomes blocking once the maintained scenes carry no
finding. The tree does not hold a pane's edge inside a window, so text
cut by an inner pane is still not seen.

### SD7 — The chat window

The primary reader is someone who asks a model to work on their data and
in their windows, and stays in charge of what it may do. They know their
data. They have not read the harness.

- **One bar row**, which wraps where the window is narrow: New
  conversation, the title, the state chip, the ceiling on what the model
  may do in words. The words open the settings. What is used rarely is
  in the window's menu bar (SD8), not in a button of the bar's own. A
  second row holds the marks that the conversation read untrusted or
  holds confined content, which are the conversation's and outlast a
  task, and, while a task runs, stopping it; at most one notice follows.
- **Facts behind the chip.** The conversation's id, the endpoint, the
  context used, what the first message fixed, its label, and the task's
  id with what it was granted are in the state chip's inspector. The
  label is stated as the tier with who it lets read the conversation,
  the read that raised it, whether untrusted content was read, and the
  providers the content was sent to (ADR-0311): the two marks say that
  something happened, the inspector says what.
  The chip names the state in the reader's word; the operations keep the
  machine's.
- **The side pane appears when it has something to show**: the
  artefact's first content, a decision that lives there, or a view the
  person opened from the menu or from the words. It opens once for each
  such occasion and stays closed when the person closes it. Before that
  the transcript has the window. Analytics and Stops are the builder's
  and are reached from the menu.
- **The dock stays.** The transcript, the composer and the side views
  are tabs of one dock, so a view can be dragged beside another and the
  window keeps its layout. A side view is declared only while it is
  open, and closes from its tab or from the View menu; SD8 has what the
  dock needs for the column to come back where it was declared. The
  transcript and the composer, each alone in its leaf, draw no strip.
- **One rendering.** The guided view that a chatlet already uses is the
  plain chat's only view, in a window and embedded in a pane. The Simple
  view setting and the bar's Details fold are retired; the inspector
  takes the fold's place.
- **One way to switch views.** The artefact's own views (read,
  source, revisions, lint, images) are tabs of a dock as the window's
  are, not a second control inside the pane.
- **Decisions** are drawn by SD3 and weighted by SD4. A proposed change
  says what accepting and rejecting each do. A question about the
  artefact heads the artefact's own tab and brings that tab forward
  once, so the tab strip is not pushed out of the pane by it.
- **The artefact keeps its name.** The view, the setting and every
  sentence about it say *Artefact*, the word the tools, the journal and
  the help already use. Relabelling it *Document* for the person was
  tried and taken back: it gave one thing two names, and a document is
  also what mdedit opens and what a chatlet is made from.
- **Steps in the reader's words.** A step in the transcript reads as what
  was done; the tool's name and the call's telemetry are in a hover or
  under the step's details.

What the model may do, what is asked before it acts and what is recorded
do not change; neither does the operations catalog. This replaces the
last paragraph of ADR-0379 §SD5, and the Simple view setting of
[ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)'s
fourth milestone.

### SD8 — The toolkit's mechanism first, and extended where it falls short

The first rebuild of the chat window, on 2026-11-22, replaced the dock
with panels and a pane header of its own, and put the rare commands in a
single menu button. The person testing it found three mechanisms for
panes in one window and a menu where the window's menu bar belongs. Both
came from routing around the toolkit: the dock places a tab that appears
later in its first leaf, so a column could not come back where it was
declared, and instead of adding that to the dock the window was rebuilt
without it.

- A window's rare commands are in its menu bar (`c.MenuBar`), as play's
  and the host's are.
- Panes are tabs of a dock (`c.DockArea`). Switching between views is
  the dock's tab strip, at every level.
- Where the mechanism lacks one capability, the capability is added to
  the mechanism. The dock gains it here: a tab that appears is placed in
  the leaf it was declared in, and that leaf's split is made again when
  it is gone.
- A control composed from primitives follows the theme it is drawn in.
  The segmented bar's outline takes the theme's rounding for a button,
  and its segments do not touch when one is hovered.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/decision` | added | its demo, the conformance sweep, the packageprops table |
| `selector.StyleSegmented` | reshaped (look only) | captures and SVG goldens of every bar that uses it |
| `scene/layoutcheck` invariants | `cut` added | the imzero2-drive skill's table, scenes' `allow` lists |
| the host's attention cards and the agent's request | redrawn through `decision` | scenes that match those buttons by role and name are unaffected; captures change |
| the chat window's bar, panes and settings | reshaped; the Simple view's persisted key no longer declared | the chat's scenes, its help book, chatlet captures |
| the dock's reconciliation of declared tabs | a tab that appears goes to its declared leaf, not the first; a tab no longer declared is removed leaf by leaf | every dock that declares a tab only some of the time (tally's results, mappingplanview's outputs); the browser host digest |
| `ButtonFluid` | `Extend` (the label stays on one line), `HoverText` (a tooltip without a wrapping scope), `ReserveFrame` (the frame's room kept while no frame is drawn) and `Fill` added | the IDL, the generated bindings, the browser host digest |
| `DockAreaFluid` | `Closable` and `CloseRequested` (the tab's own close button, reported to the app, which stops declaring the tab) and `HideTabBarWhenAlone` added | the IDL, the generated bindings, the browser host digest; a dock that hides a strip loses the tabs' context menus |
| `styletokens.RoundingButton` | added | the selector's outline; a test that reads the light theme's value from the host |
| `fsmview.Options` | `Details`, `ChipLabel`, `ChipTooltip` and `Inline` (the chip in the caller's row, on its centre line) added; an auto-anchored popup opens beside its toggle, not over it | every app that mounts a phase chip gets the popup's new position |

## Alternatives

- **A guideline for hand-drawn decisions, no widget.** Not taken: five
  renderers already disagree on wrapping, type strength and emphasis, and
  a consent sentence was cut in one of them.
- **No primary on any request.** Tried first and not kept: a dialog of
  equal buttons has no focal point, and most requests are the expected
  next step of something the person asked for.
- **The affirmative as primary on every request**, cautioned or not. Not
  taken: beside a warning that the request may have been steered, a fill
  on *Approve* favours the answer the warning is about.
- **The decline as primary on a request.** Not taken: it makes each
  routine approval a search without making it more considered.
- **Keeping the Simple view as an option.** Not taken: SD2.
- **Finding cut text from pixels.** Not taken: the tree already holds
  each node's bounds; reading glyphs back from a raster is a second
  source of truth for what the host drew.

## Consequences

### Positive

- A consent's consequence is read at body strength and cannot be cut by
  a missing wrap.
- The transcript, which the chat window exists for, has the window until
  something else earns room.
- The chat keeps one bar rendering.

### Negative

- Every host card changes its look for every app at once: the options
  run left to right with the affirmative first, and an asker's stated
  reason and plan now sit below them.
- The chat embedded in another app's pane keeps a row of view names of
  its own, a second way to switch views that SD8 otherwise rules out:
  the dock's tab strip scrolls where it overflows and cannot wrap, and
  five names do not fit a narrow pane's width. Its two menus sit at the
  end of that row and read as command buttons there.
- A scene that finds a closable tab by a part of its title also finds
  the tab's close button, whose name holds the title.
- In the light theme a hovered segment keeps a faint ring from the ghost
  kind, and its fill reaches the bar's outline; the button has no stroke
  to override.
- A leaf dragged shorter than the artefact's header and tab strip shows
  none of a question waiting in it.
- The builder loses the conversation's id, endpoint and token count at a
  glance; they are one click away.
- The chat's scenes that reach a tab or the Details fold by name have to
  be rewritten, and their captures retaken.
- A person who set the Simple view finds the setting gone; the default
  they get is closer to it than to the full bar.
- A check box's state is not in the accessibility tree, so the menu's
  Watch and Urgent change their label with their state to stay
  assertable in a scene, which the keelson-ux skill otherwise advises
  against.
- A scene has to press a menu's item with the pointer; an accessibility
  click leaves the menu open.

### Neutral

- The emphasis rule is enforced only for the widget's own callers; a
  decision drawn by hand elsewhere can still differ.
- A conversation that has read untrusted content is common once a model
  works in windows, so the unfilled *Approve* is not rare; the note
  beside it says why.

## Deferred

- **Where the host's capture row belongs** for a reader who is not the
  builder. The switches exist (`KEELSON_WINDOW_SVG_BUTTON`,
  `KEELSON_WINDOW_PAUSE_BUTTON`). Trigger: the review of this ADR's
  verification captures, where the row is the first thing in each.
- **A checked place for the brief**, on the manifest or in the help
  corpus test, and a launcher that shows it. Trigger: a second app
  written against SD1.
- **A searchable list of a window's commands** drawn from its operations
  catalog. Trigger: an app whose menu grows past what one menu holds.
- **Real roles in the accessibility tree** for tabs, rows and chips.
  Trigger: a check that has to count commands.
- **What the desktop says while a window holds the question.** By
  ADR-0379 §SD1 a question drawn in the focused chat window is not a
  card in the host's queue, so the menu bar reads "nothing waits on you"
  beside a window whose chip reads "your turn". Every cold reader named
  this as the most confusing thing on that capture. It is left to a
  review of attention management as a whole, which the person asked for
  on 2026-11-22, and is not patched here: the same review has the card
  that opens over the window that asked, the three places a question can
  appear (the window's form, the queue's card, the agent's modal), and
  how often the person is asked at all
  ([ADR-0381](./0381-what-one-approval-covers-endpoints-host-tables-launch-counts-and-standing-permissions.md),
  proposed).
- **One vocabulary for what a model may do.** The ladder's levels, the
  window modes and the settings' options overlap. It touches the grant
  model and is its own decision.

## Verification plan — Tier 1

The chat window is read cold before and after: the first-open capture, a
capture mid-turn and a capture with a decision open, given without
context to a reader who has not seen this ADR, with three questions (who
is it for, what does it do, what would you do first). Beside that: the
layout invariants including `cut` over the size, theme and density
matrix; the transcript's share of the window at the app's surface size
before an artefact exists, which SD7 expects to be more than half; and
each of the chat's top tasks as a scene that reaches its controls by
name. A rule the rebuilt window needed and the keelson-ux skill did not
have is added to the skill; that list is the result of the validation.

**First run, 2026-11-22.** One cold reader per side, a fresh session each
time, three captures; the scenes behind the two sets are not the same
scenes, so the comparison is indicative.

| Capture | Before | After |
| --- | --- | --- |
| first open, confidence of 10 | 5 | 7 |
| a proposed change waiting | 6; what Accept does was guessed | 8; both answers quoted from the window |
| the export question | 4; three sentences cut at the pane's edge | 7; no sentence cut |

On the first-open capture the transcript's share of the window went from
about a quarter to about seven tenths, estimated from anchors in the tree
sidecars, since a panel has no node of its own. With the side pane open
on a decision it is about a fifth. The layout invariants report nothing
inside the chat window on the six captures taken, at the default size and
at about 620 by 500. Not run: the light theme, the density matrix, a
person of the role.

**Second pass, 2026-11-22.** The person testing the first rebuild found
the lone menu button, the panels in the dock's place, the unpolished
segmented bar and the nested switch in the artefact (SD8). The window
went back onto the dock and the menu bar; the dock gained the placement
rule; the artefact's views became tabs of a dock nested in its tab. The
chat and chatlet scenes pass but for one whose cause is a race in
another app's operation. The cold read was not repeated on this pass.

**Third pass, 2026-11-22.** The dock's tabs close from the tab; a pane
that holds one view draws no strip; the segmented bar keeps its
segments' positions on hover (measured from the tree sidecars at rest
and on hover, in both themes); the artefact's reading view is named
*Read*; a command is not offered beside the question it raised. A cold
read of this state gave 7, 7 and 6 of 10 on the same three kinds of
capture: against the first rebuild's 7, 8 and 7 that is no gain a single
reader can show, and the export question's new sentence about its label
was the thing least understood on that capture. The chatlet scenes pass
one at a time and two of three time out waiting for the host when run
together on a machine under heavy load from other work; whether load is
the whole cause is not established.

What the after reader still did not understand: "Chat with scripted",
"label CLEAR", *Endorse* beside an export that also endorses, "what it
sent", and the menu bar's "nothing waits on you" (Deferred). Still on the
surface against SD7: an *Export* in the pane's header beside the one in
the question, the line "revision 0 · 0 lines" above a first proposal,
and the call counts in a waiting turn.

## Status

Proposed 2026-11-22.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0379](./0379-the-chat-window-one-place-for-a-decision-ids-unique-per-conversation-and-what-the-person-reads.md) — the chat window's earlier pass; SD7 replaces the last paragraph of its §SD5.
- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — chatlets and the guided view; SD7 retires the Simple view setting of its fourth milestone.
- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md) — the host's queue, whose cards SD3 redraws.
- [ADR-0273](./0273-imzero2-ids-button-kinds.md) — the button kinds SD4 assigns.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the contract `widgets/decision` follows.
- [ADR-0363](./0363-porting-from-an-end-user-egui-application.md) — the layout invariants SD6 adds to.
- [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) — the design system whose second tier asked the hierarchy question this ADR answers for decisions.
