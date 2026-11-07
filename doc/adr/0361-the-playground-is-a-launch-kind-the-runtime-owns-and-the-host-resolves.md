---
type: adr
status: proposed
date: 2026-11-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0361: The playground is a launch kind the runtime owns and the host resolves

## Context

No downstream module has asked to replace play as of 2026-11-07. The witness
for this record is a documented plan rather than a consumer (the keelson-app
skill's X1 allows either): the maintainer stated on 2026-11-07 that play and
chat are regular apps a consumer may replace, and the
[keelson-app skill](../skills/keelson-app/SKILL.md) records it in §0 and lists
the missing abstraction in §12. Chat is replaceable already: the harness that
holds the model grant is whatever the person registered as a coordinator
([ADR-0318](./0318-model-inference-is-the-harnesss-apps-expose-operations.md)).
Play is not, because every handover in the tree names it.

**The role.** "The playground" is what a window opens when it hands its data
to the person for composition (D3, D4 of the skill). As used in the tree it
does four things:

- **Open on a statement and an endpoint.** SQL for the editor, whether to run
  it, and whether it runs against the configured ClickHouse or the in-process
  keelson endpoint ([ADR-0135](./0135-app-launch-requests.md) §SD7).
- **Open on a published dataset alias or a bundle.** The window binds the
  aliases and follows publish and retract
  ([ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) §SD7),
  or opens a bundle's document and its datasets together
  ([ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) §SD4).
- **Be embedded, attenuated, as an applet or a bundle view.** An applet is
  play with a fixed buffer and a reduced toolbar
  ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)); a bundle view is the
  same inside another app's window.
- **Serve a catalog.** Agents drive the window through play's operations
  ([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)), and the
  chat's coordinator prompt names them.

**The coupling** (observed 2026-11-07 by grep over import statements; the
skill's §0 names the same three shapes):

1. **The launch leaf.** [`apps/play/launchcfg`](../../apps/play/launchcfg/launchcfg.go)
   holds `PlayLaunch`, its kind `playLaunch`, play's `AppId` and the endpoint
   constants, and registers the kind's decode probe with `kindcheck` in its
   own `init()`. Eight packages outside play import it to call
   `windowhost.RequestOpen(launchcfg.AppId, launchcfg.Kind, …)`: six apps or
   demo apps (chat, imzrt, mdedit, tally, writingstylescope, the regex
   explorer) and two runtime packages (`appcenter`, `trailviewer`). The
   runtime ones point a runtime window at one app by id. The kind's
   vocabulary members (`vdd.MembPlayLaunch*`) are already in the runtime's
   vocabulary; only the DTO and its codec live under `apps/`.
2. **The play package itself.** `sqlapplet` imports `apps/play` whole: it
   builds applets and bundle views from `play.NewLivePlayApp` and
   `*play.PlayApp`, offers `play.OperableOperations` as a bundle view's
   catalog, and installs its applet document parser into play with
   `play.SetAppletDocParser`. `adhocdemo` imports it for
   `play.BundleSpec` and `play.PublishBundle`, a producer-side helper that
   composes a bundle's document, checks it with the installed parser and
   publishes it through `adhocdata`. Importing `apps/play` registers play,
   since its `init()` does.
3. **Wiring.** The capslock gate blank-imports every registering app, play
   among them, and keeps a baseline entry under play's id. The tab commands
   name play as the default app and call `play.RegisterHostSql`; the
   desktop carousel blank-imports play and calls the same function.

A consumer that wants its own playground would today have to fork every
caller in shape 1, and would find runtime windows (the app center's "open in
play" sections, the trail viewer's *Send to play*) unable to reach it.

The constraints the answer works under:

- **Governance is the runtime's** (skill §0, X4). Which app a handover
  reaches, whether it may launch, and the record that it was opened belong to
  the host's open path, which already refuses with a reason, applies the
  launch limit ([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md))
  and persists the request as a fact (ADR-0135 §SD6). A substitution
  mechanism must not move any of that into callers.
- **A launch config is a vocabulary kind with a generated codec**, kinds are
  append-only (ADR-0135 §SD2, §SD8), and `Manifest.LaunchKind` names exactly
  one kind per app (§SD3). Launch rows and stored workingsets
  ([ADR-0148](./0148-app-workingsets.md)) carry the kind name.
- **A seam is typed and registered in Go at the wiring site** (X2), and
  registration order selects nothing (X3).
- **An embed is in-process value passing** of a concrete app type. The
  host-mediated embed of [ADR-0155](./0155-app-embed-seam.md), which would
  acquire an embedded instance by app id and kind, is accepted and has no
  implementation in the tree (observed 2026-11-07: no `EmbedHostI`).

## Design space (QOC)

**Question.** How does a caller hand data to "the playground" so that a
consumer can put its own app in that place, without moving governance out of
the runtime?

**Options.**

- **O1 — A playground launch kind in a runtime leaf.** The launch DTO moves to
  a new runtime leaf, package `playground` under `public/keelson/runtime`. A caller opens the
  kind without naming an app; the host routes the open to the launchable app
  whose manifest takes that kind. Play's leaf stays as an alias.
- **O2 — A registered role in the app registry.** A manifest declares
  `Roles: [playground]`; a caller requests an open by role, and the host
  resolves the role to an app. The DTO still needs a home, so O2 includes
  O1's leaf.
- **O3 — The leaf stays the contract; a consumer registers its app under
  play's id.** Callers keep naming `launchcfg.AppId`; a consumer omits play
  and registers its own app with that id and kind.
- **O4 — Confine imports and stop.** Every caller keeps naming play, from one
  file and through the leaf only; nothing is abstracted.

**Criteria.**

- **C1 — Substitutability.** A consumer replaces play as the destination of
  every handover, runtime windows included, without editing boxer's callers.
- **C2 — Callers that change.** How many packages in the tree are edited.
- **C3 — Governance stays in the runtime.** Resolution, refusal, the launch
  limit and the audit row stay in the host's open path, and the record names
  the app that really opened.
- **C4 — Bundle and embed surfaces.** The option addresses more than launch:
  publishing a bundle and embedding an attenuated playground.
- **C5 — Migration cost.** Wire changes, persisted rows, generated code, and
  the period during which both shapes exist.
- **C6 — Risk of a second path.** A second way to reach the playground, or a
  second declaration that can disagree with the first.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | +  | −− |
| C2 | −  | −  | ++ | +  |
| C3 | ++ | ++ | −− | +  |
| C4 | +  | +  | −  | −− |
| C5 | +  | −  | ++ | ++ |
| C6 | +  | −  | −  | ++ |

- **O1** covers launch and bundle publishing; the embed stays play's (SD6),
  which is why C4 is `+` and not `++`. Its cost is one edit per caller. The
  by-id open remains for callers that mean play specifically, so two forms
  reach play, but both go through the one subject and the one audit row.
- **O2** gives the same substitution as O1 at a higher price: a manifest
  field, a registry index, a column in `keelson('apps')`, and a request form,
  on top of O1's leaf. The role declaration sits beside `LaunchKind` and can
  disagree with it (an app that claims the role and takes another kind).
  `keelson('apps')` already has a `launch_kind` column, so "which app is the
  playground" is a query under O1 without a second declaration.
- **O3** changes nothing in the tree and breaks identity. An app id is a Go
  import path by convention, and facts, query-run stamps, the persist store,
  help books and the capslock baseline are keyed by it: under O3 they
  attribute a consumer's code to boxer's play, and play's stored workingsets
  would be delivered to a different app. The bundle and embed surfaces stay
  play's.
- **O4** is the first step of every other option, and the end state the
  principle rules out.

## Decision

**We propose O1: the playground's launch contract moves to a runtime leaf,
an open may name a kind instead of an app, and the host resolves the kind to
the one launchable app that takes it.** Bundle publishing moves with the
contract; the embed, the catalog and the wiring stay play's, for the reasons
below.

### SD1 — The launch contract moves to a runtime leaf, `playground`

The leaf holds the launch DTO, its kind, the endpoint constants and the
`kindcheck` registration, and a helper that opens the playground over
`windowhost.open`. It has no app and registers none (X5). The DTO moves
unchanged on the wire: the kind stays `playLaunch` and every column keeps its
name and type, so launch rows and stored workingsets written before the move
decode under it. The Go names in the leaf name the role, not play.

### SD2 — An open names a kind, and the host resolves it

A launch request may carry a kind and no target app. The host's open service
resolves it at each open:

- the launchable apps whose `LaunchKind` equals the kind are the candidates;
  the launch limit removes apps from that set as it removes them from every
  launch;
- one candidate is the target;
- several candidates and a designation for the kind: the designated app is
  the target. The designation is a call on `app.Registry` made at the wiring
  site, validated against the app's manifest, refused when repeated for the
  same kind;
- several candidates and no designation: the open is refused, and the reason
  names the candidates. Picking one by registration order would be the
  load-order selection X3 rules out;
- no candidate: the open is refused with a reason saying no app in this host
  takes the kind.

The resolved open then runs exactly as a by-id open: same subject, same
caller caps, same size cap, same probe, same reply. The persisted request
records the app that opened and that it was resolved from the kind, so the
audit row names the app, not the request's absence of one. This is the
composition shape ADR-0310 §SD1 calls a slot: one provider, or a declared
choice.

The rule is written over kinds, not over the playground: the host learns no
role names. The playground is the first kind the runtime owns; a second role
is a second leaf and no host change.

### SD3 — What a playground honours, and what is a hint

An app that takes the kind must honour `Sql`, `AutoRun`, `Endpoint`,
`Datasets`, `DatasetNames` and `Bundle`, since those carry the data a
handover hands over. `Tab`, `BandsSql` and `Live` name play's panes and play's
live lane; another playground may ignore them, logging a warning as play does
for an unknown tab. The leaf's doc comments carry this split, so a caller can
see which fields every destination respects.

### SD4 — Play's leaf becomes aliases; play declares the leaf's kind

`apps/play/launchcfg` re-exports the leaf's DTO, kind and constants as
aliases and drops its `kindcheck` registration, which would otherwise run
twice. `AppId` stays there, as play's own id, for callers that mean play and
not the role. Play's manifest declares the leaf's kind. The aliases go when
nothing outside `apps/play` imports the package.

### SD5 — Bundle publishing moves to the leaf

`BundleSpec`, `ComposeBundleDoc`, `PublishBundle`, the parsed document type
(`AppletDoc`) and its parser slot move to the leaf; `apps/play` keeps aliases
and forwarders. Reasons:

- A bundle is an input of the role: a playground opens one through the
  launch DTO's `Bundle` field, so the document it reads is part of the
  contract and not of play.
- The helper touches no play state. It composes a document, checks it with
  the installed parser, and publishes through `adhocdata`. It sits in play,
  per its own comment, because play's `publish_result` is a producer and
  sqlapplet imports play; a runtime leaf below both resolves that without
  the detour.
- A producer such as `adhocdemo` then publishes a bundle without importing,
  and so registering, play.

`sqlapplet` installs its parser into the leaf instead of into play. A bundle's
`Tabs` stay pane names a playground may ignore, as SD3's hints are; the
composer checks their shape, not that play has them.

### SD6 — The embed stays play's

`NewLivePlayApp`, `*PlayApp`, `sqlapplet.NewEmbedded`, `BundleView` and
`OperableOperations` are not given a role-level home. An applet is play with
play's attenuation (the toolbar, the tab set, the AutoRun and Live gates);
abstracting it means a substitute reimplementing that attenuation behind an
interface the tree has no second implementation of. A role-level embed has a
natural form once ADR-0155's host-mediated embed exists: acquire by kind,
resolved as SD2 resolves an open. Until then a consumer that substitutes the
playground and keeps applets links play for them, and designates its own app
for the kind (SD2).

The operations catalog also stays the app's own. A substitute declares its
own catalog, and the coordinator prompt that names play's operations is the
person's registration (ADR-0318) to change with it.

### SD7 — Capslock, the tab commands and the carousel stay as they are

These are wiring sites, which X2 names as the place a deployment composes.
The capslock gate imports play to check play, not to use the role; its list
is every registering app, kept complete by its own test. The tab commands and
the carousel are boxer's own hosts choosing play, including play's SQL
registrations (`play.RegisterHostSql`), which a host that links play needs
and a host that links another playground does not. A consumer writes its own
host the same way, naming its own app.

### SD8 — Callers open the playground, not play

Every handover that means "the playground" opens the leaf's kind through the
leaf's helper and stops importing play's leaf, keeping the import in one file
as D4 asks. The runtime packages among them (`appcenter`, `trailviewer`)
then import a runtime leaf instead of an app. The button text the skill
prescribes (*Open in play*) may stay: it describes boxer's default host, and
the reply's window key says which window opened.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| runtime leaf `playground` (new) | added: the launch DTO and kind, the endpoint constants, the open helper, the bundle composer and publisher, the bundle document type and its parser slot | the codec golden moves from play's leaf, byte-identical |
| `launchrequest` codec and its vocabulary | reshaped additively: a request may carry a kind and no target; the persisted row records the resolved app and that it was resolved | the codec's generated code and golden; readers of launch rows |
| `windowhost` open service | resolution by kind, three refusals (none, several undesignated, designation invalid) | `RequestOpen`'s documented refusals |
| `app.Registry` | added: a designation of an app for a kind | wiring sites that link two takers |
| `apps/play/launchcfg` | reshaped: aliases; its `kindcheck` registration removed | play's own tests |
| `play.BundleSpec`, `ComposeBundleDoc`, `PublishBundle`, `AppletDoc`, `SetAppletDocParser` | moved; play keeps aliases and forwarders | sqlapplet's parser install |

## Alternatives

The QOC section carries O2, O3 and O4 and their verdicts: O2 is rejected
until a second role exists or a role arises that is not a launch config
(X1); O3 because it gives a consumer's code boxer's identity; O4 as an end
state, though its confinement is SD8's first step. Further options:

- **A new kind, `playgroundLaunch`, instead of keeping `playLaunch`.**
  Rejected: kinds are append-only, so play would take two kinds with the same
  columns while `Manifest.LaunchKind` names one, and stored workingsets and
  launch rows under the old name would need a reader for each. The cost of
  keeping the name is a wire name that says *play* where it now means the
  role.
- **A function slot the wiring site sets, which callers call to open the
  playground.** Rejected: resolution would happen in the caller's process
  rather than in the host, so the launch limit and the audit row would see
  only whatever app the slot named, and a caller on another host would not
  see the slot at all.
- **A role-level embed interface now.** Rejected for this record (SD6): there
  is one implementation, and ADR-0155's seam, which the interface would sit
  on, is not built.
- **A minimum operations catalog every playground must declare.** Deferred:
  no second playground exists to say which operations are common, and a
  catalog is the app's own declaration (ADR-0269 §SD2).
- **Picking the first registered taker when several exist.** Rejected:
  registration order is import order, which selects code by load order (X3).

## Consequences

### Positive

- A consumer replaces play as the destination of every handover by
  registering one app, with no edit to boxer's callers, runtime windows
  included.
- A producer publishes a bundle without linking play.
- Runtime packages stop importing an app to open the playground.
- "Which app is the playground here" is a query over `keelson('apps')` by
  `launch_kind`.

### Negative

- Every handover caller is edited once, and play's leaf carries aliases until
  the last one moves.
- The wire name `playLaunch` keeps play's name for the role.
- A consumer that keeps applets or bundle views links play and so has two
  takers; it must designate one, or opens are refused.
- Opening a bundle needs an applet parser, which only `sqlapplet` installs;
  a host without `sqlapplet` refuses bundles, as it does now.
- Agents still drive the playground through play's catalog and the prompt
  that names it; a substitute without that catalog is a different surface to
  the model.

### Neutral

- Resolution by kind works for any kind. App-owned kinds have one taker in
  the tree, so opening them by kind resolves to their owner; this record
  moves no caller of those to the by-kind form.
- An open by play's id remains valid for a caller that means play.

## Migration — Tier 1

- **Breaks.** Nothing on the wire: the DTO's kind and columns are unchanged,
  and the request's new form is additive. No Go caller breaks while the
  aliases stand.
- **Path in the tree.** By kind of change:
  - *Runtime:* add the leaf; add resolution and its refusals to the open
    service; add the designation to the registry; extend the launch request
    codec and regenerate it.
  - *Play:* turn `launchcfg` into aliases and drop its registration; declare
    the leaf's kind; turn the bundle composer and parser slot into aliases
    and forwarders.
  - *sqlapplet:* install the parser into the leaf; nothing else.
  - *Handover callers* (apps and runtime packages that open play on data):
    open the leaf's kind through its helper, from one file.
  - *Bundle producers:* import the leaf for the composer and publisher.
  - *Capslock, tab commands, carousel:* nothing.
  - *Docs:* the keelson-app skill's §0, D4 and §12 point at this record once
    it is accepted.
- **Path for a consumer that substitutes the playground.**
  1. Register an app whose manifest takes the leaf's kind; decode the leaf's
     DTO in `Mount`; honour the fields SD3 requires.
  2. Either link neither play nor anything that imports it (`sqlapplet`,
     `adhocdemo`, the tab commands), or designate the app for the kind at the
     wiring site, or remove play with the launch limit.
  3. Declare the app's own operations catalog, and edit the coordinator
     prompt book if agents should drive it.
  4. To open bundles, link an applet parser (today, `sqlapplet`'s; see
     Deferred).
- **Old shape.** Play's `launchcfg` aliases are kept until no package outside
  `apps/play` imports them, then removed; the compiler is the check.

## Verification plan — Tier 1

- **Lane.** Default `go test`.
  - In `windowhost`: an open by kind reaches the sole taker; is refused with
    no taker and with two undesignated takers, naming them; reaches the
    designated taker; skips a taker the launch limit removed; persists a row
    naming the resolved app. A fixture app taking the kind stands in for a
    substitute.
  - In the leaf: the codec golden is byte-identical to play's old golden, and
    a launch row and a workingset record encoded under the old package
    decode under the new one.
  - The existing handover scenes (writingstylescope's, the trail viewer's)
    keep passing once their callers open by kind.
- **What would fail.** A wire change to the moved DTO, a by-kind open that
  picks among several takers silently, a refusal without a reason, or a row
  that records no target.
- **Gap.** No playground other than play exists in the tree, so substitution
  is tested against a fixture, not against an app a person uses.

## Deferred

- **A role-level embed** — applets and bundle views over any playground.
  Trigger: ADR-0155's host-mediated embed is built, or a consumer asks to
  embed its own playground.
- **A playground catalog contract** — operations every playground declares,
  and a coordinator prompt written against them. Trigger: a second
  playground that agents should drive with the shared prompt.
- **The agent's `launch` verb by kind.** It resolves app names; opening "the
  playground" by name waits for the same trigger.
- **The applet parser outside `sqlapplet`,** so a host opens bundles without
  linking play. Trigger: a consumer that wants bundles and not play.
- **A manifest role field (O2).** Trigger: a second role, or a role that is
  not a launch config.
- **The tab commands' default app by kind.** Trigger: a consumer that uses
  boxer's tab command unchanged with its own playground.

## Status

Proposed — awaiting review by the maintainer.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [keelson-app skill](../skills/keelson-app/SKILL.md) — §0 (the principle and the three coupling shapes), D3, D4, X2, X3, X5, §12.
- [keelson-app adherence survey](../adr-background-work/keelson-app-adherence-survey.md) — where the coupling was first recorded as wider than the leaf.
- [ADR-0135](./0135-app-launch-requests.md) — launch requests, `LaunchKind`, the open subject; §SD6 the audit row, §SD8 append-only kinds.
- [ADR-0155](./0155-app-embed-seam.md) — the host-mediated embed SD6 waits on.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) and [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) — datasets and bundles, the inputs SD3 and SD5 carry.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — applets as attenuated play.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) — §SD1, the slot shape SD2 follows.
- [ADR-0318](./0318-model-inference-is-the-harnesss-apps-expose-operations.md) — chat's substitutability.
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) — the launch limit resolution respects.
- [ADR-0148](./0148-app-workingsets.md) — workingsets stored under a launch kind.
- [ADR-0017](./0017-regex-explorer-extraction-handoff.md) — §SD4, why `AppId` sits in the leaf.
