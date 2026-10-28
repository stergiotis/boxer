---
type: adr
status: proposed
date: 2026-10-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0341: Lading publish — a write-only capability, with the placement check at the door

## Context

The lading store ([ADR-0198](./0198-fs-snapshot-store.md)) is written by
whoever holds ClickHouse credentials: tally, mdedit, the `ladingfs` command
and `ladingadhoc` ([ADR-0222](./0222-tally-composition-surface.md) §SD5) each
connect from the registered `CLICKHOUSE_*` variables and write as themselves.
ADR-0198 left the capability broker out on purpose. Its update of 2026-08-20 (M4)
calls the visibility check "a seam, not a wiring" and names binding it to the
broker as "a decision rather than an implementation gap".

Two prospective writers do not fit the current posture:

- **The chat's artefact export** ([ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md), proposed). The writer is a
  process that runs a model with tools behind an egress wall, and what it
  writes carries labels ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)):
  screenshots are captures (level 2), and the conversation may be confined.
  [ADR-0145](./0145-sealed-app-data.md) makes sealed data a placement
  property, refused where it may not go. Nothing checks placement on the way
  into lading.
- **A file upload from the operating system**, reading files the person
  picks into the store. That case is anticipated here but not designed.

Reading is a different matter. play and tally read every mount
(`ladingsql.VisibleAll`), and anyone who can read the ClickHouse tables can
read every row. The audience of the store is set by ClickHouse access, not by
keelson, so a read-side capability would bind only the apps that chose to use
it.

## Decision

We will add a host service that publishes a tree into the lading store on a
bus request, under a mount tag the calling app declares. The service checks
the tree's label against what the store may hold before any row is written.
Reads stay as they are.

### SD1 — One subject family, write only

The service answers `lading.publish.<tag>`. A request carries a mount (zero
mints one), a retention class (zero is 30 days), a label, and the files: a
flat list of paths and their bytes, which the service presents to
`ladingingest` as an `io/fs.FS`. Directories are implied by the paths, so a
path that is also another's directory is refused. Every file is stored whole:
the per-file inline bound is raised to the request bound (SD4), and a snapshot
that holds any file only by reference fails, since the store has no source to
fetch it from. The reply is the mount id, the snapshot instant, its expiry,
the number of files and their bytes. There is no retract: a snapshot expires
(ADR-0198 §SD1), and a per-mount purge stays an operator act.

A request may ask for the check alone, a dry run that writes nothing and
leaves no trail row. The service answers it at once, so a caller learns in
seconds that the publish would be refused, or that the host runs no service,
before it does something a refused publish would leave standing, such as an
endorsement. A publish gives up at a deadline counted from its receipt, a
margin before the caller stops waiting, and closing the service cancels the
publishes in flight; a publish the caller gave up on has written no snapshot
readers see. A failed publish can leave the mount's policy row and rows that
no snapshot commits.

The service holds the store's connection. It connects from the `CLICKHOUSE_*`
variables on the first publish, and again after any failed one, so a host without
a reachable store starts and answers each publish with the reason. Before
each write it verifies the store's shape, and it never provisions the store:
the `ladingadhoc` posture. A caller holds no ClickHouse credentials for
writing. Apps that write today keep their direct path (Migration).

### SD2 — The tag is the app's, declared as its subject

ADR-0198 §SD3 gives the mount tag to the application. An app that publishes
declares the capability `lading.publish.<tag>` on its manifest, for one tag
value, to publish. The bus's own subject check then keeps the app to that tag, and no
manifest field is added. The service reads the declarations at start and also
refuses a sender whose manifest does not name the tag, which a broad pattern
such as `lading.publish.>` would otherwise let through. A declaration to
subscribe only owns nothing. A tag two apps declare
is logged at start and owned by neither. The service mints mount bodies under
the tag, and refuses a request naming a mount under another one. Any window of
the app may write any mount under its tag.

### SD3 — The placement check

The request's label is the caller's statement of the tree's confidentiality
tier and whether it is confined, in ADR-0311 §SD1's terms. The service
refuses:

- a **confined** tree, always: the lading tables are not sealed, and ADR-0145
  keeps sealed data off every place that is not;
- a tree whose **tier** is above the store's ceiling, a registered
  variable ([ADR-0009](./0009-environment-variable-registry.md)) whose
  default is `AMBER`. Everyone who can read the store reads what is in it,
  and in the deployments this repository targets those readers are the
  organisation.

The label is asserted, not derived: the service cannot see the caller's
journal. That matches the threat model, which does not isolate apps inside
one process ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md),
ADR-0240's "never intra-process isolation"). What the service adds is one
place where the check is made, the same for every writer that uses it. The
integrity axis is the caller's: whether an act needs an endorsement
(ADR-0311 §SD3, law L15) is decided where the act is requested.

### SD4 — Retention and size

The caller names a retention class. The service refuses one above the
deployment's longest class, a registered variable defaulting to 90 days. A
request is bounded in bytes, by a registered variable defaulting to 256 MiB,
the ad-hoc dataset quota (ADR-0240); a larger tree is refused, not split.

### SD5 — Every publish is a trail row

Each publish, admitted or refused, is one row on the audit trail
([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)), a
request that does not decode included: the caller's app and window, the tag,
mount and snapshot, the label asserted, the decision and its reason, the
bytes, and every path with its file's hash, sorted by path. A dry run is not
a publish and has no row.

### SD6 — Deferred, recorded

- **The read side.** Trigger: a mount whose audience must be narrower than the
  store's. `MountVisibilityI` is where it plugs in.
- **Moving the existing writers onto the service.** Trigger: one of them needs
  something the service gives, such as the placement check or the trail row.
  `ladingfs` is an operator tool and is expected to stay direct.
- **Streamed publish** for trees past the request bound. Trigger: the file
  upload, whose files are not bounded by a budget the way screenshots are.
- **The file upload itself**: the picker, its consent, and the label a file
  from the operating system is given.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| bus subjects `lading.publish.<tag>` | added | the service's wire types (`ladingpublish`); hostboot's service set |
| app manifest | no field; an app declares the subject for its tag as a capability | the service's owner map |
| environment | the tier ceiling, the longest class, the request bound, registered | `doc/env-vars.md` |
| audit trail | a `ladingPublish` row kind | the trail store's generated code; the runtime vocabulary and its golden |

## Alternatives

- **Gate reads as well.** Kept for later (SD6). While play and tally read every
  mount and ClickHouse access defines the audience, a read capability would
  constrain only its own users.
- **Each writer checks placement itself.** The check would be re-implemented
  per app and drift, and a new writer could omit it. ADR-0281 put captures
  behind one enforcement point for the same reason.
- **`ladingadhoc` as the handover.** It writes with the caller's credentials,
  keeps the tree one to two days, and checks no label. It fits a scratch tree
  an app computed, not data that carries a label.
- **An ad-hoc dataset of files.** Sealed and process-scoped, so placement is
  never in question, but an ad-hoc dataset is a table handed to SQL. It
  follows the publishing window's life, which the reader cannot extend, and
  the lading readers do not read it.

## Consequences

### Positive

- A writer with labelled data has one place that refuses what the store may
  not hold, and a trail row for each attempt.
- A writer needs no ClickHouse credentials of its own.
- The file upload has its path into the store before it is designed.

### Negative

- Two write paths coexist: the service, and the direct one the existing
  writers keep. "Publish through the service" binds only the writers that use
  it.
- The label is the caller's assertion. A caller that understates it is not
  caught by the service.
- One more host service, which every host that wants the subject must run,
  headless and browser-tab hosts included.
- A whole tree travels in one bus request.

### Neutral

- Exported data is readable by every reader of the store for its retention
  class. That is the store's nature; callers say so to the person.

## Migration — Tier 1

Nothing to migrate: the subject is new, and no current writer changes.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane for the check, the owners and the bus
  path, with no store; the integration lane for a publish into a provisioned
  store, read back through the adapter readers use.
- **What would fail.** A confined tree, a tier above the ceiling, a class
  above the longest, a mount under another app's tag, or a request past the
  bound, each written instead of refused, and each refusal without a trail row;
  a file above `ladingingest`'s default inline bound stored by reference; a
  dry run without a service that waits out a publish's timeout; a publish
  queued past its deadline that still writes.
- **Gap.** That a caller asserts the right label is the caller's test, not
  this one.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0198](./0198-fs-snapshot-store.md) §SD1, §SD3, update of 2026-08-20 — the store, caller-owned tags, the capability left as a decision.
- [ADR-0145](./0145-sealed-app-data.md) — sealed data as a placement property.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) §SD1, §SD3 — the label and endorsement.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) — the store-owned capability pattern, and the quota reused.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) — one enforcement point for captures.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the audit trail.
- [ADR-0222](./0222-tally-composition-surface.md) §SD5 — `ladingadhoc`.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — the threat model.
- [ADR-0009](./0009-environment-variable-registry.md) — environment variables.
- [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md) (proposed) — the chat's artefact export, the first client.
