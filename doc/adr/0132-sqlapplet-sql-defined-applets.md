---
type: adr
status: accepted
date: 2026-07-19
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-19
---

# ADR-0132: sqlapplet — SQL-defined applets over the play engine

## Context

`play` rests on two premises that together make a stored query almost an
application already ([ADR-0097](./0097-play-reactive-query-graph.md),
[the play architecture](../explanation/play-architecture.md)):

- **One buffer is the artifact** (SD3). The SQL text, including its
  `SET param_*` prelude, is the complete reproducible state; the query graph
  is recovered by static analysis, never authored beside the text.
- **The UI is derived from the SQL.** Panels light up from result-shape
  conventions — Kanban from `lane`/`title` columns
  ([ADR-0122](./0122-play-kanban-panel.md)), content-typed Detail cells from
  `label@mime` ([ADR-0123](./0123-play-content-typed-detail-cells.md)), the
  World map from country codes ([ADR-0114](./0114-play-world-choropleth-panel.md)),
  param widgets from `{name:Type}` slots
  ([ADR-0124](./0124-play-param-editing-widgets.md)) — and interactivity
  (selection cross-filtering, viewports, time extents) is the signal model
  (SD8): unbound params written by panels at interaction rate.

The consequence: a curated SQL buffer, run with the exploration chrome
hidden, is a small application — call it a **SQL applet** — at a marginal
cost near zero. The seams for that attenuation shipped and are dogfooded:
`NewLivePlayApp` is the documented embedding constructor, the tab registry is
instance-scoped and customized between construction and mount
(`Tabs().Add/Replace/Remove`, ADR-0097 slice 6a/6b), and the editor-delivery
ops were built expressly so "a saved-query library" could be an embedder
(slice-6 D5 Update).

Demand precedes this design. The repo already carries applets in unassembled
form: the Snippets tab (fenced SQL in an embedded markdown help book), the
canonical topology queries ([topology-queries how-to](../howto/topology-queries.md),
[ADR-0126](./0126-appliance-topology-as-data.md)), the generated History
projection (`ComposeHistorySql`), and the `BOXER_PLAY_SQL` +
`BOXER_PLAY_AUTORUN` + `BOXER_PLAY_FOCUS_*` env incantations the screenshot
tours use — an applet defined as an environment, lacking only a name and a
launcher entry.

The cost floor being undercut: the smallest real apps today run ~400–1000
lines of Go plus a blank import in the shell and a capslock baseline entry.
Each is first-class in exchange: a `Manifest` in the registry buys the Apps
menu, `--launch`, per-open audit facts, a `keelson.apps` row
([ADR-0094](./0094-keelson-introspection-tables.md)), a help book, and —
via the `log_comment` stamp and `queryrunsd`
([ADR-0115](./0115-query-observability-data-plane-strategy.md)) —
attributable query runs.

Two recorded positions constrain the design. First, the app registry was
deliberately **un**-flooded once: the demo gallery originally dual-registered
every demo as a folded AppI and retracted to gallery-only (the C3 migration,
`runtime/app` EXPLANATION) — per-applet registration must answer that
kill-reason. Second, the storage doctrine: apps do not own files; the
`StorageI` handle is the only sanctioned mutable-state path.

## Design space (QOC)

**Question.** How do stored, SQL-defined applets become first-class in boxer
— named, launchable, inventoried, observable — without re-acquiring the cost
of an app?

**Options.**

- **O1 — Gallery-only host.** One host app lists the applet corpus and opens
  each in place (the demo-gallery shape). No per-applet manifests.
- **O2 — Minted manifests from a committed corpus.** Applet definitions are
  markdown docs (frontmatter + fenced SQL) committed as embedded books; a
  host mints one real `Manifest` per applet at startup and serves every
  instance by embedding play with the chrome attenuated.
- **O3 — Per-applet Go registration.** Each applet is a small Go file
  wrapping `NewLivePlayApp` with a literal SQL string.
- **O4 — Runtime-authored applets.** "Save as applet" inside play, persisted
  through `StorageI`; the host reads definitions from the store.
- **O5 — Separate publishing stack.** Render stored SQL to a distinct
  surface (static site / web), Evidence.dev-style.

**Criteria.**

- **C1 — Marginal cost per applet** (authoring + build + review).
- **C2 — First-classness**: launcher, `--launch`, audit identity,
  introspection row, per-applet query observability.
- **C3 — Registry hygiene**: engages the C3 demo-gallery retraction.
- **C4 — Curation and governance**: who vets an applet, where the record is.
- **C5 — Authoring loop**: distance from a play exploration to an applet.
- **C6 — Reuse**: rides shipped seams rather than new machinery.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 gallery | O2 minted manifests | O3 per-applet Go | O4 runtime-authored | O5 web stack |
|----|-----------|---------------------|------------------|---------------------|--------------|
| C1 | ++        | ++                  | − (Go + wiring)  | ++                  | −− (new stack) |
| C2 | −− (anonymous) | ++             | ++               | +                   | − (outside the runtime) |
| C3 | ++        | + (bounded corpus)  | +                | − (unbounded)       | n/a          |
| C4 | +         | ++ (commit review)  | ++               | − (no review gate)  | −            |
| C5 | +         | + (copy into a doc) | − (recompile mindset) | ++             | −−           |
| C6 | +         | ++                  | +                | + (needs store schema) | −−        |

## Decision

Adopt **O2**: a host app (`apps/sqlapplet`) that mints one real `Manifest`
per applet from committed markdown definitions and serves each instance as
an attenuated embedded play. O4 is deferred, not killed (see Alternatives).

- **SD1 — One doc is one applet.** An applet definition is one markdown
  document in an embedded FS ("applet book"), reusing the existing
  frontmatter convention (`markdown.Doc.Frontmatter`) and the fenced-SQL
  corpus pattern the Snippets tab established. Frontmatter carries the
  manifest fields (`title`, `icon`, optional `tabs`, optional `endpoint`);
  the doc's filename base is the applet **slug**; the prose body is the
  applet's help page; the **first `sql` fence is the buffer** — required to
  be pasteable-complete (paste into play, press Run, same applet). Further
  fences with a role marker in the info string carry the panel-local SQL
  that legitimately lives outside the buffer (v1: the Timeline bands
  template; the Map raster template stays deferred). A doc without a primary
  fence is plain prose and mints nothing, so a book can carry an overview
  page.

- **SD2 — Minted manifests, synthetic ids, explicit startup.** The host
  registers a factory-backed `Manifest` per applet with id
  `github.com/stergiotis/boxer/apps/sqlapplet/<slug>`, `Category:
  "Applets"`, and the doc as the applet's help book. Ids follow the existing
  synthetic-basename precedent (demo ids) and inherit the durability
  invariant: renaming a slug is a deprecation event. Contributing packages
  ship applet books through a registration seam mirroring the help facility
  (`sqlapplet.RegisterBook`); the shell calls one explicit minting hook
  during startup (the `introspecthost.Start` precedent) before the window
  host runs, so `--launch` and the Apps menu see the full set. The C3
  kill-reason is answered by **curation, not mechanism**: the corpus is
  committed and review-gated, so the set stays deliberate and bounded; if it
  ever grows past what the Apps menu comfortably lists, the recorded
  fallback is a gallery surface for the long tail with pinned applets
  keeping manifests.

- **SD3 — The attenuated surface.** An applet instance is a fresh
  `PlayApp` via `NewLivePlayApp` with: the chrome tabs (Editor, Preview,
  History, Snippets, Graph, Diagnostics, Map) removed pre-mount; the result
  panels per SD4; the status bar kept (errors and drift must surface, never
  vanish); param widgets re-homed to a pinned **params strip** in the top
  bar — the one genuinely new render mechanism, since today they draw above
  the SQL editor; Run/Cancel retained; AutoRun on mount **only for
  read-classified applets** (SD5) — a mutating applet always waits for an
  explicit Run; the Live toggle default-on when the buffer has unbound
  signals. The escape hatch back up the slope is v1-minimal: a "Copy SQL"
  action (the buffer is the artifact — the clipboard is a faithful export).
  Richer routes — remount-with-chrome, or bus-delivered `ReplaceSql` into a
  full play window via the `app.{id}.request.{name}` taxonomy — are
  recorded, deferred until the copy path demonstrably fails someone.

- **SD4 — `tabs: auto` is the default, a list is the override.** Absent
  frontmatter, the applet shows the result panels whose channel negotiation
  accepts the executed shapes (the SD6 accept/reject contract already
  answers "which panels apply"). An explicit `tabs:` list pins the set and
  order, each entry a registry slug with an optional `:ctename` binding
  suffix riding slice-6c per-panel node bindings — which is how one buffer
  serves several sub-views ("Table · recent", "Timeline · by_day") without
  any second authoring surface.

- **SD5 — A query security class, derived from the buffer.** A new
  structural analysis beside the passthrough classifier
  ([ADR-0117](./0117-passthrough-table-classifier.md)) assigns every buffer
  one of three classes: **read** (statements provably retrieval-only, no
  egress table functions), **read-egress** (retrieval that reaches outside
  the endpoint — `url()`, `file()`, `s3()`, `remote()` and kin), and
  **mutating** (anything else: DML/DDL, `SYSTEM`, `KILL`, non-param `SET`,
  `INTO OUTFILE`, or **any statement the analysis cannot classify** — the
  ADR-0117 conservative direction, "cannot prove → the stronger class").
  Classification runs on the canonical authored buffer *before* pre-execute
  passes, so `keelson('…')` classifies as introspection read rather than as
  the `url()` it later expands to; pass-generated rewrites are trusted
  machinery. The class serves three consumers: play's Diagnostics
  **Security context** section gains a class line with witnesses (the
  statement or function that forced the class); the applet launcher and
  window badge the class; and the sqlapplet client **enforces** `readonly`
  on the wire for read-classified applets — defence in depth, not the
  boundary itself (the hygiene-not-security stance of ADR-0026 §SD10
  holds; the committed corpus is the actual gate). The exact `readonly`
  value (1, or 2 if parameter binding under 1 proves incompatible) is
  verified against the pinned ClickHouse at implementation time. The honest
  limit mirrors the Malloy entry of ADR-0097: the guarantee reaches only as
  far as static analysis of the SQL text sees.

- **SD6 — The corpus is testable by construction.** A repo test walks every
  registered applet book and asserts per doc: frontmatter validates, the
  primary fence parses, canonicalizes, splits, and classifies (an
  unclassifiable applet is a build failure, not a runtime surprise), aux
  fences carry known roles, and slugs are unique. Live verification (EXPLAIN
  against a reachable server) stays best-effort and out of the hard gate,
  matching the existing EXPLAIN-suite stance. At runtime a broken applet —
  schema drift, endpoint down — renders its error state through the kept
  status bar and lane error surfaces; it must never silently show stale
  results as fresh.

- **SD7 — Endpoints; the `chhttp` deferral.** v1 applets target the
  env-configured ClickHouse exactly as play does; frontmatter
  `endpoint: introspection` opts a doc onto the in-process ADR-0094
  endpoint, which today serves only param-less applets (its `/query`
  rejects `param_*`, surfaces no read counters, and streams no progress
  headers). Closing that — plumbing a param channel through the chlocal
  broker, real counters, progress — and extracting the server side of the
  ClickHouse HTTP dialect into a reusable `chhttp` package is recorded as
  its own follow-up ADR rather than gating this one (descope over gate).

- **SD8 — The graduation boundary.** An applet is SQL and frontmatter,
  never Go. The moment one needs a custom panel, a new capability, or
  bespoke layout, it graduates to an embedder app built on `NewLivePlayApp`
  — the host is not a plugin system, and applets cannot widen the host's
  declared capability surface (capslock and the cap broker see only the
  host).

- **SD9 — Applets are rows; runs are attributable.** The minted per-applet
  app id rides the existing `log_comment` stamp, so `queryrunsd` capture
  yields per-applet `QueryRun` facts with no new machinery. A
  `keelson.applets` introspection provider (slug, title, class, tabs, book,
  endpoint) makes the corpus itself queryable — an applet listing the
  applets is the ADR-0094 self-observation stance applied to this feature.

- **SD10 — Naming.** The concept is **SQL applet**; the package, host app,
  and id segment are **`sqlapplet`**. "Applet" carries the intended
  association — small, declarative, rendered by a hosting runtime — and the
  term predates and outlives its Java episode. `playapplet` was rejected
  because durable public ids must not embed the engine name (play is the
  current renderer, the SQL is the definition) and because it suggests the
  applet opens play — the chrome it exists to hide. A bare `applet` package
  name was rejected as under-specific in a repo where non-SQL applet forms
  are conceivable.

## Alternatives

- **Gallery-only host (O1).** Rejected as the primary shape: applets would
  be anonymous — no `--launch`, no audit identity, no `keelson.apps` row, no
  per-applet query attribution — which is precisely the "first-class"
  half of the goal. It survives as SD2's recorded fallback for a long tail.

- **Per-applet Go registration (O3).** Rejected: it re-acquires the cost
  floor (a Go file, a blank import, review of code rather than of a query)
  and turns the authoring loop from "commit a doc" back into "write a
  program". The embedder path already serves the cases that genuinely need
  Go (SD8).

- **Runtime-authored applets via StorageI (O4).** Deferred, not killed —
  "Save as applet" inside play is the natural v2 and the point where the
  storage doctrine genuinely bites (definitions become mutable state with
  no commit review). Trigger: the committed corpus demonstrably failing an
  operator who authors queries on a deployed box, plus the moderation story
  for an unreviewed launcher entry.

- **Separate publishing stack (O5, the Evidence.dev shape).** Rejected:
  Evidence composes *pages* from many independent queries and renders them
  on a second stack; here that would mean N graphs per surface with **no
  shared signal store** — cross-panel selection, viewport, and time-extent
  reactivity are properties of one graph. play already composes multi-view
  surfaces from one buffer via CTEs plus per-panel bindings (SD4), which
  keeps the reactive semantics and the single-artifact premise. What is
  adopted from Evidence is exactly the definition format: SQL fenced in
  committed markdown with frontmatter.

- **Multi-applet documents ("sub-applets").** Deferred with a trigger. The
  need it names is mostly served *inside* the buffer (CTE-bound tabs, SD4);
  a doc carrying several independent applets would need heading-scoped
  frontmatter conventions and compound slugs. Revisit when a family of
  applets shares prose so tightly that one-doc-per-applet demonstrably
  duplicates it (the topology suite is the candidate witness).

- **Datasette-style sidecar metadata** (SQL files plus a separate manifest
  file). Rejected: two artifacts that drift, and the prose — the applet's
  help page — loses its home. One markdown doc keeps definition,
  documentation, and manifest in a single reviewable unit while the buffer
  stays pasteable-complete (SD1).

## Consequences

### Positive

- The marginal applet is a markdown doc: no Go, no shell wiring, no
  capslock delta. The committed corpus makes review the curation gate.
- Full first-classness rides existing machinery: launcher entry, `--launch`,
  audit facts, `keelson.apps` and `keelson.applets` rows, a help page, and
  per-applet `QueryRun` attribution — the last being observability the
  surveyed commercial dashboard layers do not give.
- play gains the security-class Diagnostics line and the params-strip
  mechanism regardless of how far applets are taken.
- The exploratory→confirmatory arc closes inside one toolchain: explore in
  play, freeze the buffer into a doc, and the doc remains a play buffer
  forever (the escape hatch is a paste).

### Negative

- v1 adds an applet only at build time; a deployed box cannot grow one
  without a new image (O4 is the recorded answer, deferred).
- Slugs become durably public names the day they ship; renames are
  deprecation events.
- The security class is a new analysis to maintain, and its guarantee stops
  at what static analysis of the text can see — server-side views,
  dictionaries, or UDFs can reach further than the buffer shows. `readonly`
  enforcement backstops the read class only.
- The params strip and the minting host are new render/runtime code, and
  the Apps menu grows with the corpus until the SD2 gallery trigger fires.

### Neutral

- play remains the sole authoring surface; sqlapplet adds no second one
  (the SD3/SD12 stances of ADR-0097 are untouched).
- The wire dialect is unchanged — an applet speaks to its endpoint exactly
  as play does; `endpoint: introspection` usefulness tracks the SD7
  follow-up.
- Whether the class should also ride captured `QueryRun` facts is left to
  the query-observability line (ADR-0115), not decided here.

## Status

Accepted (2026-07-19). Prior-art note: this ADR extends the external survey
of [ADR-0097](./0097-play-reactive-query-graph.md) along the **publication
axis** it deliberately left uncovered — freezing an exploration into a
curated artifact — rather than duplicating the reactive-DAG entries.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## Update (2026-07-19) — implemented; the §SD7 introspection restriction is lifted

All slices shipped within the acceptance day: the §SD5 classifier with its
Diagnostics class line, the host with minted manifests and the starter
book, the §SD3 attenuated surface (chrome tabs removed, minimal top bar
with the Copy SQL escape hatch over `clipboard.write` — the one capability
minted manifests now declare — and the params strip pinned into the top
panel whenever an embedder removed the Editor tab). Two findings recorded
elsewhere: coexisting PlayApp windows needed a per-instance widget-id salt
(`WidgetIdStack.SetBaseSalt`; the ADR-0026 §SD9 contract met in salt form
because one play instance spans a dozen driver-owned stacks), and
`--launch` resolves a SQL WHERE over the manifest table, so multiple
applets launch via `subject_alias IN (…)`.

§SD7's restriction — introspection applets must be param-less — is lifted
by [ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md) M3: the
starter book's `runtime-env` applet now carries a `pattern` parameter
bound against the in-process endpoint.

## Update (2026-07-19) — O4 realized: runtime-authored applets ("Save as applet")

The O4 deferral is picked up on operator demand (its recorded trigger).
The design keeps every §SD1 invariant by making the runtime store carry
the *same artifact* the committed books do:

- **O4-D1 — Store-as-document.** A saved applet is the identical markdown
  document shape — frontmatter, prose, one `sql` fence — persisted through
  the runtime persist facility (`StorageI`, `boxer.facts`-backed where
  ClickHouse is up) under the store service's own alias: one key per
  applet (`applet_<slug>`; persist keys are single NATS tokens, so no
  dots) plus an `index` key, since `StorageI` has no enumeration. One
  format, one parser, no second definition language.
- **O4-D2 — An audited save seam.** play composes the document and
  publishes it on `applet.store.save` (request/reply, the Powerbox
  pattern; the cap declared in play's manifest). The `appletstore` service
  is the moderation gate the original deferral asked for: it validates the
  slug, parses, classifies — an unparseable buffer is *refused with the
  classifier's error*, the §SD6 corpus gate's runtime equivalent — checks
  collisions, persists, and mints. The contract (subject, wire shapes)
  lives in a neutral runtime package so play need not import the host.
- **O4-D3 — Boot order and precedence.** Committed books mint first;
  stored documents mint second, and a stored slug that collides with a
  committed one — or fails to parse — is skipped with a warning. Boot
  never blocks on runtime state, and curation outranks it.
- **O4-D4 — Overwrite, not delete.** Saving an existing stored slug
  replaces the stored document and the live definition (the minted
  factory resolves the definition at open time), while the manifest's
  display metadata stays as-registered until the next boot — a recorded
  staleness, not a hidden one. Deleting a stored applet is deferred until
  a UI consumer for deletion exists.
- **O4-D5 — The generated document.** Frontmatter (title, icon, the
  documentation-standard type/status keys), a one-line provenance note,
  and the buffer as the single fence. A buffer that itself contains a
  fence line is refused rather than silently producing a broken document.

## Update (2026-07-21) — §SD3 deferral resolved by ADR-0135

§SD3 deferred bus-delivered "open this buffer in the full playground"
until the Copy SQL escape hatch demonstrably failed someone; that
trigger fired, and the mechanism landed as
[ADR-0135](./0135-app-launch-requests.md) — not via the per-app
`app.{id}.request.{name}` taxonomy this ADR sketched (the target is not
mounted when the request arrives), but via the host-level audited
`windowhost.open` subject with a leeway-declared launch config. play
accepts kind `playLaunch` (`apps/play/launchcfg`): the applet toolbar
composes the current buffer into a `PlayLaunch` and a full play window
opens seeded with it, priority above `BOXER_PLAY_SQL` and the persisted
session. Copy SQL stays as the transport-free fallback.

## Update (2026-07-21) — O4 authoring surface factored into its own app

With [ADR-0135](./0135-app-launch-requests.md) landed, the O4 "Save as
applet" authoring surface — the slug/title/icon form O4-D2 inlined in the
playground toolbar — is factored out into a standalone window,
`apps/sqlappletcreator`, which the playground now *launches* over
`windowhost.open` rather than hosting. What moves and what does not:

- **The store seam is unchanged.** O4-D1/D3/D4 stand verbatim: the same
  `applet.store.save` request/reply, the same `appletstore` service as the
  single moderation gate (validate → classify → persist → mint), the same
  document shape (O4-D5). Only *who composes and submits* moved.
- **A launch config, not a bus taxonomy.** The playground hands the creator
  the buffer and its authoring endpoint through a leeway-declared launch
  config, kind `appletCreate` (`apps/sqlappletcreator/appletcreatecfg`) — a
  dedicated kind rather than a reuse of play's `playLaunch`, so nothing
  embeds "play" in the creator's durable launch contract. The "Save as
  applet…" button composes the config and opens the creator; the form
  decodes it at Mount and seeds an editable buffer (a plainly-opened creator
  window starts empty and is still usable).
- **The composer moved to the contract.** `ComposeAppletDoc` — the O4-D5
  document shape — moved from play into the neutral `appletstore` package,
  beside the wire types it produces and the store's gate parses back; the
  store's corpus round-trip test now shares it instead of reaching into
  play.
- **Capability hygiene (revises O4-D2).** The save cap no longer lives in
  play's manifest: play sheds `applet.store.save` and declares only
  `windowhost.open` for the launch, while the creator declares
  `applet.store.save` — the authoring capabilities now sit on the sole
  authoring app. The playground is a query tool again, not an applet author.
- **Two outputs — mint *and* export (the "A+B" cut).** The creator offers
  both a Save and an Export. **Save** is the store mint above (O4-D2), which
  remains the primitive for "publish this applet into the running system": it
  is validated, classified, and immediately launchable, and its durability
  follows the wired persist backend. **Export** writes the same composed
  document to a user-chosen file through the fs Powerbox save dialog
  (`fs.dialog.write` + a granted write handle) — a durable, user-owned,
  legible `.md`, the file-save counterpart of play's existing "Load .sql"
  read dialog. Export is not a mint: it produces the source artifact, not a
  registered app. The two answer different needs (make it live now vs. keep
  the file); a filesystem-*backed* applet library — Save itself going through
  the Powerbox and the runtime minting from a watched directory — stays out
  of scope as a larger change to O4-D1's keystore model.

This is the second real adopter of ADR-0135 (after play's own "Open in
Playground"), and it leaves the inline menu with no remaining consumer.

## Update (2026-07-30) — correction: the O4 store is not durable today

O4-D1 describes the store as "`boxer.facts`-backed where ClickHouse is up".
That overstates what is wired: the store persists through `StorageI`, and
the persist service behind it runs on `persist.NewMemoryBackend()` (the
carousel's only wiring) — the facts-backed `StorageBackendI` of ADR-0026
§SD6 was never built. The facts *store* being CH-backed does not help; no
adapter connects the persist service to it. Consequence: saved applets and
the `index` key live exactly as long as the process — a restart silently
empties the runtime applet library. Export (fs Powerbox save) remains the
only durable path out of an authoring session.

The repair is recorded as a milestone of ADR-0151 (proposed): a thin
`persist.FactsBackend` over the existing `FactsStoreI`
`WriteState`/`LatestState`/`DeleteState` methods, plus the carousel wiring
flip. When that lands, stored applets become durable with no change to this
ADR's design; until then, O4-D1's durability phrasing should be read as
intent, not behavior.

## Update (2026-07-30, later) — the O4 store is durable again

The backend named in the correction above is built and wired (see
[ADR-0026](./0026-app-runtime-and-capability-subjects.md), Update
2026-07-30). Stored applets and the `index` key now land as `boxer.facts`
`KindState` rows whenever ClickHouse is up, so a restart no longer empties
the runtime applet library, and O4-D1's "`boxer.facts`-backed where
ClickHouse is up" describes behavior rather than intent. This ADR's design
is unchanged — the store still persists through `StorageI` and knows nothing
about what lies behind it.

Two details of the repair touch this ADR's store specifically. Its identity
`runtime.appletstore` is synthetic: the store is a runtime service, never a
registered app, which is why the persist service attributes the durable app
id from the bus envelope rather than resolving the subject alias through the
app registry — a registry lookup would have missed this consumer. And the
`index` key's newline-joined enumeration workaround (O4-D1) is unaffected:
`StorageI` still has no listing operation, and that remains an open
limitation rather than something durability closed.

Export (fs Powerbox save) is no longer the only durable path out of an
authoring session, but it stays the only *portable* one.

## Update (2026-08-01) — the §SD3 escape hatch shows the document, not the buffer

The "Copy SQL" button is gone from the minimal toolbar. A **"Definition"**
toggle stands where it was, opening a right-hand drawer that renders the
applet's own markdown document — the frontmatter that is its manifest, the
prose, and the SQL — through the `widgets/markdown` view.

§SD3 chose the clipboard on the argument that the buffer is the artifact.
That reads as half the story now. The document is what the applet was minted
from, and it is what a reader needs in order to judge the numbers on screen:
which endpoint answered, which panels the author pinned, what the query is
actually counting. A button that exported the SQL text left all of that
behind, and left no in-app route to it — the prose reaches the Help center,
but only for applets whose manifest ships a book, and never beside the result
it explains.

Mechanically:

- `AppletDef` carries the document's raw markdown (`Source`), and
  `NewEmbedded` hands it to the instance through a new play seam,
  `PlayApp.SetDefinitionMarkdown`. Both mounting paths — a minted applet and
  an embedder ([ADR-0134](./0134-adhoc-datasets.md) §SD7) — go through
  `NewEmbedded`, so both get the drawer.
- The toggle is gated on a definition existing, not on the minimal toolbar
  and not on a bus: reading the document reaches nothing outside the process.
  An ordinary playground has no definition and grows no toggle.
- The clipboard hatch survives inside the drawer as a `Copy` button per
  fenced SQL block (`markdown.Doc.RenderActions` — the mechanism the Snippets
  tab and Docs pane already use). It still needs `clipboard.write`, and
  without a bus the buttons are withheld rather than rendered dead. The §SD8
  cap list is therefore unchanged; only the stated reason moved from the
  toolbar to the drawer.

Not done: the drawer parses the bytes handed to it, not the book's `fs.FS`,
so a document embedding an image degrades to markdown's glyph-hyperlink
fallback. Carrying the FS through the seam is deferred until a corpus
document wants one.

## Update (2026-08-01, later) — a `md preamble` fence, over the results

§SD1 gave the document three jobs: frontmatter is the manifest, prose is the
help page, the first `sql` fence is the buffer. It left no way to say
something *on* the results — the caveat a reader needs before reading the
numbers, not after going looking for it. A fourth job, opt-in:

````markdown
```md preamble
Every knob the process **declares**, not every knob it reads.
```
````

The instance renders it as markdown inside the central panel, above the
DockArea, so it spans every result pane and sits below the controls rather
than among them. `markdown` is accepted as a synonym for `md`. The strip is
absent, not empty, when the fence is.

This is an aux fence, in the shape `sql bands` already established (§SD1),
rather than a reuse of the document's leading prose. Two reasons: prose
serves the Help center and the Definition drawer, where length is free and
over the results it is not; and an author is the one who knows which
sentence is a caveat on the data and which is background. Existing applets
therefore gain nothing they did not ask for.

Two more rules fall out of the parser rather than the design, and are worth
knowing before writing one. `scanFences` does not nest, so a preamble cannot
itself contain a fenced block. And the strip is not resizable — it fits its
content the way the top bar fits its buttons — so a preamble long enough to
crowd the panes is the signal to move that prose into the document proper,
where the drawer already shows it.

The dogfood is `book/runtime-env.md`.

## Update (2026-08-02) — §SD4's auto rule gains a default-off list

§SD4 says `tabs: auto` shows "the result panels whose channel negotiation
accepts the executed shapes", delegating the question to the SD6 accept/reject
contract. That contract answers per frame, not per applet: a panel no applet's
shape will ever satisfy still takes a tab and still draws its rejection. Play's
new Sankey panel is that case — it binds two convention-named CTEs carrying a
conserved quantity, which nothing in the corpus has — so under auto it was a
permanently rejecting tab on every applet window.

`autoOffResultTabIDs` names the panels auto skips. It is a default and not an
attenuation: such a panel stays listable, so an applet whose buffer does carry
the contract asks for it in `tabs:` and gets it.

The same change closes a hole the addition exposed. A tab classified as neither
chrome nor a result panel survived attenuation unconditionally — no `tabs:`
list could name it, so none could prune it either — and both Sankey and the
Experiments pane had landed in play that way, riding along on every applet.
Experiments is chrome by §SD3's own criterion (its subject is a built-in
fixture, not the applet's result) and is removed with the rest of it. The
classification is now pinned by a test that enumerates play's registry, because
the failure mode is silent: adding a panel to play quietly widens every
applet's surface, and nothing else notices.

## Update (2026-08-05) — an explicit `tabs:` list decides which tab opens

§SD1 made `tabs:` a *set* — which panels an applet keeps. Nothing read its
order, so the tab a window opened on came from the dock instead: a fresh leaf
activates its own first tab, and that is play's registration order, which
starts at `table`.

The result was that four of the five books' most characteristic documents
opened on the wrong thing. `bookcapmap/cap-map` is titled "Capability map" and
is a treemap; it opened on a grid of the `id`/`parent`/`value` columns that
feed the picture. `booktopo/component-deps` and `booktopo/topology-map` are
graphs whose prose says so in the first sentence; both opened on a table.
`bookpprof/profile-flame` is a flamegraph and opened on the stack rows behind
it. In every case the document had already named the right panel first and was
simply not being listened to.

So the list is now ordered where it can be: **the first entry is the tab the
applet opens on**, raised in `attenuateTabs` through the `ActivateTab` seam the
editor-delivery ops already used. The set semantics are unchanged, and so is
the tab strip's left-to-right order — registration order still decides where a
tab sits, and only which one is *active* changes.

Three boundaries, each deliberate:

- **`tabs: auto` raises nothing.** There is no declared order to honour, and
  choosing one would be inventing an opinion the document did not express.
- **A side-zone tab is a legitimate landing.** Zones each activate their own
  first tab, so `tabs: [detail, table]` raises Detail in its own leaf and
  leaves the body zone alone.
- **A failed activation is logged, not returned.** Every id reaching it is a
  result-panel slug the parser validated and attenuation kept, so it cannot
  fail for a corpus document — and opening on the wrong tab is not worth
  refusing to mount over.

This is a behaviour change for existing committed books, not just new ones. It
was taken rather than adding a separate `active:` key because a second field
would let a document say two contradictory things about the same list, and
because every book in the corpus already listed its subject first — the order
was carrying the intent all along.

## References

Internal:

- [ADR-0097 — play as a reactive query-graph](./0097-play-reactive-query-graph.md)
  and [the play architecture](../explanation/play-architecture.md) — the
  engine, the buffer-is-the-artifact premise, the embedding seams.
- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — the in-process endpoint, `keelson('…')`, and the provider registry
  `keelson.applets` joins.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — manifests, the registry, hygiene-not-security (§SD10), and the C3
  gallery retraction context.
- [ADR-0117 — passthrough table classifier](./0117-passthrough-table-classifier.md)
  — the structural-classification precedent and its conservative direction.
- [ADR-0115 — query-observability data plane](./0115-query-observability-data-plane-strategy.md)
  — the `log_comment` stamp that makes applet runs attributable.
- [ADR-0122](./0122-play-kanban-panel.md), [ADR-0123](./0123-play-content-typed-detail-cells.md),
  [ADR-0124](./0124-play-param-editing-widgets.md) — the result-shape and
  param conventions that let the SQL determine the UI.
- [ADR-0108 — SQL pass registry](./0108-keelson-sql-pass-registry.md) — the
  pre-execute seam the class analysis deliberately runs before.

External prior art (the publication axis; extends the ADR-0097 survey):

- **Evidence.dev** — SQL fenced in committed markdown rendered as BI pages;
  the definition format adopted here, with its page-composition model
  deliberately not adopted (see Alternatives O5).
- **marimo** (`edit` vs `run`) — the same file served with chrome hidden and
  inputs live; the sharpest analog of the play/sqlapplet duality.
- **Voilà** and **Hex App Builder** — freezing a notebook DAG into an app by
  hiding authoring cells; Hex confirms the applet layer as the natural
  completion of the reactive-graph product arc.
- **Datasette canned queries** — named SQL in metadata, `:params` become
  form inputs; the closest stored-SQL-as-page web analog.
- **SQLPage** — whole web apps in SQL with the component chosen by
  result-shape convention; independent convergence on play's
  convention-derived UI.
- **Metabase** saved questions and **Grafana provisioning** — saved SQL with
  template variables → filter widgets; dashboards provisioned from
  operator-managed files, read-only in the UI — the distribution stance SD1
  shares.
- **MS Access / dBase saved queries + forms** — the ur-form of the database
  artifact as the application.
- MacLean et al., *User-Tailorable Systems* (CHI 1990) and Myers, Hudson,
  Pausch, *Past, Present, and Future of User Interface Software Tools*
  (TOCHI 2000) — the "gentle slope": use → tweak params → open in play →
  author.
- Nardi, *A Small Matter of Programming* (1993) — task-specific languages
  as the end-user-programming sweet spot; SQL as the task language here.
- Tukey, *Exploratory Data Analysis* (1977) — the exploratory/confirmatory
  distinction this feature operationalizes: play explores, an applet is the
  confirmatory residue worth keeping.
- Chattopadhyay et al., *What's Wrong with Computational Notebooks?*
  (CHI 2020) — the documented publishing/sharing pain of exploration tools.
