---
type: explanation
audience: contributor relying on the watchbill claim, or building another coordination primitive on ClickHouse
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
---

# What the watchbill claim guarantees

The watchbill claims a job with one conditional lightweight `UPDATE` on one
row and reads the row back to learn whether it won
([ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md) §SD3). This page
states what that buys and what it does not, in the vocabulary of Jepsen's
[consistency models](https://jepsen.io/consistency/models) and
[phenomena](https://jepsen.io/consistency/phenomena), so that a reader
deciding whether to build on the same primitive — or to reach for a
transaction it does not have — has the answer in one place. The measurements
behind every line are in
[the ClickHouse primitives note](../adr-background-work/watchbill-clickhouse-primitives.md);
this page adds no figure and quotes none.

## The operation, and the conditions it holds under

One statement: `UPDATE <job table> SET state, workerRun, attempt WHERE id = ?
AND state = 'queued' AND runAfter <= now`, with `update_parallel_mode = 'sync'`
stated on the statement, followed by a `SELECT` of the row by the same client.
The row's key is immutable; the table carries the block-position columns the
lightweight update needs; expiry is a lightweight `DELETE`; the only `ALTER`
is a `MODIFY SETTING` at provisioning. These are the conditions. The
guarantees below hold **only** with all of them and on **one server**; a
replicated table or a second server is a different system, and the claim on
it is unmeasured.

## Guarantees, by Jepsen's models

Jepsen separates **single-object** models (a history of operations on one
register) from **transactional** models (multi-object transactions). The
claim lives in the first family.

| Scope | Model | Holds? | Why |
| --- | --- | --- | --- |
| one row, its conditional updates | **Linearizable** | yes, under the conditions | `sync` mode serialises every update on the table, so a guarded update on one key is a compare-and-set: of N concurrent claims exactly one changes the row, and the read-back tells each which it was. Measured with twenty claimants and no double in hundreds of rounds |
| one row, one client's statements | **Read Your Writes**, **Monotonic Reads**, **Monotonic Writes** | yes | the executor runs a client's statements in order over one connection, and a statement sees every part committed before it began, the client's own included |
| one row, across clients | **Sequential** | not claimed | two clients' reads of a row are each consistent with commit order, but nothing ties one client's read to another's write in real time except the update path itself; only the writes are linearizable, not an arbitrary read |
| many rows | **Read Committed** at most, per statement | per statement only | a statement sees only committed parts, so there is no dirty read; but two statements are two histories |
| the job row and its event row | **none** | no transaction exists | the event is a second statement after the update; a crash between the two leaves a claim without its event |
| the table under a heavyweight mutation | **none** | no | measured: a default-mode `DELETE` racing the claim let two claims win, the second evaluated against a snapshot from before the first |

So the honest label is: **a linearizable compare-and-set on a single row, on
a single server, with a fenced set of statements, and nothing above the
row.** Nothing in Jepsen's transactional column — not Read Uncommitted —
applies, because there are no multi-object transactions to grade; the
per-statement read behaviour is *like* Read Committed, but each statement is
its own transaction, so Repeatable Read, Snapshot Isolation and above are not
on offer.

## Phenomena, by Jepsen's list

| Phenomenon | On the claim path | Elsewhere on the table |
| --- | --- | --- |
| **P4 Lost Update** | prevented for the guarded columns, by serialised updates | **possible** the moment a heavyweight mutation runs beside an update — the one measured failure; and possible between the job row and the event row taken together, since they are not one write |
| **P0 Dirty Write**, **P1 Dirty Read**, **G1a Aborted Read** | prevented: a part is visible only once committed, and there is no uncommitted state to read | same |
| **G1b Intermediate Read** | not applicable to one statement; **possible** across the update and its event: a reader may see the row `running` before its `running` event exists | same shape for every transition |
| **P2 Non-Repeatable Read**, **P3 Phantom**, **A5A Read Skew** | **possible** across any two statements — the queue read and the claim are two statements, which is exactly why the claim guards on state again | possible |
| **A5B Write Skew** | not applicable: no two-row invariant is checked by two transactions; each guard is on the row it writes | not applicable |
| **Stale Read** | possible for a reader that is not the writer, bounded by commit; the worker never acts on a read it did not guard | possible |
| **Lost Write** (durability) | **possible after a power loss**: ClickHouse does not fsync a part on insert by default, so an acknowledged patch part can be lost with the page cache. At-least-once delivery is what tolerates it; a job can be forgotten, never run twice because of it | same |

The phenomena the design *relies on* being possible are the two-statement
ones: the worker re-reads and re-guards rather than assuming a queue read is
still true, and the event is written only after the update was read back as
won, so a missing event is always "the transition happened and its trail did
not", never the reverse.

## What the measurement is, and is not

Twenty claimants on one machine, one server, hundreds of rounds, no double
under the conditions and one double outside them. That is evidence for the
model, and it is what the integration lane keeps checking. It is not a
proof: the serialising property is the server's, and a history-checking test
in Jepsen's own style (Elle over recorded operations) would be the way to
*claim* linearizability rather than observe it. Nothing in this repository
does that.

## What follows for a second consumer

- A primitive built the same way inherits the same model and the same
  conditions. The conditions are the part that is easy to lose in a copy,
  which is why they are carried by one package —
  [`recordstore/rowcas`](../../public/storage/recordstore/rowcas) — rather
  than repeated as SQL fragments.
- Anything that needs two rows to move together needs a design that does not
  exist here: a single row that carries both, or an idempotent second step
  the reader tolerates missing, as the event row is.
- A fact that is only ever appended — a presence row, a transition — needs
  none of this; the plain insert path carries it, and the runtime heartbeat
  answers liveness (ADR-0223 §SD4).
