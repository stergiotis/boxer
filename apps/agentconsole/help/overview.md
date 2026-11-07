---
type: explanation
audience: end-user
status: draft
title: The agent console
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The agent console

The agent console is a coordinator driven by hand (ADR-0269): a window that
asks for a task grant over another window and calls that window's
operations through the host's `runtime.agent` services, showing every
outcome. It is the developer's way to exercise an app's catalog — and the
window host's frame, queue and dispatcher — without a model; the chat app
is the coordinator a model drives. Everything the console does, a chat's
model does through the same services under the same checks.

The console must be registered as a coordinator for the host to answer it
(`BOXER_AGENT_COORDINATORS=agentconsole`); a grant then needs the person's
approval in host chrome. On the headless host, `BOXER_AGENT_TEST_GRANTS`
issues grants without the person, for scenes.
`BOXER_AGENTCONSOLE_DESTINATIONS` seeds the destinations field of a new
window, since a headless scene cannot focus an empty unnamed input.

## The window

The first row opens windows and holds the grant: **Open operations demo**
opens a window of the operations demo and fills the **Window** field with
its key; the `app` field with **Open** does the same for any app, by id or
subject alias. **Request grant** and **Stop** start and end the task. The
second row is **Destinations**, a comma-separated list of what the task may
reach beyond its windows (`keelson:apps`, `clickhouse:localhost:8123`).
Under them a line reads `no grant` or `grant task-…`.

The **Operation** row has the operation's name (`get_state` at first) and
the verbs **Describe**, **Capture**, **Capture PNG** and **Turn**; the
multiline **arguments** field holds the operation's arguments as JSON
(`{}` at first); **Call** sends them. Below: the console's note, a
`waiting on N request(s)` line while calls are out, and the call log, newest
first, at most 50 rows.

Every request runs off the render goroutine, as work of the window's
lifetime scope: the host refuses `runtime.agent` requests made on the render
goroutine, and waits for the scope's work at close.

## The grant request

**Request grant** needs a window key; without one the note says "give a
window key first". It asks the host for a task whose plan is "drive window
N by hand", with one entry — that window in *act* mode — and the
destinations listed. The request waits for the person's decision: in the
desktop host it is a card in the attention queue (ADR-0324), not a modal,
and the console's note reads "asked the person; waiting for approval" until
it is answered. Approved, the note reads "act on window N" and the grant
line names the task; the demo window carries the task's badge, from which
the person can lower the mode to *observe* — a later write then waits as a
widening, in the same queue.

A grant ends with its coordinator (ADR-0269 §SD6): **Stop** ends it, and so
does closing the console, once its calls have left.

## Calling an operation

**Describe** lists the task's windows to find the one the key names, then
asks the host to describe that window's app — the one operation in the
field, or all of them — and prints `name (effect): summary` with each
argument schema. **Turn** does what a coordinator does before each model
turn: it lists what other writers changed in the task's windows since the
previous turn and lifts the task's pauses (ADR-0269 §SD3, §SD8); the note
counts the changes and names each writer, resource and window.

**Call** sends the operation with the arguments field, under a key `kN`
and the reason "by hand from the agent console". A row appears as
`kN · operation · sent` and then takes the outcome's phase and reason —
`completed` for a query, `rendered` for a command that applied,
`proposed` in *suggest* mode, `refused` with the app's reason, `conflict`
when the person was editing the resource or it moved. A phase that is not
final is polled, two seconds a poll, until it is. A refusal's reason is the
app's own, and names the remedy where the app gives one.

**Capture** and **Capture PNG** ask the host for a capture of the window as
SVG or PNG (ADR-0281); the row shows the capture's media type and either
its path or `N bytes, sealed`.

## Reading a result

A final outcome with a result is read through the host and shown under its
row:

- plain text, when the operation's output is trusted;
- `untrusted, from <source>: <text>` when the app marked its output
  untrusted — the operations demo's `get_state` does, since the note is
  text someone typed. The prefix is how a coordinator shows its model such
  content: delimited and attributed, as data, never instruction (ADR-0269
  §SD7);
- `confined: data handle <handle> (<source>)` when the result derives from
  sealed data the caller may not read; the handle can be passed on, not
  opened;
- `read: <error>` when the result could not be read.

## No catalog of its own

The console declares no operations, and its test asserts so. A coordinator's
catalog is other windows' (keelson-app rule A1): what it holds — a key typed
into a field, a log of outcomes — is the person's working state at this
window, and nothing another agent should read or drive. What the console
did is in the host's records, where every coordinator's work is:
`keelson('agent_grants')` and `keelson('agent_actions')`, and the audit
trail on `boxer.facts`.
