---
type: adr
status: proposed
date: 2026-08-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0202: retire arrow-go's parquet packages

## Context

boxer used `arrow-go/v18/parquet` in exactly three places, all on the *write*
side. Nothing in the tree has ever read a parquet file through it:

| Site | What it did |
| --- | --- |
| `dml.WriteArrowRecords` | a second sink parameter, `w2 *pqarrow.FileWriter`, beside the Arrow IPC one |
| `leeway dml example convert` | `--outputFormat=parquet`, one of two values |
| `jsonbench jsonmap ingest` | `--parquet-out`, the [second-substrate trial](../trials/leeway-second-substrate/README.md)'s arm W |

The carrying cost is four modules, and the interesting one is not obvious from
the import path:

| Module | Reached via |
| --- | --- |
| `github.com/apache/thrift` | parquet's metadata codec |
| `github.com/andybalholm/brotli` | `parquet/compress` |
| `google.golang.org/grpc` | `pqarrow` → `arrow/compute` → `arrow/flight` |
| `google.golang.org/genproto/googleapis/rpc` | `grpc` |

`pqarrow` is the Arrow↔parquet bridge, and it imports `arrow/compute`, which
imports `arrow/flight` — an Arrow RPC service. So writing a file linked an RPC
stack. That is the fact that turned "unused dependency" into a decision worth
recording rather than a tidy-up.

Parquet *interop* is not affected by any of this, which is what makes the
removal cheap: every parquet file this repository has produced or consumed for
measurement came from ClickHouse's own `FORMAT Parquet`, not from arrow-go. The
one trial that used the Go writer did so to prove a point about the writer
(below), not to move data.

## Decision

We remove the three parquet call sites and, with them, the two CLI surfaces
they backed. Arrow IPC becomes the only file format leeway's DML helper writes.
Parquet as an interchange format stays available through ClickHouse.

### SD1 — the CLI flags go with the format, not just the imports

`--outputFormat` had two values; with parquet gone it would be a one-value enum
with a validating callback. Deleting it is smaller than keeping scaffolding for
a format nobody asked for. `jsonbench`'s `--parquet-out` goes the same way, and
`--database` becomes `Required` — which is what its hand-written "one of
`--database` or `--parquet-out` is required" check stood in for.

### SD2 — no sink interface is introduced at the seam

`WriteArrowRecords` dispatched on which of two writer pointers was non-nil. The
tempting refactor is to replace that with a sink interface so a third format
can slot in later. Rejected: an interface with one implementation is a guess
about a consumer that does not exist, and the concrete `*ipc.FileWriter`
parameter reads better than an abstraction over one thing.

### SD3 — arm W is not preserved; the trial records that it is manual again

The second-substrate trial's arm W asked whether *leeway's own writer* is
neutral, not just its layout, and `--parquet-out` was how it asked. Its numbers
stand as measured. Re-running it now means reinstating the flag — the same
uncommitted-change footing the 2026-08-07 run was already on, per that trial's
logbook. No script under `doc/trials/` invokes the flag, so nothing scripted
breaks; the trial's M3 entry says so directly rather than leaving a reader to
discover it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `dml.WriteArrowRecords` (exported, under `public/`) | reshaped — `w2 *pqarrow.FileWriter` parameter removed | its two in-tree call sites |
| `leeway dml example convert` | removed — `--outputFormat` and its enum | nothing; `--compression` still selects zstd against uncompressed for IPC |
| `jsonbench jsonmap ingest` | removed — `--parquet-out`, `--parquet-compression`; `--database` now `Required` | the trial's arm W (SD3) |
| `go.mod` | removed — four modules | `go.sum` |
| `THIRD_PARTY_NOTICES.md` §3.2 | edited — `apache/thrift` dropped from the Apache-2.0 NOTICE list | nothing |

## Alternatives

- **O1 — keep `pqarrow`, drop `arrow/compute` and `arrow/flight`.** Rejected:
  `pqarrow` imports compute directly, so severing it means vendoring arrow-go.
- **O2 — keep the writer behind a build tag.** Rejected: a tag that nothing in
  CI sets is an untested code path, and the modules return the moment anyone
  sets it.
- **O3 — swap to a standalone parquet writer with a smaller tail.** Rejected on
  the same ground as O2 — the cheapest writer is still carrying cost for a
  capability with no consumer.
- **O4 — remove (chosen).** Four modules leave; nothing in the tree loses a
  capability it was using.

## Consequences

### Positive

- Four modules leave the build graph, including a gRPC stack that no boxer code
  calls. Fewer modules to audit under `govulncheck` / `osv-scanner`, and fewer
  to carry in the airgapped bundle.
- `WriteArrowRecords` has one sink instead of a two-pointer nil-dispatch.

### Negative

- Producing parquet from Go, in-process, now needs the dependency back. Arm W
  is the only known case (SD3).

### Neutral

- Reading parquet is unchanged, because it never went through arrow-go:
  `clickhouse-local`'s `FORMAT` surface — the data-conversion path
  [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) reasons about, parquet in
  and JSONEachRow out — is a ClickHouse capability and is untouched.
- `google.golang.org/protobuf` stays a direct dependency. It is imported by
  `public/app/commands/protogen`, not reached through parquet, so only the gRPC
  half of that tail leaves.
- Background work that counted this module graph keeps its numbers as written.
  [ADR-0173](./0173-code-volume-self-inspection.md) (proposed) uses
  `andybalholm/brotli` as its worked example of third-party lines that are data
  rather than logic; that row will be absent from the next code-volume run.

## Migration — Tier 1

- **Breaks.** `dml.WriteArrowRecords` drops its fourth parameter. The two CLI
  flag sets stop being accepted.
- **Path.** A caller deletes the trailing `nil` or writer argument. There is no
  shim: a caller that was passing a real `*pqarrow.FileWriter` has no
  replacement and needs the dependency reinstated.
- **Regeneration.** None. No generator, IDL, or wire format is involved.
- **Old shape.** Removed outright. No deprecation window — both call sites were
  in-tree and moved in the same commit.

## Verification plan — Tier 1

- **Lane.** Default `go build` / `go test` with the repo tags, plus
  `go mod tidy --diff` and `scripts/ci/lint.sh`.
- **What would fail.** A reintroduced import shows up as drift in
  `go mod tidy --diff` — the four modules return to `go.mod` together. The
  direct check is that
  `go list -deps -tags="$(cat ./tags)" ./...` names no package under
  `arrow-go/v18/parquet`. Note that `go list -m all` still reports all four
  modules, because it answers over the module graph and other dependencies
  require them in their own manifests; only `-deps` answers over the build.
- **Gap.** No gate asserts the absence. Nothing stops a future import beyond
  the tidy diff being noticed in review, and that is accepted — the cost of a
  re-import is visible in `go.mod`, which is where it would be argued anyway.

## Status

Proposed — awaiting review by the code owner.

The removal landed first, in `249f2bd4`, and this ADR was written after it
rather than before. That is the wrong order for a Tier-1 surface; it is
recorded here rather than smoothed over. Measured on execution: `go build` and
the full `go test` lane green with the repo tags, `go vet` and `gofmt` clean,
`go mod tidy --diff` clean, capslock green, doclint clean on both edited
documents. `lint.sh` loses one errcheck warning — the removed `defer
w2.Close()` — and gains none.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0194](./0194-retire-egui-snarl-binding.md), [ADR-0195](./0195-retire-puffin-egui-dependency.md)
  — the two prior dependency retirements this follows in shape.
- [doc/trials/leeway-second-substrate/](../trials/leeway-second-substrate/README.md)
  — arm W, the one consumer of the removed writer (SD3).
