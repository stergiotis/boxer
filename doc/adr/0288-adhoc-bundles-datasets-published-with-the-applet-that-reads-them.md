---
type: adr
status: proposed
date: 2026-10-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0288: Ad-hoc bundles — datasets published with the applet that reads them, and play as the one place they are queried

## Context

A model working across windows under an
[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
task moves data between apps by reading it and writing it again: play hands
it a bounded sample of a result as text, and an app whose operations take
code or drawing commands takes only those. Every number that crosses from
one window to another passes through the model's context and is retyped.
Nothing checks the copy — a transposed digit or a dropped row yields a
plausible wrong answer and no error — what can cross is bounded by a model
turn, and nothing records which result fed which call.

[ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)'s
ad-hoc datasets already carry Arrow tables from an app to play by alias,
sealed, owned and followed. What they lacked for agent work: the SQL that
reads a dataset in a pane's shape
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) could not travel with
it; an app other than play could not read a dataset back without writing
SQL; two publishers of one alias silently replaced each other; and nothing
recorded which task or call published a dataset, or what it was made from.

## Design space (QOC)

**Question.** By what path does a derived table travel from one window of
an agent's task to another, so that the bytes the consumer sees are the
bytes the producer wrote?

**Options.**

- **O1** — *Transcription*: the model reads a bounded result and writes the
  values into the next call.
- **O2** — *Host result references*: ADR-0269's references, opened to
  cross windows; the model passes reference ids instead of values.
- **O3** — *Ad-hoc bundles*: ADR-0240's datasets, published as a unit with
  the applet document that reads them; play is the query surface.

**Criteria.**

- **C1** — *Fidelity*: the consumer receives the producer's bytes.
- **C2** — *Reach*: the person can open, inspect and chart what crossed,
  in the same tools, without the agent.
- **C3** — *Size*: tables far beyond a model turn can cross.
- **C4** — *Lifetime and audit*: what crossed has an owner, a lifetime and
  a record of who published it and from what.

**Assessment.**

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | ++ |
| C2 | −  | −− | ++ |
| C3 | −− | +  | +  |
| C4 | −  | +  | ++ |

O2 keeps references inside a task's host state: the person cannot open one
in play, a reference carries no SQL that says what it is for, and it dies
with the task. O3 reuses a store that already has ownership, quotas, sealed
storage and a follower, and puts every crossing where the person already
looks at data.

## Decision

We publish derived tables between windows as **bundles**: one applet
document and the ad-hoc datasets it reads, published, republished and
retracted as one unit, with their provenance recorded as data. Querying,
transforming and visualising a dataset happen in play; any other app reads
a dataset whole, through a read that takes no statement.

### Subsidiary design decisions

- **SD1 — A bundle is an applet document plus its datasets.** The document
  is ADR-0132's applet format, so "SQL in a shape a pane draws" is the
  existing pane contracts, not a new shape language. Inside the document a
  dataset is named by its **local name**; the service mints its global
  alias as `<bundle>__<local>` (`adhocdata.DatasetAlias`), so a document
  never needs rewriting. The document is held in process memory beside the
  sealed datasets, bounded, never written to disk.

- **SD2 — Publish is atomic, and one constructor writes documents.** One
  request carries the whole bundle; every stream is sealed and every check
  made before anything goes live, so a failure leaves nothing of the
  publish. A republish replaces the document and every dataset; the
  previous revision's datasets withdraw in ADR-0240 §SD4's two phases, so a
  reader holding one finishes. A retract, or the publishing window closing,
  withdraws the bundle whole, and a bundle's datasets are reached only
  through it. Bundle events have subjects of their own, outside
  `adhoc.event.>`, so a consumer following datasets never decodes one. The
  service stores the document without parsing it; `play.PublishBundleE` is
  the checked constructor: a producer states the bundle — SQL, panes,
  datasets by local name, provenance — and the constructor composes the
  document one way, parses it with the applet parser, and refuses one that
  reads a dataset the bundle does not carry or asks for an endpoint other
  than introspection. It lives in play because play is a producer and
  sqlapplet, which owns the parser, imports play; sqlapplet installs the
  parser into play (`play.SetAppletDocParser`).

- **SD3 — An alias has one owner, and a window publishes under its own.** A
  publish under a dataset or bundle alias another owner holds is refused
  (`adhocdata.ErrAliasHeld`); owner is ADR-0240 §SD2's. A publisher whose
  app can open two windows publishes **window-scoped**: the service, which
  knows the sender's window from the envelope, publishes under
  `adhocdata.WindowAlias` and answers with the alias it used. Consumers bind
  that alias under a local name their SQL reads — through
  `adhocdata.Follower`, play's launch config, or `bind_dataset`'s `as`. A
  grant never names a local name: two windows' `keelson('stats')` are two
  datasets, so play judges a run by each bound name's global alias and
  bundle.

- **SD4 — play opens bundles and publishes results.** A window opens a
  bundle by launch config or `open_bundle`: it moves to the introspection
  endpoint, the document becomes its buffer, the datasets are bound under
  their local names, and the window follows the bundle through republish
  and retract. The bus is never asked on the render goroutine, so an open
  completes on the frames after the command. A document runs on open only
  if it is a plain read (ADR-0132 §SD5).

  | Operation | Effect |
  | --- | --- |
  | `list_bundles` | none — the live bundles, and this window's last publish |
  | `open_bundle {alias}` | document |
  | `publish_result {bundle, local_name?, sql?, tabs?, title?}` | **consequential** — the whole main result as a one-dataset bundle |
  | `bind_dataset {alias, as?}` | document |

  `publish_result` is consequential as ADR-0269 §SD5 defines publishing:
  the person confirms each one, unless the task's grant carries
  `publish:<prefix>` and the bundle's alias starts with that prefix
  (`app.OperationConsent`, which any consequential command may declare). The
  person approves that grant as any other (ADR-0269 §SD6) — standing
  consent for the task's lifetime, scoped by name — and each publish under
  it is still audited, with the destination that admitted it. A prefix is
  at least one character, and the grant never covers an alias another
  owner holds (SD3). Consent waives the confirmation, not the mode: in
  suggest mode the call is still a proposal, which the person accepts.
  The person publishes too: play's top bar has a Publish menu with the
  same form — bundle alias, panes — and their publish runs the same
  handler, audited without a call context.

  `publish_result` publishes only a whole main result: a result the row
  cap cut short is refused, since a dataset made of a prefix
  would miss rows with nothing to say so, and a node's lane does not record
  whether it was cut. An agent's run reading a bundle needs
  `keelson-bundle:<bundle>` or `keelson:<global alias>` in its grant; both
  are host reach. A task reads and runs on what it
  published without either: the dataset service records the attested
  task of each live revision and carries it on a resolve and a read, so
  the decision is by identity, and a republish by the person or another
  task ends it.

- **SD5 — Every bundle operation is audited, and provenance is data.**
  Every request is audited under a context the host attests. A request an agent's call caused
  carries the call's `(task, epoch, call)`. The dataset service takes
  nothing more from the requester: it asks the dispatcher
  (`app.CallContextI`), which answers only for a call it sent to that app
  and window in a live task, with what it recorded — conversation, turn,
  the model call and tool call. An unconfirmed claim is refused. A call is
  attested from the moment it is sent for as long as its task lives, so
  work that finishes after the call answered — a run, an evaluation —
  publishes under the call that started it; the row says whether the call
  was still in flight.

  A publish carries its provenance: the statement that produced the rows,
  as the producer ran it, and the handles of the datasets that statement
  read. The service resolves each input to the alias and stream digest it
  holds, so lineage names bytes, not the producer's word.

  Each publish, republish, retract, withdrawal and read — and each resolve
  an agent caused — is a row on ADR-0277's trail: context components from
  the attestation, and per dataset its alias, handle, rows, stream length
  and BLAKE3 digest of the stream as sealed; a publish adds the document,
  the source statement, the inputs and the columns with their summaries,
  so a bundle is described and reconstructible from `boxer.facts` alone.
  The digest is what shows the bytes a consumer read are the bytes a
  producer published.
  Rows join the dispatcher's action rows on `(task, call)` and the model's
  messages on `(conversation, turn)`; `keelson('adhoc_bundles')` and
  `keelson('adhoc')` carry the live state with the same context.

  **A dataset is summarised when it is sealed.** The pass that digests a
  stream also summarises each column: nulls, minimum and maximum where the
  type orders, an approximate distinct count, and a bounded sample of
  values. The summaries are in `keelson('adhoc_bundles')`, which
  `list_bundles` reads, and on the publish's trail row, so an agent learns
  what a bundle holds without opening it and the trail describes the data,
  not only its bytes.

- **SD6 — Only play queries a dataset.** Any other app reads a dataset whole
  with `adhocdata.ReadAllE`: the stream as sealed, with its digest, which
  the reader checks. No statement travels, so filtering, joining and
  aggregating stay play's, and their result is a `publish_result`. An
  agent's read is held to its task's grant as a run in play is, and a
  refusal names the destination (`adhocdata.GrantError`); an app can check
  `adhocdata.ReadDestinations` against the call's grant to refuse in its
  command. keelson.query (ADR-0253) refuses a sealed table, so it cannot
  become a second query surface over datasets. A receiver that hands the
  bytes on — into a sandbox, onto a wire — carries the handle, revision and
  digest with them; boxer cannot check that, so the receiver's own tests
  hold it.

- **SD7 — A bundle view is one constructor.** A receiver shows a bundle with
  `sqlapplet.NewBundleView` and adds `sqlapplet.BundleViewCaps` to its
  manifest. The view builds an embedded play from the document, binds the
  datasets, runs once they are bound (a plain read only), and follows the
  bundle: new data rebinds, a new document rebuilds the view, a retract
  leaves it waiting. Its Open in Playground opens the bundle, whose local
  names mean nothing in another window's buffer. A manifest is fixed
  before any bundle exists and an embedded applet's capabilities are the
  embedder's (ADR-0132 §SD8), so a bundle document asks for nothing beyond
  `BundleViewCaps`, which SD2's constructor enforces.

  **A view does its work on the frame it is drawn.** Answers and events wait
  in a mailbox, and the frame that next draws the view applies the bundle
  as it stands, not the sequence it missed. A view a receiver culls or
  keeps in a tab costs nothing until it is drawn.

- **SD8 — A bundle view is plain or operable.**
  - **Plain** is for the person: it draws and takes the person's input,
    appears in captures (ADR-0269 §SD11), and adds nothing to the
    receiver's catalog.
  - **Operable** offers a subset of play's operations to agents through the
    receiver's catalog (`sqlapplet.BundleViewOps`, a view built with
    `Operable`): play's own operations, prefixed `bundle_`, with play's
    arguments plus `bundle_view`, which names the view and may be left
    out when the window has one; `bundle_list_views` lists the views,
    their bundles and panes. Each is served by the play embedded in the
    view — play's handler, availability and agent limits — so a run from
    a view is play's run, judged by the bundle's name:

    | Offered | Not offered |
    | --- | --- |
    | reading state, results and history | changing the SQL, binding datasets, publishing — the document is the publisher's; a changed document is a new bundle, made in play |
    | `run`, `cancel_run`, `set_param` | authoring tools: explain, trace, flow, completion |
    | the read, option and select operations of the panes the document names | the operations of panes it does not show |
    | pane verdicts (`list_panes`) and the last run's error | the rest of play's diagnostics |

    The subset is play's to declare (`play.OperableOperations`), beside
    the catalog it is taken from, and a test fails until every pane
    operation play adds is offered or excluded on purpose. The catalog is
    static, so an operation for a pane no view shows is declared and
    reported unavailable with the reason. A view does its work on the
    frame it is drawn (SD7), so a run an agent asks of a view the receiver
    does not draw waits for its next frame; `BundleView.Pending` says so,
    and a receiver that culls views draws a pending one until it is not.

- **SD9 — Many bundles per receiver.** A receiver may publish a bundle per
  item it shows. The count quota has two levels — per process, which
  bounds the sealed files held open, and per owner, so one receiver cannot
  take the process's whole count — and the byte quota bounds the store
  whatever the count. A many-bundle receiver keeps one alias per item,
  republishes it when the item changes, retracts it when the item goes,
  and does not keep per-item bundles after close.

### Deferred and open

- **Only play queries (SD6).** keelson.query could admit a sealed table
  under the read's attestation and grant check, stamped like a run, so a
  consumer projects a dataset rather than copying it whole. Trigger: a
  consumer that needs a slice of a dataset larger than it can take in.
- **Parameter values across a new document.** A rebuilt bundle view starts
  from the document's parameters; carrying the person's values needs play
  to expose its parameter state. Trigger: bundles republished while the
  person works in them.
- **Units.** A unit per column, declared by the producer, would tell an
  agent what a number means; it needs a unit vocabulary first. Trigger: a
  result misread for want of its unit.
- **Names carrying identity.** A window-scoped alias names a per-run
  instance key; a lookup keyed on alias and owner would keep names clean.
  Trigger: aliases that must mean the same across runs.

### Milestones

- **M1 — Bundle records in `adhocdata`:** ✓ atomic publish, republish and
  retract, the bundle catalog, the two-level quota.
- **M1a — Audit under an attested context:** ✓
- **M2 — The whole-dataset read and the keelson.query gate:** ✓
- **M3 — play opens bundles and publishes results:** ✓
- **M4 — One owner per alias, window-scoped publishes:** ✓
- **M5 — The end-to-end lane:** ✓ with provenance and lineage.
- **M6 — Bundle views:** ✓ plain views; adhocdemo is a receiver.
- **M7 — Operable views:** ✓ `BundleViewOps`, raw mounted commands in
  appops; adhocdemo's view is operable.
- **M8 — Publish grants:** ✓ `app.OperationConsent`; `publish_result`
  declares `{publish, bundle}`.
- **M9 — Column summaries:** ✓ in the bundle catalog, on the trail row,
  in `list_bundles`.
- **M10 — The person's publish:** ✓ the Publish menu in play's top bar.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Capability subjects (ADR-0026) | added: `adhoc.bundle.publish`, `.resolve`, `.retract`, `adhoc.bundle.event.>`, `adhoc.read` | the manifests of apps that publish, open, show or read bundles |
| Bus codecs (ADR-0240 §SD8) | the adhoc request, reply and event kinds gain the bundle, document, streams, handles, provenance, on-behalf-of context, window scope, digest and grant destination | the generated adhoc codecs; vdd's assignment golden |
| Introspection tables | added: `keelson('adhoc_bundles')` with column summaries; `keelson('adhoc')` gains the bundle and call context; `keelson('agent_actions')` and `keelson('operations')` gain `consent` | catalog providers |
| The trail store (ADR-0277) | added: the `AdhocDataset` component, its archetype and view; the agent action row's `Consent` | `runtime/trail` regeneration; `trailviews`; the runtime vocabulary's golden |
| `app` exported API | added: `CallContext`, `CallContextI`; `OperationSpec.Consent` (`OperationConsent`) | the dispatcher; every view of a catalog the model reads, and `keelson('operations').consent` |
| `adhocdata` exported API | bundles, provenance, `ReadAllE`, grant errors, window-scoped publishers, local names on `Follower`; publish refuses a held alias | every publisher of a fixed alias |
| `launchcfg.PlayLaunch` | added: `Bundle`, `DatasetNames` | leeway codec regeneration |
| play | catalog: `list_bundles`, `open_bundle`, `publish_result`, `bind_dataset as`; API: `PublishBundleE`, `SetAppletDocParser`; the Publish menu | play's ops and caps tests; the chat's guidance |
| Agent grant destinations | added: `keelson-bundle:<bundle>` and `publish:<prefix>`, host reach | play's agent limits; the dataset read's check |
| keelson.query gate (ADR-0253) | refuses sealed tables | `keelsonquery.Gate` |
| `sqlapplet` | added: `NewBundleView`, `BundleViewCaps`, `BundleViewOps` | receivers' manifests and catalogs |
| `appops` | added: `MountedCommandRaw`, `MountedQueryRaw`, `Set.MountedAvailable` | catalogs that mount a component |
| play's exported API, for embedders | added: `OperableOperations`, `OperableResources`, `PlayApp.ServedOperations`, `FrameServed`, `SetDatasetOrigin` | sqlapplet's bundle views |

## Alternatives

- **O1, transcription.** Rejected per the QOC: no check on the copy, and a
  bound set by the model turn.
- **O2, host result references across windows.** Rejected per the QOC: the
  person cannot open what crossed, and it lives only as long as the task.
- **Datasets without a document.** A consumer would have to be told in the
  model's words what the table is and how to chart it; the document lets
  the person open the bundle and see what the producer meant.
- **Namespacing aliases in the service and rewriting documents to match.**
  Rejected: rewriting SQL text is the fragile half; local names give the
  same isolation with the document untouched.
- **Letting other apps query datasets through keelson.query.** Not taken,
  for capacity rather than principle: play is the one place the agent
  limits, the run history and the panes are implemented. Reopening it is
  under Deferred and open.
- **`publish_projection` rebuilt on `publish_result`.** The projection's
  datasets are bound by handle in the window that computed them; a bundle
  adds nothing there, and an agent that wants them elsewhere publishes a
  query over them.
- **Durable bundles.** A person-confirmed `promote` writing a bundle's
  streams into a durable store keyed by digest, so a trail row resolves
  after its windows closed. Rejected (2026-10-07): the trail's digests,
  statements, inputs and summaries prove what crossed and how it was made,
  and an analysis is reproduced by running it again, not by keeping its
  intermediates. A table worth keeping is written to a store of its own
  by the tool that owns it.
- **A lone dataset from `publish_result`.** Rejected: what crosses would
  carry no SQL that reads it and no record of what made it.
- **`publish_result` as a document-level command.** Rejected: ADR-0269 §SD5
  names publishing consequential, and a dataset is readable beyond what the
  task's grant covers.

## Consequences

### Positive

- A table crosses between windows as the producer's Arrow bytes, at up to
  ADR-0240's per-dataset quota, and a digest proves it hop by hop inside
  the process.
- Where an agent's numbers came from is answerable with SQL over
  `boxer.facts`: which call and turn published a bundle, from which
  statement, reading which bytes.
- An app cannot attribute a publish to a turn that did not cause it.
- Two windows can no longer replace each other's dataset by accident.
- Showing a bundle is one constructor and one manifest entry.

### Negative

- Without a `publish:` grant, a confirmation per `publish_result`; with
  one, the person consents per task and prefix, not per publish, and sees
  each publish only in the trail.
- A truncated result cannot be published.
- A publisher whose app can open two windows publishes window-scoped, and
  whatever opens its data binds the window's alias under a local name.
- A bundle document is limited to what every receiver can grant; one that
  needs another endpoint is opened in play, not embedded.
- Bundles die with their windows; the trail then holds digests,
  statements and summaries of streams that are gone, by decision.
- Sealing a stream costs a summarising pass beside the digest.
- An operable view's subset is a second list of play's operations to keep
  in step with play's catalog.
- On a host without a durable trail the audit is lost, as all trail rows
  are; `BOXER_TRAIL_REQUIRED` (ADR-0277 §SD3) refuses agent-caused work
  there instead.

### Neutral

- Datasets resolve on the introspection endpoint only, so a statement
  cannot join a dataset with a server table (ADR-0145); moving a server
  result into a dataset is a `publish_result`.

## Migration — Tier 1

- **Breaks.** A publish under a plain alias another owner holds is refused.
  Every fixed-alias publisher in boxer moved to window-scoped publishes in
  the same commit as the refusal; downstream publishers follow with their
  pin bump past it.
- **Regeneration.** The adhoc codecs, `launchcfg`'s codec, the trail store
  and the vocabularies' assignment goldens.
- **Old shape.** Unowned aliases are removed outright; nothing persists an
  alias across processes.

## Verification plan — Tier 1

- **Lane.** Default `go test` over `adhocdata` (atomicity, ownership,
  attestation, grant-checked reads, provenance, the audit),
  `keelsonquery` (sealed tables refused), `trail` (a bundle row joins the
  action that caused it; clickhouse-local), play (bundle windows,
  `publish_result`, the grant naming bundles, the constructor under
  sqlapplet's parser) and `sqlapplet` (bundle views catching up when
  drawn). The end-to-end lane is
  `agent.TestABundleCrossesWindowsWithItsProvenance`: under an approved
  grant, one play window publishes (the person confirms), another opens
  the bundle and publishes a result derived from it, a third party reads
  the dataset; rows, digests, lineage and the joins to the action rows on
  `(task, call)` are asserted.
- **What would fail.** A partial bundle live after a failed publish; a
  second owner's publish accepted; bytes differing between publish and
  read; an agent's publish applied without confirmation, or its read
  without a grant; an audit row missing, or carrying a turn the dispatcher
  did not record.
- **Gap.** Fidelity past a receiver's own transcoding is the receiver's
  lane, not boxer's.

## Status

Proposed — awaiting review by p@stergiotis.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-07 — what implementation changed, and the evidence

The body states the decision as built through M6. What implementation
taught, kept for its reasons:

- **Reading before writing.** ADR-0269 §SD1 refuses a command that writes
  a resource the task never read; the end-to-end lane found that nothing
  read the bundle or the followed datasets, so no agent could call
  `publish_result`, `open_bundle` or `bind_dataset`. play's state read now
  covers both, and `publish_result` writes nothing of its window.
- **Asynchronous open.** play asks the bus nothing on the render
  goroutine, so `open_bundle` cannot report pane verdicts in its outcome;
  `list_panes` reports them after.
- **An independent review** of M1–M4 for data-centricity found the storage
  and audit layer sound and asked for three corrections, all made: reads
  held to the grant (an agent's read outside play escaped it), provenance
  as data rather than prose in a document held in memory, and one checked
  constructor for documents. It also unified the audit's byte unit and
  made bundle members carry the bundle's revision.
- **Applet slugs are not bundle names.** The parser holds a document's
  file name to the applet book's slug rule; a bundle's document is parsed
  under its alias made a slug, so any alias opens.
- **What a view costs.** On the headless host with the CPU rasterizer
  client at 1000×800, idle: a window with one bundle view showing 21 table
  rows — Go 1.0 ms, Rust 1.3–1.5 ms, 15.3 KB sent per frame; a minimal
  demo app — 0.8 ms, 0.6 ms, 6.8 KB. Read from the status bar's frame
  label through a scene's `tree` step (ADR-0248).

### 2026-10-07 — the open options decided

Durable bundles rejected (Alternatives). Publish grants (M8), column
summaries computed at seal (M9) and the person's own publish (M10) taken
into the decision. keelson.query over datasets, parameter values across a
new document, names carrying identity and units stay deferred on their
triggers.

### 2026-10-07 — M8–M10 built

- **Consent is the spec's, not publish's.** `app.OperationConsent` names a
  destination class and the argument it matches, so the dispatcher stays
  ignorant of bundles and another consequential command can take the same
  path. Every view of the catalog the model reads shows the consent as
  `publish:<bundle prefix>`, and a confirmation the grant could have
  spared says which destination would have, so the model learns what to
  ask for from the refusal. The action row names the destination that
  admitted a call.
- **Summaries are JSON literals.** A minimum, a maximum and each sampled
  value are JSON, so an empty-string minimum is told apart from a type
  that does not order; a timestamp, NaN or infinity is a string of its
  Arrow rendering. The distinct count is a HyperLogLog of precision 12,
  within 5% in the lane. `list_bundles`' statement now runs in a test
  against a live catalog on clickhouse-local.
- **The person's publish is a gesture.** The Publish menu runs
  `publish_result` through play's catalog as the person, so it takes the
  same checks and the same audit as an agent's, without a call context.

### 2026-10-07 — M7 built

- **The view argument is `bundle_view`.** `view` was the plan, but
  play's own `set_dist_options` takes a `view`; an operation's
  arguments are play's struct with one field in front, and the bytes the
  dispatcher encodes decode into play's type unchanged, since the
  decoder ignores the field play's type lacks.
- **An embedded play is framed as a served one.** A play window judges
  and settles the agent mark around its frame, which is what makes a run
  an agent asked for the task's; an operable view does the same
  (`PlayApp.FrameServed`), and records each dataset's origin so the
  grant is asked for the bundle, never the local name.
- **A defect the lane found.** A play pinned to this process's
  introspection endpoint — every applet and bundle view — resolved as a
  manual endpoint, so an agent's run there was asked for
  `clickhouse:<loopback>`. The static resolver now classes that base as
  the introspection plane.
- **The lane.** `agent.TestAnAgentDrivesAnOperableBundleView` drives a
  receiver's view through the dispatcher: list, state, a pane the bundle
  does not show refused, the SQL not offered, a run refused until the
  grant lists `keelson-bundle:<bundle>` and applied after.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the applet document format.
- [ADR-0135](./0135-app-launch-requests.md) — launch requests.
- [ADR-0145](./0145-sealed-app-data.md) — sealed data and the placement wall.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) — ad-hoc datasets.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scenes.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — keelson.query.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — app operations and task grants.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's operations catalog and agent limits.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the audit trail.
