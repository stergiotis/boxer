---
type: adr
status: proposed
date: 2026-11-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0387: What a binary does when a stored table is older or newer than its schema

## Context

A generated record store creates its table with `CREATE TABLE IF NOT
EXISTS`, which leaves an existing table exactly as it is. So every change
to a store's generated columns meets the old shape at the next start of
every deployment that already holds the table, and two builds sharing a
server meet each other's shape all the time.

Before this decision the generated stores read `SELECT *` and decoded at
the readers' default column indices — by position. `VerifySchema`
demanded the exact column count and names in order and compared no types.
An additive column therefore broke a new binary on an old table and an
old binary on a migrated table, so no rollback and no two versions on one
server. What a process did about a failed check differed per store: the
state table fell back to memory with a warning, the journal and the
snapshot store returned errors, the metrics tee was fatal, the query-run
service checked a superset by name, and the facts table at host boot, the
watchbill tables and the trail were not checked, so a watchbill worker
over an older table failed every poll. No table carried any record of the
schema that created it, and migrations were hand-transcribed `ALTER`s in
draft recipes.

Inserts had a related gap. A failed `Flush` kept its records and resent
them under a new batch id, with no deduplication token, into plain
MergeTree tables: an insert that landed while the client saw a timeout
was written twice on retry.

Column names carry leeway's encoding and value aspects
([ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md)), so tuning
a codec or an aspect renames a column without changing what it holds.

## Decision

**SD1 — Reads go by name.** Every generated read selects the generated
columns explicitly, and the decode binds the read-access readers to the
result's columns by name before loading a record. A column the store does
not know is read past; a column the store reads that the table lacks
fails the read.

**SD2 — A table gets a verdict, not a pass/fail.** A store's contract is
its generated column list (names, ClickHouse types, codecs, parsed out of
the DDL the generator composes) and its stamp. A live table, read from
`system.tables` and `system.columns`, is judged against it:

| Verdict | When | A store using the table |
|---|---|---|
| `ok` | every generated column present by name with a matching type; columns beside them are listed as extra | reads and writes |
| `migratable` | generated columns missing, or present under a name that differs only in leeway aspects (same type) | fails the reads that name a missing column |
| `incompatible` | a column the store reads has another type, or is MATERIALIZED / ALIAS / EPHEMERAL | fails |
| `absent` | no table | creates it, when it provisions |
| `unchecked` | the shape could not be read | — |

A relation that computes its rows when read — a view, a Merge over other
tables — is also read once for no row, since the catalogue lists its
columns whether or not a `SELECT` from it is accepted; a refused read is
`incompatible` with the server's refusal as the reason
([ADR-0402](./0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md),
update of 2026-11-22).

Types are compared with the whitespace outside literals removed; a
difference only in `LowCardinality` wrappers is a note, since the Arrow
output the stores ask for decodes both alike. Codecs are not compared.
`VerifySchema` keeps its name and signature and returns the verdict as an
error unless it is `ok`; `JudgeSchema` and `SchemaContract` stand beside
it.

**SD3 — One boot-time policy.** A process runs one check over the stored
tables it uses, keeps the verdicts on a board served as
`keelson('schema_tables')`, and logs one line per table whose verdict is
not `ok`. The check reports; it does not change which backend a process
uses. `BOXER_SCHEMA_STRICT` makes any verdict other than `ok` a boot
failure. The host checks the facts table — which covers the facts-bound
stores, whose generated columns a test pins to it —, the state table and,
where their services started, the journal's tables and the watchbill
pair; a watchbill worker is not started over a job or event table whose
verdict is not `ok`.

**SD4 — The mechanical part of a migration is derived, never applied at
boot.** For a `migratable` table the statements come from the contract:
`ADD COLUMN IF NOT EXISTS … AFTER` the generated predecessor, and for an
aspect-only rename `RENAME COLUMN` plus `MODIFY COLUMN … CODEC(…)`. A
plain MergeTree table without a deduplication window gets `MODIFY
SETTING`, and the table is stamped last. Anything else — a type change,
an absent table, a rename of a column a key, index, TTL or expression
names — is refused with a pointer to `doc/migration/`. `boxer schema plan`
prints the statements and changes nothing; `boxer schema apply --yes`
runs them.

**SD5 — Tables carry a stamp.** A generated store's `CREATE TABLE` ends
with a `COMMENT` naming the table kind, a digest of the generated column
list, the leeway naming generation and a digest of the membership-id
assignment. The verdict reads it to say whether an older or a newer
schema wrote the table; a table without one is judged by its columns
alone. `schema apply` restamps with `MODIFY COMMENT`, and leaves a
comment no store wrote as it is. The stamp follows the leeway catalog's
view stamps: a string a reader compares, no version marker beyond its
prefix.

**SD6 — An insert's retry is idempotent where the table allows it.** A
store keeps each unacknowledged insert as its own pending batch and
retries it alone, ahead of rows buffered since, under the batch id of its
first attempt, which rides as `insert_deduplication_token`. Newly created
plain MergeTree tables keep `non_replicated_deduplication_window = 1000`.
A table created without the setting ignores the token: retries there can
still duplicate until `schema apply` adds the window. Inserts go through
the per-insert settings seam; an executor without it logs once that the
token is not sent.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| generated store reads | `SELECT <generated columns>` and by-name reader binding | every `*_store.out.go`, regenerated |
| `VerifySchema` semantics | exact positional check → verdict as error | callers keep the signature; extra columns now pass |
| generated DDL | `COMMENT` stamp; `non_replicated_deduplication_window` on plain MergeTree | every `*_ddl_clickhouse.out.sql`, regenerated |
| generated `Flush` | per-insert pending batches, stable batch id, deduplication token | write observers are told each insert's rows under its own id |
| `recordstore` | `ColumnSpec`, `TableContract`, `SchemaVerdict`, `TableStamp`, `PendingBatch`, `InsertSettings.DeduplicationToken` | additive |
| env registry | `BOXER_SCHEMA_STRICT` | `doc/env-vars.md` |
| CLI | `boxer schema plan|apply` | — |
| introspection | `keelson('schema_tables')` | — |

## Alternatives

- **Keep the positional decode and make the check stricter.** Rejected:
  it keeps every additive change breaking in both directions, which rules
  out rollback and two builds on one server.
- **Auto-apply additive migrations at boot.** Rejected: an `ALTER` on a
  shared server is an operator's decision, and a host that alters the
  tables of another build on start makes that decision for them.
- **A schema-version table.** Rejected: a counter would need a registry of
  generations nobody maintains; the column digest is derived from what the
  table is, and the structural verdict holds for an unstamped table.
- **Reuse `jackstay.Judge` for the verdict.** Its verdict compares two
  live tables, sorting keys and engines included, and has no notion of an
  aspect rename; the plan reuses jackstay's statement renderers instead.
- **One deduplication token per `Flush` over pending and new rows.**
  Rejected: ClickHouse drops a later insert carrying a token it has seen,
  so new rows riding a retried token would be lost — the per-insert
  pending batches avoid it.

## Consequences

### Positive

- An additive change no longer breaks either direction: a new binary
  reports the old table as `migratable` and fails only the reads of the
  new columns; an old binary reads past them.
- An operator sees every table's verdict in one place and gets the
  statements for the mechanical cases.

### Negative

- Every read carries the projection list, a few kilobytes of SQL for the
  widest tables.
- A table with a deduplication window drops an insert whose blocks it has
  seen within the window even without a token; a store over an in-tree
  executor sends one with every insert, but another writer inserting
  identical blocks into the same table — or a store over an executor
  without the per-insert settings seam — would be deduplicated.
- Asynchronous inserts deduplicate under their own setting; a store
  configured for them does not get SD6.
- An executor that answers no catalog read — a test fake, an in-memory
  backend — makes every table judge absent, and a check that requires an
  ok verdict refuses to open the store. `recordstoretest.CatalogExecutor`
  answers the two reads from the `CREATE TABLE` statements it was sent.
- The per-insert settings seam is a second insert method: a wrapper that
  embeds a concrete executor and overrides `InsertArrow` alone is bypassed,
  because the store calls the promoted `InsertArrowWith`.
  `recordstoretest.HookExecutor` routes both seams through one hook.

### Neutral

- `VerifySchema` passes tables it used to refuse (extra stored columns),
  which is the point of SD1.
- The snapshot store's tables are created by their own composer and carry
  no stamp; they are judged by columns. The facts table's composer stamps
  it with its columns and its layout
  ([ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)).

## Migration — Tier 1

Nothing to migrate: existing tables keep working as they are. `boxer
schema plan` proposes the window and the stamp for them; applying it is
the operator's choice.

## Verification plan — Tier 1

- **Lane.** Default `go test` (clickhouse-local where on PATH, skipping
  otherwise) for the store round trips, the verdicts, the deduplicated
  retry and the stamp; the `integration` lane for `schema apply` on
  clickhouse-local.
- **What would fail.** A store that read by position again fails the
  extra-column round trip; a retry under a new token fails the
  duplicated-rows count; a facts-bound store regenerated against another
  facts shape fails the host's shared-columns test.
- **Gap.** Nothing has been run against a server; type spellings the
  server normalises beyond whitespace would read as incompatible until
  the comparison learns them.

## Choices for review

- The verdict is called `migratable` rather than additive, because it also
  covers aspect-only renames.
- Under the non-strict default the policy is report-only: the state table
  still falls back to memory, the journal still falls back to memory, the
  metrics tee is still fatal on a non-ok verdict — each now judged by the
  same verdict. Only the watchbill worker changed: it is no longer
  started over a table it cannot poll.
- The deduplication window is 1000 blocks.
- The facts table's `CREATE` was left to the facts layout;
  [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)
  stamps it and gives it the window.
- Codec differences are not part of the verdict.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the generated record store.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — aspects in column names.
- [ADR-0259](./0259-jackstay-guided-clickhouse-to-clickhouse-sync.md) — jackstay's verdict and statement renderers.
- [ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md) — the batch id.
- [doc/migration/README.md](../migration/README.md) — recipes for what the plan refuses.
