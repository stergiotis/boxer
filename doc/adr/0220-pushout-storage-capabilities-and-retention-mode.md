---
type: adr
status: proposed
date: 2026-09-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0220: pushout storage capabilities, a purge ledger, and an explicit retention mode

## Context

The pushout engine's persistence seam, `repo.StorageI`
([ADR-0079](./0079-pushout-production-storage-codec-exchange.md)), is one
interface over four concerns with different optionality: envelopes and
the applied log, which every deployment needs; the retention ledger,
which matters only where `Sweep` runs; and the snapshot, which is an
accelerator that also happens to be the only durable carrier of purge
markers (ADR-0079 Q5). The engine cannot tell which variant it is
running in, so mismatches fail late and silently:

- A store whose `SaveSnapshot` discards and whose `LoadSnapshot` reports
  none passes `Open`, replays correctly, and then lets `Sweep`
  acknowledge a purge that the next restart undoes. The verb's
  documented guarantee — purge markers durable before return — is a
  promise the engine makes on the store's behalf and cannot check.
  Measured in-session: after a crash-reopen over such a store the
  purged node's content was back.
- The one optional extension so far, `BatchAppenderI`, is discovered by
  type assertion. A decorator that embeds `StorageI` hides it by
  accident — the fault-injecting test store does exactly this — so a
  store's capabilities depend on how it is wrapped, not on what it is.
- The conformance suite (`repo/storagetest`) requires a snapshot round
  trip, so an honest store that cannot or will not persist snapshots
  is non-conformant even where nothing depends on them.
- Whether purge durability is a compliance property or a consistency
  property depends on the deployment. Under the vault of
  [ADR-0025](./0025-pushout-forget-architecture.md), patch content
  carries tokens and a lost purge re-exposes nothing; without the
  vault it re-exposes deleted personal data on the replica
  ([explanation](../explanation/pushout-sweep-and-purge-durability.md)).
  Today that distinction exists only in prose; the engine behaves
  identically in both.

A store that keeps envelopes as rows rather than frames re-encodes on
read. Identity survives that — the hash is over the canonical item, not
the frame — but the seam's "bytes equal to those put" does not, and
today such a store has no way to say so.

The forces: an append-only or fully structured backend wants to drop
snapshots and be told what it loses; a compliance deployment wants to
be refused at `Open` rather than surprised at recovery; a structured
store wants a contract it can keep; and the engine should not need to
know anything about vaults or law to arrive at any of these.

### The shapes in play

What the engine persists, and what each piece can rebuild:

```
            ┌──────────────────── engine, in memory ────────────────────┐
            │ applied set ──apply──▶ graph: live nodes, tombstones      │
            │                              (stamp, deleters,            │
            │                               content | PURGED)           │
            └───────────────────────────┬───────────────────────────────┘
                                        │ StorageI
   ┌───────────┬───────────────┬────────┴────────┬────────────────┬──────────────┐
   ▼           ▼               ▼                 ▼                ▼              ▼
 envelopes   applied log   retention ledger        snapshot
 hash→frame  hash sequence node→stamp, purged      graph+applied
 core        core          Sweep only (SD2)        accelerator
   └────── replay rebuilds the graph ──────┘       (before SD2: also the
                                                    only purge carrier)

 rebuildable from log + envelopes:  every node, edge, tombstone, deleter
 not rebuildable:                   stamps and purges — the ledger
```

Three store shapes that exist or are wanted, and what each can
honestly declare:

```
 A. directory of files                 B. append-only fact table
 ─────────────────────                 ─────────────────────────
 envelopes/<hash>   frame, immutable   key            row
 applied.txt        append + atomic    env/<hash>     frame blob, first wins
                    replace            log            entry rows; reset = tombstone
 snapshot.bin       atomic replace                    row, keep what follows
 retention.txt      atomic replace     snapshot       whole copy per save, latest
                                       retention      whole copy per save, latest
 Snapshots ✓  RetentionLedger ✓       Snapshots ✓  RetentionLedger ✓
 ReplaceApplied ✓  ExactBytes ✓        ReplaceApplied ✓ (emulated)  ExactBytes ✓

 C. structured rows, no frames, no snapshot
 ──────────────────────────────────────────
 patch row        hash, author, description, producer, timestamp
 dependency rows  patch → dep
 change rows      patch, ordinal, kind, node, content | up/down context | src/dest
 log              version entity, latest wins        (or: retraction rows, deferred)
 retention rows   node → stamp, purged
 GetEnvelope      = rows → EnvelopeV1 → re-encode      hash re-checked on read
 Snapshots ✗  RetentionLedger ✓  ReplaceApplied ✓ (versioned)  ExactBytes ✗
 — the change rows ARE the additive snapshot: state(S) = changes of patches in S
```

How recovery reaches the graph in each shape, and where purges come
from:

```
 A / B, snapshot covered by log      C, and A / B without a snapshot
 ────────────────────────────────    ─────────────────────────────────
 load snapshot ─▶ graph              load log ─▶ for each hash:
 replay log \ snapshot.applied          get envelope, apply ─▶ graph
 seed stamps from ledger             seed stamps from ledger
 purges: snapshot, then ledger       purges: re-apply from the ledger (SD2)
         on top                              or LOST without one
                                             (Guarantees.RetentionDurable)
```

What `Open` checks once modes and capabilities exist (SD3):

```
 Retention mode      needs                  Sweep
 ───────────────     ─────────────────────  ──────────────────────────────
 None                nothing                refused, ErrUnsupported
 Hygiene (default)   nothing                allowed; report.Durable is the store's truth
 Compliance          RetentionLedger        allowed; Durable is always true

 Open(opts, store):  mode.needs ⊆ store.Capabilities()  ?  proceed
                                                       :  ErrCapability naming the flag

 Guarantees (SD6) = f(mode, capabilities), read by the consumer:
   SweepAllowed       mode ≠ None
   RetentionDurable   RetentionLedger ∧ mode ≠ None
   UnrecordSupported  ReplaceApplied
   Recovery           Snapshots ? FromSnapshot : FullReplay
```

## Decision

We will make the store declare what it persists, give the purge set a
carrier of its own so the snapshot is purely an accelerator, make the
deployment's retention expectation an explicit option that `Open`
validates against the store, have `Sweep` and recovery report what they
achieved, and give consumers one derived contract, `Guarantees`, so
they never reason about capabilities or modes at all.

**SD1 — declared capabilities, not type assertions.** `StorageI` gains
`Capabilities() Capabilities`, a value the store computes from what it
implements and a decorator inherits by embedding (overriding it when
it changes the truth). The engine reads it once at `Open` and never
asserts on the store's dynamic type. Four flags:

| Capability | Meaning when true | Meaning when false |
| --- | --- | --- |
| `Snapshots` | `SaveSnapshot` persists and `LoadSnapshot` returns it | both are advisory; the engine never calls them, and recovery always replays the log in full |
| `RetentionLedger` | `UpdateRetention`/`LoadRetention` (`SaveRetention` until [ADR-0221](./0221-pushout-storage-batch-verbs-delta-ledger.md), which made the write a delta) persist stamps **and purge markers** (SD2) | the ledger is never written; stamps reset to replay time on recovery and purges do not outlive the process |
| `ReplaceApplied` | `ReplaceApplied` is atomic and durable | `Unrecord` is refused with `ErrUnsupported` |
| `ExactEnvelopeBytes` | `GetEnvelope` returns the bytes put | it returns a re-encoding with the same identity; the read-side hash check is the guard, and the conformance suite skips byte-equality |

`AppendAppliedBatch` joins the interface outright — a store with a
single-write path uses it, any other store loops — so the former
`BatchAppenderI` extension and its type assertion go away. Envelopes
and the applied log are the core and carry no flag.

**SD2 — the purge marker rides the retention ledger.** `RetentionEntry`
gains `Purged`. `Sweep` writes the ledger before it acknowledges, and
`Open` re-applies the ledger's purges after replay, dropping the content
and setting the purged status exactly as the sweep did. `Sweep` makes
exactly **one** durable write — the ledger when the store keeps one,
else a snapshot when it keeps those, else nothing — so a fault or crash
leaves the purge either done or never started; the snapshot is not
refreshed by a sweep over a ledger store, and recovery restores the
older snapshot and re-applies the ledger's purges on top. With this the
snapshot holds nothing that the log, the envelopes, and the ledger
cannot rebuild: `Open`'s covered-snapshot rule stays as an optimisation,
and `Close` and `Checkpoint` skip the snapshot entirely over a store
that disclaims it.

**SD3 — an explicit retention mode, validated at `Open`.**
`repo.Options.Retention`:

| Mode | `Sweep` | Requires | Intended for |
| --- | --- | --- | --- |
| `RetentionHygiene` (zero value, the default) | allowed; `SweepReport.Durable` reports the store's truth | nothing | vaulted deployments, where a purge destroys tokens (ADR-0025 Architecture A) |
| `RetentionNone` | refused, `ErrUnsupported`; the ledger is never read or written | nothing | deployments that never purge |
| `RetentionCompliance` | allowed; `Durable` is always true | `RetentionLedger` | raw-content deployments relying on `Sweep` for storage limitation |

`Open` fails with `ErrCapability`, naming the missing flag, when the
mode's requirement is not met. The default preserves every existing
caller's behaviour; `RetentionCompliance` is opt-in because it is the
mode that refuses. The engine learns nothing about vaults: the mode is
the only consequence of that choice that reaches it.

**SD4 — verbs report what they achieved.** `SweepReport.Durable` is true
when the purge markers reached the ledger before return.
`RecoveredEvent.PurgesRestored` counts the markers recovery re-applied
from the ledger. `ErrUnsupported` names a verb the store's capabilities
or the mode rule out; `ErrCapability` names an `Open` refused by SD3.

**SD5 — the conformance suite follows the declaration.** `storagetest`
reads `Capabilities()` and runs each check the store claims and a
negative check for each it disclaims: a store without `Snapshots` must
report none after a save, one without `RetentionLedger` likewise, one
without `ReplaceApplied` must return `ErrUnsupported` and leave the log
intact. A declared no-op is conformant; an undeclared one is a defect.
Envelope byte-equality is checked only under `ExactEnvelopeBytes`; a
re-encoding store's gate is the engine's own tests over it.

**SD6 — one consumer-facing contract.** `Repo.Guarantees()` returns
four facts derived once at `Open`: `SweepAllowed`, `RetentionDurable`
(stamps and purges survive a restart), `UnrecordSupported`, and
`Recovery` (whether the next `Open` can use a snapshot). A consumer
that needs a property checks one field. Capabilities and modes are the
store's and the engine's vocabulary; the explanation page and the
package documentation describe `Guarantees` and the three modes to
consumers, nothing else.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `repo.StorageI` | `Capabilities()` and `AppendAppliedBatch` added; `BatchAppenderI` removed | `filestore`, `pushoutstore`, every test store, `storagetest` |
| `repo.RetentionEntry` / ledger formats | `Purged` added | the filestore's line format (a trailing `purged` field, old lines still read); `pushoutstore` writes the purged subset as a second row under its own key, no DTO change |
| `repo.Options` | `Retention` added | `Open` validation; callers that sweep for compliance |
| `repo.Repo` | `Guarantees()` added; `Sweep` one durable write; `Unrecord`, `Checkpoint`, `Close` honour capabilities | hooks consumers, the crash-recovery tests, the pijul backend |
| `repo.SweepReport`, `repo.RecoveredEvent` | `Durable`, `PurgesRestored` added | hooks consumers |
| `repo` sentinels | `ErrUnsupported`, `ErrCapability` added | transports must preserve them across the carrier like `ErrMissingDependency` |
| `repo/storagetest` | capability-driven; negative checks | every conformance run |
| `pushoutgraph/store` | `PurgeContent` added | recovery |
| `pijul` backend | `WithRetention`, `WithoutSnapshots` knobs | the rapid state-machine harness draws both |
| `verification/formal/.../crash_recovery.qnt` | purge sets, `sweep`, `PurgeDurable`; a const-parameterised core with two instances | `package.json` scripts; the README status table |

## Alternatives

- **Keep type assertions, add more optional interfaces.** Rejected: a
  store's capabilities would keep depending on its wrapper, and the
  suite could not know what to test.
- **Teach the engine about the vault** (an option naming the content
  policy). Rejected: the engine never reads content semantics, and the
  retention mode already captures the only consequence that reaches
  it; a vault flag would be a second name for the same bit.
- **Make the snapshot mandatory and refuse stores without it.**
  Rejected: it keeps an accelerator load-bearing for correctness and
  shuts out append-only and structured backends for no gain once SD2
  exists.
- **A separate purge ledger beside the retention ledger.** Rejected:
  both are per-tombstone, replica-local facts written on the same
  events, and one flag on one ledger is one capability for a store to
  declare instead of two.
- **Put purge markers in the applied log as entries.** Deferred, not
  rejected: it is the natural end state for an append-only seam, but it
  changes the log's meaning and belongs with the retraction-entry
  design for `Unrecord`, which is its own decision.
- **A typed envelope seam** (`PutEnvelope`/`GetEnvelope` over
  `envelope.EnvelopeV1`, the engine owning the only codec registry).
  Rejected: it reshapes the seam for every store to serve the ones
  that keep rows, and it ends "the envelope re-ships in its original
  codec" (ADR-0079 O2b) for all of them. The bytes seam stays; a store
  that re-encodes declares `ExactEnvelopeBytes = false` and pays its
  own decode, and the read-side hash check remains the guard for both.
- **Let `Sweep` write both the ledger and the snapshot.** Rejected: two
  durable writes give a fault between them a state that is neither
  "never happened" nor "happened", which the crash-equivalence tests
  forbid for every verb.
- **Expose capabilities and modes to consumers directly.** Rejected:
  six mechanisms are not a contract; `Guarantees` is.

## Consequences

### Positive

- Mismatches fail at `Open` with a named capability, not at recovery.
- A store may drop snapshots honestly; the snapshot is an accelerator
  everywhere.
- The two deployment shapes of the explanation page are two option
  values with distinct, tested behaviour.
- Decorators cannot accidentally change what a store can do.
- Consumers read four facts and never see a capability.

### Negative

- Every `StorageI` implementation, including test doubles, gains two
  methods and a ledger change.
- Modes and store shapes multiply the sweep and recovery test matrix;
  the harness draws them so the cost is wall-clock, not code.
- `RetentionCompliance` refusing to open is new operational friction
  for a deployment that upgrades a store without the ledger.
- A store with `ExactEnvelopeBytes = false` re-encodes on relay, so
  "re-ships in its original codec" (ADR-0079 O2b) holds per store, not
  fleet-wide. Identity is unaffected, since the hash is over the
  canonical item, not the frame.

### Neutral

- The default mode changes nothing for existing callers.
- The GRG1 snapshot format is untouched; only what depends on it moves.
- A sweep over a ledger store no longer refreshes the snapshot, so a
  snapshot may be older than the last sweep; recovery re-applies the
  ledger's purges on top of whatever snapshot it restores.

## Migration — Tier 1

- **Breaks.** `StorageI` implementations that lack `Capabilities()` or
  `AppendAppliedBatch` stop compiling. Code that asserted on
  `BatchAppenderI` stops compiling.
- **Path.** Add `Capabilities()` returning the truth about the store
  (`AllCapabilities()` for a store that persists everything); add
  `AppendAppliedBatch` (a loop over `AppendApplied` is conformant);
  round-trip `RetentionEntry.Purged`; run `storagetest`. Callers that
  sweep for compliance set `RetentionCompliance` and fix what `Open`
  reports.
- **Regeneration.** None: the filestore's ledger line gains an optional
  trailing field, and `pushoutstore` reuses its retention DTO for the
  purged subset under a second key.
- **Old shape.** `BatchAppenderI` removed outright; it had one
  implementation and shipped days before this ADR. Old filestore
  ledgers (three fields per line) still read.

## Verification plan — Tier 1

- **Lane.** Default `go test`, all in `repo`, `repo/storagetest`, and
  `pijul`: the conformance suite over the filestore, the recordstore
  adapter, a no-snapshot and a no-ledger decorator of the filestore,
  and an in-memory store under all sixteen declarations; a semantics
  golden enumerating every mode × declaration with `Open`'s verdict,
  the derived `Guarantees`, and the verbs' agreement with them; a crash
  matrix (store shapes × modes, sweep then crash-reopen) where
  `Guarantees.RetentionDurable` predicts what survives; fault injection
  on `Sweep`'s single write for both carriers; the rapid state-machine
  harness drawing the mode and the store shape per fleet. Quint:
  `crash_recovery.qnt` as a const-parameterised core instantiated for
  both carriers and for ledger-only, with `PurgeDurable` in `Safety`,
  and `crash_recovery_unsafe_purge.qnt` instantiating it with neither
  carrier and required (by `expect_violation.sh`) to violate it.
- **What would fail.** A store that disclaims a capability and behaves
  as if it had it, or the reverse; a `RetentionCompliance` open over a
  ledger-less store that succeeds; a purge absent after crash-reopen
  where `Durable` was true; a `Guarantees` value that disagrees with
  any verb's outcome; a Quint trace in which a returned sweep is undone
  by recovery under either safe instance, or NOT undone under the
  counterfactual.
- **Gap.** Fleet-wide erasure across re-cloning (ADR-0079 OQ-7) is
  untouched: a fresh clone has no ledger of any kind. A re-encoding
  store (`ExactEnvelopeBytes = false`) has no in-tree instance, so its
  engine-level gate is unexercised until one lands.

## Status

Proposed — implementation landed with this record for review together;
awaiting review by the pushout code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-05 — ADR-0221 reshapes the verbs, not the flags

The ledger write became a delta (`UpdateRetention`) and the envelope
verbs gained batch forms (`PutEnvelopes`, `LoadEnvelopes`); the four
capabilities, the retention modes, `Guarantees`, and every decision
above are unchanged. A `Sweep` is still exactly one durable write — now
a delta carrying only the purge markers it set.

## References

- [ADR-0079](./0079-pushout-production-storage-codec-exchange.md) — the
  storage seam, Q5 purge durability, the retention ledger, OQ-7.
- [ADR-0025](./0025-pushout-forget-architecture.md) — the vault; SD8
  tombstone sweep.
- [Sweep and purge durability under a data vault](../explanation/pushout-sweep-and-purge-durability.md)
  — the mechanism and the two deployment shapes this ADR names.
- `verification/formal/algebraicarch/pushout/crash_recovery.qnt` — the
  recovery model whose refinement map SD2 extends.
