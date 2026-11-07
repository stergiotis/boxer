---
type: reference
audience: end-user
status: draft
title: The turn machine
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The turn machine

Where the conversation's turn stands is one state (ADR-0265 §SD8): what a
running turn is waiting on, or how the last one ended. The **Turn** chip in
the bar names it, and a click opens the graph below with the history of the
turn's steps. The window never drives the machine: every frame observes
the state — from the turn in flight, the edit in progress and the
transcript — and mirrors it, so the chip can never disagree with the
window. A move the declared graph cannot reach is logged as a warning, and
counted.

## The states

| State | Meaning |
| --- | --- |
| `idle` | no message was sent in this conversation |
| `model` | a model call is in flight |
| `tool` | a tool the model called is running |
| `your turn` | the turn waits on you: a request for access, a question, a confirmation |
| `answered` | the last turn was answered |
| `stopped` | the last turn called tools and ended without an answer |
| `failed` | the last turn's model call failed or was refused |
| `cancelled` | you cancelled the last turn |
| `editing` | the last turn is taken back, its message in the composer |

A turn is running in `model`, `tool` and `your turn`. A conversation
without Apps, Questions or an Artefact never leaves `model` while a turn
runs; with them, the tool loop reports which of the model, a tool and you
it waits on.

## What moves each edge

- `idle → model`: **Send**.
- `model → tool`: the model calls a tool. `tool → model`: the tool returns.
- `tool → your turn`: the tool asks you — a grant request, a question, a
  consequential command to confirm. `your turn → tool`: you decide.
- `model → answered`: the answer. `model → failed`: the call was refused or
  failed. `model → stopped`: the call failed after tool calls were made, or
  the turn ran out of rounds; its calls happened, unlike a failed turn's.
- From `model`, `tool` and `your turn`: **Cancel** → `cancelled`, **New
  conversation** → `idle`.
- From any ended state (`answered`, `stopped`, `failed`, `cancelled`):
  **Send** → `model`, **New conversation** → `idle`.
- From `stopped`, `failed` and `cancelled`, putting the message back
  (**Edit** on a message that got no answer) shows the turn before it, so
  one ended state may move to another.
- `answered → editing`: **Edit**. `editing → model`: **Send**;
  `editing → answered`: **Cancel edit**; `editing → idle`: **New
  conversation**.

A frame samples the turn, so a tool call that starts and ends between two
frames shows as an edge that skips `tool`; such a path is declared and
logged at debug, not warned.

## The queries an agent reads

The window's operations catalog (ADR-0269) is the machine and nothing
else: an agent sharing this window reads where its turn stands and no
more. Nothing in the catalog sends a message or changes a setting.

| Operation | Returns |
| --- | --- |
| `turn_state` | the current state, the transitions allowed from it, and the latest steps (at most 16) |
| `turn_machine` | every state and every allowed transition |

Both are queries, read the resource `turn_state`, and are offered to
agents; a window that has no machine mounted answers with an empty view.
The same shape is what every app's mounted machine offers, so an agent
that has read play's or jackstay's needs nothing new here.
