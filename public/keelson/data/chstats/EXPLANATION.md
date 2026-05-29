---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chstats — Explanation

`chstats` is the ClickHouse-side counterpart to boxer's in-process
`tdigest`. It lets the boxenplot widget (and any future LV-aware
consumer) push the quantile sketch into a ClickHouse server when the
data already lives there, avoiding the round-trip of raw rows.

## Why pushdown

The Go-side `tdigest` is fast — 75 ns/op push, 30 ns/op query. But at
n > ~10⁶ the dominant cost is not the sketch; it's shipping the bytes.
For a typical 8-byte float per observation, 10⁶ rows = ~8 MiB over the
network for a single distribution; 100 grouped distributions over the
same dataset → 100× the egress. The sketch runs server-side anyway in
ClickHouse via the native `quantilesTDigest` aggregate — we just need
to ask for its output instead of the rows.

`chstats` is the bridge: it builds the SELECT-list fragment that asks
CH for the LV ladder, and decodes the result back into the same
`[]letterval.LVLevel` shape `boxenplot.Render` already consumes. The
widget code doesn't change; only the data-source layer does.

## Boundary

`chstats` is intentionally pure: three small functions, no I/O. It
stops at SQL string construction and result decoding. Callers
orchestrate the actual round-trip via whatever ClickHouse client
they already use:

- **`keelson/data/chclient`** — HTTP-based, native protocol via
  `clickhouse-go` v2; what the runtime services use.
- **`keelson/data/chlocalpool`** — local `clickhouse-local`
  subprocesses; cheap and offline, useful for tests and tools.
- **Direct `os/exec` of `clickhouse-local`** — what the integration
  test in this package uses.

Decoupling lets the same SQL flow through any of these. It also keeps
`chstats` free of the dependency closures (cgo, driver builds, HTTP
client knobs) that would otherwise force every LV consumer to pick a
particular CH client.

## The three primitives

### `LVQuantiles(maxDepth) []float64`

Returns the deduplicated quantile points needed for LV depths
1..maxDepth, in a stable order:

| Depth | Adds quantiles    |
|-------|-------------------|
| 1     | 0.5               |
| 2     | 0.25, 0.75        |
| 3     | 0.125, 0.875      |
| k     | 2⁻ᵏ, 1 − 2⁻ᵏ      |

The order is the order `BuildLVSelect` emits to ClickHouse, and the
order `LevelsFromArray` reads back. Stability matters: a future
maintainer cannot reorder the ladder without re-running every
integration test that depends on position.

### `BuildLVSelect(valueCol, maxDepth) string`

Emits the SELECT-list fragment:

```sql
quantilesTDigest(0.5, 0.25, 0.75, 0.125, 0.875, ...)(valueCol)
```

The caller splices this into a full SQL. Pair with `count() AS n` in
the same SELECT — `LevelsFromArray` needs the observation count to
populate `LVLevel.TailCount`. Typical shape:

```sql
SELECT quantilesTDigest(0.5, 0.25, 0.75)(latency_ms) AS lv_qs,
       count() AS n
FROM   requests
WHERE  ts >= now() - INTERVAL 1 HOUR;
```

`valueCol` is spliced verbatim — `chstats` does no SQL quoting. Pass
already-quoted identifiers (`"col with space"`) or fully-qualified
references (`t.latency_ms`) where needed.

### `LevelsFromArray(arr, n, maxDepth) []letterval.LVLevel`

Maps the `quantilesTDigest` result array (in `LVQuantiles` order)
plus `n` to the `[]LVLevel` shape `boxenplot.Render` consumes.

`TailCount` is populated **analytically** as `⌊n · 2⁻ᵏ⌋`, not from a
separate query. The LV depth was chosen precisely so each tail has
`MinTailCount` observations; the analytical count is exact-by-design.

A too-short `arr` leaves the corresponding LV bounds at zero rather
than panicking. This is defensive against partial / clipped result
arrays from a failed query.

## When NOT to use chstats

- **n < ~10⁶** — the round-trip overhead exceeds the sketch cost.
  Pull raw rows and use the Go-side `tdigest` directly.
- **Live dashboards with sub-second refresh** — even quantilesTDigest
  takes ms per group; for 100+ groups refresh latency dominates.
  Cache the LV array client-side and refresh on a budget.
- **Multi-shard, where each shard must return state for client-side
  merging** — `quantileTDigestState` returns a binary AggregateFunction
  blob suitable for `quantileTDigestMerge` on the client. `chstats`
  doesn't decode the binary state today; the architecture supports
  adding it as a parallel decoder when a real caller asks.

## Further reading

- ClickHouse `quantilesTDigest`:
  <https://clickhouse.com/docs/en/sql-reference/aggregate-functions/reference/quantiletdigest>
- Sibling: [`tdigest`](https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/tdigest) — the Go-side counterpart.
- Sibling: [`letterval`](https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/letterval) — the QuantileOracle / LVLevel contract this package fulfills via the array decoding path.
- ADR-0029 §SD8 (memory: `reference_clickhouse_local`) — local CH
  binary availability for offline tests.
