---
type: adr
status: accepted
date: 2026-07-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0155: App embed seam — hosting a registered app's body inside another app

## Context

The app contract was designed host-agnostic: `AppI` is
Manifest/Mount/Frame/Unmount, the host owns the window, and "the same
app source runs unchanged across hosts"
(`public/keelson/runtime/app`). The
[app-composition survey](../adr-background-work/app-composition-survey.md)
concluded that canvases, node bodies, and zoomable scenes are all *new
hosts* for this contract, and named the general embed seam its S2 step
— the one missing contract most composition work depends on.

Two implementations bound the design space today:

- **windowhost** (the full-service host) does seven things at open
  that embedding currently gets none of: it mints a monotonic instance
  key; mints a per-open bus client carrying the target's manifest caps
  plus the host-injected persist cap; tags a per-instance logger;
  assembles the mount context (launch config, reason, a fresh
  host-salted widget-id stack — though with `stop=nil`, a known Cancel
  gap); refcounts mounts per `AppI` instance (`instMount`), so a
  singleton is Mounted once and Unmounted at last release; and writes
  `AppLifecycle` Started/Stopped facts keyed by the instance key.
- **`sqlapplet.NewEmbedded` + `apps/adhocdemo`** (the zero-service
  embed) bypasses `AppI` entirely: the inner play instance is
  constructed and configured through bespoke methods, rendered via
  `inner.Render()` inside the embedder's Frame, closed by hand in
  Unmount, shares the embedder's bus client, and appears in no
  lifecycle fact. Its only identity trace is the composed query-run
  stamp `<embedder-id>#<slug>`.

A structural constraint shapes the answer: the collaborators
host-grade bookkeeping needs — `FactsStoreI`, `BusProvider` — are held
by the host and unreachable from apps. (The same reachability gap
currently blocks ADR-0151's M4: the column-width resolver needs a
facts-store subset no app can reach.) An embed is in-process value
passing — an `AppI` instance and a Ui scope cannot ride the bus — so
the bus-subject route that solved app-launching (ADR-0135) does not
apply here.

Constraints adopted from the survey's rules: the four-method contract
stays frozen (D3, DN1); data crosses on typed channels (DN9); no
ungated global input (DN6); embedded mounts must become queryable
(D4).

## Decision

An embedder acquires a hosted instance of a registered app through an
optional host capability, and the returned handle does for a Ui region
exactly what windowhost does for a window. Names below are open to
review at acceptance.

### SD1 — Host mediation via an optional capability

The host exposes `app.EmbedHostI` on the mount context, type-asserted
by embedders exactly as `WindowFocusI` is — the established
optional-capability pattern, costing existing hosts and test fakes
nothing:

```go
type EmbedHostI interface {
    AcquireEmbedded(appId AppIdT, kind string, cfg []byte) (EmbeddedHandle, error)
}
```

A context without the capability cannot acquire embeds (tests, CLI,
one-shot bootstrap); the bespoke `NewEmbedded` route remains available
for what it already serves. The implementation lives beside windowhost
and shares its collaborators (registry, `BusProvider`, facts store,
run id) — wired by the shell exactly once.

### SD2 — Identity in facts: same key space, plus a parent key

An embedded instance draws its instance key from the same monotonic
key space as window keys, so `task.ForApp`'s `OwnerTileKey` join and
every existing lifecycle query keep working. Its `AppLifecycle`
Started/Stopped rows carry the **target's real app id**, plus a new
**embedder/parent instance key** column so containment is queryable —
the embed edge the survey's descriptive ports view (S1) needs.
Vocabulary lands by appending at the end of the windowhost dimdata
cohort file, the documented ordering-safe move.

### SD3 — Identity split: target for state, composed for attribution

The embedded mount context presents the **target's identity
everywhere state is keyed**: `ctx.AppId()` returns the target id, and
the seam mints a bus client with the target id and the target's
manifest caps (plus the persist injection), exactly as a window open
does — multiple clients per id are already the norm. The composed
stamp `<embedder-id>#<slug>` remains what it is today:
**attribution**, on query-run stamps and on the parent/caller columns
of facts rows — never a keying identity.

**Verified against ADR-0151 column-width overrides (2026-07-31).**
Width overrides are the newest identity-keyed state and make a sharp
test case:

- A `ColumnWidthRow`'s identity is `(AppId, Tier, Scope, ColumnKey)`,
  and the resolver is constructed with an `AppId` that "scopes every
  read and write. Overrides never cross apps"
  (`egui2/colwidth/resolver.go`). Whatever identity the mount context
  presents *is* the keying identity.
- Under this decision, an embedded instance resolves and writes the
  same rows as a windowed instance of the same app: a column dragged
  in either follows the content to the other. That is ADR-0151's own
  recorded stance — it rejected a per-instance dimension as an
  anti-feature and absorbs cross-window races by merge — extended
  unchanged to embeds.
- The counterfactual fails systemically: a composed-identity context
  would fork width rows per embedder, and — because the persist
  backend threads `StorageRef.AppId` from the bus envelope's sender —
  would likewise fork the persist alias and any future
  sender-attributed state. Fragmentation, not sharper provenance.
- Stability under salting: width identity is content-derived
  (`blake3short(name+type)` column keys, app-chosen table tags), not
  widget-id-derived — so SD5's per-embed id salts cannot destabilize
  it. Had widths been keyed on widget ids, every window and every
  embed would fragment; ADR-0151's fingerprint design is precisely
  what makes embedding compose.

### SD4 — Lifecycle: lazy mount, real cancel, shared refcount

Mount runs lazily at the handle's first Frame; a Mount error renders
the host's failed-mount label in the region, never an embedder crash.
`handle.Close()` (idempotent) unmounts and writes the Stopped row; the
embedder is contractually required to call it from its own Unmount.
The handle shares windowhost's `instMount` refcount, so a singleton
app that is windowed *and* embedded is Mounted once and Unmounted at
last release. The seam passes a **real stop channel**, closed at
`Close()` — `MountContextI.Cancel()` fires for embeds; windowhost's
own `stop=nil` gap is unchanged here and stays a recorded follow-up.

### SD5 — Frame discipline: seam-owned salts, delegated focus

`handle.Frame()` owns what windowhost owns per frame: it prepares the
inner id stack and pre-pushes the instance-unique salt, so inner apps
need no play-style self-salting. It stamps `WindowFocused` as *the
embedder's own focus ANDed with an embedder-supplied predicate*
(visible / active region), giving every process-global-input consumer
its DN6 gate. The full pane-grained focus doctrine remains the
survey's F7, deliberately open.

### SD6 — Config delivery through the existing gate

`AcquireEmbedded` takes `(kind, cfg)` through the same boundary as
`OpenWithConfig`: manifest `LaunchKind` match, 64 KiB cap, kindcheck
probe; delivered frozen at Mount via `LaunchConfig()`. The reason is
`LaunchReasonCaller` — no enum growth, adopter precedence logic
untouched — and the persisted `LaunchRow` carries the embedder's real
id as `CallerAppId`.

### SD7 — Workingsets: excluded

The workingset save path is window-reap-driven and stays so. An
embedded instance neither saves nor restores a workingset; an
embedder that wants inner state composes it into its *own* workingset
through the target's exported compose seams (play's `ComposeLaunch`
precedent). Recorded deferral, revisit with a concrete adopter.

### Probe item (implementation gate)

Whether `Panel*Inside` bodies behave inside an arbitrary sub-region is
unproven: play's Render is proven only at window-body scope (adhocdemo
itself calls `PanelTopInside` there). The implementation must probe a
panel-using app inside a constrained region before the canvas host
(S3) relies on it, and record the outcome in a dated Update.

## Alternatives

- **Pure library helper, no host involvement.** Formalizes today's
  bypass. Killed as *the* seam: apps cannot reach `FactsStoreI` or
  `BusProvider`, so embeds stay invisible to the facts plane (D4) and
  share the embedder's bus identity. It survives only as what
  `NewEmbedded` already is — a family-specific configurator.
- **Composed-identity bus client.** Killed by the SD3 verification:
  identity-keyed state (persist alias, column widths, any
  sender-attributed rows) fragments per embedder; attribution belongs
  on stamp and caller/parent columns, not in keying identity.
- **Bus-mediated acquisition (a `windowhost.embed` subject).** Killed:
  an embed passes an `AppI` instance and a per-frame Ui scope —
  in-process values that cannot ride the bus. ADR-0135's routing
  answer does not transfer.
- **Transplanting windows onto the embedder's surface.** Killed:
  `c.Window` blocks are top-level; the survey found no support for
  moving live windows into a transformed child scope (DN5).
- **Growing `AppI`/`MountContextI` with required methods.** Refused:
  the optional capability keeps the four-method contract frozen for
  every existing implementer (D3, DN1) — the compound-document
  failure mode the survey's rules exist to refuse.

## Consequences

### Positive

- Embedded mounts become ordinary lifecycle facts with a queryable
  containment edge — the S1 ports view draws real embeds.
- The canvas host (S3) and node bodies (S1) get their prerequisite:
  any registered app hostable in a region with host-grade
  bookkeeping.
- Singleton semantics are correct by construction (shared refcount);
  `Cancel()` finally fires for embedded instances.
- State facilities behave identically for windowed and embedded
  instances — verified concretely against ADR-0151.

### Negative

- Host surface grows: the capability, the handle, shell wiring, and a
  vocabulary column. The embedder carries a contractual `Close()`
  obligation — a leaked handle is a leaked mount (the failed-mount
  label and lifecycle rows make it visible, not impossible).
- The panel-in-sub-region probe may constrain which apps are
  embeddable in v1; that is a fact to discover, not a defect of the
  seam.

### Neutral

- `NewEmbedded` stays: applet documents are not registered `AppI`s;
  the two seams serve different grains and may converge later.
- Hygiene-not-security unchanged: caps and audit, not an in-process
  security boundary.
- F7 (pane-grained focus) and workingsets-for-embeds remain open,
  each with a recorded trigger.

## Status

Accepted (2026-07-31). Forks settled in the design dialogue the same
day; SD3 was verified against ADR-0151's implemented identity scheme
pre-acceptance.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [app-composition survey](../adr-background-work/app-composition-survey.md)
  — §4 (embedding), §13 (port↔signal), F2/F7; rules D3, D4, DN1, DN5,
  DN6, DN9.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — the contract, caps, host-salted id stacks (§SD9).
- [ADR-0135 — app-launch requests](./0135-app-launch-requests.md) —
  the config gate (kind match, size cap, kindcheck), launch facts,
  caller attribution.
- [ADR-0148 — app workingsets](./0148-app-workingsets.md) — launch
  reasons, the window-reap-driven save path SD7 defers to.
- [ADR-0151 — table column-width overrides](./0151-table-column-width-overrides.md)
  — the identity-keyed state used to verify SD3; its M4 shares this
  ADR's reachability context.
- [ADR-0132 — sqlapplet](./0132-sqlapplet-sql-defined-applets.md) and
  [ADR-0134 — ad-hoc datasets](./0134-adhoc-datasets.md) — the
  existing embed seam (`NewEmbedded`, §SD7/§SD8) this ADR generalizes
  beside, and the composed-stamp attribution precedent.
