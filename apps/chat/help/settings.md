---
type: reference
audience: end-user
status: draft
title: Settings and ceilings
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Settings and ceilings

The **Settings** tab holds every option of a chat window and the scale of
what the model may do (ADR-0280). The scale is one ladder with two
markers: **may** is the ceiling, the most the settings here let the model
ask for, which the host enforces; **now** is what the running task was
granted, by you in the host's dialog, under that ceiling. The chat computes
neither bound's effect: it relays the settings and draws what the host
reports.

At the top, **Show the model's reasoning as it writes** draws the reasoning
under a running model step, from the moment you tick it. Off, the step only
says it is reasoning, and the text is under the step's details once the call
ends.

**Simple view** folds the bar under **Details**, brings **Artefact** to the
front when the model first writes it, and describes a running turn as
"working…" with a count of steps. The next chat window opens the way you left
this. A chatlet's document sets its view, so its Settings do not offer this.

## What the model may do

- **Apps** — let the model work in app windows. A checkbox before the first
  message; a line afterwards, since it is fixed at the first send.
- **At most** — Read, View, Edit, Run, Outside: the highest effect class
  the model may ask for. Read keeps every window in *observe* mode; the
  others map to view, document, run and consequential effects.
- **Changes** — Ask first or Apply directly: whether a change waits for you
  as a proposal (*suggest* mode) or applies (*act* mode).
- **Reach** — This host, Data endpoints, Network: how far a task's
  destinations may go.
- **Pace** — At a pace I can follow, or As fast as it can: whether the host
  spaces the model's changes so each can be followed and stopped.
- **Open windows**, **Arrange the desktop** — whether a grant may let the
  model open windows and arrange them.
- **A tool per operation** — each operation of the task's windows as a
  typed tool beside `call_operation`.
- **Pixels** — none, ask, ask-once, captures: which of the artefact's
  screenshots the model may see, and with whose consent (ADR-0287); **Only
  to a local model** keeps them to a model on this machine or one the host
  trusts with sealed data.
- **rounds per turn** — a slider within the host's range.

Under the scale a line says why *now* is where it is: no task yet, Apps
off, or the task the grant belongs to.

## What the harness reads from the environment

The window's starting state comes from variables declared per ADR-0009;
the person moves most of them in Settings afterwards.

| Variable | Sets |
| --- | --- |
| `BOXER_CHAT_ALLOW` | where **At most** starts: read, view, edit, run or outside (`run` by default) |
| `BOXER_CHAT_ROUNDS`, `BOXER_CHAT_ROUNDS_MIN`, `BOXER_CHAT_ROUNDS_MAX` | the rounds slider's start and range (24; 4 to 96 by default) |
| `BOXER_CHAT_PIXELS`, `BOXER_CHAT_PIXELS_LOCAL` | the Pixels level and its local-only switch |
| `BOXER_CHAT_PIXELS_MAX`, `BOXER_CHAT_PIXELS_LOCAL_REQUIRED` | the host's cap on Pixels, and whether local-only is required: the person cannot set a chat above them |
| `BOXER_CHAT_IMAGE_BYTES`, `BOXER_CHAT_IMAGE_COUNT`, `BOXER_CHAT_IMAGE_PIXELS` | the artefact's screenshot budget per conversation (ADR-0284 §SD4) |
| `BOXER_CHAT_OPERATION_TOOLS` | **A tool per operation** |
| `BOXER_CHAT_APPS`, `BOXER_CHAT_QUESTIONS`, `BOXER_CHAT_ARTEFACT`, `BOXER_CHAT_ADVANCED`, `BOXER_CHAT_DRAFT` | the options and the composer's text of a new window, for scenes |
| `BOXER_CHAT_PROFILE`, `BOXER_CHAT_CALL_WAIT`, `BOXER_CHAT_PENDING_MAX`, `BOXER_CHAT_WAKES_MAX` | the harness profile and the asynchronous call's waits (ADR-0358) |

The host's own variables shape what the chat can ask for:
`BOXER_AGENT_COORDINATORS` names the apps the host answers as
coordinators; `BOXER_LLM_RETAIN` decides whether Keep can keep;
`BOXER_LLM_TRUSTED_HOSTS` names the endpoints trusted with sealed data.

## Why the host, not the window, enforces

The ceiling is sent with every request for access, and the host's
dispatcher refuses anything above it — before you are asked, and on every
later call, launch and destination — whatever the grant says (ADR-0280).
The grant is the authority (ADR-0269 §SD6): the chat presents its handle
and the dispatcher checks, stamps and records each call. So the settings in
this tab are declarations the chat hands to the host; the chat adds no
allowlist, approval prompt or budget of its own, and a line of enforcement
in the window would be one nobody else could check.

ADR-0318 keeps the chat the one app-side holder of the model grant as a
review convention, not an enforced one. Its update of 2026-11-04 narrows
what a second holder could do unnoticed: since ADR-0357 the model service
refuses a call that does not name the journal entry written before it, so
a second harness would still journal; the approvals, ceilings and labels
remain this harness's to apply.

Two things are the chat's own to apply, and the tab says so where they
show. The artefact's policy — whether the model may write it, and whether
a write waits as a proposal — follows **At most** and **Changes**, and the
chat applies it, because the host never sees an artefact call. **Pixels**
is applied by the chat as well, within the host's cap: the host sees only
the confined label on what reaches the model. The screenshot budget's
ceilings (ADR-0284 §SD4) are environment variables; a tool that would pass
one is refused with the use and the ceiling, and changes nothing.

## Conversation and model

The **Conversation** section repeats the id and what the conversation was
started with — Keep, Questions, Artefact — and reminds that New
conversation takes the settings above. The **Model** section names the
model and its endpoint, says where it runs — on this machine, off it, or
off it and trusted with sealed data — and its context size and the source
the size was learned from.
