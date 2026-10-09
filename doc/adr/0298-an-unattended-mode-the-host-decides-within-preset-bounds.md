---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0298: An unattended mode — the host decides within preset bounds

## Context

An agent task stops whenever it needs the person: a grant request, a call
outside the grant (a widening), a proposal in suggest mode
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD5–§SD6), a spent budget, a passed deadline, a consequential command. A
task meant to run for hours with nobody watching stalls at the first of
these until the request expires (`BOXER_AGENT_REQUEST_TIMEOUT`).

The bounds that make the person's answers safe are already set before a
run: the coordinators allowed to ask (`BOXER_AGENT_COORDINATORS`), the
ceiling a coordinator relays ([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md)),
the call budget range (`BOXER_AGENT_CALLS_MIN`/`_MAX`), the deadline
(`BOXER_AGENT_DEADLINE`), the pace (`BOXER_AGENT_PACE`) and the launch limit
([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md)). Test
grants (`BOXER_AGENT_TEST_GRANTS`) already let the host stand in for the
person, on the headless host only and without suggest mode.

A mode that applies those bounds instead of asking is a first step towards
long-running tasks.

## Design space (QOC)

**Q1 — How does a binary get the mode?**

- *O1: an environment variable alone.* Any deployment can turn it on,
  including one whose operator did not mean to ship it.
- *O2: a build tag alone.* A tagged binary is always unattended; going back
  takes another build.
- *O3: a build tag that makes it possible, a variable that turns it on.*
  **Chosen:** only a deliberate build can carry the mode, and that build
  still runs attended unless a run asks otherwise.

**Q2 — Which of the person's decisions does the host take?**

- Grant requests and widenings. **Chosen:** each is bounded by the ceiling
  before anyone decides, so the person's answer adds no bound the host lacks.
- Suggest-mode proposals. **Chosen**, where the grant made them: under a
  ceiling that allows act, suggest is the grant's choice, not a bound.
- More time past the deadline. *Not chosen:* the deadline is a bound the
  mode runs inside.
- Confirming a consequential command (publishing, exporting). *Not chosen:*
  it acts outside the app.

**Q3 — On which hosts?**

- *O1: headless only*, as test grants. Long-running work is often watched
  on the desktop.
- *O2: both*, with the mode shown in the status bar. **Chosen:** the build
  tag already gates the mode, and the bar says it is on.

**Q4 — Name.** "YOLO" and "guardless" say the guards are gone; they are
not, only the person's answers are. **"Unattended"** names what changes:
nobody is there to answer.

## Decision

### SD1 — Compiled in, then turned on

The build tag `boxer_unattended` sets the constant `agent.Unattended`; it is
not in [`./tags`](../../tags) and goes on the command line of the build that
wants it:

```sh
go build -tags="$(cat ./tags),boxer_unattended" ...
```

In a binary built with it, `BOXER_AGENT_UNATTENDED=true` turns the mode on
for a run. Without the tag the constant is false, the mode's paths compile
out, and the variable is refused with a warning in the log.

### SD2 — What the host decides

With the mode on, the dispatcher decides in the person's place:

- **A grant request** from a registered coordinator, and a **widening** —
  asked for, or made by a call outside the grant — is approved as asked.
- **A suggest-mode proposal** is accepted on arrival and routed as any
  call, paced as one — when the ceiling allows act, so that the proposal is
  the grant's and not the ceiling's. Under such a ceiling suggest lands as
  act; a suggest ceiling keeps its proposals for the person.
- **A call a widening let further**, which then needs another widening, is
  held for that one too, decided by the same rules.

It does so only for a request **under a ceiling**: a coordinator that sends
none leaves the host nothing to bound what it approves, so its requests
reach the person as before.

The grant events record `DecidedBy: "host"` with the reason `unattended`
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)).

### SD3 — What stays the person's

- **More calls**, when the budget is spent, and **more time**, when the
  deadline has passed. A widening for a late task is the person's too,
  since approving it moves the deadline on.
- **A consequential command**, unless a grant destination is standing
  consent for it (ADR-0288 §SD4).
- **Everything the ceiling refuses**: the ceiling is checked before anyone
  decides, so the mode cannot lift it.

These wait in the dialog and expire as before. A task left alone runs until
its budget or its deadline, whichever comes first, and then waits.

### SD4 — Visible to the person and told to the model

The host's bottom status bar carries a segment for the mode: absent in a
binary built without it, `unattended:off` in one built with it, and a
warning-toned `UNATTENDED — agents act without asking` when it is on. The
words carry the state; the tone is not the only channel (ADR-0031 §SD5).

With the mode on, the badge in each window a task works in adds
`unattended`. A request the host leaves to the person says in the dialog
why it was left — no ceiling, a spent budget, a passed deadline — and a
confirmation says the host still asks for every change outside the app.

A grant the host approved says so in its reply. The chat passes that on to
the model with the grant: what fits the settings is approved at once, what
is left to the person may wait for hours, so the model works on rather than
waiting for them, and names in its answer what is left for them.

### SD5 — Deferred, recorded

- **A host ceiling for the mode**: a ceiling the host imposes on every task
  while the mode is on, so that the bound does not rest on the coordinator.
- **An unattended budget and lifetime**: more calls and more time granted
  automatically up to a host-wide cap, which is what a task longer than one
  deadline needs.

## Consequences

### Positive

- A task bounded by its ceiling, budget and deadline runs without stopping
  at the person's dialog.
- The mode cannot reach a binary that was not built for it.

### Negative

- The ceiling a coordinator relays becomes the only bound on what a task
  holds; the coordinator is trusted to relay it, as in ADR-0280 §SD2.
- Suggest mode, granted under a ceiling that allows act, loses its meaning
  under the mode.
- A task cannot outlive one budget or one deadline without the person.
- The mode's tests run only with the tag; the default lane checks that the
  mode stays off without it.

### Neutral

- Test grants are unchanged and take precedence for a new grant on the
  headless host.
