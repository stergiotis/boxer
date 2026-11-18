---
type: adr
status: accepted
date: 2026-11-18
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-18
---

# ADR-0382: Embedding is by role — a pane under its host, and no host-mediated embed of a registered app

## Context

[ADR-0155](./0155-app-embed-seam.md) (accepted 2026-07-31) decided a
host-mediated embed: an embedder would acquire a hosted instance of any
registered app through an optional `EmbedHostI` capability, and the returned
handle would do for a Ui region what windowhost does for a window — an
instance key from the window key space, a parent-key column on lifecycle
facts, a shared mount refcount, a real stop channel, seam-owned id salts and
delegated focus, and config through the launch gate. The
[app-composition survey](../adr-background-work/app-composition-survey.md)
named that seam its S2 step, the prerequisite for a descriptive composition
graph (S1) and a canvas of applets (S3).

Observed 2026-11-18, the mechanism has no implementation and no consumer.
What has grown instead:

- **Panes.** Every embed in the tree draws a role-shaped component under its
  host: an SQL applet or a bundle view inside another app
  ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD8,
  [ADR-0134](./0134-adhoc-datasets.md) (superseded by ADR-0240) §SD7,
  [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md)),
  a chat pane ([ADR-0368](./0368-an-embedded-chat-a-chatlet-drawn-in-another-apps-window-under-the-hosts-governance.md)),
  a notebook pane
  ([ADR-0380](./0380-live-notebooks-records-computed-when-read-and-shown-as-a-bound-notebook.md)
  (proposed) §SD5). Each lives under the host's identity, governance and
  lifetime. The convention is settled across these records and stated in
  none of them as one contract.
- **Data and launch.** Composition between apps goes by publishing an ad-hoc
  dataset and asking the host to open play, or another app, over it
  ([ADR-0135](./0135-app-launch-requests.md)). It is the route apps take
  most often.
- **The canvas case needs no embed.** A grid of panels of one kind under
  shared variables — the Grafana-class surface the survey's S3 aimed at —
  is one app drawing renderers over Arrow frames, with one query scheduler
  per window; a panel hands its statement to play by publishing a dataset.
  Embedded play instances cannot place several panels of one kind in a grid
  with shared variables. A committed dashboard becomes an app of its own
  (ADR-0378 (proposed)), with an identity of its own rather than a composed
  one.
- **The parts that did spread.** ADR-0155's optional-capability pattern
  (its §SD1) is how host-held collaborators reach apps — `colwidth.HostI`,
  `app.BusCloserI`, `app.WindowFocusI` and others — and its identity rule
  (its §SD3) is cited wherever state is keyed. Both are practised slightly
  differently from how ADR-0155 wrote them: the capabilities ride the frame
  context, not the mount context, and an app built over another app (a
  specialised playground) presents its own id and keys its own state. The
  window-side stop-channel gap ADR-0155 §SD4 left open was closed by
  [ADR-0188](./0188-app-instance-effect-tracking.md) §SD2.

Two records wait on the unbuilt seam: ADR-0361 §SD6 and its deferred
role-level embed name "ADR-0155's host-mediated embed is built" as their
trigger, and the keelson positioning page carves it out as accepted and
unbuilt.

### What the compound-document systems show

The survey cites OLE, OpenDoc, KParts and Bonobo as systems that "died of
contract weight". That is accurate for OpenDoc and Bonobo and wrong for
KParts, and the difference is the useful part. What follows is a reading of
the public record, not a measurement; dates are to the year.

- **KDE dropped a CORBA component model for an in-process one.** KDE 1's
  OpenParts sat on CORBA. It was abandoned in 1999 for its cost and
  complexity, and KDE 2 (2000) shipped KParts instead: components are
  shared libraries loaded into the host process, written against the same
  toolkit, with a small base (`ReadOnlyPart` opens a URL, `ReadWritePart`
  also saves) and DCOP, later D-Bus, for the little that crosses
  processes. KParts is still part of KDE Frameworks.
- **GNOME kept CORBA and lost the component model.** Bonobo, built on
  ORBit, was deprecated during the GNOME 2 series and gone by GNOME 3.
- **What survived in KParts is role-shaped.** The parts in lasting use each
  serve a typed role above the generic part: an editor (KTextEditor, in
  Kate and KDevelop), a terminal (Konsole's part behind
  `TerminalInterface`, embedded in Dolphin and Kate), a document viewer
  (Okular's part). A host embeds *a terminal*, not *an app*.
- **The universal shell lost.** Konqueror — file manager, browser and
  viewer by embedding whichever part handled a MIME type — gave way to
  dedicated applications; Dolphin became KDE 4's file manager.
- **The costly part was merging chrome.** XMLGUI merged an active part's
  menus and toolbars into the host's: description files to maintain, and
  menus that changed as focus moved. It is OLE's in-place activation in a
  lighter form.
- **Optional extension interfaces held, as a hidden protocol.** Where the
  base contract fell short, KParts added interfaces a host probes for at
  runtime (`BrowserExtension`, `StatusBarExtension`). They worked; which
  ones a host and a part agree on is known only by convention.
- **In-process means shared failure and shared ownership.** A part that
  crashes takes its host down, and which side deletes the part and its
  widget, in what order, was a recurring source of crashes.

Mapped onto this tree: embedding in-process with one toolkit is already the
case. The pane route is the KParts role interface; the host-mediated seam
is the generic part and the Konqueror shell, the piece the record shows
least use for. The survey's rule DN1 — no in-place activation, negotiation
or storage protocol — is the XMLGUI lesson, and ADR-0155 §SD5's delegated
focus was the first step toward it. The optional frame-context capability
is KParts' extension interface, with its convention risk.

## Decision

We will not build a host-mediated embed of a registered app. An embed is a
pane a family exports and its host drives; composition between apps goes by
data and launch. ADR-0155's capability pattern and identity rule carry
forward, restated as they are practised. This record supersedes ADR-0155.

### SD1 — No host-mediated embed of a registered app

`EmbedHostI`, the embedded handle, the parent-instance-key column, the
shared refcount for embeds and config delivery through `AcquireEmbedded`
(ADR-0155 §SD1's capability, §SD2, §SD4–§SD7) are withdrawn. A registered
app is drawn whole only in a window.

The question reopens on a concrete case that needs a registered app drawn
inside another app under its own identity and lifecycle, which a pane
cannot serve. A proposal then argues against the survey's DN1 and the
record above, not only for the case.

### SD2 — Composition between apps is data and launch

An app that wants another app to work on its data publishes the data as an
ad-hoc dataset or bundle and asks the host to open the other app over it.
The open goes through the launch gate (ADR-0135) — kind match, size cap,
caller attribution — and lands in facts as any open does. The receiving app
is a window of its own, with its own identity, grants and lifecycle rows.

### SD3 — An embed is a pane under its host

A family that offers itself for embedding exports a pane: a constructor and
a type the host drives. The contract the existing panes share:

- **Identity.** The pane takes the host's app id and a key that names it
  within the host. `<host>#<key>` is its composed identity, used for
  attribution (§SD5).
- **Lifetime.** The host calls the pane's Mount, Frame and Unmount (or
  Close) from its own, each with its own context. The pane holds no
  lifetime of its own and writes no lifecycle fact.
- **Governance.** The pane's capabilities are the host's to declare: the
  family exports them (`EmbedCaps` or a `*Caps` list) and the host joins
  them into its manifest. The pane uses the host's bus, storage, lifetime
  scope and grants.
- **Frame discipline.** The pane scopes its widget ids by a salt derived
  from its identity, so two panes of one host do not collide, and reads
  per-frame host capabilities (§SD4) off the frame context the host hands
  it.
- **No chrome merging.** A pane draws in the region it is given. It does not
  contribute menus, toolbars or top-bar items to its host, and it does not
  negotiate its size.

The applet embed predates the composed-identity key and passes the stamp
and bus explicitly; it is the same contract in an older spelling and is not
migrated.

### SD4 — Host-held collaborators reach apps as optional frame-context capabilities

Carried from ADR-0155 §SD1. A collaborator the host holds and an app must
not (the facts store, the bus provider) reaches the app as a narrow
interface the host's context implements and the app type-asserts. A host
without it, a test fake or a headless one, costs nothing. The capability
rides the frame context; a mount-time need is met by asserting at the first
frame.

A capability goes to whoever asserts it, so it stays narrow: a verb set
scoped to the caller's own id. Anything that must be gated per app takes a
grant instead ([ADR-0185](./0185-durable-app-state-manager.md) Q2 records
why).

### SD5 — State is keyed by the identity the context presents

Carried from ADR-0155 §SD3, sharpened. State is keyed by the app id the
context presents — the app whose window the person sees. A pane's composed
identity is attribution: on query-run stamps, on caller columns, on what a
pane stamps itself with. Inside the host's own storage, the pane's key may
prefix the pane's keys so that two panes of one host keep their state apart
(ADR-0368 §SD3); that subdivides the host's state, it does not add an
identity. An app built over another app presents its own id and keys its
own state.

State whose key is content-derived (column widths under
[ADR-0151](./0151-table-column-width-overrides.md)) composes with this
unchanged, since no widget id or salt enters it.

## Alternatives

- **Keep ADR-0155 accepted and record a deferral in its `## Updates`.**
  Rejected: the body would describe a mechanism the Updates withdraw, and
  the pane contract — the decision that did happen — would still have no
  record. That is the documentation standard's case for Tier 3.
- **Build ADR-0155 now.** Rejected: no consumer, the canvas-shaped
  case is served better without it, and its focus
  and lifecycle parts are the slope the compound-document record warns
  about.
- **A shared Go interface every pane implements.** Deferred, not rejected.
  The panes differ in configuration by role, and ADR-0361 refused a
  role-level abstraction with one implementation. A mechanical check over
  pane constructors is the lighter form (Deferred).
- **A bus sub-identity per pane.** Rejected as ADR-0155 and ADR-0368 rejected
  it: identity-keyed state would fork per embedder, and the services would
  have to trust a sub-identity inside the host's own sender.
- **Embedded play instances as dashboard panels.** Rejected: a
  grid of panels of one kind with shared variables is a viewer drawing
  renderers over frames, not N playgrounds.

## Consequences

### Positive

- The pane contract that four families already follow is stated once, with
  its limits (no chrome merging, no own lifetime).
- ADR-0361's deferred role-level embed waits on a stated trigger rather
  than on a seam no one is building.
- Nothing in the app contract or the host grows.

### Negative

- A pane leaves no lifecycle fact of its own. Containment is visible only
  through attribution stamps, which covers query runs and what a pane
  stamps; the survey's rule D4 (every relation queryable) is met partly.
- A registered app cannot be drawn whole inside another app. A family that
  wants to be embeddable writes its own pane.
- A panic in any Frame, a pane's included, takes the host process down. No
  host recovers a frame panic (observed 2026-11-18); this record does not
  introduce that, and panes share it.

### Neutral

- ADR-0155 stays readable as superseded. Code and how-to citations of its
  §SD1 and §SD3 moved to §SD4 and §SD5 here; accepted records that cite it
  as history were left as written.

## Surfaces — Tier 1

Documents and comments only; no code changes shape. Moved in the accepting change.

| Surface | Change | Moves with it |
| --- | --- | --- |
| ADR-0155 | status `superseded`, `superseded-by: ADR-0382`, pointer at the top of the body | — |
| ADR-0361 §SD6 and its deferred role-level embed | trigger restated: a concrete case under §SD1 | — |
| ADR-0368 §SD3 | reference to §SD5 here | — |
| keelson positioning page | the "accepted and unbuilt" embed interface entry removed | — |
| how-to guides, keelson-app skill, Go comments | citations of ADR-0155 §SD1 / §SD3 retargeted to §SD4 / §SD5 | — |

## Verification plan — Tier 1

- **Lane.** None mechanical, because the decision is to not build a surface.
  The windowhost column-width test already asserts that the frame context
  the host wraps presents the window's app id, which §SD5 restates.
- **What would fail.** A pane that keys host storage under a composed
  identity, or a family that merges chrome into its host, is caught in
  review only.
- **Gap.** The pane contract has no conformance check (Deferred).

## Deferred

- **Lifecycle facts for panes.** A pane's mount and unmount as rows under
  the host's instance key, which would draw embeds in a composition graph.
  Trigger: the survey's S1 view is built and finds the stamps insufficient.
- **Frame-panic containment.** Recovering a panic in one window's or one
  pane's Frame and showing a failed label in its place. Trigger: a panic
  reported from a pane or app in use, or a pane from outside the tree.
- **A conformance check over pane constructors.** In the shape of the
  widget conformance check: a pane salts its ids, exports its caps, takes a
  host id and key. Trigger: a fifth pane family, or a pane that breaks one
  of §SD3's rules.

## Status

Accepted 2026-11-18. ADR-0155 is superseded, and the surfaces above
moved, in the same change.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0155 — app embed seam](./0155-app-embed-seam.md) — the decision this
  record supersedes; §SD1 and §SD3 carry forward as §SD4 and
  §SD5.
- [app-composition survey](../adr-background-work/app-composition-survey.md)
  — §4 and §9 (the compound-document systems), F2, rules D3, D4, DN1.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD8,
  [ADR-0134](./0134-adhoc-datasets.md) (superseded by ADR-0240) §SD7,
  [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md)
  — the applet and bundle-view embeds, the composed stamp.
- [ADR-0368](./0368-an-embedded-chat-a-chatlet-drawn-in-another-apps-window-under-the-hosts-governance.md)
  — the chat pane; the key that keeps panes apart.
- [ADR-0380](./0380-live-notebooks-records-computed-when-read-and-shown-as-a-bound-notebook.md)
  (proposed) §SD5 — the notebook pane.
- [ADR-0135](./0135-app-launch-requests.md) — the launch gate §SD2 routes
  composition through.
- [ADR-0185](./0185-durable-app-state-manager.md) — why a capability that
  must be gated is a grant, not an assertion.
- [ADR-0188](./0188-app-instance-effect-tracking.md) §SD2 — the window stop
  channel that closed ADR-0155 §SD4's follow-up.
- [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)
  §SD6 — the role-level embed that waited on ADR-0155.
- [ADR-0378](./0378-dashboardlets-a-kind-for-a-dashboard-minted-from-a-committed-document.md)
  (proposed) — a committed dashboard minted as an app of its own.
