---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Where the tax actually comes from

Follow-up inspection of the same loaded tables, prompted by the headline
numbers in [`results.md`](./results.md) looking worse than the model should
imply. It corrects one attribution in that document and isolates the dominant
cost, which turns out **not** to be the storage model.

## Storage: the leeway machinery is 8 % of the table

Arm B's 147 MB, by column role:

| Bucket | Compressed | Share |
| --- | --- | --- |
| Shredded values (`:val:`) | 114.34 MiB | 81.5 % |
| Plain identity / time (`id:`, `ts:`, `lc:`) | 14.59 MiB | 10.4 % |
| Membership identity — the JSON paths (`mrhp`, `lmr`, `lr`, `hr`) | 8.31 MiB | 5.9 % |
| Support columns (`*card`, `len`) | 3.01 MiB | 2.1 % |

**Correction to `results.md` § Size.** That section attributed the 3.68×
uncompressed inflation to "every value carrying its JSON path as a string in
the parameter column". That is not where the bulk is: the path columns are
5.9 % of the compressed table. The uncompressed inflation is mostly the
*support* columns — `lmrcard`, `lrcard`, `hrcard`, `len` are each 8 bytes per
attribute per column and there are several per section, so they dominate the
uncompressed figure while compressing 100–220× and costing almost nothing on
disk. The membership machinery as a whole is 8 % of arm B's disk footprint.

Of the plain-identity 14.59 MiB, **12.61 MiB is `id:naturalKey`** — a 16-byte
blake3 of the DID that this trial's ingester writes per row. Arm A has no
equivalent. It is a near-incompressible hash, and it alone is 8.6 % of arm B's
total. That is the ingester's choice, not the facts model's requirement.

## Why the values compress worse than arm A's single JSON column

Arm A: one physical column, 97.35 MiB compressed from 643 MiB — **6.6×**.
Arm B's string payload: 110 MiB from 291 MiB — **2.6×**.

Two candidate causes, both measured on the same bytes (`jsonbench_probe`):

| Layout | Compressed | Ratio |
| --- | --- | --- |
| Mixed in one array column, `ORDER BY ts` (= arm B today) | 110.10 MiB | 2.65 |
| Mixed, `ORDER BY did` | 102.10 MiB | 3.23 |
| One column per JSON path, `ORDER BY ts` | 101.44 MiB | 3.07 |
| One column per path, `ORDER BY did` | 89.87 MiB | 3.47 |

Splitting per path — the thing that looked like the obvious culprit, since one
`stringArray` column holds CIDs, DIDs, URIs, ISO timestamps, revs and post
text together — buys only **8 %**. Sorting by `did` buys **10 %**. Together
they recover 18 %, taking the string payload from 110 MiB to 90 MiB. Neither
explains the gap; arm A's advantage is mostly its JSON codec, and the largest
paths (`/commit/cid` 54 MiB, `/commit/record/subject/uri` 33 MiB, `/did`
30 MiB) are base32 hashes with genuinely little redundancy to find.

The `ORDER BY did` row is worth noting for arm D: re-keying helps compression
independently of whatever it does for pruning.

## Latency and memory: it is the path reconstruction, not the model

Q1 against arm B, two formulations reading the *same* column:

| Formulation | Time | Peak memory |
| --- | --- | --- |
| `arrayFirst` over a reconstructed path vector (what `queries-facts.sql` does) | 0.083 s | 168 MB |
| `v[2]` — a fixed index into the same value column | 0.012 s | 21 MB |
| *(arm A, for reference)* | 0.005 s | 2.5 MB |

**The membership path reconstruction costs 7× the time and 8× the memory.**
Strip it and arm B's Q1 is within **2.4×** of arm A rather than 13.8×.

This relocates the headline. The trial reported 9–15× latency and up to 76×
memory as the cost of holding the corpus in the facts model. Most of that is
not the storage model — it is the cost of *asking for a value by its path in
SQL*, which forces every query to materialise three whole array columns and
rebuild a per-attribute path vector before it can evaluate one predicate.

The fixed-index form is not a usable answer — attribute position is an
accident of ingest order, not a contract. But it bounds the model's intrinsic
cost, and it says where the leverage is: a read path that resolves
path → position once per part instead of once per row. `leeway-read-access-codegen`
generates exactly that shape for Go consumers (`MembershipPacks`,
"Accelerators (O(1) lookups)" per the leeway SDK notes); nothing equivalent
exists for SQL consumers, and this trial reached ClickHouse directly. That gap
is the finding worth carrying forward, and it is a stronger candidate for arm
D's companion than re-keying alone.

## Reproducing

The probe tables are left in the `jsonbench_probe` database. Drop with
`DROP DATABASE jsonbench_probe`.
