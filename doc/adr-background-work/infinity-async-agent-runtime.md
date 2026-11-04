---
type: explanation
audience: whoever decides how long-running agent work runs on the chat coordinator (ADR-0265), app operations (ADR-0269) and the run journal (ADR-0308)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative. Compiled 2026-11-04 from about thirty pages of the project's
> public documentation at <https://infinity.hydro.run>. **Clean room:** no source
> code or repository was read, and the prose is paraphrase, so this page records
> concepts and stated behaviour, not an implementation. The pages state no
> version, licence or stability label. Nothing here is a decision; the decision
> this fed is [ADR-0358](../adr/0358-asynchronous-operations-a-pending-outcome-and-a-turn-a-completion-wakes.md)
> (proposed).

# Infinity: an agent runtime whose turns are not processes

Infinity is a Rust framework for running many concurrent agents, from a Hydro
project co-led by developers at UC Berkeley's Sky Computing Lab, AWS and others.
It has three parts: a runtime library, a wire protocol for tools (RAP, the
Reactive Agent Protocol) and a local coding agent built on the runtime. It is
read here for one axis: how an agent waits.

## Architecture

The central rule is that a unit of execution never blocks on anything external.

- **A slice.** Work for a thread runs in slices: load the history, prepare the
  input (drop duplicates, drop messages for closed threads, route events), run
  at most one completion, save, send at most one asynchronous tool call, exit.
  Synchronous tools run inside the slice and loop into another completion.
- **One inbox per thread.** User text, tool results, subscription events, child
  reports, authorisation challenges and timers are one message type on one
  queue, processed in order per thread and in parallel across threads.
- **State outside the process.** A conversation store holds history and a state
  store holds the processed message ids. There is no in-memory state machine,
  so a thread with nothing queued has no running code.
- **Interchangeable substrate.** On AWS a FIFO queue with per-thread message
  groups and a function invocation per active group; embedded, one channel and
  worker task per thread.

RAP makes the tool side match. A tool receives one HTTP POST carrying its
arguments and a callback URL, acknowledges at once, and later POSTs its result
to the callback. Further callbacks carry subscription events, requests for a
user choice, and authorisation. Cancelling a call and closing a thread are
best-effort notices. The protocol defines no timeouts.

## Characteristics

- **Crash behaviour.** Delivery is at-least-once, with duplicates detected by
  message id. Output streams into a per-turn buffer committed at the end of the
  turn, and when a tool call ends a turn the buffer is flushed before the call
  leaves, so the call is durable before its result can arrive.
- **Events and context.** By default a temporary child thread handles each
  subscription event and the parent hears only if it mattered, which keeps the
  parent's context clean at the cost of a completion per event. Events marked
  associative go straight into the thread, which is how partial output reaches
  the model.
- **Interruption.** User text interrupts a running completion. A pending
  non-passive call holds back events and child reports, but not user text.
- **Children.** A child thread inherits history up to the spawn point, or starts
  empty, and reports back as a synthetic tool result.
- **Observers.** An observer can attach at any time and receive a snapshot of
  committed history, the partial turn and pending choices.

## Benefits, by evidence

| Benefit | Evidence |
| --- | --- |
| Low memory per idle agent | Measured by the project: 80,000 idle agents at about 103 KB each on a Raspberry Pi 5, one thread, **in-memory stores**, so durable storage is not in the figure. The page's headline says 75k. |
| Idle agents cost nothing; scale-to-zero on serverless | Claim; the pages read give no cost figures |
| Same code on a laptop and in the cloud | Claim; the design supports it |
| "First" runtime of this kind; "zero latency" | Marketing |

## What the documentation leaves open

- What happens to a call that was sent and never finished. A tool server may
  lose acknowledged work on restart, and tools are not required to be idempotent.
- How an individual tool call is approved. The specification requires that users
  consent to and control tool operations, and defines no mechanism for it.
- Audit: logging invocations is a recommendation, with no provenance format.
- Exact-once side effects, replay semantics, and numeric limits on children or
  subscriptions.

## Against boxer, as of this page's date

Each row is a comparison on one axis, not an overall ranking.

| | Infinity | Boxer |
| --- | --- | --- |
| Waiting on a tool | The slice exits | The turn's job polls, bounded unless it waits on the person |
| Asynchronous tools | The default | Declared by ADR-0315 §SD6; no app declared one and nothing wrote `completed` |
| A crash with an act unsettled | Largely not stated | Settled by effect class, never blindly re-sent (ADR-0308 §SD4) |
| Authority before a tool runs | Consent required, no mechanism | Grant, ceiling, per-call confirmation (ADR-0269, ADR-0280) |
| Tamper evidence and labels | None described | Chain, vault, labels (ADR-0314, ADR-0320, ADR-0311) |
| Concurrent work in a conversation | Children, subscriptions, sleeps | One turn, sequential tools; delegation only reserved |
| Lifetime | Independent of any client | Bound to the window (ADR-0322); window-less runs out of scope (ADR-0308 §SD11) |

## What this suggests for boxer (analysis, not sourced)

**What transfers** is a shape: a turn that ends rather than waits, one inbox that
makes a completion the same kind of input as anything else, and an event handled
apart from the thread that subscribed to it. ADR-0355's long turns are the
problem these bear on.

**What does not transfer** is the headline. A single-person cell (ADR-0307) has
no use for tens of thousands of idle agents or for scale-to-zero, so the measured
benefit says little about boxer's own case.

**The two designs meet at one rule.** Infinity flushes before a call leaves;
ADR-0308 §SD2 writes the journal before an act leaves. A runtime that reloads
from a record on every slice needs that record to be complete, which is why
requiring it (ADR-0357, proposed) comes before asynchronous work.

**The hard conflict is authority, not scheduling.** A grant dies with its window
instance. A turn that outlives its window needs a new answer about who is
authorised and on whose behalf, which is the reason ADR-0358 stays inside the
window and leaves children, subscriptions and window-less runs to a later record.

## Pages read

All under `https://infinity.hydro.run`: the home page; `/docs/infinity-runtime/`
`architecture`, `agent-systems/overview`, `agent-systems/observers`,
`agent-systems/step-mode`, `agent-systems/customizing-the-engine`,
`agent-systems/dynamic-configuration`, `built-in/threading`,
`built-in/built-in-tools`, `low-level/overview`, `serverless/architecture`,
`serverless/rap-mcp-servers`; `/docs/rap/` `what-is-rap`, `about/architecture`,
`about/rap-servers`, `about/subscription-events`, `about/agent-runtime`,
`about/mcp-compatibility`, `using-rap/building-a-rap-tool`,
`using-rap/building-a-runtime`, and `spec/` `overview`, `basic/lifecycle`,
`basic/transport`, `basic/tool-invocation`, `basic/tool-result`,
`basic/toolsets`, `basic/thread-closure`, `basic/tool-cancellation`,
`basic/migration`, `server/subscription-events`, `server/oauth`,
`server/user-choice`, `server/view-updates`; `/docs/infinity-code/` `overview`,
`background-agents`.
