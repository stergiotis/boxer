---
type: adr
status: proposed
date: 2026-07-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0017: Regex explorer extraction hand-off — both engines, published as two joinable datasets

## Context

The regex explorer's extraction dead-ends inside its own window. The Preview tab knows
every match and capture group with byte offsets, computed locally by Go's `regexp`
([ADR-0054](./0054-regex-explorer-offset-authority.md)); the List tab knows what ClickHouse
returned from `extractAll` / `extractAllGroups`. You can look at both, but you cannot
*query* either, and you cannot compare them except by eye.

That last part matters more than it sounds. The app's own List tab already warns that the
two can disagree — `extractAll` returns capture group 1, **not** the full match, whenever
the pattern captures — so the surface that exists to predict ClickHouse hands the user a
caveat in a UI label and no way to check it against their own pattern.

Two capabilities landed since that gap opened, and together they close it:

- **Ad-hoc datasets** ([ADR-0134](./0134-adhoc-datasets.md)) — an app publishes an Arrow
  IPC stream over `adhoc.publish` and gets back a handle that resolves as
  `keelson('<handle>')` at the in-process introspection endpoint.
- **Launch requests** ([ADR-0135](./0135-app-launch-requests.md)) — an app opens another
  app's window with typed arguments over the audited `windowhost.open` subject;
  `launchcfg.PlayLaunch` seeds play's editor and can auto-run it.

A downstream consumer has already proven the pattern end to end, and `apps/adhocdemo`
dogfoods the publish half in-tree.

## Decision

We will publish **both** engines' extraction results as two ad-hoc datasets, side by side,
and open a play window seeded with a `FULL OUTER JOIN` over them.

This turns ADR-0054's Go-versus-ClickHouse duality from a caveat in a UI label into
something the user can `JOIN` — an SD1 tripwire in the large, over their own pattern rather
than over a fixed corpus.

### SD1 — Two datasets, published side by side

**`regex_matches`** — from Go's `FindAllStringSubmatchIndex`, the offset authority per
ADR-0054. One row per (match, group); group 0 is the full match:

```
match_idx  Int32   -- 0-based match ordinal
group_idx  Int32   -- 0 = whole match, k = capture group k
group_name String  -- (?P<name>…) name, or ''
text       String  -- the matched substring
start_byte Int32   -- -1 when the group did not participate
stop_byte  Int32
matched    UInt8   -- 1 when the group participated
```

**`regex_ch_extract`** — whatever `listLane` currently holds, keyed the same way so the two
join:

```
match_idx  Int32   -- ordinal within extractAll / extractAllGroups
group_idx  Int32   -- 0 = the extractAll element; k>=1 = extractAllGroups[match][k]
text       String
```

Both are encoded with `ipc.NewWriter` — Arrow IPC **stream** form, no file footer, which is
what ADR-0134 expects. (Note the contrast with the `chlocalbroker` `InputTables` path, which
takes the *file* form; the two are easy to confuse and fail differently.) Published via
`adhocdata.PublishRequest`, **reusing the prior handle on republish**, so one window holds
at most two datasets against the `MaxDatasets = 64` cap.

Publishing one dataset — Go's, or ClickHouse's — would have been simpler and would have
answered a different, less interesting question. The comparison *is* the feature.

### SD2 — The join key, and why `join_use_nulls = 1` is load-bearing

```sql
-- regex explorer: one extraction, both engines.
--   go_* — Go regexp (RE2), the byte-offset authority (ADR-0054)
--   ch_* — ClickHouse extractAll/extractAllGroups, the engine being predicted
-- A NULL on either side is a disagreement worth looking at. Note that
-- extractAll returns capture group 1 — not the full match — whenever the
-- pattern captures, so group_idx 0 lines up only for group-less patterns.
SELECT coalesce(g.match_idx, c.match_idx) AS match_idx,
       coalesce(g.group_idx, c.group_idx) AS group_idx,
       g.group_name,
       g.text AS go_text,
       c.text AS ch_text,
       g.start_byte, g.stop_byte
FROM keelson('<goHandle>') AS g
FULL OUTER JOIN keelson('<chHandle>') AS c
  ON g.match_idx = c.match_idx AND g.group_idx = c.group_idx
ORDER BY match_idx, group_idx
SETTINGS join_use_nulls = 1
```

Without `join_use_nulls = 1` a missing side comes back as `''` rather than `NULL`, which
reads as *agreement on an empty string* — the exact failure this surface exists to expose.

Two `keelson('…')` calls in one statement are already proven (`introspectengine`'s
encrypted test queries an ad-hoc handle and `env` in a single statement), and the expansion
is a nanopass rewrite
([`keelsonsql`](../../public/keelson/runtime/introspect/keelsonsql/keelsonsql.go)), so
nothing new is asked of the engine.

### SD3 — The degraded single-dataset path

If the CH lane holds nothing fresh — bus unwired, query in flight, pattern invalid — only
`regex_matches` is published and the seeded SQL degrades to a plain `SELECT` over it, with
a comment saying so. A partial result, clearly labelled, beats a failed click: the Go half
is always available (it needs no bus at all), and it is still worth querying.

### SD4 — `AppId` moves into `launchcfg`

play's `AppId` moves from `apps/play/app_register.go` into the leaf package
`apps/play/launchcfg`, with `play.AppId` retained as `const AppId = launchcfg.AppId`.

Without this, `regex_explorer` → `apps/play` would drag the whole play app — and its
registry-registering `init()` — into every host that links the `regexsummary` widget,
**silently registering the SQL playground as a side effect of using an inspector**. This is
the `appletcreatecfg.AppId` precedent, applied to play: the launch contract is the neutral
leaf both sides import.

Verified before the move: `apps/play` does not depend on `regex_explorer` (`go list -deps`),
and `keelson/runtime/app` does not depend on `launchcfg`, so there is no cycle. The existing
external `play.AppId` call site keeps compiling untouched.

### SD5 — Lifecycle, and the embedded retract gap

- Publishing and opening happen off the render thread. A click snapshots both result sets
  **on the render thread** and hands them to a goroutine (the imzero2 rule: workers never
  call `c.*`); status folds back under the existing `mu`, which already covers the
  "written by workers, read by the render thread" field group. A re-click while in flight is
  dropped.
- The manifest gains three `Pub` caps: `adhocdata.SubjectPublish`,
  `adhocdata.SubjectRetract`, `windowhost.OpenSubject`.
- `AppInstance.Unmount` retracts both handles alongside the existing `cancelQueries()`.

**The gap, stated plainly:** `EmbeddedApp` has no teardown hook — `regexsummary` keeps
embedded explorers alive for the widget's lifetime — so an embedded explorer that published
would hold its handles until process exit. We add `EmbeddedApp.Close()` for hosts that can
call it, and record that nothing currently calls it. The exposure is bounded by handle
reuse (at most two per instance) and by the ephemeral store. In practice the embedded case
degrades earlier and more visibly: the host's bus client carries the *host app's* manifest
caps, which will not include `adhoc.publish`, so the publish is refused with a clear status
line rather than silently doing nothing.

### SD6 — Where the affordance lives

A single row outside the tab `DockArea` in `renderBody` — neutral ground, since the Go half
comes from the Preview tab and the CH half from the List tab. Putting it inside either tab
would imply that tab owns the hand-off.

**Above** the `DockArea`, not below it. The first cut put it below, and the
`regex-explorer-highlighting` demo capture showed why that does not work: the `DockArea`
takes the rest of the body's height, so a row emitted after it lands off the bottom of the
window and the button cannot be clicked at all.

## Alternatives

- **Publish only the Go side.** Simpler, and the Go side needs no bus. But it answers
  "what did Go find", which the Preview tab already shows; the disagreement is the part
  worth querying.
- **Publish one pre-joined dataset, computed in Go.** Moves the join off the engine and into
  the app, so the user cannot re-slice it, and it would bake in one interpretation of how
  the two engines' ordinals line up — exactly the thing under examination.
- **Render a comparison table in the explorer itself.** No new capability needed, but it is
  a fixed view: no filtering, no aggregation, no joining against anything else, and one more
  bespoke table widget to maintain.
- **`INNER JOIN` instead of `FULL OUTER`.** Would hide every row where the two engines
  disagree about *existence* — the most interesting disagreement there is.
- **Import `apps/play` directly for `AppId`.** Rejected per SD4: it registers the SQL
  playground as a side effect of linking an inspector widget.

## Consequences

### Positive

- The Go/ClickHouse duality becomes queryable over the user's own pattern, not a fixed
  corpus.
- `play.AppId` becomes importable from a leaf, so any future requester avoids the same
  transitive-`init()` trap.
- Reuses two capabilities as-is; nothing new is asked of the bus, the engine, or the codec.

### Negative

- An embedded explorer that publishes has no retract path (SD5). Bounded and recorded, not
  fixed.
- The explorer now depends on `adhocdata` and `windowhost` — a wider blast radius for a demo
  app than it had.
- Two datasets per window against a 64-dataset cap; a user opening many explorer windows and
  clicking in each consumes the cap faster than one dataset would.

### Neutral

- The seeded SQL is a starting point, not a contract: the user is expected to edit it. Its
  exact text is pinned by a test only so that changes to it are deliberate.
- Group-numbering parity between the two engines is asserted by the join, not proven by it —
  a mismatch shows up as NULLs for the reader to interpret.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0054](./0054-regex-explorer-offset-authority.md) — Go `regexp` as the offset
  authority, and the SD1 engine-fidelity tripwire this generalises.
- [ADR-0134](./0134-adhoc-datasets.md) — ad-hoc datasets, the Arrow IPC stream form, handle
  reuse, and the `MaxDatasets` cap.
- [ADR-0135](./0135-app-launch-requests.md) — the audited `windowhost.open` subject and
  typed launch configs.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the `appletcreatecfg` leaf-package
  precedent SD4 follows.
- [ADR-0015](./0015-regex-pattern-syntax-highlighting.md) — the other half of this
  milestone.
- [regex-explorer-m3-plan](../explanation/regex-explorer-m3-plan.md) — the survey this ADR
  freezes.
