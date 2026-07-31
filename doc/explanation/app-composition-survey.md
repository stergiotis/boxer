---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Design-space survey, compiled
> 2026-07-31, ahead of any decision: nothing in here is settled, and an ADR —
> not this page — is where any of it would become one. Provenance is
> three-tiered and marked throughout: (a) claims about this repository were
> verified against the working tree on the compile date; (b) two external
> facts (egui `Scene`, JSON Canvas) were verified against their upstream
> sources; (c) the literature in §9 and the prior-art anchors in §12–§13
> come from general knowledge —
> titles and venues are given so a reader can verify, but the works were not
> re-read for this survey. Treat tier (c) as pointers, not citations.

# Composing keelson apps — a design-space survey

## 1 Question and scope

Keelson apps compose today at exactly two grains: a human opens several
windows side by side, and — since ADR-0135 — one app can open another with a
typed argument record. The question this survey maps: what would it take to
compose apps into *deliberate, higher-order user experiences*, and which of
the candidate shapes are worth building first. Four target shapes set the
scope, plus a fifth axis the literature adds:

- **Workflows** — static or dynamic step sequences with data handover
  (assistance / wizard experiences).
- **Canvases** — several apps co-displayed as panels of one surface, in the
  way Grafana composes panels into a dashboard.
- **Ports and wires** — data-flow made visible: attachment points on app
  surfaces, connected by drawn (bezier) links that duct data.
- **ZUI** — a zooming user interface, where scale is the organizing
  principle; egui recently added machinery for this.
- **Composition as a document** — the literature's recurring answer (§9):
  the composition itself as a durable, inspectable artifact.

The survey's one structural claim, argued in §11: these are not five
features. They are five *presentations* of one underlying record — a set of
participants, typed data bindings between them, and a presentation document.
Most of the substrate for that record already exists in the repository; the
options differ mainly in which renderer gets built over it, and in how the
missing pieces (an embedding contract, output-port declarations) are cut.

## 2 What the substrate already provides

This section is inventory, not proposal. Each item was verified in the
working tree on the compile date.

### 2.1 The app contract is already host-agnostic

`public/keelson/runtime/app` defines the contract: `AppI` is
`Manifest / Mount / Frame / Unmount`; app↔window is 1:1 **and the host owns
the window**. A `SurfaceWindowed` app must *not* open its own window — the
host wraps `Frame()` in a window scope it controls, pre-pushes a
per-instance widget-id salt before every frame, and mints the app's bus
client with its declared caps at open. The package documentation states the
consequence directly: placement is the host's decision, and "the same app
source runs unchanged across hosts."

This is the single most load-bearing fact in the survey. Embedding an app in
another app, placing it on a canvas, or rendering it inside a zoomable scene
are all *new hosts for the same contract*, not surgery on apps.

### 2.2 windowhost — the shell is an in-process window manager

`public/keelson/runtime/windowhost` renders every open app as a top-level
`c.Window` inside one shared egui context, tracks the active window via the
`WINDOW_TOPMOST` response flag plus a sticky `pickActiveWindow`, stamps
`WindowFocused` onto each app's frame context (`app.WindowFocusI`), reaps
closed windows, saves/restores workingsets, and services the audited
`windowhost.open` subject. `OpenOrRaise` exists for singleton-ish reuse.
Notable constraint for §8: `c.Window` blocks are top-level in the egui
model — a window cannot be transplanted into a transformed child scope, so
any scene/canvas placement goes through app *bodies*, not through moving
existing windows.

### 2.3 One-shot typed handover: launch requests (ADR-0135)

[ADR-0135](../adr/0135-app-launch-requests.md) shipped the composition
primitive: `windowhost.open` carries a leeway-declared config DTO
(`Manifest.LaunchKind`, kind-checked, ≤64 KiB), delivered frozen at Mount,
persisted as a launch fact with caller attribution. Three deferrals recorded
there matter here: a reuse/focus window policy, a URL scheme, and — most
relevant — **content-based routing** (the Plan 9 plumber shape), recorded as
"a router would be an ordinary caller layered *over* `windowhost.open`."
A workflow conductor is exactly such a caller.

### 2.4 Continuous typed handover: ad-hoc datasets (ADR-0134)

[ADR-0134](../adr/0134-adhoc-datasets.md) shipped the data plane between
live apps: an app publishes an ephemeral, typed, encrypted-at-rest dataset;
a grant hands the `keelson('<handle>')` reference to a consumer; republish
bumps a revision that triggers a live re-run; retraction happens at unmount;
`keelson('adhoc')` catalogs it. `apps/adhocdemo` demonstrates the full
producer→consumer pipeline inside one window today: compute rows → publish →
embed a SQL applet bound to the handle → republish → the applet re-runs.

### 2.5 The curation axis: applets and workingsets (ADR-0132, ADR-0148)

[ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md) established that a
*frozen, committed* argument record is an app in its own right (a SQL applet
is play with the chrome removed and the buffer fixed), and
[ADR-0148](../adr/0148-app-workingsets.md) formalized the other end of the
same axis: **ambient workingset → launch config → applet** — one DTO family,
three mutability tiers. Any composition record should expect to live on this
axis too (user-composed and ambient at one end, committed and curated at the
other), rather than inventing a new one.

### 2.6 An embedding seam exists, for one family

`apps/sqlapplet/sqlapplet_embed.go` (`NewEmbedded`) lets any app host a SQL
applet inside its own window: composed identity stamp `<embedder-id>#<slug>`,
caps riding the embedder's manifest, the embedder driving the inner
Mount/Frame/Unmount by hand. It is play-specific. The generalization — the
same seam for an arbitrary `AppI` — is the main missing contract in this
whole space (§4).

### 2.7 Rendering machinery already in stock

- **Node editor**: [ADR-0021](../adr/0021-imzero2-snarl-node-editor-binding.md)
  bound `egui-snarl` — Go-authoritative topology, typed/colored pins, bezier
  wires, connect/disconnect/move/select events fetched back to Go, viewport
  zoom 0.2–2.0× with a crisp-text mode, and **per-node deferred bodies keyed
  by u64** — arbitrary egui2 content can render inside a node. Its ADR
  context names the motivation: a visual builder for query/transform
  pipelines. The ports-and-wires UX has its widget already. (Update
  2026-07-31: a Go-native rewrite replacing the `egui-snarl` dependency
  is decided and in flight; the surface above is the capability
  baseline it inherits.)
- **Static graph layout**: `widgets/layeredgraph` (graphviz-WASM layered
  layouts, pan/zoom, hover/click) — the read-only complement, proven in
  play's Network tab.
- **Panel composition inside one app**: play is the in-app precedent —
  egui_dock tabs, panels bound to query nodes by CTE-name convention,
  shared selection signals, `widgets/lazypane` gating heavy bodies on
  actual visibility (the dormancy/LOD primitive a canvas will need).
- **Zoom/pan container in the dependency, not yet in the IDL**: the vendored
  egui (≥ 0.35) includes `egui::Scene`, a pannable/zoomable container for
  arbitrary child UIs, added in egui 0.31 precisely "to make it easier to
  implement a graph editor" (verified upstream). Only snarl uses it
  internally today; egui2 exposes no standalone Scene container.
  [ADR-0140](../adr/0140-imzero2-hover-scoped-wheel-capture.md) already
  solved per-canvas wheel ownership — the input-nesting groundwork.

### 2.8 The facts plane: composition is already queryable

Launches (caller→target, config), workingsets, app lifecycle rows, ad-hoc
dataset catalog and grants, declared caps, and — one level down —
[ADR-0126](../adr/0126-appliance-topology-as-data.md) process/socket
topology are all typed facts or introspection tables. A "what talks to
what" graph is *already a query*, not a data structure waiting to be
designed. This is the descriptive half of the ports idea (§7) nearly for
free.

## 3 A vocabulary for the design space

Four notions recur; naming them keeps the options comparable.

- **Participant** — an app, an applet, or (finer) a play panel bound to a
  query. The applet tier (§2.5) is the natural default participant: cheap to
  mint, declarative, already manifest-bearing.
- **Port** — a *declared, typed attachment point*. Most already exist as
  manifest surface: `LaunchKind` (typed input), cap subjects (stream
  in/out), applet `datasets:` aliases (dataset inputs), workingset kind.
  The one real gap is the **output side**: what an app *publishes* (its
  datasets, its useful subjects) is observable in the facts plane but not
  declared anywhere enumerable. Making outputs declarable is the only new
  manifest surface this whole space seems to need.
- **Binding (wire)** — a (source port, sink port, channel) triple. The
  channel is chosen from the existing handover inventory:

  | Channel | Shape | Typing | Lifetime | Shipped |
  |---|---|---|---|---|
  | Launch config (0135) | one-shot push at Mount | leeway kind + kindcheck | frozen per window | yes |
  | Ad-hoc dataset (0134) | publish / SQL pull + revision signal | bounded column set | until retract | yes |
  | Workingset (0148) | ambient save/restore | LaunchKind DTO | across closes | yes |
  | Bus subject (0026) | stream / request-reply | per-subject codec | while mounted | yes |
  | Facts / CH tables | durable pull, joins | leeway schema | durable | yes |

  No new channel appears to be required; a "wire" is a *presentation* of one
  of these five.
- **Presentation** — the document that arranges participants for a human:
  a sequence (workflow), a layout (canvas), a wiring diagram (ports view),
  a scale hierarchy (ZUI). Presentations are data; §2.5's axis says where
  such data lives (ambient / launched / committed).

## 4 Option A — embedding (compound windows)

**What it is.** App B renders inside app A's window (or inside a host
surface that is not a window at all). The prerequisite for canvases (§6),
node bodies (§7), and ZUI (§8) alike.

**What exists.** The host-agnostic contract (§2.1); the play-specific
`NewEmbedded` with its composed stamp and caps-ride-embedder doctrine
(§2.6); per-instance id-salting including play's multi-stack `SetBaseSalt`
precedent; `lazypane` for dormant bodies.

**What's missing.** An `AppI`-general embedded-mount seam: construct a
registered app, mount it against a context whose identity is the composed
stamp, drive its `Frame` inside a caller-chosen Ui scope, unmount with the
embedder. The three design problems are (a) **identity** — composed stamp
vs. a host-minted instance key; what the lifecycle/audit rows say;
(b) **focus and input** — `WindowFocusI` is window-grained; an embedded body
needs a pane-grained equivalent, or chord/global-input handling breaks
exactly the way the multi-instance Ctrl+Enter broadcast did before the
focus seam; (c) **caps** — v1 doctrine exists (caps ride the embedder;
hygiene-not-security), it just needs restating at the general seam.

**Risks / lessons.** The compound-document systems of the 1990s (OpenDoc,
OLE, later KParts/Bonobo — §9) died partly of contract weight: embedding
dragged in negotiation protocols, in-place activation, storage models.
The existing contract is already minimal (four methods, host-prepared
context); the risk to manage is *growth pressure* on it, not its absence.

## 5 Option B — workflows (composition in time)

**What it is.** A guided sequence: step N's app produces data, step N+1's
app receives it; chrome shows progress; "static" = authored steps,
"dynamic" = an assistant proposes the next step.

**What exists.** Every ingredient except the conductor: launch requests
(open step N+1 with a typed config), ad-hoc datasets (hand over live data;
revision signals already re-run consumers), workingsets (suspend/resume a
step), audit facts (a completed run leaves a queryable trail *by
construction* — launch rows + dataset revisions + lifecycle rows).
ADR-0135's deferred content router is the natural conductor shape: an
ordinary caller above `windowhost.open` that picks targets by rule rather
than by name.

**What's missing.** (a) A workflow *record* — steps, per-step participant +
launch config template, handover declarations, completion condition;
(b) a conductor — realistically a plain app (no new runtime concept) that
owns the record, opens/embeds steps, watches for completion signals, and
renders stepper chrome; (c) a completion vocabulary — "step done" as a
dataset publication, an explicit user action, or a bus event.

**Static vs. dynamic.** The static wizard needs no intelligence and
exercises the whole record. The dynamic tier slots in later without
re-architecture: [ADR-0120](../adr/0120-play-natural-language-ask-panel.md)
(NL ask) and [ADR-0139](../adr/0139-semantic-layer-text2dsl.md) (grounding)
give the assistant; the port/kind declarations give it an *enumerable,
typed action space* — the same property Apple's App Intents exploit, and
ADR-0135 already cites App Intents as its closest modern analog. This is
premise P7 of `why-boxer.md` (interfaces for agentic systems via
machine-readable surfaces) applied to composition.

**Risks.** Wizard UIs age badly when they gate expert users; the literature
(mixed-initiative UI, §9) argues for workflows as *suggestions over an open
surface*, not corridors. The record should permit "all steps visible,
recommended order" as a rendering, not hard-code linearity.

## 6 Option C — canvases (composition in space)

**What it is.** N participants co-displayed in one arranged surface —
Grafana's dashboard model.

**Grafana, decomposed.** A Grafana dashboard = grid layout + per-panel
query/viz + *template variables* (the shared-signal handover: one dropdown
re-parameterizes many panels) + drill-down links (escape into a fuller
tool). The boxer translation is direct: grid of applets + shared params +
launch requests as drill-down. Notably, **play already is this internally**
— panels over one query graph with shared signals — and the applet tier
exists precisely to freeze play into panel-sized units. So the cheap v1 is
**canvas-of-applets**: a committed "canvas book" (the sqlapplet book
pattern extended: several docs/fences + a layout + shared param
declarations), minted as one manifest. No embed seam needed if the canvas
host is itself the embedder via `NewEmbedded` — the seam generalization
(§4) is needed the day a *non-applet* app joins a canvas.

**Shared signals across participants** are the hard semantic. Play's own
lesson (the layered-graph panel's local-selection clamp) is that signal
scoping is subtle even inside one app; across participants it needs an
explicit contract — plausibly "a canvas declares named params; participant
ports bind to them", mirroring how applet `datasets:` aliases bind handles.

**Layout as data.** JSON Canvas (verified: open MIT spec, `.canvas`,
nodes text/file/link/group + edges) is the current interchange floor for
infinite-canvas documents — worth mirroring in shape (nodes + optional
edges + groups) even if no interop is intended, because it encodes the
consensus minimum.

**Risks.** Cost: N live participants each run full frame machinery;
`lazypane` gating and Live-run gating for off-screen panels are mandatory,
not optional. And a canvas must not become a second play: fine-grained data
composition stays SQL; the canvas composes *apps*.

## 7 Option D — ports and wires (composition as topology)

**What it is.** Ports rendered on participant surfaces; bezier connectors
ducting data. Two modes with very different costs:

**Descriptive (read-only) — nearly free.** The real dataflow is already in
the facts plane (§2.8): launch edges, dataset publisher→grantee edges, cap
subject edges, plus ADR-0126's process/socket layer underneath. A first cut
is literally a play query rendered in the existing Network tab; a dedicated
surface upgrades it to snarl with per-node bodies (live thumbnails via §4).
Value: composition debugging, "why is this applet live", audit made
spatial. This mode also *forces the port vocabulary into existence* —
naming what the edges attach to — which is exactly the missing manifest
surface (§3), discovered against real data instead of invented.

**Prescriptive (wiring edits the world) — the expensive half.** A drawn
wire executes grant / launch-with-config / bind operations; a deleted wire
retracts. All the operations exist; what's new is the editor semantics:
typed pin compatibility (kind matching — snarl's pin `kind` u32 maps
directly), partial-wiring states, and the authority question (does the
canvas own the wiring, or does it request it from the apps?).

**Lessons from the genre (§9).** Wires work at *coarse granularity and low
edge counts* (Max/MSP patches degrade into spaghetti; every mature system
grew subpatches — i.e., embedding — to recover); typed pins are what keep
casual wiring sane; Tableau's dashboard actions show the inverse failure —
its inter-view wires exist but are invisible in dialogs, so nobody can read
a dashboard's dataflow. Boxer's stated premises favor the visible-and-
queryable version. The trap to refuse: wires as a general programming
surface. SQL remains the fine-grained composition language; wires connect
*apps*.

## 8 Option E — ZUI (composition by scale)

**What it is.** One surface where zooming out gives overview and zooming in
gives detail, possibly replacing window management altogether.

**Machinery status (verified).** `egui::Scene` — pan/zoom over arbitrary
child UIs — is in the vendored egui; snarl already runs its viewport on it;
egui2 does not yet expose it standalone. Binding it is ordinary IDL work
(container with a deferred body, in the dock/snarl mold). Two constraints:
`c.Window` is top-level, so a ZUI hosts app *bodies* (needs §4), not
transplanted windows; and text/tessellation quality under scale costs real
work — snarl's `crispMagnifiedText` (pre-scale style, cap zoom at 1×) shows
both the problem and one honest workaround.

**The real feature is semantic zoom.** Geometric shrinking of a full app
body is legible only near 1×. The literature's core result (Pad++, §9) is
that zoom levels should swap *representations*: icon → title+status line →
summary widget → full body. That is an LOD contract per participant — a
natural optional capability interface (the `WorkingsetComposerI` pattern:
apps that can render a summary declare it; others get a scaled thumbnail or
an icon). `lazypane`'s probe-gating is the dormancy mechanism for far-away
participants.

**Navigation hazards are known and named.** "Desert fog" (Jul & Furnas,
§9): at some zoom states nothing on screen indicates where anything is.
Mitigations are standard — overview inset, zoom-to-fit, landmarks, animated
transitions — and should be treated as requirements, not polish. Prezi's
trajectory is the cautionary consumer tale: spatial travel does not by
itself convey structure.

**Assessment.** ZUI-as-shell (windowhost replaced by one zoomable world) is
the biggest bet in this survey and the least reversible; ZUI-as-canvas-
viewport (the §6 canvas gains Scene pan/zoom + LOD tiers) captures most of
the value at a fraction of the risk, and windowhost keeps working beside
it. The scrolling-WM lineage (PaperWM, Niri) even suggests an intermediate:
a 1-D strip placement mode is a canvas with one axis.

## 9 Literature (tier c — pointers, not citations)

Grouped by what each contributes; all from general knowledge unless marked
verified.

**Composition-as-document.**
- *Boxer* (diSessa & Abelson, 1986): spatial containment as the single
  structuring principle — everything is a box, boxes nest, what you see is
  the thing itself ("naive realism"). The strongest philosophical precedent
  for embedding + zoom as one idea.
- *OpenDoc / OLE / KParts*: compound documents; died of contract weight and
  platform politics — the lesson is minimal embedding contracts (§4).
- *JSON Canvas* (verified: jsoncanvas.org, MIT, 2024): the modern consensus
  minimum for canvas documents — typed nodes + edges + groups.
- Ink & Switch's *Muse* (spatial thinking) and especially *Embark* (2023):
  documents with embedded live views and data flowing through document
  structure — a workflow-with-handover rendered as a document, the closest
  recent cousin of §5+§6 combined.

**Routing and intents.** Plan 9 *plumbing* (Pike) — content-based routing
under user-owned rules; Android intents; Apple App Intents (typed,
enumerable actions — the assistant substrate). ADR-0135 already carries
this lineage; the deferred router is the workflow conductor.

**Task/activity workspaces.** *Rooms* (Henderson & Card, 1986) — named
workspaces of windows per task; Bardram's *activity-based computing* —
activities as first-class, suspendable, migratable objects spanning apps.
ADR-0148's deferred "desktop-level session snapshot" is exactly a Room; a
canvas or workflow instance is an activity with a presentation attached.

**Dataflow canvases.** Max/MSP & Pure Data (media patching), LabVIEW,
Simulink, Blender nodes, Unreal Blueprints, Node-RED, n8n; Yahoo! Pipes
(2007–2015) as the canonical death — hosted, no textual escape hatch;
Observable (reactive doc-shaped dataflow); Enso (dual text/visual);
tldraw's "computer" experiment (2024, canvas of wired components).
Recurring findings: typed ports or spaghetti; coarse granularity or
unreadability; subpatches (= embedding) always emerge; keep a textual
projection (boxer's: the record is facts, queryable in play).

**ZUI.** Pad (Perlin & Fox, 1993); Pad++ (Bederson & Hollan, 1994 —
semantic zoom, portals); Jazz/Piccolo toolkits; Raskin's zoomworld (*The
Humane Interface*, 2000); "desert fog" (Jul & Furnas, 1998) for the
navigation failure mode; Prezi as the consumer-scale outcome. egui `Scene`
(verified: added 0.31, PR #5505) is the local implementation substrate.

**Assistance.** Horvitz's mixed-initiative principles (1999): assist over
an open surface, defer to user control — the argument for workflows as
recommendations rather than corridors (§5).

## 10 Dos and don'ts

What §§2–9 settle compresses into working rules. Two framing caveats
first. These rules are worth writing down *here* because the substrate
already supplies their preconditions — typed channels before any wire
UI, a four-method embedding contract, composition relations landing in
audited facts, host-owned placement — in one process and one data
model. Systems holding pieces of that conjunction exist (moldable
Smalltalk-lineage toolkits, commercial node environments, reactive
notebooks); the conjunction is the rare part. That is a fit claim
scoped to this project's own premises, not a market claim, and the
substrate is still two contracts short of it (§4's embed seam, §3's
output ports). Second: a rule anchored only in tier-(c) literature is
a hypothesis with good pedigree, not a law; and none of these rules
decides a §14 fork — where one touches a fork it names the failure
mode and leaves the mechanism open.

### Do

- **D1 — Make the record first-class; treat every UX as a renderer.**
  Wizard, canvas, wiring view and zoom hierarchy should be four
  presentations of one facts-modelled record, so renderers ship
  independently and none blocks the others — the ADR-0126 move applied
  to UI, and the host-pulled-state shape that finally shipped elsewhere
  as Wayland session management, which ADR-0148 already implements.
- **D2 — Type a wire before drawing it.** A binding names two ports and
  one of the five §3 channels; a kind mismatch is refused at the
  boundary, in the ADR-0135 kindcheck mold. Typed pins are the one
  common trait of the node systems that survived; untyped was Pipes.
- **D3 — Grow hosts, not the app contract.** Canvas cells, node bodies
  and scenes are new hosts for the same four-method `AppI`. Pressure to
  add negotiation, activation or storage protocols to the contract is
  the compound-document failure mode knocking (§4).
- **D4 — Keep every relation queryable.** A grant, launch or binding
  that exists must land in facts; the ports view stays a query over
  reality, never a parallel data structure. Tableau's invisible
  dashboard actions are the counterexample; P3/P4 the doctrine.
- **D5 — In the ports view, descriptive before prescriptive.** Draw the
  real topology first and let it correct the port vocabulary before any
  wire-create gesture edits the world (§7).
- **D6 — Zoom by representation, not only by scale.** Per-participant
  LOD tiers (icon → summary → body), with overview inset, zoom-to-fit
  and landmarks treated as requirements — semantic zoom is the feature,
  desert fog the named failure (§8).
- **D7 — Keep a textual/data projection of every composition.**
  Committed books and queryable records are the escape hatch the hosted
  canvases lacked (§9's recurring dataflow lesson).
- **D8 — Render workflows as recommended order over an open surface.**
  Steps visible, order suggested, gating only where a handover
  genuinely requires it — the mixed-initiative principle; corridor
  wizards age badly (§5).
- **D9 — Budget dormancy from the first canvas.** Off-screen or
  far-zoomed participants must stop costing: lazypane probe-gating and
  Live-run gating are mandatory at N participants, not polish (§6).
- **D10 — One vocabulary for humans and agents.** Whatever §14 F3
  decides about declaring outputs, the assistant's action space and the
  port surface must be the same names — P7, the App Intents property
  (§5).
- **D11 — Derive bindings where derivable; author only the rest.** An
  applet that references a dataset handle already implies that edge;
  drawing derivable edges from reality keeps the authored record small
  and keeps D4 honest — the reference-derived-DAG lesson (§12).
- **D12 — Make propagation policy a per-wire, class-gated attribute.**
  A wire defaults to live only where the consumer's reaction is
  provably read-class — the ADR-0132 AutoRun gate generalized;
  side-effectful consumers default to discrete or acknowledged
  handover (§13).

### Don't

- **DN1 — Don't grow a component model.** In-place activation, format
  negotiation and storage protocols are what sank the compound-document
  systems; the minimal contract is the asset being protected (§4).
- **DN2 — Don't let wires become a programming language.** Wires
  compose participants; SQL composes data. The moment a canvas wants
  loops or conditionals in wires, the answer is an applet, not richer
  wires — Blueprints sprawl, Max subpatching (§7).
- **DN3 — Don't ship geometric-only zoom as "the ZUI".** Pan/zoom
  without representation change is a demo, not a feature — the Prezi
  lesson. §14 F6 keeps the sequencing open; the end state is not open.
- **DN4 — Don't mint a second dialect.** The composition record rides
  existing kinds and stores; a schema-less composition doc repeats what
  ADR-0135 killed as O3.
- **DN5 — Don't transplant windows.** `c.Window` is top-level; canvas
  and scene placement go through app bodies via the embed seam (§2.2,
  §8). Moving live windows onto a canvas is the path this survey found
  no support for.
- **DN6 — Don't let embedded bodies read global input ungated.** The
  multi-instance chord broadcast is the proven failure; every consumer
  of process-global input needs a focus gate, whichever mechanism §14
  F7 picks (§4).
- **DN7 — Don't rebuild play on the canvas.** A canvas growing query
  editing, signals and result panels is play re-implemented; applets
  exist precisely to freeze play into panel-sized participants (§6).
- **DN8 — Don't let two signal scopes overlap silently.** Cross-
  participant params need one explicit owner — canvas-declared,
  port-bound; the layered-graph selection clamp is the in-app proof
  (§6).
- **DN9 — Don't hand data over by in-process reference.** Direct
  embedder ops are fine as chrome (play's delivery methods), but data
  crosses on typed channels — handles, configs — or the relation is
  invisible to D4 and unportable to the distributed line (§2.6, §3).
- **DN10 — Don't quote SQL inside SQL.** A condition or query carried
  as a string literal inside a row is escaping plus meta-level
  confusion; conditions and queries are named fences referenced by
  name from rows — the multi-fence applet book is the precedent, the
  expressions-in-YAML-strings failure of CI formats the warning (§12).
- **DN11 — Don't model an acknowledgment as a value.** "Accepted" is
  an event with an audit row — the launch-request precedent — not a
  signal cell; flattening events into values is the duality play
  already refused (§13).

## 11 Synthesis — one record, many renderers

The five shapes share a semantic core:

> **composition record** = participants (apps/applets, by manifest id +
> config) + typed bindings (port → port over one of the five channels) +
> presentation (sequence | layout | wiring | scale tiers).

Everything boxer-specific argues for making *that record* the first-class
object — leeway-modelled, facts-audited, on the ambient→launched→committed
axis like every other durable intent (P3 one data spine; ADR-0126
topology-as-data; ADR-0135 launches-as-facts). Each UX is then a renderer:
a wizard walks the record in order; a canvas draws its layout; a ports view
draws its bindings; a ZUI adds scale tiers to the layout. Renderers can
ship one at a time against the same record, and none blocks the others.

A sequencing that respects descope-over-gate, each step independently
useful:

1. **S1 — descriptive composition graph** (read-only). A snarl- or
   layeredgraph-rendered view over the *existing* facts plane (launch
   edges, dataset grants, caps). Zero new semantics; forces the port
   vocabulary into existence against real data; a play query in the
   Network tab is the free prototype.
2. **S2 — the general embed seam** (small ADR). `AppI`-level `NewEmbedded`:
   composed identity, pane-grained focus doctrine, caps-ride-embedder
   restated. Unlocks §6/§7 bodies and §8.
3. **S3 — canvas v1: canvas-of-applets.** Committed canvas book (sqlapplet
   pattern extended with layout + shared params); runtime layout changes
   persist as the canvas app's workingset. Grafana-class value, mostly
   existing machinery.
4. **S4 — workflow v1: a linear presentation over the same record.** A
   conductor app + stepper chrome + completion-by-dataset-publication.
   Dynamic/assistant tier deferred until ADR-0139 matures.
5. **S5 — Scene binding + semantic zoom tiers** on the canvas host
   (LOD capability interface; desert-fog mitigations as requirements).
   ZUI-as-shell only reconsidered after S3+S5 produce evidence.

The named non-goals of earlier drafts are absorbed into §10 as
DN1–DN4.

## 12 Would SQL syntax suit the composition record?

Raised in the design dialogue: should the document that describes a
composition be written in SQL? The question splits into three claims
with different answers; this section analyzes, F1 (§14) decides.

**As the read surface — already settled.** D4/D7 require the record to
land in queryable facts; whatever the authoring syntax, introspection
is SQL. Nothing to decide.

**As the authoring syntax — yes for the relational core, no beyond
it.** The parts of the record that genuinely are relations author well
in SQL, and this repository is unusually well tooled for exactly that
cut:

- Participants and bindings are rows, and play already treats
  SELECT-shaped SQL with by-name CTE conventions as a declarative
  binding language (`edges`/`vertices` in the layered-graph panel,
  kanban's `lanes`). Authoring composition's core as grammar1-parseable
  SELECT fences extends a proven idiom rather than inventing one.
- The capability none of the surveyed canvas formats has:
  **intensional participant sets**. Because participants live in
  catalogs (`keelson('apps')`, the applet manifest table,
  `keelson('adhoc')`), a participant list can be a predicate — "every
  applet tagged ops", "every app currently publishing a dataset" — and
  the composition stays current by construction. Grafana approximates
  this with template variables and repeat panels; JSON Canvas cannot
  express it. sqlapplet's `--launch` (a SQL WHERE over the manifest
  table) is the pattern in miniature.
- The tooling dividend: a closed grammar1 SELECT surface inherits the
  nanopass stack — the sqleditor with completion (ADR-0147),
  highlighting (ADR-0130), the ADR-0132 security classifier (a
  composition doc classifies as read), param harvesting. Most of a
  composition editor already exists.
- Where SQL is the wrong tool: nested typed configs (launch DTOs)
  belong in frontmatter, as in applet books; **geometry** arguably does
  not belong in the authored doc at all — layout is workingset-tier
  state the canvas writes back, and JSON Canvas's conflation of content
  with position need not be copied; and **SQL inside SQL** (a condition
  as a string literal in a binding row) is DN10 — conditions are named
  fences, the multi-fence applet book being the precedent.

**As runtime semantics — the sleeper strength.** Workflow gates as SQL
predicates over the facts plane ("step 2 unlocks when this dataset has
a revision"; EXISTS over launch and lifecycle rows) are expressible
today, because every completion signal already lands in facts. One
constraint worth adopting: prefer **monotone** gates — EXISTS over
append-only facts, so a step once completed stays completed; a NOT
EXISTS gate can flip a finished step back as new facts arrive.

**Prior art (tier c, like §9).** dbt is the strongest positive
precedent: SQL files plus a YAML sidecar, with the DAG *derived from
references* rather than declared — which is where D11 comes from (an
applet referencing a dataset handle implies that edge; author only the
non-derivable rest: launch edges, param wires). Terraform/HCL teaches
the same derive-from-references lesson. GitHub Actions and Argo are
the negative precedent for the YAML alternative: declarative formats
that inevitably needed expressions and bolted a mini-language into
string literals — an escape-hatch failure a first-class SQL expression
language sidesteps. Stored-procedure orchestration is the standing
warning for SQL-as-control-flow: the doc stays declarative; imperative
execution belongs to the conductor.

**Layered conclusion (input to F1).** Durable record = leeway facts
(DN4 is non-negotiable). Authoring source = the applet-book pattern
extended: frontmatter for identity and nested config; closed-grammar
SELECT fences for participant selection, non-derivable bindings and
workflow gates; geometry excluded from the authored doc; derivable
edges derived (D11). In one line: SQL alone as the orchestration
language — no; SQL as the relational core inside the established book
format — yes, with leverage specific to this substrate.

## 13 Ports meet play's signals — and how live is a wire?

Two questions raised in the design dialogue, which turn out to be one
topic: how the §3 port concept relates to play's signal concept
([ADR-0097](../adr/0097-play-reactive-query-graph.md)), and whether a
wire should propagate live or by explicit, user-acknowledged handover.
Play has already prototyped both the port taxonomy and its propagation
policies in miniature, one level down. Code claims here were verified
in `apps/play/play_graph.go` and `apps/play/play_datasets.go` on the
compile date.

### 13.1 The relation: a port is a promoted signal

Play runs an internal trinity, not one concept:

- **signal** — precisely an *unbound param*: `SignalID → env.Param` in
  an immutable copy-on-write snapshot with a revision (glitch-free
  reads); SET-bound names shadow signals, so the signal set is "what
  the buffer leaves open". Every write carries provenance
  (`signalMeta{writer, revision}`), same-value writes deduplicate, and
  a human/machine writer split feeds the Live circuit breaker.
- **channel** — a panel's typed record input (`main`, `events`,
  `lanes`, `edges`, …), schema-gated per frame via `AcceptForChannel`
  — a record-valued port in everything but name.
- **trigger** — deliberately *not* a graph signal:
  `NotifyDatasetRevision` is documented as "a trigger, not a graph
  signal". In FRP vocabulary (tier c): signals are behaviors, triggers
  are events, and play already refused to flatten one into the other.

The embed seam already carries the port mapping ad hoc: param port ↔
`SetSignal`, dataset port ↔ `BindDataset` plus the revision trigger,
config port ↔ Mount seeding. The relation is therefore
interface-to-implementation: **a port is the manifest-tier,
composition-scoped promotion of a signal or channel; a signal is what
a port binding becomes inside a running instance.** Three findings
follow:

1. **Input ports of the play family are derivable** (D11): the unbound
   slots *are* the port set, already typed by slot syntax
   (`{lim:UInt64}`) and already rendered by the
   [ADR-0124](../adr/0124-play-param-editing-widgets.md) widgets —
   harvest, don't re-declare.
2. **A provenance gap**: `SetSignal` writes land unstamped (the
   default writer identity, `app`), indistinguishable in the Signals
   chrome from play's own writes. A wire must stamp a writer identity
   — extending the existing `signalWriter*` vocabulary — which is also
   what makes D4 hold across the seam, and it forces a decision on how
   external machine writes interact with the circuit breaker's
   human/machine split.
3. **The output half is missing**: delivery ops are one-way in; no
   read/subscribe op exists. An output port is an *exported signal* or
   an *observable node* — the
   [ADR-0129](../adr/0129-play-layered-graph-panel.md) SD7
   observe/bind deferral resurfacing as F3's output question.
   Cross-participant crossfilter then needs no new semantics: the
   canvas observes A's exported signal and delivers into B — dedup,
   revisions and the breaker already exist per instance; the canvas
   only ferries values.

Scope discipline carries over unchanged: signal names recur in every
instance, so a canvas binds `(instance, name)` pairs and is the one
stamped writer per canvas-level name (DN8, the selection-clamp lesson
generalized).

### 13.2 Propagation policy: live, discrete, acknowledged, pinned

How eagerly a wire moves data is a per-wire policy, not a property of
wires as such — and the substrate already runs most points of the
axis somewhere. Tanimoto's *liveness levels* (tier c) name the scale:
L1 a static description, L2 executable on demand, L3
edit/event-triggered, L4 continually streaming.

| Mode | Semantics | In the substrate today |
|---|---|---|
| Static (L1) | the record describes; nothing flows | committed books |
| Discrete pull (L2) | a user gesture moves each transfer | Run; the "Open in Playground" launch |
| Acknowledged | transfer offered, receiver (or user) accepts | none yet — nearest are request/reply (protocol-level) and workingset write-iff-dirty (intent-gated) |
| Live (L3/L4) | revision-triggered re-run | Live main + dataset revision trigger, per-instance |
| Pinned snapshot | an immutable result is handed over | QueryRun pins ([ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)) |

The acknowledged row is the genuinely new mode a workflow UX would
add; the literature (tier c) supplies its conventions and the warnings
for the live one. For live: reactive notebooks (Observable) buy
consistency by enforcing an *acyclic* dependency graph, while patcher
environments (Max/PD) permit cycles only through explicit delay
elements — a cross-app wire graph must pick one (refuse cycles per
name, or require an explicit damping element), because play's circuit
breaker guards only one instance; spreadsheets ship the same toggle as
calculation modes (automatic vs manual). For acknowledged:
office-suite link updating ("update links?", a per-document policy),
the mobile share sheet, and clipboard copy→paste are all two-consent
handovers; the settings-dialog instant-apply vs explicit-apply split
is the same axis at widget scale. For discrete: Plan 9 plumbing is
per-gesture dispatch — every transfer is a user act, which is what
keeps it legible.

Three design consequences, two of which are recorded as rules:

- **Policy is per-wire and class-gated** (D12). The substrate already
  gates AutoRun on the ADR-0132 read class — mutating applets never
  auto-run; the generalization is that a wire defaults to live only
  when the consumer's reaction is provably read-class, and to discrete
  or acknowledged handover otherwise.
- **An acknowledgment is an event and a fact, not a value** (DN11).
  "User accepted the handover" belongs on the trigger side of the
  duality and should land as an audited fact row — the launch-request
  precedent — so a workflow's acknowledgment trail is queryable like
  everything else.
- **Defaults by renderer**: workflows default discrete-acknowledged (a
  wizard is a wizard because handovers are explicit — D8's gating);
  read-only canvases earn Grafana-style liveness with the breaker; and
  the pinned snapshot is the third default worth offering — handing a
  *specific* result forward, provenance-perfect and
  freshness-explicit, is often what an assistance flow actually wants.

## 14 Open forks for the design dialogue

- **F1 — record identity and home.** New codec kind under `runtime/codec/`?
  A book format like applets? Both (committed book ⇄ runtime record, like
  applet vs. workingset)? Where does v1 sit on the ambient→committed axis?
  §12 narrows the syntax half of this fork (facts-durable, book-authored,
  SELECT fences for the relational core); identity and the axis position
  remain the open half.
- **F2 — embed contract.** Host-mediated (windowhost mints the instance,
  hands the embedder a handle) vs. embedder-constructed (adhocdemo's
  current shape, generalized). Instance identity: composed stamp vs. real
  window-key-like instance keys in lifecycle facts.
- **F3 — output ports.** Declare app outputs (datasets, offered subjects)
  in `Manifest`, or derive them purely from observed facts? Declaration
  enables the assistant/action-space story; observation is zero-cost but
  can't promise. §13 adds: for the play family the input side is
  derivable today (unbound slots), and the output side is concretely
  the missing export op (exported signals / observable nodes, the
  ADR-0129 SD7 deferral); the manifest-vs-observed question remains
  for everything else.
- **F4 — wire executor.** Who performs a prescriptive wire: the canvas app
  itself (ordinary caller, per ADR-0135's router stance), or a dedicated
  broker service? The router-as-ordinary-caller precedent suggests the
  former.
- **F5 — first renderer.** This survey recommends S1 (descriptive graph)
  because it is cheap and corrective — it will likely *change* the port
  vocabulary before anything durable ossifies. A user may reasonably prefer
  S3 (canvas) for immediate end-user value.
- **F6 — Scene binding timing.** Bind `egui::Scene` early (S3 gets pan/zoom
  from day one) vs. defer until semantic-zoom tiers are designed (avoid
  shipping a geometric-only zoom that then needs un-teaching).
- **F7 — focus doctrine for embedded bodies.** Extend `WindowFocusI` with a
  pane-grained sibling, or make the embedder responsible for gating global
  input (status quo of the two safe consumer shapes documented on the fetch
  accessors)?
- **F8 — canvas ↔ play boundary.** Do canvas shared-params reuse play's
  signal machinery (via applet param ports) or define a thinner
  canvas-level binding that only *sets* applet params? The clamp lesson
  says: don't let two signal scopes overlap silently. §13 argues the
  thinner binding — `(instance, name)` pairs, the canvas as each name's
  one stamped writer, per-wire propagation policy (D12); the export-op
  shape and the external-writer tier for the Live circuit breaker
  remain the open half.

## References

Internal: [ADR-0021](../adr/0021-imzero2-snarl-node-editor-binding.md),
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md),
[ADR-0057](../adr/0057-demo-registry-and-drivers.md),
[ADR-0097](../adr/0097-play-reactive-query-graph.md),
[ADR-0120](../adr/0120-play-natural-language-ask-panel.md),
[ADR-0126](../adr/0126-appliance-topology-as-data.md),
[ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md),
[ADR-0134](../adr/0134-adhoc-datasets.md),
[ADR-0135](../adr/0135-app-launch-requests.md),
[ADR-0139](../adr/0139-semantic-layer-text2dsl.md),
[ADR-0140](../adr/0140-imzero2-hover-scoped-wheel-capture.md),
[ADR-0148](../adr/0148-app-workingsets.md),
[why-boxer](./why-boxer.md).

External, verified this survey: egui 0.31 release notes ("Scene container",
<https://github.com/emilk/egui/releases/tag/0.31.0>); JSON Canvas
(<https://jsoncanvas.org/>). Literature in §9, §12 and §13: tier (c), see
the provenance note.
