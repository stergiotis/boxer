---
type: adr
status: accepted
date: 2026-10-08
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-08
---

# ADR-0295: recordstore call identity, an observing executor and write observers — the mechanical half of an audit trail

## Context

A consumer building a records-of-processing trail — GDPR Art 30, and Swiss
FADP Art 12 with the logging its ordinance asks of some automated processing
(read as in [ADR-0027](./0027-pushout-forget-swiss-fadp.md)) — needs two kinds
of fact. The *meaning* of an access is the consumer's: purpose, legal basis,
which data subject, what erasure means. The *mechanics* are not: for whom a
read or a write was issued, when, as which statement or batch, against which
table, how many rows, with what outcome. They are the same for every store.

Every call a generated record store
([ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md)) makes
crosses [`recordstore.ExecutorI`](../../public/storage/recordstore/recordstore.go),
and so do the paths that bypass a store's `Begin`/`Flush`: `rowcas` updates
([ADR-0223](./0223-watchbill-durable-work-on-facts.md) §SD3), hand-built
`ALTER … DELETE`, and ledgers calling `InsertArrow` directly. Before this
decision the seam recorded none of the mechanics. No executor observed calls
or stamped `log_comment` — the ADR-0115 SD7 stamp lived in play's client
only. `ReferenceStamper`
([ADR-0112](./0112-dimensionstore-interned-facts-additive-memberships.md))
ran under `context.Background()` and had no principal dimension. A `Flush`
named no batch.

Prior art: [`keelson/runtime/audit`](../../public/keelson/runtime/audit/audit.go)'s
`AsyncSink`, and
[ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)'s
host-stamped `Origin` with its write-ahead semantics.

## Decision

Give recordstore the mechanics and nothing more — a call identity on the
context, executors that stamp it into `log_comment`, a decorator that emits
one event per call, and a write observer on generated stores — and leave all
vocabulary to the consumer. One call leaves up to three records, joined by its
batch id B:

```text
ctx ← call identity (origin vouched · claims stated)
 │
 ├─ BeginCtx ─► actor stamper ─► actor id on the row's attributes  (SD8)
 │
 └─ Flush: mint B, ctx ← B ─────► WriteObserver.Durable(B, rows)   (SD7)
     │                             each row with its committer
     ▼
   ObserveExecutor ─────────────► CallEvent{…, B, digest, identity} (SD4)
     │
     ▼
   storeexec / chexec ──────────► log_comment {…,"batch":B}         (SD3)
                                   └─► system.query_log
```

| Record | Written by | Strength | Limit |
| --- | --- | --- | --- |
| `CallEvent` | the process | op, class, table, counts, outcome | best effort unless a required observer gates the call |
| `query_log` row | the server | survives without boxer running | the identity in it is still the client's claim |
| `WrittenKey`, actor id | the store | names each row's committer | only for writes through a generated store |

### The threat that shapes it

SQL text carries personal data. A `ScanOpts.KeyPrefix` may be an email
address, a hand-built `DELETE WHERE` names a data subject, and a ClickHouse
exception quotes the statement it failed on. A trail that copied them would be
a second store of the data it accounts for, with its own erasure problem. So
no record carries SQL text or values, and the identity fields are opaque
references. Every copy of the stamp — the request URL and `chexec`'s command
line, a proxy's access log, `system.query_log` — lies outside the consumer's
control.

### Subsidiary design decisions

- **SD1 — Call identity rides the context, split by who vouches for it.** ✓
  [`callident.CallIdentity`](../../public/identity/callident/callident.go)
  holds an `Origin` (run, app, instance) that the host vouches for, as in
  ADR-0277, and `Claims` (principal, purpose, correlation) that the caller
  states. `WithClaims` keeps the origin already on the context. In one process
  the split is a convention: any code with a context can call
  `WithCallIdentity`, so the vouching is only as strong as the host's control
  over the contexts its apps receive. The principal must be a pseudonymous
  reference. The package sits under `public/identity`, below both recordstore
  and keelson, because play's stamp needs it and has no store.

- **SD2 — One `log_comment` format, below its producers and its parser.** ✓
  The wire struct moves from `queryrunfacts` to
  [`logcomment.Stamp`](../../public/db/clickhouse/logcomment/logcomment.go).
  Play, the executors and `queryrunfacts.ParseStamp` share it, and its JSON
  keys are the contract. It gains `principal`, `purpose`, `correlation` and
  `batch`; existing keys are unchanged. `Marshal` repairs invalid UTF-8
  rather than failing, because a failed stamp would send the statement out
  anonymous, with nothing to say so.

- **SD3 — Executors stamp what the context carries, and nothing else.** ✓
  `storeexec` sends the stamp as the `log_comment` URL setting on every verb;
  `chexec` passes `--log_comment`. A context with no identity and no batch id
  sends nothing. A statement's own `SETTINGS log_comment` wins — ClickHouse's
  rule, and tested — which keeps queryrunsd's self-capture exclusion intact;
  `chhttp` already tolerates the setting. A `readonly=1` user refuses any
  setting, so `storeexec.Options.DisableStamp` opts out.

- **SD4 — One event per call, carrying a digest of the SQL, never its text.** ✓
  [`ObserveExecutor`](../../public/storage/recordstore/observe.go) decorates
  any `ExecutorI` and emits one `CallEvent` per call: op, a class for `exec`
  (DDL, mutation, other), table, counts, batch id, a 128-bit BLAKE3 digest of
  the SQL, timing, outcome with the ClickHouse error code, and the identity. A
  query's event fires once, when its stream ends, however it ends. A call that
  sends no request — a sequence never iterated, an empty insert — emits
  nothing. `ExecutorI`'s ownership rules are unchanged. The class comes from a
  lexer over the statement's head. DDL removes data too (`DROP`, `TRUNCATE`,
  `DROP PARTITION`), so an erasure audit reads both classes. Anyone holding
  the digest of a guessable statement can confirm a guess at it;
  `ObserveOptions.DigestKey` confines that to key holders. Keyed or not, the
  digest is pseudonymised data in GDPR Art 4(5)'s sense. `CallEvent.Err` is
  for in-process use; a durable observer keeps `Outcome` and `ErrCode`.

- **SD5 — Observers never fail a call; a required one gates it beforehand.** ✓
  An observer is one method, `ObserveCall(CallEvent) error`, and by default
  its error is counted and ignored. `Required(o)` adds an *intent* event
  before the statement is sent, and an error there refuses the call
  (`ErrObserverRefused`). This is ADR-0277 §SD3's write-ahead: nothing proceeds
  unrecorded, and an intent without a done event reads as an access of
  unknown outcome. A required observer's failure on the done event is counted,
  not returned (see *Alternatives*). It must be passed to `ObserveExecutor`
  directly and must record synchronously; construction panics otherwise.
  `NewAsyncObserver` is the fan-out that never blocks: a bounded buffer, with
  every event either forwarded or counted as dropped.

- **SD6 — A batch id per insert attempt, carried on the context.** ✓
  A generated `Flush` mints a `BatchIdT` (a nanoid) per `InsertArrow`
  attempt. The observing executor uses the id on the context or mints one,
  and the executors stamp it. A retry is a new batch, because it may carry
  rows committed after the failure. The three records agree for a store over
  a stamping executor. They need not when a caller reuses its own
  `WithBatchId` context across calls, or behind `ipcexec`, which writes no
  stamp.

- **SD7 — Generated stores take a context and report what became durable.** ✓
  The generator adds `BeginCtx`, `DeleteCtx` and `Ingest<Kind>Ctx`; the old
  verbs pass `context.Background()`. The context reaches every stamper. The
  optional `WriteObserver`
  ([`WriteObserverI`](../../public/storage/recordstore/writeobserver.go)) is
  told each committed `WrittenKey`, including the identity it was committed
  under, then exactly once either `Durable`, with the batch id, or
  `Discarded`. The identity is the per-row attribution: a flush's stamp names
  whoever called `Flush`. A failed flush keeps its keys for the retry; a
  panicking observer loses that one report rather than receiving it twice.
  The observer is notification-only and independent of the cache-view hooks.

- **SD8 — An actor dimension that refuses to stamp without a principal.** ✓
  [`dimension/actor`](../../public/storage/recordstore/dimension/actor/actor.go)
  is the second ADR-0112 dimension, shaped like provenance. It interns the
  claimed principal and purpose with the vouched app, and stamps the id on
  every attribute a row writes. With no principal on the context it fails,
  so under ADR-0112 SD2's fail-fast rule an actor-stamped store refuses the
  write. That covers `Begin`, and the default `Delete`: its marker row cannot
  carry the id, but it still consults the stampers. ADR-0112 SD5's ordered
  flush keeps a descriptor durable no later than its rows, unless the store
  sets `BestEffortStampFlush`, which such a store should not. Run and instance
  stay out of the key, which would otherwise grow with every run.

### Deferred and open

- **Tamper evidence and retention.** Both are the consumer's, built over the
  events.
- **Query-run capture of stamped store traffic.** queryrunsd's `stamped` scope
  keys on `run_id` and copies up to 16 KiB of each query's text into
  `boxer.facts`. Once a host puts a run on store contexts, store statements
  fall into that scope — the copy the threat above rules out. (Scope `all`
  captured them already.) Lifting a digest for rows that carry `batch` is the
  likely fix; it belongs to ADR-0115.
- **Lifting the new keys into facts.** `principal`, `purpose`, `correlation`
  and `batch` reach `query_log` but no `QueryRun` membership; that needs
  vocabulary entries.
- **Read-only users.** Retrying unstamped on refusal, as play does (ADR-0181),
  would drop audit stamps silently. Carrying the batch in `query_id` would
  survive a read-only user and is the candidate fallback.
- **Host wiring and enforcing the origin.** Nothing in keelson attaches an
  identity to the contexts it hands apps; which component builds the origin is
  the host's decision. No unforgeable capability lets a consumer check that an
  origin was vouched for.

### Milestones

- **M1 — Call identity and the shared stamp format (SD1, SD2).** ✓
- **M2 — Stamping executors (SD3).** ✓
- **M3 — The observing executor and its fan-out (SD4, SD5, SD6).** ✓
- **M4 — Generated stores: context at Begin, write observer (SD7).** ✓
- **M5 — The actor dimension (SD8).** ✓

## Surfaces — Tier 1

- `recordstore.ExecutorI` — unchanged; everything here is additive.
- The `log_comment` JSON keys of `logcomment.Stamp` — read by
  `queryrunfacts.ParseStamp` and by the `stamped` capture scope (`run_id`);
  additive.
- Generated store API — `BeginCtx`, `DeleteCtx`, `Ingest<Kind>Ctx` and the
  `WriteObserver` field; every in-tree store is regenerated.
- `chclient.Client` — settings-taking variants of `Exec`, `Query` and
  `InsertArrow`.

## Alternatives

- **Fail the call when an observer fails after it.** Rejected. A failed
  `InsertArrow` makes `Flush` resend, so a landed insert would land twice, and
  a query's rows are already with the consumer. The intent gate refuses before
  anything leaves.
- **Record redacted SQL.** Rejected: redaction needs a parser that knows which
  literal is personal, and the statement it does not understand is the one it
  lets through.
- **Hooks on `Begin`/`Flush` only.** Rejected: they miss `rowcas`, hand-built
  mutations and direct `InsertArrow` calls. The write observer adds only what
  the executor cannot see — which keys a batch carried.
- **Put the identity in recordstore, or keep `queryrunfacts.Stamp`.**
  Rejected: play's stamp has no store, `chexec` must not import keelson, and
  the coding standard forbids the alias that would keep the old name.
- **Carry the batch id in `query_id`.** It survives into `query_log`, and a
  read-only user accepts it. Rejected as the carrier: it names one execution —
  the server refuses a second query under a running id — its callers already
  own it (play reuses one per lane, ADR-0097 SD5), and it holds one value where
  the stamp holds the identity as well.

## Consequences

### Positive

- A statement any store or ledger issues through a stamping executor is
  attributable in the server's own log, with no boxer process running.
- One decorator covers every path, including those that bypass `Begin`.
- A consumer learns which entities a batch made durable without decoding
  Arrow, and joins that to the event and the log row by batch id.

### Negative

- Every store insert now carries a `log_comment` with at least its batch id; a
  `readonly=1` user needs `DisableStamp`.
- An insert is attributed to whoever flushes it; per-row attribution needs the
  write observer or the actor stamper.
- Digests are pseudonymised data; keying them removes comparability across
  deployments.
- The classifier is a lexer: an unrecognised statement is `other` with no
  table, and a `WITH` clause's inner `FROM` may be reported as a query's table.
- Every generated store grows by the `*Ctx` verbs and the observer plumbing.

## Migration — Tier 1

Callers that attach nothing are unaffected, apart from the batch-only stamp on
store inserts. `queryrunfacts.Stamp` is gone: import `logcomment.Stamp`
instead (in-tree: play and the parser). Generated stores are regenerated in
place, and their existing verbs keep their signatures.

## Verification plan — Tier 1

| Lane | Goes red when |
| --- | --- |
| `recordstore` unit tests | an op kind, a classification, early-break counting, required-mode gating or placement, or async drop accounting regresses |
| `storeexec` integration test (`integration` tag, live server) | a stamp stops reaching `system.query_log`, or the URL setting starts winning over a statement's own `SETTINGS log_comment` |
| `recordstore/example` tests | `BeginCtx` stops reaching the stampers; a key is reported zero times or twice across flush, retry, discard, a re-entrant commit or a panicking observer; a row loses its committer's identity; a default `Delete` escapes the actor stamper |
| `dimension/actor` tests | the stamper accepts a context without a principal, or its natural key becomes ambiguous |
| `logcomment` and `queryrunfacts` tests | an existing key stops parsing, or invalid UTF-8 drops a stamp |

## Status

Accepted 2026-10-08. M1–M5 were built and then revised after an adversarial
review the same day.

## Updates

### 2026-10-08 — host wiring decided against, for now

[ADR-0296](./0296-audit-events-as-a-trail-component.md) §SD7 takes the
deferred *host wiring* item: the host attaches no `CallIdentity` to the
contexts it hands apps, because `MountContextI` hands an app no
`context.Context` to attach one to. Each process sets its own identity
with `WithCallIdentity` and adds claims with `WithClaims`. The item stays
open on the question it depends on — where a context reaches an app — and
is not closed by this.

The trail's recorder is the first consumer of `BeginCtx` and the write
observer: `Recorder.Event` commits under the caller's context, and
`trail.WithWriteObserver` attaches an observer to every store the recorder
builds.

## References

- [ADR-0027](./0027-pushout-forget-swiss-fadp.md) — the FADP reading this repository uses.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — generated record stores.
- [ADR-0112](./0112-dimensionstore-interned-facts-additive-memberships.md) — reference stampers, fail-fast, ordered flush.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — query-log capture and the SD7 stamp.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — play's read-only degradation (Update 2026-10-01).
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — `rowcas`.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — `Origin`, write-ahead.
