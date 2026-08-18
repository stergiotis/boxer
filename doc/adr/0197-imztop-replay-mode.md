---
type: adr
status: proposed
date: 2026-08-18
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0197: imztop replay — a second bundle source, not a second publisher

## Context

`imztop` shows the last ten minutes and nothing before that. Everything older
than its sliding window is gone from the process, and until recently it was
gone from the system too.

That changed with [ADR-0184](./0184-sysmetrics-persistence-tee.md). The
persistence tee ADR-0090 §SD9 reserved as phase P5 is built: with
`sysmetricsd --tee`, every bundle the metric plane carries is written to
`boxer.facts` through a generated record store, as thirteen append-shaped kinds
keyed per `(host, domain)`. The store's own comment on that key states the
consequence — *"Latest is the current state and Replay is the history"*
([`sysmtee/rows.go`](../../public/keelson/runtime/sysmtee/rows.go)).

So the history exists, the read verb exists, and nothing reads it. Three
properties of what shipped make the read cheaper than it would otherwise be:

- **`SysmetricsStore.Replay(ctx, key, fromOrder, ReplayOpts{To, Limit})`** is
  generated, returns `iter.Seq2[*SysmetricsEntity, error]` ascending by order,
  and the entity carries typed `option.Option[SysCpu]` and friends. Rebuilding
  a domain snapshot is the mechanical inverse of `sysmtee/rows.go`, not a
  decode of an opaque payload.
- **One timestamp per bundle.** [`sysmtee.go`](../../public/keelson/runtime/sysmtee/sysmtee.go)
  takes `sampleTime(snap)` once and stamps every kind of that bundle with it,
  so re-assembling a bundle from thirteen keys is an exact merge on the order
  column rather than a tolerance join.
- **Rates were stored as rates.** `ReadBytesPerSec` / `RxBytesPerSec` are
  computed by the collector before publication, so replay needs no
  re-derivation from counter deltas and a gap in stored history cannot
  manufacture a rate spike.

ADR-0020 recorded the want and deferred it twice: §SD14 — *"No timeline
scrubbing; that is post-M5 if at all"* — and its non-goals — *"scrubbing back
through the 10-minute history is a follow-on if users ask"*. This is that
follow-on, now that there is more than ten minutes to scrub through.

**What makes it a decision rather than a task** is that imztop is the one app
in the tree with no capability at all. ADR-0090 turned it into a pure
subscriber: no `/proc`, no filesystem, no dialled connection, one declared
`sysmetrics.>` subscribe. Reading `boxer.facts` puts a database client in that
process. Whether the read belongs there, on the bus, or outside the app is the
question this ADR answers.

## Design space (QOC)

**Criteria.** C1 fidelity of the replayed view against the live one; C2 what
the control surface costs (seek, speed, step); C3 blast radius on other
consumers of the metric plane; C4 imztop's capability surface; C5 size of the
first shippable cut.

- **O1 — replay publishes onto the metric plane.** A replayer reads the store
  and publishes bundles on `sysmetrics.{host}.bundle`; imztop needs no change
  at all.
- **O2 — replay on its own subject family, with a control channel.**
  `sysmetrics.replay.{session}.bundle` plus a request/reply control subject.
- **O3 — a second `SamplerI` inside imztop** reading the store directly.
- **O4 — replay as a host launch mode.** A `sysmreplay` entry point reads the
  store and feeds a private in-process bus, the way the screenshot tour already
  feeds synthetic bundles; imztop is untouched.

**O1 fails C3 outright, on evidence rather than on principle.** Two live
consumers cannot tell a replayed bundle from a current one.
[`introspecthost.go`](../../public/keelson/runtime/introspect/introspecthost/introspecthost.go)
runs a `LatestHolder` on the bundle wildcard to back the `keelson.procs` and
`keelson.sockets` introspection tables — replay would publish yesterday's
process table as the current one. And `sysmtee` itself subscribes to that
subject, so replaying while the tee runs re-persists the history it is
replaying. O1 also fails C2: there is no consumer→producer channel, by
decision.

**O2 fixes C3 and still fails C2 in spirit.** ADR-0090 §SD5 is explicit that
*"a control plane, if ever needed, is a separate bidirectional subject family,
not a weakening of this one"* — so O2 is the sanctioned shape. It is
nonetheless a streaming protocol plus a control protocol built to move data
between two objects in the same process, which is the whole of C5 spent before
the first frame renders.

**O4 preserves C4 perfectly and costs C2.** Replay controls would live outside
the app, so the app cannot offer seek or speed without O2's control channel
after all; and live/replay switching becomes relaunching.

**O3 is chosen.** It wins C2 (the transport is a cursor on a Go object), C5
(the fold, the windows and all eleven panels are reused unchanged) and C3
(nothing is published, so no other consumer observes anything), and it is the
only option that pays a real price on C4. That price is stated in SD7 rather
than minimised.

## Decision

### SD1 — Replay is a second source into the existing fold, not a publisher

imztop's `Sampler` has exactly one input: `onBundle(*sysmsnap.BundleSnapshot)`
([`imztop_sampler.go`](../../apps/imztop/imztop_sampler.go)). Everything
downstream of it — the sliding windows, the per-process EWMA, the published
`PublishedSnapshot`, every panel — is indifferent to where the bundle came
from. Replay therefore adds a *source*, and changes nothing about the fold.

Nothing is published onto `sysmetrics.*`. The metric plane keeps its meaning:
what is on it is what is happening now.

### SD2 — The seam is `SamplerI`, and it widens by three methods

`SamplerI` already exists. The concrete-`*Sampler` coupling in the render path
is small enough to enumerate: `IsPaused` and `Pause` in the top bar, and
`Interval` for the CPU heatmap's "N seconds ago" cursor. `renderApp` and
`renderTopBar` take `SamplerI`; `Interval` joins the interface; the package
singleton is typed as the interface.

A replay source is then a `SamplerI` whose `Latest()` is fed by a store cursor
instead of a subscription. It is not a subtype of `Sampler` and does not
inherit its consumer.

### SD3 — Stored timestamps ride through; wall-clock pacing is the speed control

`Producer.tick` never stamps `SampledAtUnixMs`; the source does. Replay returns
the stored value, so two things follow without any further work: the plot time
axis shows historical time, and the observed-cadence derivation in `onBundle`
(the delta between consecutive `SampledAtUnixMs`) reports the cadence the
scraper actually ran at, which is what the heatmap cursor and the EWMA
time-constant want.

That leaves wall-clock pacing free to mean *replay speed*. The two clocks are
independent by construction rather than by convention: content time comes from
the row, delivery time from the transport.

### SD4 — A bundle is a thirteen-key merge on the order column

Replay opens one `Replay` cursor per kind for the selected host and window, and
advances them in lockstep on the order column. The merge is exact because the
tee stamps one `ts` per bundle across all kinds (Context); it is not a nearest
match and needs no tolerance.

A kind absent at a given order yields a nil domain on the reconstructed
snapshot. The fold already handles that — every branch in `onBundle` is guarded
(`if bundleSnap.CPU != nil`, `else { push(nil) }`), because a live bundle whose
collector failed looks the same. A host that never ran the GPU collector
replays with no GPU panel data, and no new code path is needed to express it.

### SD5 — Process-wide, like Freeze

The `Sampler` is a process-wide singleton (`samplerOnce`), so today's Freeze
already stops every open imztop window at once — recorded in ADR-0020's
2026-07-31 Update as *"a process-singleton frame-drop that freezes every panel
at once"*. Replay follows it: entering replay puts every window on the same
historical cursor.

Per-window replay is the better interaction and the singleton is where it would
have to be fixed, but that is a change to the app's lifecycle that touches the
tour and the bisection tests, and it is not what makes replay valuable. It is
deferred, not rejected: SD1's source seam is per-source, so a later per-window
cut moves the singleton without redesigning the source.

### SD6 — A bounded window, row-exact; decimation deferred

`histN` is `HistoryWindow / UpdateInterval` — 600 slots — fixed when the
Sampler is constructed. A ten-minute replay at 1 Hz fills it exactly; a
six-hour replay would stream 21 600 bundles through a 600-slot window to show
the last 600.

v1 replays a range sized to the live window and uses the generated `Replay`
verb unchanged. The honest long-term answer is to make ClickHouse return N
buckets over an arbitrary range instead of N rows — the statistics belong in
SQL, and ADR-0184 §SD6's `LW_GET` surface is where that would be written — but
`Replay` has no aggregation, so server-side bucketing is a hand-written query
against the read surface and a materially larger first cut. Recorded as a
deferral so that "replay only goes back ten minutes at a time" is a known
limit rather than a discovered one.

### SD7 — The capability declaration names a reach, not a route

ADR-0026 §SD10's gate (`public/keelson/security/capslock`, enforced from
`lint.sh` in compare mode) maps `CAPABILITY_NETWORK` onto a manifest cap whose
pattern begins `nats.`, `ch.`, `kafka.` or `net.`. imztop gains a ClickHouse
client, so it must declare one.

**The wrinkle is that there is no bus route to declare.** The only `ch.`
subject family in the tree is `ch.local.exec.*`, which fronts `clickhouse-local`
(ADR-0028), not the server holding `boxer.facts`. So the entry imztop adds is a
capability *declaration* — a `Reason` a reviewer reads and a gate that checks it
was written — and not a subject anything publishes on. This ADR proposes
declaring it as such and saying so in the comment, rather than inventing a
broker to make the declaration literally true. It is the first non-routed entry
in a `Caps` list, which is a precedent worth noticing at review; the
alternative, routing store reads through a new bus service, is O2's cost
arriving by another door.

**What is given up is stated plainly:** imztop stops being the zero-capability
app. ADR-0090's property was specifically about `/proc` and the ADR-0085
sandbox, and an outbound database read does not reopen that; but "imztop holds
no capability" becomes "imztop holds one, for history" and every claim to the
contrary in the tree needs correcting with the change.

### SD8 — Replay shows what was stored, and says so

The tee stores thirteen kinds. `sysmsnap.BundleSnapshot` carries more than
thirteen things. The gaps are known and must be visible in the UI rather than
rendering as plausible emptiness:

- **No sensors kind and no container kind.** The Sensors tab has no data in
  replay, and the top bar's container badge is absent.
- **Process command lines, user and uid/gid** live in `SysProcCmd`, gated
  behind `--tee-proc-cmd` and off by default (ADR-0184 §SD8's correction of
  ADR-0090 §SD8 for the durable form). A typical deployment replays a process
  table with names but no command lines.
- **Per-interface IP lists and raw mount options** were dropped at write time.
- **`BundleSnapshot.Errors`** is not persisted, so a domain that failed at
  scrape time is indistinguishable in replay from one that was never wired.

The existing Freeze work is the precedent for how to handle this: ADR-0020's
2026-07-31 Update added a "FROZEN — live updates paused" banner precisely so a
stale-but-plausible view is not mistaken for a live one. Replay gets the same
treatment, and a panel with no stored kind says it was not recorded rather than
showing an empty plot.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `imztop.SamplerI` | gains `Interval()`; becomes the render path's type | `renderApp`, `renderTopBar`, the heatmap cursor |
| imztop `Manifest.Caps` | new `ch.`-prefixed entry (SD7) | the capslock baseline; ADR-0026's gate |
| imztop top-bar control | Freeze/Go-live becomes Live / Frozen / Replay | the tour, the screenshot fixtures |
| imztop package deps | new: `sysmfacts`, `storeexec`, `chclient` | the capslock report; `go list -deps` expectations |
| `sysmsnap`, `sysmetricsbus`, `sysmtee` | **unchanged** — replay neither publishes nor writes | nothing |
| `boxer.facts` schema, DDL, rows | **unchanged** — read-only consumer | nothing |
| ADR-0184's thirteen kinds | **unchanged**; gains its first reader | nothing |

## Alternatives

- **Publish replayed bundles on the metric plane (O1).** Rejected on evidence:
  the introspection `LatestHolder` and `sysmtee` both consume that subject and
  cannot distinguish replay from live.
- **A replay subject family with a control channel (O2).** The shape ADR-0090
  §SD5 sanctions if a control plane is ever needed. Rejected for v1 as a
  protocol built to connect two objects in one process; it is the right answer
  the day a second consumer wants the same replay, or replay must cross a
  process boundary.
- **Replay as a host launch mode (O4).** Keeps imztop at zero capabilities and
  reuses the tour's proven private-bus wiring. Rejected because seek and speed
  then need O2 anyway, and switching between live and history becomes
  relaunching. Reconsider if SD7's declaration is judged too expensive.
- **A play applet over the stored kinds instead.** The data is already
  SQL-visible through ADR-0184 §SD6, and ADR-0172's chart panel would draw it.
  Rejected as a substitute rather than a complement: what makes imztop worth
  replaying is the per-core grid, the CPU heatmap, the topology treemap and the
  process table, none of which a chart panel reproduces.
- **Per-window replay (SD5).** Better interaction; deferred with the singleton
  it depends on.

## Consequences

### Positive

- The eleven panels replay with no per-panel work — the fold is the reuse
  point, and SD3 means the time axis and cadence readouts are correct without
  being special-cased.
- ADR-0184's stored history gains its first consumer, which is also the first
  end-to-end check that what the tee wrote is what a reader can use.
- The metric plane keeps its invariant: everything on `sysmetrics.*` is
  current.
- Replay is exact where it has data — stored rates, stored timestamps, an exact
  bundle merge — so a replayed frame equals the live frame it was written from.

### Negative

- imztop is no longer capability-free (SD7), and the manifest entry it declares
  routes nowhere.
- SD7's declaration is the first of its kind, so the gate's contract is being
  read slightly against its grain.
- Replay reaches back only one window at a time until SD6's decimation lands.
- Four classes of data do not replay at all (SD8); the Sensors tab is the most
  visible.
- Replay is process-wide (SD5) — no live window alongside a historical one.
- A ClickHouse client in the GUI process is a blocking, single-goroutine read
  path that must never touch the render thread.

### Neutral

- The tee stays opt-in and default-off; a deployment without `--tee` has
  nothing to replay, and replay must say that rather than appear broken.
- The store read is `SELECT`-only. Nothing in this ADR writes.

## Migration — Tier 1

None for stored data — the schema, the vocabulary and the thirteen kinds are
untouched, and this is a new reader of rows that already exist.

For the app: the `SamplerI` widening (SD2) is source-compatible for every
caller outside package `imztop`, since the interface is only consumed
internally. The capslock baseline gains imztop's `NETWORK` entry in the same
commit as the manifest cap, or the lint gate fails on drift.

## Verification plan — Tier 1

- **Round-trip equality.** Publish a known bundle through a private in-process
  bus into a tee backed by a live table, replay the same window, and assert the
  reconstructed `BundleSnapshot` equals the published one field-for-field over
  the thirteen stored kinds. This is the assertion ADR-0184's integration test
  stops short of — it counts rows rather than comparing values — so it verifies
  the write direction as much as the read.
- **Merge exactness.** A window in which one kind is missing at some orders
  (a collector that failed mid-run) must reconstruct with nil domains at
  exactly those orders and no shifted alignment. Asserted against the
  column-major alignment contract ADR-0184 §M4 records, which leeway does not
  enforce.
- **Time axis and cadence.** After replaying a window scraped at 1 Hz,
  `Interval()` reports ~1 s regardless of the replay speed, and
  `HistoryTimeUnixSec` spans the historical range (SD3).
- **No publication.** With replay running, a subscriber on
  `sysmetrics.*.bundle` receives nothing, and `sysmtee` row counts do not
  change.
- **Capability gate.** `lint.sh` green with the new manifest entry, and red
  when the entry is removed while the client remains.
- **Visual.** A screenshot tour pass in replay mode, showing the mode banner
  and the not-recorded state on the Sensors tab (SD8).

## Status

Proposed — awaiting review.

Milestones, if accepted:

- **M0 — the seam.** `SamplerI` widened, render path retyped, singleton typed
  as the interface. No replay yet, no new dependency, behaviour unchanged.
- **M1 — reassembly.** The thirteen-key merge and the `SysmetricsEntity` →
  `sysmsnap` inverse, tested against a tee round-trip. Library only; imztop
  does not import it yet.
- **M2 — the replay source.** A `SamplerI` over M1 with a cursor and a
  transport, off the render thread.
- **M3 — capability and wiring.** The manifest entry, the capslock baseline,
  the ClickHouse client construction, and the "no tee, nothing to replay"
  path.
- **M4 — the control surface.** Live / Frozen / Replay in the top bar, range
  selection, the mode banner and SD8's not-recorded states.
- **M5 — the tour.** A replay-mode capture, and the ADR-0020 Update recording
  which of its two scrubbing deferrals this closes and which it does not.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — the tee that makes this
  possible; §SD3 the per-`(host, domain)` key replay reads by, §SD6 the SQL
  read surface SD6 defers bucketing to, §SD8 the opt-in default and the
  process-command gate SD8 inherits.
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — the metric plane; §SD5
  the bisection this reuses and the sentence that governs control planes, §SD9
  the P5 phase 0184 built.
- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop itself; §SD14
  and the non-goals are where scrubbing was deferred, and the 2026-07-31 Update
  is the Freeze vocabulary SD8 extends.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD10 and its
  2026-07-15 Update: the gate SD7 must satisfy.
- [ADR-0126](./0126-appliance-topology-as-data.md) — §SD5's latest-holder is
  one of the two consumers that make O1 unworkable.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — where the store
  read belongs if it wants progress reporting and audit.
- [leeway SQL read surface](../explanation/leeway-sql-read-surface.md) — the
  entry point for SD6's deferred server-side bucketing.
