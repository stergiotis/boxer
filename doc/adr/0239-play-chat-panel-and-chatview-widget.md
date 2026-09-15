---
type: adr
status: proposed
date: 2026-09-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Design dialogue in progress; this
> file is the living snapshot of it (AGENTS.md § ADRs). Do not implement as if
> accepted.

# ADR-0239: a `play` chat pane — a transcript from a query result, its bubbles drawn through the gloss catalog

**In one paragraph:** a new `play` result pane ("Chat") renders a result set
carrying a time, a sender and a body as a message transcript — two-party
dialogue laid out like a phone's SMS view, several parties laid out like a group
chat, chosen from the data — over a new `widgets/chatview` widget whose input is
columnar and whose state is host-owned. The bubble body is not a chat-specific
renderer: every column the contract does not claim renders inside the bubble
through its gloss ([ADR-0186](./0186-play-gloss-catalog.md)) exactly as the
Detail pane renders a row, so a markdown message, an image attachment or a JSON
payload cost the pane nothing new. Reactions and participants arrive as optional
channels, one row per occurrence, because that is the one shape play can read
and the one every reference model (Matrix, iMessage, Stream) stores.

## Context

play has panes for a board ([ADR-0122](./0122-play-kanban-panel.md)), a
timeline ([ADR-0043](./0043-imzero2-timeline-widget.md)), a file tree and a dozen
charts, and no pane for the most common event log of all: a conversation. The
markdown corpus ([ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md)) and the
watchbill trail ([ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md))
are both timestamped, attributed text; so is any chat export, any support
transcript and any LLM session. The Detail pane can show one such row; nothing
shows the sequence as the thing it is.

Substrate facts that shape the design:

- **There is no chat widget to bind.** The egui third-party catalogue lists no
  message or bubble widget; the crates it does list for the surrounding
  problems — `egui_commonmark`, `egui_virtual_list`, `egui_infinite_scroll` —
  cover ground the tree already has in `widgets/markdown` and the etable's
  visible-range gate. A binding would buy nothing and cost an IDL surface.
- **The reference data models agree**, and they agree on a row-per-occurrence
  shape. Matrix carries a reply, an edit, a thread and a reaction each as its
  own event relating to a target event id (`m.in_reply_to`, `m.replace`,
  `m.thread`, `m.annotation` with a `key`); iMessage keeps `is_from_me` on the
  message row, participants in a handle table and chat membership in a join
  table; the flutter and Stream client models carry `author`, `createdAt`,
  `status`, `repliedMessage`, `type`, and reactions and attachments as lists
  the server denormalises. WhatsApp's export writes system events as timestamped
  lines with no sender.
- **play cannot read a struct or a map.** The un-glossed cell renderer
  degrades an Arrow struct or map to a length, and no pane reads one. A
  `reactions Array(Tuple(...))` column would arrive unreadable. Lists of
  scalars are read everywhere.
- **The gloss catalog already renders a row as rich blocks.** The Detail
  pane's per-cell path draws markdown, code, JSON, CBOR and images from a
  `label@<media type>` declaration or a rule, caches the parsed artifact per
  value, and budgets a block's height from its line count before laying it out
  (the leeway-card block doctrine, ADR-0186's 2026-08-15 update). Its cache
  holds one row; a transcript shows many.
- **Virtualisation in this tree is the etable's** visible-range gate with
  per-row heights declared up front; the logviewer is a virtualised,
  follow-the-tail feed built on it. A `ScrollArea` offers a scroll-to-cursor
  request and nothing else: no offset, no stick-to-bottom. Wrapped-text height
  cannot be measured from Go; the shared estimator (`implot.Wrap`) over-charges
  mixed-case text by about a fifth.
- **The panel contract is named columns, not detection** (ADR-0122 §SD1), a
  contract matches on the gloss *label* so `body@text/markdown` still claims
  `body` (the Files pane's rule), a time slot is type-gated because a ClickHouse
  `DateTime` arrives indistinguishable from a count (the Timeline's rule), and
  secondary data comes on its own channel (`lanes`, `bands`, `nodes`).

## Design space (QOC)

**Question.** How does a message body reach the screen?

**Options.**

- **O1 — Chat-specific body renderer.** The widget takes a body string and a
  message kind (`text`, `image`, `file`, …) and draws each kind itself.
- **O2 — Gloss block faces, one per unclaimed column** (chosen). The pane
  claims the chrome columns; every other column renders inside the bubble
  through its gloss, on the Detail pane's per-cell path, with a per-(result,
  row, column) cache in place of the one-row cache.
- **O3 — Markdown only.** Every body is parsed as markdown; anything else is a
  link.

**Criteria.**

- **C1 — new rendering code.** How much of the bubble's content path is new.
- **C2 — reach.** Whether an image, a JSON payload or a code block in a
  message renders without a pane change.
- **C3 — consistency.** Whether the same value looks the same here and in
  Detail.
- **C4 — widget purity.** Whether `widgets/chatview` stays free of play and of
  the gloss package (the two-layer rule of ADR-0186 §SD1).

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | +  |
| C2 | −  | ++ | −− |
| C3 | −  | ++ | −  |
| C4 | +  | ++ | +  |

O2. The widget never sees a gloss: it takes a plain text body for the common
case and an optional per-message block callback carrying a declared height,
the contract `leewaywidgets.CellBlock` already has. play fills the callback
from its gloss resolution. O1 would re-implement the catalog's faces one kind
at a time; O3 would mis-render every plain-text export that happens to contain
an asterisk.

**Question.** How is a long transcript scrolled?

**Options.**

- **O1 — etable with estimated row heights.** One virtual row per message or
  separator, heights from the wrap estimator, visible-range gate, stick to the
  tail with the table's scroll-to-row.
- **O2 — `ScrollArea` over a bounded tail window** (chosen for the first cut).
  The newest N messages are emitted every frame inside a vertical scroll
  area; an "older" affordance at the top widens N; the scroll-to-cursor
  request holds the tail while `follow` is set.
- **O3 — a painter-lane canvas** with its own text layout, like graphview.

**Criteria.**

- **C1 — layout fidelity.** Bubbles sized by egui's own wrap, no gap or clip
  from an estimate.
- **C2 — cost at 10k messages.** Opcodes built per frame.
- **C3 — code.** Distance from a proven in-tree pattern.
- **C4 — reversibility.** Whether the choice can be swapped later behind the
  widget's API.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −  | ++ | +  |
| C2 | ++ | −  | ++ |
| C3 | +  | ++ | −− |
| C4 | +  | ++ | −  |

O2 for the first cut, O1 recorded as the deferral it is. A chat is read from
its tail; a window of a few hundred messages is what every messaging client
draws, and the kanban board emits two thousand framed cards into one scroll
area today. The estimate's fifth-of-a-line error is visible on *every* bubble
under O1, where the leeway card could accept it on a twelve-line block. The
widget's input does not encode the choice, so the etable path can replace the
scroll area when a measurement says the window is too small (§SD8).

## Decision

Build `widgets/chatview` — a columnar, host-owned-state transcript widget on
egui2's frame and label primitives — and a `play` "Chat" pane over it that
claims named columns, folds the result into the widget's model, renders each
bubble's unclaimed columns through their glosses, and joins optional
participant and reaction channels by key.

### SD1 — Panel contract: named columns, matched on the gloss label

The pane is a `PanelI` with three channels: `main` (required, the messages),
`participants` (optional) and `reactions` (optional). Column names match on
the gloss label, so `body@text/markdown` claims `body`.

`main`:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `ts` | timestamp | yes | when the message was sent; the transcript is ordered by it |
| `sender` | any | yes | who sent it; the formatted value is the participant key |
| `body` | any | yes | what was said; rendered through its gloss, plain wrapped text when unglossed |
| `id` | any | no | the message's identity; the row index when absent |
| `reply_to` | any | no | the `id` this message quotes; drawn as a quote strip inside the bubble |
| `system` | bool | no | a system line — a join, a leave, an encryption notice — drawn centred without a bubble |
| `deleted` | bool | no | the body was retracted; the bubble shows a placeholder, the body is not rendered |
| `edited_at` | timestamp | no | the message was edited; an "edited" mark with the time on hover |
| `status` | string | no | `sent`, `delivered`, `read`, `failed`; a mark on the viewer's own bubbles |
| `conversation` | any | no | which chat the row belongs to; a picker appears when more than one value is present |

**Every unclaimed column renders inside the bubble** after the body, through
its gloss, in schema order — a block face when the gloss has one (an image,
a JSON payload), the inline face otherwise (a size through `gloss/bytes`, a
URL). An unglossed unclaimed column renders as a weak caption:value line. This
is how an attachment reaches the bubble in the first cut: one column per
attachment kind, `photo@image/png` beside `body`, null where the row has none.

Type gates follow the Timeline's rule: `ts` and `edited_at` must be Arrow
timestamps; `system` and `deleted` must be booleans; everything else is
any type, because the cell formatter is total. A wrong type rejects with the
contract in the reason, and the reject names the shortest query that
satisfies it.

`participants` (join key: the formatted `sender` value):

| column | type | required | meaning |
| --- | --- | --- | --- |
| `sender` | any | yes | the key |
| `name` | any | no | the display name; the key when absent |
| `color` | string | no | a design-system colour token name, as the Timeline's bands take it; the qualitative cycle by first appearance when absent |

`reactions` is a first-class part of the contract, fed the way the Kanban
pane's lanes are (ADR-0122 §SD6): by a CTE named `reactions` in the same
query, demanded on its own lane so a costly aggregation never blocks the
transcript. A conversation and its reactions are one thought; a second buffer
would be a second thing to keep in sync. One row per reaction, Matrix's
annotation shape:

| column | type | required | meaning |
| --- | --- | --- | --- |
| `id` | any | yes | the message reacted to |
| `key` | any | yes | the reaction — an emoji or a word |
| `sender` | any | no | who reacted; listed on hover |

The pane aggregates reactions per (message, key) into a count, ordered by
first appearance, and the widget draws them as pills under the bubble with
the reactors on hover. An unmatched `id` is counted and reported in the
status line, never silently dropped. Rows of `main` with a null `ts` or
`sender` are skipped and counted the same way. Likewise `participants` is
read from a CTE of that name.

Rejected: an `attachments` channel in the first cut (one row per attachment,
`id`, `name`, `media@…`). It is the right shape for a message with several
attachments and it is §SD8's first deferral; the glossed-column form covers
the export formats at hand, which carry at most one media item per line.

### SD2 — Who is "me", and which layout follows

The viewer's identity comes from one place: a picker in the pane's options
bar listing the participants, "nobody" by default. A `me` column was
considered and dropped — iMessage's `is_from_me` is one export's convention,
and a column the contract claims for it would be a second route to the same
state as the picker, with the two disagreeing on the first result that carries
both. A query that knows the viewer can still say so in the `sender` value it
produces. Layout is derived, never declared:

- **Dialogue** — two non-system participants and a viewer: the viewer's
  bubbles on the right without a name, the other's on the left without a
  name, no avatars. The phone's SMS view.
- **Group** — anything else: every bubble on the left with the sender's name
  in the participant's colour and an initials disc, the viewer's own bubbles
  on the right when a viewer is set. The group-chat view.

With no viewer a two-party result renders as a group of two, which is
readable and honest; the picker is one click away. Detection of "me" from the
data — the sender with the most messages, the last sender — was rejected for
the same reason ADR-0122 rejected lane detection: nothing distinguishes the
viewer but intent.

### SD3 — The widget's model is columnar, its state is the host's

`chatview.Model` is one slice per attribute over message ordinals, the shape
`widgets/tree` and the timeline's event layout already take, with the
reactions as a ragged co-array in the leeway idiom (values plus offsets):

```go
type Model struct {
	TimeMS  []int64   // UTC epoch milliseconds, ascending
	Sender  []int32   // index into Participants; -1 for a system line
	Body    []string  // plain text; ignored for a message the host draws through Block
	ReplyTo []int32   // ordinal of the quoted message; -1 for none
	Flags   []FlagsE  // Me | System | Deleted | Edited, a bitset
	Status  []StatusE // StatusNone, Sent, Delivered, Read, Failed

	Participants []Participant // Name, Color, Initials

	// Reactions of message i are Keys[ReactionOff[i]:ReactionOff[i+1]] with
	// their Counts and Who; len(ReactionOff) == len(TimeMS)+1.
	ReactionOff   []int32
	ReactionKey   []string
	ReactionCount []int32
	ReactionWho   []string // ", "-joined senders for the hover
}
```

The host owns everything that persists across frames — `chatview.State`
carries the selected ordinal, the `Follow` flag, a requested scroll target and
the tail-window size — so persistence, "jump to reply" and "select from
another pane" are plain assignments, as with `tree.State`. Ordinals are the
widget's only identity, so a host whose result is rebuilt between frames
projects its stable keys (the `id` column) onto `State` before each render and
reads the widget's changes back out of `Result`, exactly as ADR-0176 §SD11
prescribes for the tree. A transcript appends at the tail, so ordinals are
stable under the common change.

The per-message block callback is the leeway card's contract, restated
locally so the widget depends on neither `leewaywidgets` nor `gloss`:

```go
type Block struct {
	Height float32 // declared before layout; the body scrolls inside when it runs short
	Render func()  // runs at draw time; must scope its own widget ids
}
type Input struct {
	Ids      *c.WidgetIdStack
	ScopeKey string
	Model    *Model
	State    *State
	Block    func(ordinal int) (Block, bool) // nil: every body is Model.Body
	Layout   LayoutE                         // LayoutAuto, LayoutDialogue, LayoutGroup
	Location *time.Location                  // day separators and hover times
	FillHost bool
	MaxHeight float32
}
type Result struct {
	Clicked    int32 // ordinal of the bubble clicked this frame; -1
	JumpTo     int32 // ordinal of a quote strip clicked; -1
	OlderWanted bool // the "older" affordance was pressed
}
```

### SD4 — Rows, clusters and separators are derived in the widget

From `TimeMS` and `Sender` the widget derives its row plan each frame (cheap;
cached on the model pointer and window): a day separator row wherever the
local date changes, and message clusters — consecutive messages of one sender
within a few minutes carry the name and time once, tighten their spacing and
round the bubble's sender-side corner only on the cluster's last message, the
iMessage convention. System lines break a cluster.

A bubble is a `Frame` with fill, stroke, asymmetric corner radii and inner
margin, its body width pinned to a fraction of the pane's width (`0.72`, the
common client value) read from the pane-size probe and held across the frame
the probe is absent; the body is a wrapped, non-selectable label or the host's
block. The viewer's bubbles are laid right-to-left. A system line is a
centred weak label. A quote strip is a hairline-bordered excerpt of the quoted
message's first line, a click on which sets `Result.JumpTo`. Reactions are
small pills below the bubble. Colours come from the design-system tokens and
the qualitative cycle; there are no chat-specific constants beyond the width
fraction and the cluster gap.

### SD5 — Scrolling: a tail window that follows

The transcript is a vertical `ScrollArea` over the newest `State.Window`
messages (default 300). While `State.Follow` is set the widget issues a
scroll-to-cursor at the tail every frame, so new rows keep the view pinned; a
wheel movement against the flow clears `Follow`, and a "newest" pill at the
bottom re-arms it. An "older" affordance above the first drawn row sets
`Result.OlderWanted`; the host widens the window (the model already holds the
rows, so nothing is fetched). A selection or jump target outside the window
widens it to include the target before scrolling.

This is the light cut of the second QOC question. The measurement that would
move it to the etable is a frame-time or allocation profile with the window
at its default against a 10k-message result — the lazypane doctrine, measure
before gating.

### SD6 — The pane's cache holds many rows

The Detail pane's per-cell cache holds the artifacts of one row. The Chat
pane keys its cache on (result id, row, column), bounded by the tail window
plus the selection, evicting rows that leave the window. Artifacts are the
same ones: a parsed markdown `Doc`, a highlighted code job, decoded pixels. A
block's declared height is budgeted from its line count with the card's
constants, and the body scrolls inside its own area when the budget runs
short — the doctrine ADR-0186's 2026-08-15 update set for the card.

Raw cell strings alias Arrow memory; anything retained is cloned, as the
regexp gloss already does.

### SD7 — Registration and fixture

The pane is a built-in tab with `shapeContract` and a selection signal, dock
id 31 (frozen, next free), so it appears on the strip, in the panes menu and
gets its `BOXER_PLAY_FOCUS_CHAT` knob from the registry. A click on a bubble
emits the selection with the Arrow row, so Detail follows a bubble the way it
follows a card.

The help corpus gains a "Chat transcript" snippet: a table-free `values(...)`
fixture exercising a reply chain, an edited message, a deleted message, two
reactions on one message, a system line, an image column glossed
`image/png`, and a `me` column — each an edge the recognizer must handle —
with a `reactions` CTE over a second `values(...)` block.

### SD8 — Deferred, and why

- **Etable virtualisation** (QOC 2, O1): swap in when the tail window
  measures too slow; the widget's input does not change.
- **An `attachments` channel** for several media per message: the glossed
  column covers one per row.
- **A conversation sidebar**: the picker covers switching; a list with
  unread counts and last lines is a pane of its own.
- **Threads** (Matrix `m.thread`, Stream `parent_id`): a `thread` column
  and a filtered view; the quote strip covers the reply case.
- **A composer.** The pane is a view over a result; writing back is a
  different decision (a facts kind and a bus verb), not a text box.

Storage of a chat corpus and import of any export format are outside this
decision: the pane reads a result, and where the result comes from is the
query's business.

## Surfaces — Tier 2

A new widget package and a new play pane following the panel contract; no
IDL, wire, registry or exported-API change. The `@` label matching and the
colour-token vocabulary are reused, not extended.

## Alternatives

- **Bind an external egui chat crate.** None exists; the adjacent crates
  duplicate in-tree widgets.
- **Reactions and attachments as `Array(Tuple)` columns.** Unreadable on
  play's display path; the row-per-occurrence channel is what the reference
  models store anyway.
- **Detect the viewer from the data.** Intent, not signal; rejected as lane
  detection was.
- **A painter-lane widget with its own text layout.** Exact heights and cheap
  scrolling, at the cost of re-implementing wrapping, selection and the
  markdown widget's reach.
- **Extend the Detail pane with a "previous/next row" transcript mode.** The
  Detail pane renders one row well; a transcript is a layout over many, with
  alignment, clustering and separators that belong to a widget.

## Consequences

### Positive

- Any timestamped, attributed text result — chat exports, support tickets,
  LLM sessions, the watchbill trail — reads as a conversation with one `AS`
  per column.
- Every gloss face reaches the bubble unchanged; a new gloss reaches the
  chat pane on the day it lands.
- The widget is reusable outside play by any host that can fill a columnar
  model: a transcript in a windowed app costs a fold, not a pane.

### Negative

- The tail window, not virtualisation, bounds per-frame cost; a result whose
  interesting part is not the tail pays a click per 300 messages to reach it.
- Two caches with the same artifact types exist until the Detail pane's
  one-row cache is generalised to this one.
- A message with several attachments needs several columns or the deferred
  channel.

### Neutral

- The pane adds an eleventh named-column contract to the vocabulary the
  panes menu teaches; the names (`ts`, `sender`, `body`) are the ones every
  export already uses.

## Verification plan

- **Lane.** Default `go test`: the recognizer against every reject prose and
  the fixture's edges; the widget's row plan (clusters, separators, layout
  choice) as table tests over a synthetic model; a golden of the fixture's
  model fold. The screenshot tour: a gallery demo per layout (dialogue,
  group) so the tour captures both.
- **What would fail.** A contract change that stops `body@text/markdown`
  claiming `body`; a cluster boundary that moves; a reject that names the
  wrong column.
- **Gap.** Stick-to-bottom and the "older" affordance are interaction over a
  one-frame-lagged scroll delta; they are verified interactively, not in the
  default lane.

## Status

Proposed — the design dialogue settled on 2026-09-15: the gloss body path
(QOC 1, O2), the tail window first (QOC 2, O2), the viewer from the picker
alone (§SD2), reactions as a first-class contract member fed by a CTE (§SD1),
a conversation picker with the sidebar deferred (§SD8), and storage and
import out of scope. Awaiting review by the code owner.

Built in the working tree the same day, against this text: `widgets/chatview`
with its row-plan tests and two gallery demos (dialogue, group), the play
pane with its contract and fold tests, the tab registration, the snippet
fixture and the features chapter. The two layouts were verified through the
screenshot tour, and the pane in a live play session against a ClickHouse
server on the snippet fixture: the roster and reactions CTEs joined, the
image column drew inside its bubble, the viewer picker moved a party's
bubbles to the right, a wheel movement released the tail, a bubble click
drove Detail, and a quote strip jumped to the quoted message. The "older"
widening is the one interaction left to a transcript longer than the window.

## References

- [ADR-0186](./0186-play-gloss-catalog.md) — the gloss catalog, faces and the
  card block doctrine.
- [ADR-0122](./0122-play-kanban-panel.md) — named-column contract, secondary
  channel, `@` token separator.
- [ADR-0043](./0043-imzero2-timeline-widget.md), [ADR-0176](./0176-native-tree-widget.md)
  — columnar widget input, host-owned state keyed on ordinals.
- [ADR-0217](./0217-mdedit-send-to-play-mddoc-facts.md) — row-per-occurrence
  facts with back-references; the storage shape §SD8 defers to.
- Matrix client-server specification, `m.room.message` and relations
  (`m.in_reply_to`, `m.replace`, `m.thread`, `m.annotation`).
- iMessage `chat.db`: `message.is_from_me`, `handle`, `chat_message_join`.
- `flutter_chat_types` `Message`; Stream Chat message object (`parent_id`,
  `quoted_message_id`, `reply_count`).
- egui third-party crate list: `egui_commonmark`, `egui_virtual_list`,
  `egui_infinite_scroll`.
