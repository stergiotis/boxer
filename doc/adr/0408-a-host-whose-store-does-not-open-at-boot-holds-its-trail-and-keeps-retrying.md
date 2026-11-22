---
type: adr
status: proposed
date: 2026-11-23
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration. SD1–SD8 are built (2026-11-23) as the sections below describe. They are tested against an HTTP stub that stalls, and against one ClickHouse 26.9 server on loopback behind a proxy that holds every statement from the state table's DDL on; no host has yet been seen through a real disk stall with this change.

# ADR-0408: A host whose store does not open at boot holds its trail and keeps retrying — the trail recovers, app state stays where it booted

## Context

At boot a host opens the facts store (a ping under `DefaultFactsPingTimeout`,
then the table and the read surface), and then its state table,
`boxer.persiststate`, under a 15-second bound (`persistOpenTimeout`). When the
state table does not open, the persist backend falls back to memory and the
executor it was opened on is dropped. The audit trail
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)) was
built over that executor, so it was built without one: not durable for the
whole process. With `BOXER_TRAIL_REQUIRED` set, every model call and every
egress fetch was then refused until someone restarted the host (§SD3 of
ADR-0277). One warning line at boot was the only sign; nothing retried.

The case that prompted this was measured on a workstation whose disk was
busy after a large write (an ext4 writeback stall): the state table's
`CREATE TABLE IF NOT EXISTS`, normally 13 ms, took 29.5 s. The host logged
`persist: … ensure table boxer.persiststate … context deadline exceeded`,
fell back to memory, and refused every model call for the rest of its life,
although the server answered normally seconds later. An unattended host
that boots in such a window does nothing until a person notices.

Two observations shape the decision.

- **The trail's table is not the state table.** The trail writes the facts
  table, which the facts store had already set up. The trail lost its
  executor only because it shared one with the state table and that
  executor was dropped with the backend.
- **What was opened at boot was captured at boot.** The pieces that depend
  on the store differ in whether they can change backend while the process
  runs:

| Piece | Holds at boot | Can it switch at run time? |
| --- | --- | --- |
| Trail recorder (`trail.Recorder`) | one pointer, shared by the model and egress services, the dispatcher, the audit sink, the viewer, the evidence checks | **Yes.** Every consumer asks `Durable`/`WriteAhead`/`Admit` per call. A trail that had no rows on the server has no seal to chain from, so its first durable batch starts the run's seal stream as at any boot |
| Rows written while not durable | the settings row, bus audit events, refused model calls | **Carried over** if the recorder keeps them (SD2); lost if it records nothing, as it did |
| Usage ledger (ADR-0300) | rebuilt from the trail at construction, skipped when the trail is not durable | **Yes, once**, if rebuilt before the trail admits a call (SD5) |
| App state: persist values, workingsets, column widths; the attention person table; `runtime.appstate` | read from memory by every window that mounted, often written back whole | **No.** A window that read defaults from memory and saves after a switch overwrites the state an earlier run stored with values the person never chose; carrying memory over has the same effect for every key the window rewrote. Declared: this run's app state is kept in memory and ends with it; the stored state is untouched and is what the next run reads |
| Run journal (`agentrun`), anchored heads | a store chosen at boot; runs append chain digests to it | **No.** A run begun in memory would continue its chain in another store. Declared: this run's conversations do not outlive it |
| Watchbill worker and lane | not started without the executor | **No** in this decision: their wiring into the agent service and the notices happens at boot (Deferred) |
| Facts store | memory when the server did not answer the ping or the setup | **No.** It is held by the heartbeat, the task supervisor and the watchbill liveness, and the trail mints its row ids from its stream ([ADR-0405](./0405-every-id-in-the-facts-table-is-minted-and-a-natural-key-resolves-to-one-id-on-the-server.md)); a trail landing beside a run whose start row and ids are in memory would break both |

## Decision

When the facts store reached ClickHouse and the state table did not open,
the host builds its trail over the server **held**: it keeps its rows and
sends nothing, it is not durable, and under `BOXER_TRAIL_REQUIRED` it refuses
work that leaves the machine with a reason that says the store is still
being opened. A background retry, with a capped and jittered backoff,
releases it once the server takes the rows. Nothing else changes backend
while the host runs.

### Subsidiary design decisions

- **SD1 — When a trail is held.** The facts store is on ClickHouse, the
  persist service is selected, the host is not a scene's, and the state
  table did not open within the bound: the trail is built over the executor
  that reached the server, held (`trail.WithHeld`). A host whose state table
  opened is built as before. The trail's durability no longer depends on the
  state table opening, only on the server taking the trail's own rows.
- **SD2 — Held is a state of the shared writer.** `auditrec.WithHeld`
  ([ADR-0330](./0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md)):
  the recorder buffers and seals as it does for a server that does not take
  rows, under the same backlog cap and gap row, sends nothing, reports not
  durable, and fails a write-ahead with `ErrHeld`. `Release` lands what it
  holds oldest first, each batch sealed, and only then reports durable; on
  failure it stays held and may be called again. The host writes its own
  rows while held — the settings row, the services selection, a window's
  pause — so they land with the rest. The services that skip writing while
  the trail is not durable keep doing so (What it does not cover).
- **SD3 — The retry.** Started at the end of `Boot`, once the host is up,
  on a goroutine of its own. The first wait is 2 s; each failure doubles it
  up to 1 minute; each wait is drawn uniformly between half and all of its
  value. Each attempt is bounded by `persistOpenTimeout` and is: the usage
  ledger rebuilt from the trail's store (once), then `Release`. It stops at
  the first success, and at `Close` — the first of the host's own cleanups
  to run — by cancelling the attempt in flight and waiting for the
  goroutine. A server
  that stays down sees one attempt a minute from a host.
- **SD4 — What the trail shows.** The run's first row is its settings row,
  recorded at boot and landed at the release, as at any boot. The rows of
  the held span follow in order, sealed. Once released, the host writes
  `trail.held` (outcome failed): `from` and `to`, the attempts it took,
  `refused` — the work the trail refused meanwhile (`Counts.Refused`: model
  calls' write-aheads and every `Admit`, which egress fetches, ad-hoc
  datasets, session data and the agent's disclosures ask) — and `cause`,
  why the boot's open failed. Rows the cap dropped give the gap row as
  before. With `BOXER_TRAIL_REQUIRED`, what the held span shows having left
  the machine is nothing: a refused model call's message rows and its
  refused call row land and say so. The trail viewer's trail-row rule lists
  `trail.held`.
- **SD5 — The ledger before the release.** The model service's ledger is
  rebuilt from the trail's store (`llm.Service.RebuildLedger`) before the
  trail is released, so no call is admitted against a ledger that has not
  seen earlier runs, and none of this run's calls is on the server yet, so
  none is charged twice. It is rebuilt once; a release that fails after it
  does not rebuild again.
- **SD6 — A recovery is visible.** The refusal a model call or fetch
  carries reads, for example, *the audit trail is not durable on this host
  and BOXER_TRAIL_REQUIRED is set: the host's ClickHouse store has not
  opened since 14:02:26 UTC (at boot: … context deadline exceeded); the host
  is retrying it in the background (3 attempts so far, the last: …), the
  next attempt at 14:03:31 UTC. No restart is needed: try again once it
  opens*. A caller of the model service receives it as the refusal's
  reason. `trail.Recorder.Admit`'s refusal now ends with the same reason. The status bar reads the reason
  each frame (`runtimestatus.Snapshot.TrailWhy`), so its warning goes when
  the trail is durable; `keelson('evidence_checks')` reads it per query. The
  log says so at boot, at each failed attempt and at the recovery. At the
  recovery the host registers the trail viewer's source again with the
  executor, a slot a host fills, as
  [ADR-0396](./0396-the-app-contract-a-host-promises.md) keeps it.
- **SD7 — The bound stays 15 s, and nothing becomes a setting.** App state
  does not move to a store that opens later (SD8), so the boot's bound
  decides whether a slow but healthy server's run keeps its state; a
  shorter first attempt would shorten the time to the first frame and turn
  more slow boots into runs whose state is lost. Each retry takes the same
  bound. The waits (2 s, doubling to 1 minute, jittered) bound only how soon
  a recovered server is noticed; no operator has been seen to need them
  changed, and they are constants with that reason beside them.
- **SD8 — What does not move, and what says a restart is needed.** App
  state, the run journal, the watchbill worker and everything that reads
  `Runtime.PersistExec` stay as they booted. The recovery's log line says
  so, and the status bar keeps `persist:mem`. A host whose facts store did
  not reach the server at boot is not retried: its trail says *ClickHouse
  did not answer when this host started, so this run keeps its facts and
  its trail in memory; the host does not retry them, and only a restart
  reaches the server once it answers* (`trail.WithNoBackendReason`).

### What it does not cover

- **A facts store that fell back to memory** (the server not answering the
  ping, or the setup failing). Not retried; see the table and SD8. That is
  the more common case for a host started before its server.
- **A server that goes away after boot.** The trail's backlog, write-ahead
  refusal and gap row (ADR-0330 §SD5) apply as before.
- **The boot's length on a stalling server.** The schema check
  ([ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md))
  runs after the failed open under the same bound and waits on the same
  stall; behind a proxy holding every statement for 29.5 s, `Boot` returned
  after 29.6 s and the trail was durable 1.5 s later. A stall during the
  facts store's setup is bounded by that setup's own 2 minutes.
- **The services that skip writing while the trail is not durable** —
  the agent's grant events, attention, ad-hoc datasets, session data,
  lading publishes, the moderator. Their rows of the held span are not
  recorded and not counted. With `BOXER_TRAIL_REQUIRED` the work this
  setting exists to refuse — model calls and egress fetches — was refused.
- **A host without the persist service, or a scene's host.** Its trail has
  no executor, as before.

### Deferred

- **App state moving to the store at the recovery**, with a rule for what a
  window read from memory and wrote back. Trigger: a deployment where the
  lost state of a held run matters more than a restart.
- **Starting the watchbill worker and lane at the recovery.** It needs the
  agent service's enqueue wiring and the notices re-done after boot.
- **Retrying a facts store that fell back to memory**, which needs the
  facts store, its id stream and the run's start row to move together.
- **A schema check that does not wait out a stall** a failed open has just
  shown.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `auditrec` | added: `WithHeld`, `ErrHeld`, `Recorder.Held`, `Recorder.Release`; `Durable` is false while held; `Flush` returns `ErrHeld` while held | — |
| Exported Go API: `audit`, `trail` | added: `ActionAuditHeld` | the trail viewer's trail-row rule |
| Exported Go API: `trail` | added: `WithHeld`, `WithNoBackendReason`, `Recorder.Held`, `HeldSince`, `HeldWhy`, `SetHeldWhy`, `Release`, `Counts.Refused` | — |
| Refusal text | `Recorder.Admit`'s refusal ends with the reason the trail is not durable | a reader matching the old text exactly |
| Exported Go API: `llm` | added: `Service.RebuildLedger` | the host's retry |
| Exported Go API: `runtimestatus.Snapshot` | added: `TrailWhy` | the host's status snapshot |
| Trail rows | new audit event `trail.held` | trail readers; the viewer's rule |
| Host boot | a held trail and a background retry where the state table does not open | — |
| Settings | none | — |

## Alternatives

- **(a) Retry longer at boot, with backoff, before falling back.** Shortens
  the window and keeps the lifetime failure for any stall longer than the
  retry; a host that will not show its first frame for minutes is a failure
  of its own. Not taken.
- **(b) Start degraded and switch every durable piece when the store
  opens.** Sound for the trail and the ledger only (Context). Switching app
  state either loses what windows wrote while held or overwrites stored
  state with what windows read from memory; switching the journal splits a
  run's chain. Taken for the pieces that can switch, which is SD1–SD5.
- **(c) With the trail required, refuse to come up (or exit for a
  supervisor to restart).** Turns a seconds-long stall into a restart loop
  that depends on a supervisor every deployment does not have, and a
  desktop host that will not open is worse for its person than one that
  refuses model calls for a minute and says why. Not taken; an operator who
  wants it can watch the boot warning or `evidence_checks`.
- **(d) Retry in the background, and until the store opens refuse with a
  reason that says so.** This is the decision, with (b) limited to the
  trail.
- **Hand the trail the executor unheld at boot.** The trail would be
  durable from boot, its rows held in its backlog until the server takes
  them, as for a server that goes away while the host runs. But `Admit`
  would let egress fetches leave while nothing the trail holds has landed,
  and the boot has just seen the server fail to answer; the ledger would be
  rebuilt by nobody. Not taken.
- **Retry inside the recorder's own flusher.** The flusher retries every
  second without backoff, and the host, not the recorder, knows the ledger
  must be rebuilt first and what to tell the person. Not taken.
- **Probe with the state table's DDL.** It is what failed, but a state
  table that cannot open for a reason that will not pass (a schema the
  build cannot read, a user without `CREATE`) would keep the trail held for
  the process's life; the trail's own insert is the evidence it needs.
  Not taken.

## Consequences

### Positive

- A host booted into a stall recovers its trail without a restart, within
  about a minute of the server answering, and work that leaves the machine
  is admitted again.
- Nothing the trail-required setting exists to prevent happens in between,
  and the trail shows the span, its cause and what was refused.
- The rows of the held span, the settings row first, land in order and
  sealed instead of being discarded.
- A trail no longer becomes non-durable because the state table failed for
  a reason of its own.

### Negative

- A held run keeps its app state, its journal and its watchbill as they
  booted: in memory or off until a restart, which only a log line and
  `persist:mem` say.
- The rows the services skip while the trail is not durable are neither
  recorded nor counted.
- A held trail holds up to the backlog cap in memory.
- The retry is one more goroutine, and one more statement a minute against
  a server that stays down.

### Neutral

- A server that answers at once boots as before: no hold, no retry, no
  held row.
- The boot's length on a stalling server is unchanged.

## Migration — Tier 1

No stored shape changes. A depending module that matched
`Recorder.Admit`'s refusal text exactly sees the reason appended; one that
relied on a host whose state table did not open having a non-durable trail
sees the trail recover. The behaviour-changes note lists both
([2026-11-behaviour-changes](../migration/2026-11-behaviour-changes.md)).

## Verification plan — Tier 1

- **Lane.** Default `go test`: the held writer (`auditrec`: nothing sent
  while held, a write-ahead failing with `ErrHeld`, a failed release
  staying held, the cap and gap row, loss at Close); the held trail
  (`trail`: the refusal carrying the host's reason, `Refused` counted, the
  release landing the held rows); over clickhouse-local, a model call
  refused while held with the provider not asked, the ledger rebuilt once
  from an earlier run, and the next call admitted, durable and charged once
  (`llm`); a booted host against an HTTP stub that holds every statement
  from the state table's DDL on: held at boot, refusals that name the retry
  and say no restart is needed, the retry failing while the stub holds,
  recovery without a restart, the held row and the settings row landing,
  and the status bar's reason cleared; a stub that never answers: the
  attempts back off, the trail stays held, and `Close` stops the retry
  without a goroutine left (`hostboot`, under `-race`).
- **Integration lane.** Against a live server behind a proxy that holds
  every statement from the state table's DDL on: held, refused, recovered;
  on the server the run's first audit event is the settings row, the
  `trail.held` row names the refusal and the cause, and the run's seals
  verify with no row unnamed. Against the same server answering at once:
  the store opens, the trail is durable from boot, no retry runs and no
  held row is written.
- **What would fail.** A model call or fetch admitted while held; a held
  row lost at release; a recovery that needs a restart; a retry still
  running after `Close`; a call charged twice after the release.
- **Gap.** No real disk stall was reproduced; the stall is a proxy's. The
  ledger's rebuild-before-release is tested on clickhouse-local, not on a
  server.

## Decided with the owner, 2026-11-22

The owner decided the direction before this ADR was written: a host should
keep trying in the background and recover, and the decision should be
written up for review. How far the recovery goes — the trail and the
ledger, not app state, the journal or the facts store — is this ADR's and
is listed for review below.

## Choices for review

- Only the trail (and the ledger before it) recovers; app state, the
  journal and the watchbill stay as they booted, declared rather than
  switched (SD8).
- The trail is held, not durable, until its rows land: egress fetches are
  refused during the span, which an unheld trail would have admitted.
- The retry's success is the trail landing its rows; the state table is
  not retried at run time.
- The ledger is rebuilt once, before the first release attempt that gets
  that far; a long gap between rebuild and release leaves another run's
  calls in that gap uncharged.
- First wait 2 s, doubling to 1 minute, each drawn in [½, 1] of its value;
  constants, not settings.
- The boot's bound stays 15 s, and each retry takes the same bound.
- The `trail.held` row's outcome is `failed`, like the gap row's, so the
  viewer's outcome rule lists it as well as its trail-row rule.
- The held reason uses UTC clock times to the second and clips the boot's
  error to 160 runes; it carries the error text of the executor, which
  names the server's address.
- A facts store that fell back to memory is not retried, and its trail's
  reason says a restart is needed.
- `Recorder.Admit`'s refusal now ends with the reason; the llm service's
  retention reason reads the trail's reason instead of a fixed string.
- The services that skip writing while not durable are left as they are;
  their held-span rows are not counted in `trail.held`.
- The retry starts at the end of `Boot`, so the trail stays held for as
  long as the rest of the boot takes; a boot that fails after the trail is
  built starts no retry.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD3 — write-ahead and `BOXER_TRAIL_REQUIRED`.
- [ADR-0330](./0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md) — the shared writer, its backlog and gap row.
- [ADR-0296](./0296-audit-events-as-a-trail-component.md) — the trail's own rows and the settings row.
- [ADR-0327](./0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md) — seals and their anchors.
- [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) §D3a and its Update of 2026-08-15 — the state table and its in-memory fallback.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) §SD3 — the ledger rebuilt from the trail.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — the boot's schema check.
- [ADR-0396](./0396-the-app-contract-a-host-promises.md) — the viewer sources as a slot a host fills.
- [ADR-0405](./0405-every-id-in-the-facts-table-is-minted-and-a-natural-key-resolves-to-one-id-on-the-server.md) — the id stream the trail mints from.
- [Agentic layer](../explanation/agentic-layer.md) — the trail, and what `BOXER_TRAIL_REQUIRED` promises.
