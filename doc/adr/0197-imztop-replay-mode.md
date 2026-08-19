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

### SD2 — The seam is `SamplerI`, and it widens by one method

`SamplerI` already exists, and already carries `Latest`, `Start`, `Pause`,
`IsPaused` and `Close`. The concrete-`*Sampler` coupling left in the render path
is small enough to enumerate: `IsPaused` and `Pause` in the top bar, and
`Interval` for the CPU heatmap's "N seconds ago" cursor — so only `Interval`
has to join. `renderApp` and `renderTopBar` take `SamplerI`; the package
singleton is typed as the interface.

`IntervalLabel` does **not** join it. It is `Interval().String()` with one
caller, so putting it on the interface would oblige every source to reimplement
a format; the top bar formats the duration itself instead.

Typing the singleton as an interface introduces one hazard the concrete
variable could not have: a nil `*Sampler` stored in a `SamplerI` is not `nil`,
and the heatmap cursor guards on `sampler != nil`. The only assignment is
already behind the constructor's error branch; the variable now says so.

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

**Gaps in stored history are compressed, not honoured.** History has holes —
the tee is opt-in, drops bundles under back-pressure, and stops when the box
does — and pacing an outage literally would park the transport for its length,
which reads as a hung UI rather than as missing data. The wait between two
bundles is therefore clamped (`MaxReplayGap`, 2 s at time of writing). The
*content* clock is untouched by this: the plotted timestamps still show the
hole, so the gap remains visible where it belongs, on the axis.

*Recorded during M2: a speed change applies to the wait already in progress,
not only to the next gap. Deriving the deadline afresh on each pass rather than
once per bundle is a one-line difference, and without it pressing "faster"
during a long gap appears to do nothing.*

### SD4 — Ten kinds merge on the order column; three are carried forward

Replay opens one `Replay` cursor per **per-tick** kind for the selected host and
window, and advances them in lockstep on the order column. The merge is exact
because the tee stamps one `ts` per bundle across those kinds (Context); it is
not a nearest match and needs no tolerance.

A per-tick kind absent at a given order yields a nil domain on the
reconstructed snapshot. The fold already handles that — every branch in
`onBundle` is guarded (`if bundleSnap.CPU != nil`, `else { push(nil) }`),
because a live bundle whose collector failed looks the same. A host that never
ran the GPU collector replays with no GPU panel data, and no new code path is
needed to express it.

**Three of the thirteen kinds are not per-tick, and treating them as such would
be wrong.** The tee writes `sysCpuInfo` and `sysTopology` once, on first sight
of a host, and writes `sysSocket` only when the sockets collector's own stamp
changes — dating that row *by* that stamp rather than by the bundle's, so it is
not even on the same clock. A live subscriber nonetheless sees all three on
every bundle: the collector restamps the descriptor each tick, the scraper
stamps one topology pointer onto every snapshot, and consecutive bundles repeat
one sockets snapshot until the slower collector produces a new one.

So these three are **carried forward** — the most recent row at or before the
tick being emitted. A merge that only matched the order column would give the
first replayed bundle a model name and a topology and leave every later one
without, which is not what was recorded.

Carrying forward needs a **seed from before the window**, because the usual
replay is of some hour after the tee started and the once-written rows are
older than it. The generated verbs cannot express "the newest row before T" —
`Replay` is ascending, so its `Limit` takes the earliest rows, and `Latest` has
no upper bound — so the seed is one `Scan` per carried kind under a
`ScanOpts.ExtraPredicate` selecting `order = (SELECT max(order) … < T)`. That
is trusted SQL over the envelope columns, which is what the option is for; no
leeway section is touched, so it is not the hand-written array arithmetic the
read-surface page warns against.

*Recorded during M1: this sub-decision originally read "a thirteen-key merge"
and said absent kinds yield nil domains, which is true only of the ten. The
correction came from reading the tee's write cadence rather than its row
builders.*

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

### SD6 — A bounded window, row-exact; decimation deferred *(closed by SD11)*

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

*Recorded during M2: the gate does **not** compel the declaration, so this
sub-decision is an honesty argument, not a lint obligation. §SD10's attribution
rule — as rewired by ADR-0026's 2026-07-15 Update — counts a capability only
when the classified sink is called by the app's own code, and imztop reaches
ClickHouse through non-stdlib hops. With M2's import in place the gate is green
and imztop is still absent from the capslock baseline. M3 constructs the client
rather than merely importing the package, so it must re-check rather than
inherit this result; but the declaration should be made because it is true, not
because something failed.*

*Re-checked at M3, with `chclient.New` and `Ping` called from imztop's own code:
still green, still absent from the baseline, for the same reason — the sink is
inside `chclient`, a non-stdlib hop. The entry is declared anyway.*

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
- **Per-domain sample stamps collapse to the bundle's.** Each domain snapshot
  carries its own `SampledAtUnixMs` and the tee stores only the tick's, so
  sub-tick skew between collectors does not survive. Found while building M1;
  harmless for the panels, which plot against the bundle stamp anyway.

The existing Freeze work is the precedent for how to handle this: ADR-0020's
2026-07-31 Update added a "FROZEN — live updates paused" banner precisely so a
stale-but-plausible view is not mistaken for a live one. Replay gets the same
treatment, and a panel with no stored kind says it was not recorded rather than
showing an empty plot.

*Recorded during M4, from driving the app rather than reading it. Two defects,
both of which every unit test passed through:*

*The empty state could **strand the user in replay**. `renderApp` returned early
on a nil snapshot to draw "waiting for first sample…", before the top bar — so
a replay of a host with no stored history hid the whole window chrome including
the only control that leaves the mode. The top bar now draws in every state and
tolerates a nil snapshot, and the message distinguishes the three reasons a
frame can be empty: waiting for a scraper, opening a database, and a host the
tee never covered. A mode that can legitimately show nothing must not be able
to hide its own exit.*

*"Go live" was **the control that got clipped**. It sat at the end of the
transport row, and a control row is clipped from the right — so on a narrow
window the escape hatch was behind the window edge exactly when the view gave
the user most reason to want it. It now leads the row.*

### SD9 — The range is picked off a timeline, not typed

**Proposed, not built.**

M4 shipped a window-length combo plus jog buttons, and driving it showed what
is wrong with any control of that family: it asks the user to name a range
without telling them where any data is. Replaying a host the tee never covered
produced a correct, useless answer, and the only way to find a covered stretch
was to jog and look.

So the range selector becomes a [timeline](./0043-imzero2-timeline-widget.md)
carrying two layers of context, brushed with ADR-0043 §SD16's strip:

- **Availability bands** — where stored history exists for this host. Fed
  through `BackgroundBandProducer`, which is already lazy and viewport-scoped,
  so only the visible span is materialised.
- **A load preview** — a coarse metric strip (CPU busy, or PSI, which answers
  "which resource was the bottleneck" better than utilisation does) on the rug's
  existing intensity encoding, so the user brushes toward the interesting part
  rather than toward an arbitrary hour.

The jog stays. It is the fastest way to step through consecutive windows once
you know where to look, and it costs one row. The window-length combo does not:
the strip picks the range now, and a combo that only chose where to start
looking collapses to one constant.

*Recorded during M7, from driving it. Three things the code did not say:*

*The strip belongs in the central panel, not beside the transport. A timeline is
tall, a tall top panel is a tall Ui, and egui gives a vertical separator the
available height — so putting it up there stretched the transport row's
separators down the whole window and pushed the dock out of it. The same trap
bit a second time inside the strip's own legend row, which now spaces rather
than separates.*

*It must draw in the empty state too. The strip lived after `renderApp`'s
nil-snapshot branch, so a window with nothing stored in it hid the one control
that shows where something IS stored — while the empty state's own text advised
jogging to an earlier window. That is the M4 stranding defect in a second
costume: the mode that shows nothing is exactly when its navigation must stay.*

*The oversized-range note has to be measured, not estimated. Slot count times
the observed cadence warns on the default window (a 999 ms cadence puts capacity
a half-second under a ten-minute span) and again on any young replay, where the
cadence estimate is noisy — it read 421 ms on a run whose steady rate was 1 s.
Reading the span off the timestamps the fold is holding assumes neither an even
spacing nor a settled estimate, and says nothing at all until the window is full,
which is the only point at which anything is being omitted.*

### SD10 — Availability is an envelope query; the preview is a section query

These two look alike and are not, and the difference decides how much each
costs.

**Availability** asks which time buckets hold rows for a key. Both columns
involved — the key and the order column — are `boxer.facts` *envelope*
columns, not leeway sections, so it is an ordinary `GROUP BY` over
`toStartOfInterval(...)`. No `LW_GET`, no run decoding, none of the
array arithmetic
[the read-surface page](../explanation/leeway-sql-read-surface.md) warns
against.

**The preview** asks for a metric *value*, which does live in a leeway section,
so it needs bucketed aggregation through the `LW_GET_LIST` surface ADR-0184
§SD6 ships — with that ADR's two rules in force: the channel token is
mandatory, and an `*Array` section reads through `LW_GET_LIST` even where the
Go field is a scalar.

Both ride the ClickHouse connection SD7 already opens, so neither adds a
capability. Both run off the render thread, on the transport's goroutine or a
keelson task — a preview is a bigger query than a bundle read, and it must
never be able to stall a frame.

*Recorded during M7. The availability half is built, and the SQL cost one
correction: `toStartOfInterval` on a `DateTime64` returns a plain `DateTime` —
it drops the sub-second scale along with the sub-second part — so the 64-bit
converters reject its result outright. Seconds are the right resolution for the
question anyway, so the bucket is reconstructed by multiplication rather than
asked for. Timestamps and counts are both cast explicitly, for the reason
ADR-0184's readers already know: a ClickHouse `DateTime` arrives as a 32-bit
Arrow column, and inferring the type is how the range gets lost quietly.*

### SD11 — The preview closes SD6's deferral rather than living beside it *(corrected at M8 — see below)*

Bucketing a metric into ~1000 cells over hours *is* the server-side decimation
SD6 deferred; the preview and the replay window differ in how many buckets they
ask for, not in kind. Building SD10's preview query therefore builds the
mechanism that lifts "one bounded window at a time", and the widget's own
`LODIndex` is the client-side half already written.

So SD6 is superseded in effect: the bounded-window limitation stands until the
preview lands and stops being a limitation when it does. SD6's reasoning is
kept above rather than rewritten, because it was the right call when replay had
no reason to aggregate — what changed is that something else now needs the same
query.

**Corrected at M8. The claim above is wrong in an interesting way: the preview
and the decimation are *not* the same machinery.** They share the bucketing
idea and nothing else, because a bundle is not a metric. A CPU percent has a
mean; a process table does not, a topology tree does not, and an interface list
whose members come and go does not. Averaging those would invent a machine that
never existed — a worse answer than a sparser true one.

So decimation **samples** whole recorded bundles, one per bin, and replays them
unchanged: what is lost is resolution, never fidelity. And because choosing a
representative instant per bin touches only the key and the order column, it is
the *cheap* query — coverage's class, not the preview's. §SD11 expected to pay
for `LW_GET_LIST` here and does not; only the preview strip reads a section.

SD6 is closed either way, and by a smaller change than this predicted. Measured
on the live table: a 5 h 48 m range holding 4 369 stored bundles replays as 124
frames, one per occupied bin — the bins the tee was down for produce no row at
all. The fold's cadence readout then reports 34 s, the real spacing of the
frames it is being given, rather than pretending they are a second apart.

### SD12 — Picking a range resets the fold; every range control goes through one seam

Two controls set the replay range: the availability strip's brush and the jog
buttons. Until now they behaved differently in ways nobody chose.

**The fold kept its history across a seek.** `Seek` said so — *"a caller wanting
a clean plot builds a new sampler"* — and no caller did. So brushing an
afternoon three hours from the one on screen appended its frames to the ones
already plotted, and the CPU line ran continuously across a gap that was not
there. The time axis is real, so the join is drawable; it is just not a run.

The reset therefore lives in the transport, not in a control: `run` empties the
fold on every reopen, which is where both controls already meet. What goes is
everything carried between frames — the sliding windows, the observed-cadence
carry, the per-process EWMA converging on processes the new range may not have,
and the published frame itself. Publishing nil rather than a stale one is the
point: the panels have a state for "no snapshot" and it says the honest thing
while the store is being read, where holding the previous frame would show the
old range's plots under the new range's label.

**Consumers that accumulate need to be told.** The CPU heatmap is the one panel
that does not redraw from the snapshot — it pushes a column per sample into a
ring — and it kept its columns across the reset. Worse, its push guard is
`stamp > lastPushed`, so a seek *backwards* read as a duplicate frame and froze
it for the rest of the session. Entering replay at all did that, since a
replayed stamp is older than a live one.

Timestamps cannot carry this: a seek moves them either way and the ranges may
overlap. So the frame carries `PublishedSnapshot.HistoryEpoch`, minted per fold
from a process-wide sequence and moved on every reset. A changed value means
"the frames stopped continuing each other" and nothing finer — which also
covers the render path swapping between the live fold and a replay one, a
discontinuity neither fold can see.

**The position gets a mark, not only a readout.** The transport says where
playback has got to as text; the strip did not say it at all, so the range being
replayed was visible and the instant inside it being drawn was not. It now
carries the timeline's playhead ([ADR-0043](./0043-imzero2-timeline-widget.md)
§SD18, added for this), driven from `session.Position()` each frame and cleared
when there is none — which is exactly what a seek leaves behind, and a mark held
over from the previous range would point at a moment nothing is showing.

**The jog is a brush operation.** The strip already mirrored the session window
onto its brush every frame, so a jog showed there. What it could not do is
follow: jogging steps the window a whole span at a time and walks it off the
visible axis within a few clicks, where the brush paints nothing and the button
reads as inert. The strip now pans to a window it did not choose — on the edge
only, keeping the current zoom, widening solely when the window would not fit.
Per-frame re-centring was the obvious alternative and is wrong: it makes the
strip impossible to pan away from, which is the first thing anyone does after
picking a range.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `imztop.SamplerI` | gains `Interval()`; becomes the render path's type | `renderApp`, `renderTopBar`, the heatmap cursor |
| `imztop.Sampler.IntervalLabel` | removed — its one caller formats `Interval()` itself, and it is presentation a replay source should not have to reimplement | the top bar |
| `imztop.newFold` | new (unexported) — the windowing half of a Sampler, with no source attached; `NewSampler` bolts a bus consumer onto it | `Sampler.Start`/`Close`, which now tolerate a nil consumer |
| `imztop.ReplaySampler`, `ReplayOptions`, `BundleSourceI` | new — the replay `SamplerI` and its transport (play/pause/step/seek/speed) | nothing yet; M4 drives it |
| `imztop.StoreSource` | new — the production `BundleSourceI`: dials ClickHouse, verifies the schema, binds the reader | the manifest cap; `apps/imztop`'s dependency set |
| `imztop.StartReplay` / `EnterReplay` / `LeaveReplay` / `CurrentReplayStatus` / `ActiveReplay` | new — the process-wide session (SD5) and the status a renderer polls | `App.Frame`, which now asks `activeSampler` |
| `imztop.App.frameSampler` | new (unexported) — the sampler the current frame draws, so a panel reached without it threaded (the heatmap cursor) reads the active one rather than the live singleton | the CPU heatmap |
| `imztop.App.frameReplay` | new (unexported) — the session status for this frame, read once | the top bar; the Sensors pane |
| imztop top bar | gains a Replay button, a REPLAY banner beside FROZEN, and a transport row; draws on a nil snapshot | `renderApp`, which no longer returns early |
| `ReplaySampler.SeekWindow` | new — moves both bounds, which a jog control needs | `Seek`, which delegates to it |
| `sysmreplay` (new package) | new — the reassembly library: `Reader.All`, the thirteen domain tokens, the per-kind inverses | nothing; it has one caller |
| `timeline` widget | gains an opt-in brush strip (ADR-0043 §SD16) | nothing until a caller opts in |
| `sysmreplay.Coverage` / `CoverageRuns` / `MergeCoverage` | new — the envelope-only availability query and its run merge | the replay range control |
| `sysmreplay.Preview` | new — mean CPU busy per bin, through `SysmetricsComponentSQL`'s generated projection rather than hand-written array arithmetic | the strip's rug layer |
| `sysmreplay.CountBundles` / `PlanDecimation` / `NeedsDecimation` | new — the envelope-only sampling plan that closes SD6 | `Window.Decimate` |
| `sysmreplay.Window.Decimate` | new — restricts the read to the plan's instants, via the store's own Scan verbs | `Reader.All` |
| `sysmreplay.Options.Exec` | new — the executor coverage runs on; must be the store's | `StoreSource`, which passes the one it built |
| `imztop.PublishedSnapshot.HistoryEpoch` | new — names the continuous run of frames the `History*` slices belong to (SD12) | the CPU heatmap, the one accumulating consumer |
| `imztop.Sampler.reset` | new (unexported) — empties the fold and moves the epoch; run on the goroutine that owns `onBundle` | `ReplaySampler.run`, on every reopen |
| `ReplaySampler.Seek` / `SeekWindow` | behaviour change — the fold is emptied before the new range is read; the old contract said the opposite | both range controls, which now agree |
| `slidingwindow.Window.Reset` | new — empties a window, keeping its capacity | nothing; imzrt's windows are untouched |
| `timeline.SetPlayhead` / `ClearPlayhead` / `Playhead` | new (ADR-0043 §SD18) — one caller-set instant, drawn as a caret-headed rule | the availability strip; the widget gallery |
| `timeline.Visuals.PlayheadColor` | new — the marker's ink, distinct from `NowLineColor` so the two read as two instants | nothing; defaulted |
| `timeline.WithTimeZone` | new — localises the tick axis; nil stays UTC | nothing; existing callers keep UTC |
| `timeline.ViewRange` | new — last frame's viewport, for consumers that must *query* for view-dependent data | nothing |
| imztop replay bar | window-length combo removed; availability strip added below the transport | the jog, which stays |
| imztop demo registry | two new scenes, `imztop-replay` and `imztop-replay-notrecorded` | the tour's scene count |
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
  path that must never touch the render thread. Handled at M3 by opening the
  session on its own goroutine behind a status a renderer polls, so the cost is
  a discipline the API enforces rather than one every caller has to remember.
- **The dependency surface grows measurably.** Measured at M3: importing the
  reader puts 31 ClickHouse and Arrow packages within reach of `apps/imztop`,
  against 217 non-stdlib packages it already reached. ADR-0090 §SD6 survives
  intact — `go list -deps` still shows zero collector packages, so the property
  that mattered for the sandbox is unaffected — but "imztop is small" is less
  true than it was.
- **Which host to replay is not answered.** The reader is bound to one host
  token, and imztop's live consumer is handed only the snapshot, never the
  subject it arrived on, so the app cannot learn the token from the plane
  (ADR-0184 §SD8 records that gap). M3 defaults to the local hostname, which is
  right for the co-located scraper and wrong when the plane is bridged from an
  external `sysmetricsd` on another box; the option is settable meanwhile.
  Picking a host is deferred, and closing it properly means either the subject
  in the consumer handler or a host list read from the store.

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

**All milestones below are built and unreviewed**, in the working tree rather
than committed. They are still written as a plan rather than ticked off as a
ledger: the decision they implement has not been accepted, and four of its
sub-decisions were corrected by the act of building them (§SD4, §SD7, §SD8's
loss list, and §SD11). A reviewer reading this should expect to review a
decision and its implementation together.

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
- **M5 — the tour.** Two replay-mode captures — the mode's surface, and the
  §SD8 not-recorded state on the Sensors tab — plus the ADR-0020 Update
  recording which of its two scrubbing deferrals this closes and which it does
  not. Both scenes run off the synthetic generator the live scenes use, so the
  tour needs no database and no network flag; each imztop scene asserts the
  mode it wants per frame, because the session is process-wide and the driver
  captures in name order.
- **M6 — the brush.** ADR-0043 §SD16's strip on the timeline widget: opt-in,
  its own callback, existing callers untouched. Verified in the widget demo
  before imztop uses it.
- **M7 — availability bands.** The envelope-only coverage query (SD10) behind
  a `BackgroundBandProducer`, and the timeline replacing the window combo as
  the range control (SD9). The jog stays.
- **M8 — the load preview, and SD6's closure.** Bucketed aggregation through
  the store's published projection (SD10) onto the rug's intensity encoding,
  and an envelope-only sampling plan that lifts the bounded-range limitation
  (SD11, as corrected). Done: a 5 h 48 m range holding 4 369 bundles replays as
  124 frames.
- **M9 — the range controls agree.** The fold resets on every seek, the frame
  carries an epoch so the heatmap's ring resets with it, and the jog pans the
  strip to the window it just picked, and the strip marks where playback has got
  to with ADR-0043 §SD18's playhead (SD12). Both controls reach the same seam,
  so neither can behave differently from the other by accident.

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
