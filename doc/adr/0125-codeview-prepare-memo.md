---
type: adr
status: proposed
date: 2026-07-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0125: `codeview.Prepare*` memoises; `Build*` is the escape hatch

## Status

Proposed, pre-human-review. Nothing is built.

## Context

`codeview` exposes two names per language with a documented distinction:
`Build*` "re-tokenises every call (use for dynamic strings)", `Prepare*` is "a
documented alias for static / global content where the retained holder is
constructed once and reused across frames" (`doc.go`).

They are the same function. `PrepareJson` is `return build(jsonSpec, src)`;
`BuildJson` is `return build(jsonSpec, src)`. The distinction is carried
entirely by the doc comment, and it describes *caller* discipline — call it
once, keep the holder — not memoisation. Nothing enforces it and nothing warns.

The interning in `.Keep()` does not close the gap. `BuildRetained` interns the
**already-serialized** buffer (`unique.Make(string(raw))`,
`fffi2_typed_impl.go:170`), which is downstream of all the expensive work: the
highlighter has already run and the buffer has already been built. It also
materialises `string(raw)` — a full copy of the buffer — on every call, hit or
miss, because the key must exist before the probe. What interning buys is a
stable `retainedElementId` for the Rust-side cache and one shared allocation
per distinct result. It buys nothing on the Go side.

`helphost.go:424-430` states the opposite, and acts on it:

> `codeview.PrepareMarkdown` returns an interned retained holder
> (content-addressed via `unique.Handle`) so calling it per frame with the same
> source is amortised to a single hashmap probe past the first invocation — no
> need for HelpHost to maintain its own job cache.

It then calls `PrepareMarkdown(string(src))` inside a render function.

That comment is wrong, and it is not the only site. Callers divide cleanly:

- **Correct** — the holder is built once and stored: the widget demos'
  package-level vars, `leewaywidgets_demo`, and `markdown/visitor.go:172-178`,
  which calls `Prepare*` at *parse* time and keeps the result in the segment
  tree. This is the pattern the docs describe.
- **Per frame** — `helphost.go:430` (a whole document), `play_graph_view.go:142`
  (**once per node**, inside the node loop), `play_renderer.go:1482` and
  `:1532`.

The per-frame sites matter because SQL highlighting is not a lex.
`highlight.Highlight` runs `lexHighlight`, then a full `nanopass.Parse`, then a
CST walk for semantic refinement (`dsl_highlight.go:52-68`). Measured on a
development machine (`-benchtime` 200–500x); the ratios matter here, not the
absolute figures:

| call | source | per call | allocs |
| --- | --- | --- | --- |
| `BuildSql` | `SELECT count() FROM anchor.facts` (31 B) | 129 µs | 700 |
| `BuildSql` | typical aggregate (85 B) | 145 µs | 621 |
| `BuildSql` | 3-line CTE (180 B) | **3.5 ms** | 30 849 |
| `BuildMarkdown` | ~0.5 KB | 104 µs | 441 |
| `BuildMarkdown` | ~200 KB | 26 ms | 70 313 |
| `BuildJson` | ~312 KB | 93 µs | 34 |
| map probe by source | ~1 KB | 30–45 ns | 0 |
| map probe by source | ~312 KB | 6.8 µs | 0 |

A Graph tab showing three CTE nodes re-parses roughly **10.5 ms of SQL every
frame** — most of a 60 Hz budget, before anything is drawn.

A measurement caveat worth recording, because it nearly became the argument: a
first probe benchmark reported 8.6 TB/s, which is impossible.
`mapaccess2_faststr` has a single-bucket path that skips the hash and compares
string *pointers*, and a one-entry map with a loop-invariant key hits it. The
figures above use a 128-entry map; `freshPtr` (a `string(raw)` conversion per
iteration, helphost's actual shape) and `samePtr` then agree, which is the sign
the hash is really running.

## Decision

Make the documented distinction real.

- **`Build*`** — unchanged. Always re-tokenises. For one-shot work, and for
  callers that own a better key than the source text.
- **`Prepare*`** — memoised against a package-level, bounded, content-keyed LRU.

Every current call site becomes correct: the four per-frame sites by switching
to `Prepare*` (`helphost` already calls it), and the hoisted sites keep working
exactly as they do, since a memo hit is what they already achieve by holding the
var.

### SD1 — The key is the source text, not a hash of it

The key is `(language, lineWindow, src)` — a struct with a string field, so the
map hashes the source on every probe. At 45 GB/s that is 30 ns for a query and
6.8 µs for a 312 KB document, against 129 µs–26 ms to build. The memo is free at
every realistic size, so there is nothing to buy by keying on a digest.

An `xxh3` key was rejected despite being cheaper to store. A 64-bit collision is
vanishingly unlikely at LRU scale, but its consequence is a **silently wrong
render** — one document highlighted with another's spans. This repo does not
trade a correctness cliff for a constant factor, and a memo is exactly the place
where such a bug would be unreproducible.

`PrepareGoLines(src, first, last)` carries its window in the key; the same source
at two windows is two entries.

### SD2 — The bound is bytes, not entries

Entries range from a 31-byte query to a 312 KB document — four orders of
magnitude — so an entry-count bound expresses nothing about memory. The cache
holds a **byte budget** (proposed: 8 MB of source) and evicts oldest-first until
it fits.

Charged per entry: `2 × len(src)`, an estimate covering the key (a copy of the
source) and the value (the serialized buffer, which embeds the source plus its
sections). The estimate is deliberate — asking the holder for its exact size
would couple the cache to the wire layout.

The budget is comfortably larger than any single realistic entry, so no entry is
uncacheable and no entry can evict the whole cache. 8 MB against a 312 KB
worst-case document leaves room for ~25 of them; the real working set is a
handful of help docs and a graph's worth of queries.

`simplelru.LRU` (the unlocked variant of the `hashicorp/golang-lru/v2` already
in `go.mod`) is the substrate, so the probe and the byte accounting sit under one
lock rather than two.

### SD3 — Locked, and the build runs outside the lock

The memo must be thread-safe: `markdown.Parse` calls `Prepare*` at parse time
(`visitor.go:172-178`), and the retain-once idiom the package documents is a
package-level `var doc = markdown.Parse(...)`, which runs at init on whatever
goroutine gets there. The frame path being single-threaded does not cover it.

The lock is **not** held across `build()`. A 26 ms build under a package-global
mutex would stall every other caller:

```
lock → probe → unlock
build (unlocked)
lock → add, evict to budget → unlock
```

Two goroutines racing on the same uncached source both build it, and the second
`Add` replaces the first. That is wasted work, not a wrong result — the two
holders are interned to the same bytes anyway (§Context). Tolerating it is the
price of not serialising the expensive path.

### SD4 — `play_detail_rich` keeps `Build*` and its own cache

ADR-0123's `richCellCache` stays as it is, on `Build*`. It is not redundant with
this memo, and it is the case that justifies keeping `Build*`:

- It must exist regardless, for `markdown.Parse`'s segment tree and for decoded
  image pixels — neither of which `codeview` knows about.
- Its key is `(executed, row)`, an integer pair, so a probe is O(1) rather than a
  hash of a cell that may be a megabyte.

Caching a value twice, under two keys, at two layers, would be strictly worse
than either alone.

### SD5 — Deferred

- **The `unique.Make(string(raw))` copy** in `fffi2.BuildRetained` (§Context):
  the buffer is copied on every call, hit or miss, to form the intern key. Fixing
  it — probing an `xxh3`-keyed table before materialising the string — would help
  every retained widget, not just `codeview`. It is orthogonal, it is downstream
  of the tokenize this ADR is about, and it belongs to `fffi2`.
- **The SQL highlighter's cost.** 180 bytes producing 30 849 allocations and
  3.5 ms is worth its own investigation — the shape suggests ANTLR adaptive
  prediction with a DFA cache that is not warm across `Parse` calls (ADR-0084
  made that cache resettable at `Parse` seams). A 10× win there would still leave
  ~350 µs per node per frame, so it does not remove the need for this memo, and
  this memo does not remove the reason to look.
- **Eviction telemetry.** A hit-rate counter would tell us whether 8 MB is right.
  Not built until the budget is doubted.

## Alternatives

**Fix the four callers; leave `codeview` alone.** The serious alternative, and
defensible: four hoists, no new machinery, no global state, and it is what the
docs already ask for. Rejected because the cost is not any individual caller's
fault. `codeview.BuildSql(n.SQL)` inline reads perfectly naturally; the 129 µs
call and the 3.5 ms call are indistinguishable at the call site; and the names
imply the distinction is already handled. The API misled `helphost` badly enough
that someone wrote a paragraph justifying the mistake — that is an API defect,
not a caller defect. A fifth caller will make it again, and the hand-rolled cache
count is already six counting `play_detail_rich`. Fixing callers treats the
symptom.

**Delete `Prepare*` instead.** One honest name (`Build*` = re-tokenises) removes
the false promise at a stroke, and it is the smaller change. Rejected because it
removes the promise rather than keeping it: the four per-frame sites are still
per-frame afterwards, and each still needs a hand-rolled cache. It solves the
naming complaint and none of the milliseconds.

**A weak-valued cache** (`weak.Pointer`, the mechanism `unique` itself uses),
which would need no byte budget and no eviction policy. Rejected on a specific
failure: the callers this ADR exists for do **not** retain the holder between
frames — that is precisely their bug. With no strong referent, every entry would
die before the next frame and the cache would be empty exactly when it is needed.
Weak values cache for callers who already hold the value, i.e. the ones who do
not need a cache.

**Make the SQL highlighter cheap instead** and let the per-frame calls stand.
Rejected as an alternative but kept as separate work (§SD5): `semanticRefine`
genuinely needs the CST, so the parse cannot simply be deleted, and even a 10×
win leaves ~350 µs per node per frame. It is a reason to look, not a reason to
skip the memo.

## Consequences

`codeview` gains package-global mutable state, which it did not have. That is the
real cost of this decision: a pure function becomes a function with a cache, and
tests that assume purity would be measuring the cache instead. §Validation
therefore requires the memo be observable (hit/miss) from tests.

The names stop lying. `Build*` and `Prepare*` become a real choice with a real
consequence, which is what a reader already assumes they are.

`codeview` has **zero tests** today. This ADR should not land a cache into an
untested package; the benchmark that produced §Context's table lands with it as
the regression guard.

## Validation

- Unit: a hit returns the same holder; different languages with identical source
  do not collide; `PrepareGoLines` windows do not collide; `Build*` never
  consults the memo.
- Unit: eviction — exceeding the byte budget drops oldest-first and the accounting
  returns to under budget; an entry larger than the budget is still returned
  correctly.
- Concurrency: `-race` with N goroutines preparing overlapping sources; assert no
  torn state and that a build is never run under the lock (a build that blocks
  must not block a probe).
- Bench: the §Context table, as a guard — a `Prepare*` hit must stay in the
  tens-of-ns-to-µs band, three orders below `Build*`.
- Live: the Graph tab with a three-CTE query, before and after, read off the
  status bar's Go-time (the same instrument that showed ~1.9 ms/frame on a
  literal-only `SELECT` during ADR-0123's live run).
