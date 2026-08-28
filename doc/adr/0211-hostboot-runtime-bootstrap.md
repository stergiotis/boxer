---
type: adr
status: proposed
date: 2026-08-28
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0211: A reusable runtime bootstrap for window-host adopters

## Context

The imzero2 demo carousel's `NewCommand` grew, one milestone at a time, into
the only place that knows how to stand up the keelson runtime around the
window host: process identity ([ADR-0191](./0191-runtime-instance-attribution.md)),
the facts store with its runtime-start row and heartbeat, the in-process bus
with its audit sink ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)
§SD3–§SD5), the fs, persist, clickhouse-local, ad-hoc-data and clipboard
Powerboxes, the task supervisor, the status snapshot, the window host with
its seeded windows and decorated chrome, introspection
([ADR-0094](./0094-keelson-introspection-tables.md)), and the signal-driven
closing edge ([ADR-0188](./0188-app-instance-effect-tracking.md)). Roughly
seven hundred lines, in one function, in a package whose blank imports pull
every demo app in the repository.

[ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md) made
consuming boxer from another repository a supported path, and the first
such consumer that hosts an app of its own found that it had to restate
about 120 of those lines — bus before seed, audit before open, reap before
`Shutdown` — by reading the carousel and copying the order. Nothing checks
that copy against the original; the next service the carousel gains, the
copy silently lacks. `imzhost.DecorateRenderer` was extracted for exactly
this reason for the *chrome* half; the *runtime* half had no seam.

Two constraints bound the extraction:

- The carousel must keep behaving byte-for-byte: same services, same
  order, same best-effort failure policy (an optional service that cannot
  start is logged and left unbound, never fatal), same shutdown edge.
- The extracted package must not import the demo apps, the play host or
  the applet corpus: an adopter hosting one app pays for the runtime, not
  for the gallery.

## Design space (QOC)

**Question.** How does a repository that hosts its own keelson app get the
carousel's runtime without copying it?

**Options.**

- **O1** — Keep the carousel as the only harness; adopters mount
  `carousel.NewCommand()` in their binary and register their app into the
  default registry.
- **O2** — Extract a `hostboot` package with an `Options` struct whose
  `Services` field selects the optional services, returning a `Runtime`
  that exposes every wired piece and owns `Run` and `Close`. (chosen)
- **O3** — A functional-options builder (`hostboot.New(WithFs(), WithPersist(), …)`).

**Criteria.**

- **C1 — Drift resistance.** A service added to the runtime reaches every
  host without a copy being edited.
- **C2 — Adopter cost.** What a single-app host pays in binary size, in
  dependencies, and in lines written.
- **C3 — Carousel fidelity.** Whether the carousel keeps its exact
  behaviour, so the refactor is verifiable by its existing tests.
- **C4 — Discoverability of the knobs.** Whether a reader can see, in one
  place, which services exist and what turning one off means.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | ++ | ++ |
| C2 | −− (every demo app, play, the applet corpus, ~50 MB) | + | + |
| C3 | ++ | + (thin caller, same order) | + |
| C4 | − (all or nothing) | ++ (one struct, one doc comment per field) | − (options scattered across constructors) |

O1 is the right answer for a binary that *wants* the gallery — and it stays
available: nothing stops an adopter mounting the carousel. It is the wrong
answer for a boat computer that hosts one instrument display. O3 buys
nothing over O2 for eight booleans and hides the set. O2 is chosen.

## Decision

We will extract the carousel's runtime bootstrap into
`public/keelson/runtime/hostboot` and make the carousel its first caller.

### SD1 — One `Boot`, the carousel's order, the carousel's failure policy

`hostboot.Boot(ctx, Options)` wires, in the carousel's order: identity →
facts store → runtime-start row → heartbeat → bus with audit → the selected
services (fs, persist, chlocal, adhoc, clipboard, coverage) → task
supervisor → status snapshot → window host with seeded windows (or the
screenshot-tour renderers when `IMZERO2_SCREENSHOT_DIR` is set) →
`AfterHost` hook → introspection. An optional service that fails to start
is logged and left nil on the returned `Runtime`. `Run(cfg)` creates the
imzero2 application, installs the SIGINT/SIGTERM handler (reap, shutdown,
bounded force-exit), runs the render loop and closes the runtime; `Close`
reaps and stops the services in reverse start order, draining the audit
sink last. `Reap` and `Close` are idempotent.

### SD2 — Services are a struct of booleans; the zero value is the minimal host

`Services{Fs, Persist, ChLocal, AdhocData, Clipboard, Coverage, Sysmetrics,
Introspect}`. The zero value boots what every app needs for the ADR-0026
lifecycle — identity, facts, heartbeat, bus, audit, task supervisor,
window host — and nothing else; `AllServices()` is the carousel. The
sysmetrics plane, previously unconditional under the bus, becomes a
service so a host that must not read `/proc` can say so.

### SD3 — `SeedWindows` open configured windows; a failure is an error

Beside the best-effort `LaunchApps` (opened without config, a failed open
logged and skipped, as `--launch` always behaved), `SeedWindows
[]SeedWindow{AppId, Kind, Config}` opens a window through
`OpenWithConfig` — the window-host form of an ADR-0135 launch request. A
seeded window that cannot open aborts `Boot`: the caller named that exact
window and config. The carousel exposes it as `--launch-config
<alias>=<path>`; the kind is the manifest's `LaunchKind`, the file the
encoded config.

### SD4 — What stays in the carousel

Everything that names an app: the capability inspector's audit counters
(`ExtraAuditSinks`) and backend labels, applet minting and the applet
store, the play host's pass, component and SQL-vocabulary registrations,
the Go-dependency introspection tables, and launch resolution through
`clickhouse local`. They ride two hooks — `AfterHost` (runs with bus, host
and introspection registry present, before the HTTP host starts) and
`OnClose` — and `BeforeFirstFrame`.

### SD5 — Overrides an adopter or a test needs

`Registry` (nil is the default registry), `Facts` (a caller-supplied store
replaces the ClickHouse-or-memory choice from the environment),
`FactsPingTimeout`, `KeepCoreDumps`, `VideoOutput` / `HelpHost` for the
chrome, `ShutdownGrace`.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/keelson/runtime/hostboot` | added: `Boot`, `Options`, `Services`, `SeedWindow`, `Runtime{Run, Reap, Close, OnClose}` | ADR-0179 adopters; `doc/howto/launch-apps-non-interactively.md` |
| `imzero2 demo` command | `--launch-config <alias>=<path>` added; bootstrap delegated | none — flags and behaviour otherwise unchanged |
| carousel package | `buildWindowedRenderer`, `buildStatusSnapshot`, `selectPersistBackend`, `mainE`, `decorateRenderer` removed (unexported) | none outside the package |

## Alternatives

- **Mount the carousel downstream (O1).** Stays possible; rejected as the
  *only* path because it ships the gallery with every adopter.
- **Functional options (O3).** Rejected: hides the service set that a
  reader most needs to see.
- **A `hostboot` that also resolves `--launch`.** Rejected: resolution
  needs the manifest table and `clickhouse local`; adopters resolve by
  alias or seed by config and need neither.

## Consequences

### Positive

- An adopter hosting one app writes an `Options` literal and a `Run` call;
  the next service boxer adds reaches it at the next pin bump.
- The carousel's `NewCommand` is readable again: flags, launch resolution,
  three carousel-specific registrations, `Boot`, `Run`.
- The minimal host drops the demo apps, play and the applet corpus from an
  adopter's dependency cone.

### Negative

- Two hooks and a struct of booleans are a contract; adding a service is
  now a field, a doc comment and a line in `AllServices`.
- The capability inspector's labels are set after `Boot` rather than as
  each service starts; they are read at render time, so nothing observes
  the difference.

### Neutral

- Log lines keep their wording; the prefix `carousel:` on host-level lines
  becomes `hostboot:`.

## Migration — Tier 1

- **Breaks.** Nothing outside the carousel package; the removed helpers
  were unexported.
- **Path.** Adopters with a hand-copied bootstrap replace it with `Boot` +
  `Run`; their seeded window becomes a `SeedWindow`.
- **Regeneration.** None.
- **Old shape.** Removed outright inside the carousel.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `hostboot` boots the minimal host on a
  private registry with an in-memory facts store, opens one plain seed and
  one configured seed, sees the `AfterHost` hook, reaps to zero windows,
  and runs `OnClose` cleanups in reverse; a seeded window with a rejected
  config fails `Boot`; screenshot mode requires launch apps and builds one
  tour renderer per app. The carousel's own tests (list rendering, launch
  resolution, topic classification, registry) are unchanged and pass.
- **What would fail.** A reordered service that breaks the
  bus-before-seed or reap-before-shutdown invariants shows up as a
  configured seed that mounts without a bus, or as a window left after
  `Reap`, in the boot test.
- **Gap.** `Run` (the imzero2 application and the signal handler) is not
  exercised without a client; the carousel's headless smoke in
  `doc/howto/launch-apps-non-interactively.md` covers it manually.

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app runtime the bootstrap stands up.
- [ADR-0135](./0135-app-launch-requests.md) — launch configs; `SeedWindow` is their boot-time form.
- [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md) — the adopter path this serves.
- [ADR-0188](./0188-app-instance-effect-tracking.md) — the closing edge `Reap` runs.
- [doc/howto/launch-apps-non-interactively.md](../howto/launch-apps-non-interactively.md) — `--launch-config` and env seeds.
