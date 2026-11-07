---
type: explanation
audience: end-user
status: draft
title: The operations demo
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The operations demo

The operations demo is the smallest participant in the app operations
contract (ADR-0269): one window holding a note and a level, each bound to a
widget the person edits and each a resource an agent can read and write
through the window's catalog. It exists to watch the two writers meet —
which change wins a tie, when a command waits, and what the person sees. It
has no data of its own to publish and reads none.

The window is a multiline text input (hint `note`), a slider `level` from 0
to 100, a **Clear** button with its count (`cleared N×`), a **Copy** button
with its count (`copied N×`), a line saying what the clipboard broker did
with the last copy, and a summary line: `note: N characters · level L`. The
level in the summary is printed with one decimal.

## The operations

The catalog declares two resources, `note` (the note text) and `level` (the
slider), and these operations. All of them are offered to agents.

| Operation | Kind | Effect | Reads / writes | Arguments |
| --- | --- | --- | --- | --- |
| `get_state` | query | none | reads `note`, `level` | none; returns `Note` and `Level` |
| `set_note` | command | document | writes `note` | `Text`, the new note |
| `set_level` | command | document | writes `level` | `Level`, 0 to 100 |
| `clear_note` | command | document | writes `note` | none |
| `copy_note` | command | consequential | reads `note` | none |

What each does, and what it refuses:

- `get_state` returns the note and the level as the window held them after
  the frame's command stage. Its result is marked **untrusted**: the note is
  text a person or an agent typed, so a coordinator shows it to its model
  delimited and attributed, as data (ADR-0269 §SD7).
- `set_note` replaces the whole note. Its gesture is typing in the note.
- `set_level` refuses a value outside 0 to 100 with the reason "the level is
  between 0 and 100" and changes nothing. Its gesture is dragging the
  slider.
- `clear_note` empties the note. It is also what the **Clear** button calls:
  the button goes through the gesture path (`appops.Gesture`) so an agent
  and a click run the same handler, and clears the note directly where no
  host serves the catalog (a test, the tour).
- `copy_note` writes outside the app, to the clipboard, so its effect is
  **consequential**: the host asks the person to confirm it every time an
  agent calls it, whatever mode the window was shared in (ADR-0269 §SD5).
  The **Copy** button calls the same operation.

A command in a window shared in *observe* mode is answered
`input_required` (the mode has to be raised); in *suggest* mode it is a
proposal the person accepts; in *act* mode it applies, and `set_note`,
`set_level` and `clear_note` can be undone from the call's card in host
chrome, because both resources are declared restorable.

## Two writers

Both resources carry a revision the host tracks, and each declares how to
tell that the person is editing it: the catalog reads the bound widget's
response flags through the widget's handle (`appops.WidgetEditing`). An
agent's command that reaches the window while the person is typing in the
note is a **conflict**, not an overwrite — the window's own first line says
so: "Typing in the note while a change is queued makes the change a
conflict: the person wins a tie." A command that expected an older
revision of a resource is a conflict as well, and the task is paused on
this window until its coordinator's next turn, which lists what other
writers changed (ADR-0269 §SD8). The remedy for an agent is to read
`get_state` again and resend.

## Copy, and the clipboard record

**Copy** hands the note to the clipboard broker as a labelled payload: the
text, the zero label (the note is the person's own text, so plain text),
and the source `github.com/stergiotis/boxer/apps/opsdemo:note`, the widget
it was copied from (ADR-0317 §SD1). The round trip runs off the frame, in
the window's lifetime scope, and the broker's answer is shown under the
buttons as one of:

- `copied as <verdict> · N bytes` — the copy went to the system clipboard;
- `copied by reference (<handle>): paste it inside boxer` — the broker
  handed out a `boxer:clip/…` handle instead of plaintext;
- `refused: <reason>` — the broker refused the write;
- `not copied: <error>` — the request did not reach the broker; without a
  bus the line reads "not copied: this host gives the app no bus".

The `copied N×` count is the number of Copy gestures and `copy_note` calls,
not of copies that reached the clipboard.
