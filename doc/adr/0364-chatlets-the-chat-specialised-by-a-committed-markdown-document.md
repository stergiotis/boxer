---
type: adr
status: proposed
date: 2026-11-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0364: Chatlets — the chat specialised by a committed markdown document

## Context

An SQL applet ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) is one
committed markdown document minted into an app that embeds play with its
authoring surface removed. A chatlet is the same idea over the chat
([ADR-0265](./0265-chat-app-over-retained-model-calls.md)): one document, one
launcher entry, a system prompt and the settings a scenario needs, so a person
opens it and starts. The witness is a stated plan (maintainer, 2026-11-08) and
one worked example, audit-trail analytics.

Three things differ from the applet:

- **Identity.** The chat keyed its runs, roster and resume on one compiled
  app id, and the dispatcher admits a coordinator by exact name
  (`BOXER_AGENT_COORDINATORS`). A minted id is what gives a chatlet its own
  roster, rations and trail rows, and it is refused a grant until registered.
- **Authority.** ADR-0132 §SD5 derives a security class from the buffer. A
  prompt admits no such analysis, and
  [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
  §SD3 keeps authority out of anything a run selects.
- **Format.** A system prompt is prose and often contains code blocks, which
  the applet's line-based fence scanner cannot hold.

## Decision

### SD1 — One committed document is one chatlet; committed books only

A document's frontmatter is its manifest and its settings, and its body is the
system prompt, in the `promptbook` shape. Minting follows ADR-0132 §SD1–§SD2.
A document no review stood behind is not admitted: a stored chatlet would be
an unreviewed system prompt at the person's integrity level, and there is no
classifier to gate it as `AdmitUnreviewed` gates an applet. The reviewed
corpus is the gate.

### SD2 — A preset narrows and grants nothing

`chat.Preset` carries the app's manifest, the section and the settings. Its
permissions are the window's start and the most the person can move it to; the
person can lower them and Settings offers nothing above them. The ceiling is
still the one the dispatcher enforces. A preset cannot pre-approve anything:
the model asks through `request_access` and the person decides. Capabilities,
operations and help are the chat's (`chat.BaseManifest`), so a chatlet does not
widen what review of the chat saw (ADR-0132 §SD8).

### SD3 — The prompt is one more journal section

The scenario's text is a `section` entry named `chatlet`, ordered first so the
harness's own sections, the rule on untrusted content among them, are read
after it. It is not a second harness profile: the profile's constants are
unchanged, and the section is recorded as sections are
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)).
Its digest is folded into the run's fingerprint, so editing a chatlet leaves
its earlier conversations read-only on resume, as a changed prompt book does.
A plain chat's fingerprint is unchanged. A section too large for compaction to
leave room under the profile's smallest context is refused at construction.

### SD4 — Coordinators may be selected by a predicate

`BOXER_AGENT_COORDINATORS_WHERE` is a ClickHouse boolean expression over
`keelson.apps`, in the shape of
[ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) and
[ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md)
§SD3. It is the operator's, evaluated once at boot on clickhouse-local after
the launch limit, as `launchable AND (<expr>)`. Its result joins the names
`BOXER_AGENT_COORDINATORS` lists; unset selects none. Set and not evaluable,
the host does not boot. An app registered after boot is not selected, which
committed books (SD1) do not run into. The resolved set is held by the
registry and shown as `keelson.apps.coordinator`.

The name takes `_WHERE` from its two precedents.

### SD5 — Worked example: audit-trail analytics

The trail is read through `boxer.dm_trail_*` and `boxer.agg_trail_*`, in a play
window the model opens. The settings follow from that path:

| Setting | Value | Why |
| --- | --- | --- |
| apps | on | the SQL runs in a window |
| questions | on | a trail question is usually missing its time range |
| artefact | on | the deliverable is a report with its evidence |
| allow | run | `set_sql` is a document effect and `run` a run effect; read or view cannot execute a query. Publishing and exports stay out |
| changes | apply | waiting on each `set_sql` defeats the scenario |
| reach | data | `clickhouse:<host>` needs it; network does not |
| launch | on | it opens its own play window |
| desktop | off | nothing here arranges windows |
| thinking | hidden | the person wants the report; each step's reasoning stays under its details |
| view | simple | someone asking what happened does not need the conversation's id or the ladder |
| watch | true | a turn can run for minutes (one drive took 20 rounds) |
| rounds | 48 | a margin over the default 24: one drive used 20 (below) |

An agent's run in play must classify as a read
([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)), so `allow:
run` cannot change the trail. Analysis adds its own rows to the trail; the
prompt has to say to exclude the conversation's own.

### SD6 — The window shows what it is

The empty transcript shows the document's `greeting`, up to six `starters`, each
a `{label, text}`, and what the model can and cannot do in words. Those words are
derived from the window's settings, so they cannot disagree with what the host
enforces; they claim nothing beyond a level, and an app's narrower rule stays the
document's to state. A starter fills the composer and the person sends it, as
the transforms picker does
([ADR-0318](./0318-model-inference-is-the-harnesss-apps-expose-operations.md)
§SD4); nothing is sent on a click. A **Definition** tab, only in a chatlet's
window and in the place of Analytics, since the dock's column has room for four
titles, renders the whole document with its frontmatter, as the applet's drawer
does (ADR-0132, Update of 2026-08-01): a person can read what the model is told
and the limits it runs under before the first send. The greeting and starters
are not the model's to read, so they are outside the run's fingerprint.

`thinking: hidden` starts the window without the reasoning drawn as the model
writes it; absent, it is shown, as in the plain chat. It is a starting value and
not a limit in the sense of SD2: it changes only what the window draws, so Settings
can turn it back on, the finished step keeps the text, and the fingerprint does
not see it. The key takes words because `thinking: false` reads as a model that
does not reason, which no key here can arrange.

The other keys are for someone new to the window or tired:

- `view: simple` folds the conversation's id, the ladder, the turn's state, the
  model line, the context, Open in mdedit, Watch and Urgent under a **Details**
  toggle in the bar's first row. A collapsing header was not taken: it needs a row of
  its own in the bar it exists to shorten. What holds a task, its Stop, what it
  read, the warning that a conversation is not kept and the context warning stay
  outside the fold. Settings still has the ladder.
- `followups`, in the shape of starters, are offered under the latest answer. A
  click fills the composer, as a starter's does.
- `watch: true` starts each new conversation watched
  ([ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md) §SD4). The
  person can stop watching.
- `purpose` is one sentence for the host's access dialog, shown after the
  host-derived facts and before the model's plan, in the app's name. It asks for
  nothing and approves nothing (SD2).

Without being asked for, a preset's running turn counts steps ("step 3") rather
than "round 3 of 48", which reads as progress towards a limit that is only a
limit, and its artefact tab comes forward the first time the model writes or
proposes to write it. A plain chat does neither.

## Alternatives

- **One manifest and a launch kind naming a slug.** Rejected: no launcher
  entry and no per-chatlet attribution, ADR-0132's reason for rejecting a
  gallery. A later moderator also needs the run owner to name the chatlet.
- **A family entry (`…/chatlet/*`) in `BOXER_AGENT_COORDINATORS`.** Rejected
  for the predicate, which needs no second matching syntax and says more.
- **A separate start and cap per setting.** Not taken: no scenario wants them
  to differ.
- **Evaluating the predicate in the dispatcher on each request.** Rejected for
  ADR-0272's reason: a SQL round trip on a path that waits.

## Consequences

- Editing a chatlet's prompt strands its open conversations.
- A chatlet's conversations are not in the plain chat's roster, nor the other
  way round.
- A coordinator selected by predicate is logged but is not an audit event.

## Known friction, recorded 2026-11-08

- **Two dialogs on a first session.** A committed document cannot name the
  deployment's ClickHouse host, and the model learned `clickhouse:<host>` from
  play's `get_state` after opening the window, so it asked twice. A preset's
  ceiling note now names `CLICKHOUSE_URL`'s host when the window can reach data,
  and the audit-trail prompt asks for one request. That makes one dialog likely
  and does not guarantee it: a window retargeted to another endpoint names another
  destination, and whether a model follows it has not been driven.
- **The moderator's `rounds` signal.** With `BOXER_MODERATOR` on, a turn that
  reaches round 16 raises one signal
  ([ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md)
  §SD3), and rounds start at 48 here, so several long turns inside ten minutes
  climb its ladder. Thresholds are constants of the engine (its §SD6).
- **Tool surface.** With apps, questions and an artefact on, the head is about
  2,500 tokens and the tool schemas about 5,700. The desktop tools are about
  540 of that; hiding them while `desktop` is off would change the
  preservation golden and was not taken.

## One drive, recorded 2026-11-09

One conversation from the "last 24 hours" starter, `deepseek-flash` over the
host's `boxer.dm_trail_*` and `agg_trail_*` views, a test grant in the person's
place, the moderator on. It took 20 rounds, 21 model calls and 48 agent
operations, about 784k input tokens summed over the calls. The figures of the
report recomputed outside the chat matched, its 11 `cite` assertions held, and
it excluded its own conversation as the prompt asks. It asked for access twice
(the friction above). It used play's `query`, one call for what `set_sql`, `run`
and `sample_rows` do, and the prompt now names it. One drive of one model says
nothing about how the settings do in general.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the preset's ceiling admits `run` and nothing
  past it; the person cannot exceed a limit; the section is journaled first
  under the chatlet's owner; a changed prompt changes the fingerprint and a
  plain chat's does not; an unreadable setting or an oversized prompt is
  refused. The preservation golden is unchanged. The predicate is evaluated on
  clickhouse-local, and refused without it.
- **Gap.** Whether these settings do good analytics is a trial, not a test.

## Milestones

1. ✓ The chat's seam and the coordinators predicate (SD2–SD4).
2. ✓ `apps/chatlet`: parser, book, minter, corpus test, a kind of its own for
   `kind = 'chatlet'`, the audit-trail document and a scripted scene (SD1, SD5).
3. ✓ The window: greeting and starters in the empty state, a Definition tab
   (SD6).
4. ✓ For someone new or tired: the plain statement of abilities, `view`,
   `followups`, `watch`, `purpose`, `thinking` (SD6).

## Deferred

- **A moderator per chatlet.** A frontmatter key naming a sibling document is
  additive. It needs a decision on content first: ADR-0302's events carry no
  model words by design, and journal bodies are vaulted per entry
  ([ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md)).
  Trigger: a chatlet whose sessions someone wants watched.
- **Per-chatlet app allowlist.** `Ceiling` has no such field. Trigger: a
  scenario the ceiling and the grant dialog do not bound enough.
- **Runtime-authored chatlets.** Trigger: a gate that can stand in for review.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the blueprint.
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md), [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md) — boot-time predicates over `keelson.apps`.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md), [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — coordinators, grants, the ceiling.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) — starting values and limits; authority outside the profile.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — sections and the fingerprint.
- [ADR-0302](./0302-a-built-in-moderator-loop-signals-and-an-escalation-ladder.md) — the moderator.
