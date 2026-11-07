---
type: explanation
audience: end-user
status: draft
title: The coordinator
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The coordinator

With **Apps** on, a turn is a tool loop over the host's `runtime.agent`
services (ADR-0269): the model works only in windows you share with it for
a task, every call is checked by the host's dispatcher, and what comes back
from an app reaches the model delimited and attributed, as data. The chat
is the harness that runs that loop; it is the one app meant to hold the
model grant, and every other app only declares operations (ADR-0318).

Apps, Questions, Artefact and Keep are fixed at a conversation's first
send, because the coordinator's system prompt is the conversation's first
message. A conversation started without Apps has a model with no tools; its
badge reads **no apps**, and the model says so when asked to open or drive
a window. The host answers only a chat listed in `BOXER_AGENT_COORDINATORS`.

## The grant

Nothing is shared until the model asks. It asks with `request_access`: a
one-line plan, the apps it wants to open windows of, the destinations its
plan will reach (`keelson:apps`, `clickhouse:host:port`, `publish:<prefix>`),
whether it wants to arrange the desktop, and more rounds when its plan will
not fit. The request carries the **ceiling** from your Settings, and waits
for your decision in the host's queue (ADR-0324) — the turn's state reads
**your turn** — as a card you approve, lower or refuse. The model reads
"access granted" with the terms, or "the person did not grant access" with
the reason, and a next step it can take. An expired request
(`BOXER_AGENT_REQUEST_TIMEOUT`) is a refusal for that turn only; the model
may ask again.

A **task** is one grant: which windows, in which mode — observe, suggest or
act — with which destinations, call budget and deadline. Its row in the bar
names it, with **Stop task**; the windows it holds carry a badge from which
you can lower the mode. A request while a task exists is a widening of it,
asked the same way. A grant dies with this window's instance (ADR-0322).

## Calling an operation

`describe_app` lists the apps and their operations, with each one's effect
and argument schema; `read_help` reads the books apps ship, this one
included. `call_operation` takes `window`, `operation`, `args` and a
one-line `reason` shown to you. With **A tool per operation** on, each
operation of the task's windows is also a typed tool of its own,
`w<window>_<operation>`.

The dispatcher checks every call against the grant and the ceiling, stamps
it and records it; the app answers with a phase. `completed` and `rendered`
carry a result or revisions; `proposed` waits for you in suggest mode;
`refused` is the app's own reason, with its remedy; `denied` is the
dispatcher's; `conflict` says you or another writer changed the resource,
and the task is paused on that window until the next turn lists what
changed. An `accepted` call was still queued when the wait ended and
applies at the window's next frame. A call that answers `running` has work
that outlives the wait: the turn goes on, a watcher follows it, and its
completion wakes the model (ADR-0358). A refusal is remembered within the
turn, so the model is not told the same thing twice.

The person's own input always lands first: a command that reaches a
window while you are editing the resource it writes is a `conflict`, not
an overwrite. A **consequential** command — one that writes outside the app:
a publish, an export, a copy to the clipboard — is confirmed by you every
time, by the host, whatever the mode.

## Reading what comes back

An operation's output that may carry text someone typed is marked
untrusted; the coordinator wraps it, names the window, app and operation it
came from, and instructs the model that it is data, never instruction
(ADR-0269 §SD7). Window titles and captures are untrusted too. Once such
content has entered the model's context the conversation is **tainted**:
the bar's badge says so, grant dialogs say so, and every action records it,
for as long as the conversation lasts.

A result derived from sealed data is **confined**: the model gets a data
handle it can pass to another operation and cannot read, every later call
of the conversation is labelled confined, and the host sends it only to a
loopback or trusted endpoint. Reading a window the host says holds sealed
data is held once, so the model can choose not to.

## Questions, the queue and the journal

With **Questions** on, `ask_user` puts a form of options in the
conversation and the turn waits for your answer; the same question also
waits in the host's queue, where it can be answered. Decisions the model
owes you never open a modal: they are cards in the queue, acknowledged,
shelved or answered there (ADR-0324).

Before every act the coordinator writes a journal entry — the intent, then
the outcome — and the model service refuses a call that does not name the
entry written before it (ADR-0357). The journal is the only thing the model
is ever shown (ADR-0308), and its chain heads are anchored at each turn's
end (ADR-0314). A turn is bounded by its **rounds** of model calls
(`BOXER_CHAT_ROUNDS`, movable in Settings); the last round offers no tools,
so a turn that reaches it still ends with an answer.

## The other tools

`list_windows`, `query_windows` (SQL over `keelson('windows')` and
`keelson('desktop')`) and `read_window_tree` read what the task's windows
are and show; `open_window`, `close_window`, `raise_window`, `place_window`,
`arrange_windows`, `set_layout` and `float_window` act on them, the last
three under the desktop grant; `point_out` and `clear_marks` draw on the
screen for you; `cite` checks a claim the model drew from a result against
the tables that result read, before the model states it; `recall` brings
back a tool result compaction elided; `stop_task` ends the task and passes
its windows to you.
