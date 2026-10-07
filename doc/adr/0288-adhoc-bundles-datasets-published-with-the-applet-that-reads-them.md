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
task moves data between apps by reading it and writing it again. play hands
it at most 50 rows of a result as text (`sample_rows`); an app whose
operations take code or drawing commands takes only those. Every number
that crosses
from one window to another passes through the model's context and is
retyped into the next tool's input. Nothing checks the copy: a transposed
digit or a dropped row in that hop produces a plausible wrong answer and no
error. The same path bounds what can cross at all — the result has to fit
a model turn — and leaves no record of which result fed which call.

Two mechanisms already exist for content that should not pass through the
model:

- **ADR-0269's result references and data handles**: a query's result is
  held by the host and an operation can declare an argument as a reference
  (`OperationSpec.Refs`). No operation declares one, and the chat reads
  every reference into the model's context on receipt.
- **[ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md)'s
  ad-hoc datasets**: an app publishes an Arrow stream under an alias, play
  reads it as `keelson('<alias>')`, a window follows republishes and
  retracts. Datasets are already the exchange format between an app and
  play — adhocdemo publishes a dataset and embeds an applet document that
  reads it.

What the second is missing for agent work: the applet that reads a dataset
— the SQL whose shape maps to a pane, per the pane contracts of
[ADR-0132](./0132-sqlapplet-sql-defined-applets.md) and its successors —
can only be compiled into the publishing app, never published beside the
data; an app other than play has no way to read a dataset back without
writing SQL; an alias is not owned, so two publishers of one alias
replace each other silently (`adhocdata.Service.Resolve` returns the newest);
and nothing records which task or call published a dataset.

## Design space (QOC)

**Question.** By what path does a derived table travel from one window of
an agent's task to another, so that the bytes the consumer sees are the
bytes the producer wrote?

**Options.**

- **O1** — *Transcription*: the model reads a bounded result and writes the
  values into the next call (the path in use).
- **O2** — *Host result references*: ADR-0269's references, opened to
  cross windows; the chat passes reference ids instead of values.
- **O3** — *Ad-hoc bundles*: ADR-0240's datasets, published as a unit with
  the applet document that reads them; play is the query surface.

**Criteria.**

- **C1** — *Fidelity*: the consumer receives the producer's bytes.
- **C2** — *Reach*: the person can open, inspect and chart what crossed,
  in the same tools, without the agent.
- **C3** — *Size*: tables far beyond a model turn can cross.
- **C4** — *Lifetime and audit*: what crossed has an owner, a lifetime and
  a record of who published it.

**Assessment.**

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | ++ |
| C2 | −  | −− | ++ |
| C3 | −− | +  | +  |
| C4 | −  | +  | ++ |

O2 keeps references inside a task's host state: the person cannot open one
in play, a reference has no SQL that says what it is for, and it dies with
the task. O3 reuses a store that already has ownership, quotas, sealed
storage and a follower, and puts every crossing where the person already
looks at data.

## Decision

We will publish derived tables between windows as **bundles**: one applet
document and the ad-hoc datasets it reads, published, republished and
retracted as one unit. Querying, transforming and visualising a dataset
happen in play; any other app reads a dataset whole, through a read that
takes no statement.

### Subsidiary design decisions

- **SD1 — A bundle is an applet document plus its datasets.** The document
  is the applet format ADR-0132 defines — frontmatter `datasets:`, `tabs:`,
  `endpoint: introspection`, parameters, one or more `sql` chapters — so
  "SQL in a shape that maps to a pane" is the existing pane contracts and no
  new shape language. Inside the document a dataset is named by its
  **local name**, the name in `datasets:` and in `keelson('…')`. The bundle
  has an alias of its own, which may not contain `__`; the datasets' global
  aliases are minted by the service as `<bundle>__<local>`
  (`adhocdata.DatasetAlias`), so a document never needs rewriting when it
  is published under another bundle alias. The document is held in process
  memory beside the records, never on disk, bounded at
  `MaxBundleDocumentBytes` (256 KiB); a bundle carries one to
  `MaxBundleDatasets` (16) datasets.

- **SD2 — Publish is atomic and format-agnostic.** `adhoc.bundle.publish`
  carries the bundle alias, the document, the local names and their Arrow
  IPC streams index for index, `KeepAfterClose`, and the on-behalf-of
  context of SD5 — the whole bundle in one request. Every stream is sealed
  before the commit lock is taken, and ownership, collisions and quotas are
  checked before sealing and again under the lock; a failure leaves
  nothing of the publish live. A republish swaps the document and every
  dataset, keeps the bundle's creation instant, and withdraws the previous
  revision's datasets in ADR-0240 §SD4's two phases, so a reader that holds
  one finishes. `adhoc.bundle.retract`, or the publishing window closing
  (ADR-0240 §SD5), withdraws the bundle whole; `adhoc.bundle.resolve`
  answers with the document and the datasets' local names and handles. A
  bundle's datasets are reached only through it: retracting or
  republishing one alone is refused (`ErrBundleMember`). Bundle events go
  out on `adhoc.bundle.event.published` / `.retracted`, outside
  `adhoc.event.>`, so a consumer that follows datasets never decodes one;
  each dataset still announces itself on `adhoc.event.*`, naming its
  bundle. The service stores the document without parsing it — it lives
  under `public/keelson/runtime` and the applet parser in `apps/sqlapplet`.
  The checked constructor `sqlapplet.PublishBundleE` (SD7) parses a
  document before it is published; play parses it again on open.

- **SD3 — An alias has one owner, and a window publishes under its own.** A
  publish of a dataset or bundle alias that is live under another owner is
  refused (`ErrAliasHeld`). Owner is ADR-0240 §SD2's: the publishing
  instance, or the app when `KeepAfterClose` is set; the runtime may
  publish under any alias. Resolve keeps answering the newest live
  dataset, which under this rule is the owner's. A publisher whose app can
  open two windows publishes **window-scoped**: the service, which knows
  the sender's window from the envelope, publishes under
  `adhocdata.WindowAlias(base, instance)` — `<base>_w<instance>` — and
  answers with the alias it used (`adhocdata.NewWindowPublisher`). Its
  consumers bind that alias under a **local name**: the base, which their
  SQL reads. Local names are carried by `adhocdata.Follower`
  (`LocalNames`, `FollowAs`), play's launch config (`DatasetNames`) and
  `bind_dataset`'s `as`. A grant never names a local name: two windows'
  `keelson('stats')` are two datasets, so play keeps, per bound name, the
  dataset's global alias and bundle, and the agent limits judge a run by
  those.

- **SD4 — play opens bundles and publishes results.**
  - A window opens a bundle by its launch config (`launchcfg.PlayLaunch`'s
    `Bundle`) or by `open_bundle`: it moves to the introspection endpoint,
    where datasets resolve; the document becomes the buffer, its
    definition drawer and its preamble; the bundle's datasets are bound
    under their local names; and the window follows the bundle — a
    republish reloads and rebinds, a retract leaves the window saying what
    it waits for, a publish reopens it. Nothing is asked of the bus on the
    render goroutine: the resolve runs on a goroutine of its own and the
    frame that next draws the window applies its answer. A document runs on
    open only if it is a plain read, the rule ADR-0132 §SD5 applies to an
    applet. sqlapplet, which owns the parser and imports play, hands play
    the parser at init (`play.SetAppletDocParser`); a host without it
    refuses a bundle by name.
  - Operations, under ADR-0270's catalog:

    | Operation | Class | Effect |
    | --- | --- | --- |
    | `list_bundles` | external read of `keelson('adhoc_bundles')` over keelson.query | none — and this window's last publish |
    | `open_bundle {alias}` | command | document — the window follows the bundle from the next frames; `list_panes` then says what each pane draws |
    | `publish_result {bundle, local_name?, sql?, tabs?, title?}` | command | **consequential** — the whole main result as a one-dataset bundle |
    | `bind_dataset {alias, as?}` | command | document — an alias bound under the name the buffer reads |

    `publish_result` is consequential as ADR-0269 §SD5 defines publishing:
    the person confirms each one, in act mode as in suggest. play composes
    the bundle's document: the caller's SQL over `keelson('<local name>')`
    on the panes it names (`SELECT *` on the table when it names none), and
    the query that produced the rows as the record of where they came from.
    Only a whole main result is published — one the row cap cut short is
    refused, naming the cap, since a dataset made of a prefix would miss
    rows with nothing to say so; a node's lane does not record whether it
    was cut, so a node is published by running it as the main result. The
    publish runs off the frame, under the call's on-behalf-of context.
  - An agent's run reading a bundle's dataset needs `keelson-bundle:<alias>`
    in the grant, which covers every dataset of the bundle, or
    `keelson:<global alias>` for one; a refusal asks for the bundle. Both
    are host reach in the dispatcher's ceiling: a bundle is datasets in
    this process.

- **SD5 — Every bundle operation is audited, under a context the host attests.**
  A request that an agent's call caused — publish, republish, retract,
  resolve, read — carries the `(task, epoch, call)` of the
  `app.OperationCall.OnBehalfOf` it runs under, as httpegress requests carry
  theirs. The dataset service does not take the rest from the requester: it
  asks the host's dispatcher through `app.CallContextI` (wired by hostboot
  as `app.DelegationI` is), which answers for a call only when the
  dispatcher sent it to the requesting app and window, in a task still live
  at that epoch, and returns what the dispatcher recorded for it: the
  **conversation** (the chat session the task was requested from), the
  **turn**, the model call, the provider's tool-call id and index, and the
  operation. A context the dispatcher does not confirm refuses the request,
  and the refusal's reason reaches the window; a request with no context is
  the person's or the app's own and is audited as such. Turn and
  conversation are therefore what the coordinator stated to the dispatcher
  (ADR-0277 §SD1), never what the requesting app claims.

  **The attribution outlives the call.** A call is attested from the moment
  the dispatcher sends it, not only while it is in flight, and the task's
  epoch moves only when the task ends: an operation that answers at once
  and publishes when its work finishes — a run, an evaluation, anything
  longer than an agent call's timeout — publishes under the call that
  started it for as long as the task is live. The app keeps the
  `OnBehalfOf` of the call for the work it starts and passes it with each
  request that work causes.

  The audit is an archetype on ADR-0277's trail, written through the host's
  `trail.Recorder`, one row per operation and outcome: publish,
  republish, retract, withdraw (the publishing window closed), read, and
  the refusal of any of them. Every publish, retract and read is audited,
  whoever asked; a resolve only when an agent's call caused it, since every
  bundle window resolves on each revision and the trail is for what agents
  looked up. A refused claim is audited without the context it named. A row
  carries `Origin` (stamped by the recorder from the envelope),
  `Delegation`, `Conversation` and `Cause` from the attested context, and
  the domain component `AdhocDataset`: operation, outcome and reason,
  bundle, revision, owner, and per dataset its local name, alias, handle,
  rows, bytes and the BLAKE3 digest of its stream as sealed — computed
  while sealing — with the document's digest; `Attested` says the context
  came from the dispatcher. The stream digest is what lets a reader, or a
  test, show that the bytes a consumer read are the bytes a producer
  published, without either party's word for it. The trail view
  `dm_trail_adhoc_bundles` and a timeline branch read the rows. The live
  state is in `keelson('adhoc_bundles')` — alias, owner, local names and
  dataset aliases, handles, the document's size and digest, revision,
  created, and the context of the publish that made the live revision — and
  `keelson('adhoc')` carries `bundle`, `task`, `call`, `conversation` and
  `turn`. A row joins the agent action rows on `(task, call)` and the
  model-call and message rows on `(conversation, turn)`.

- **SD6 — Only play queries a dataset.** Any other app reads a dataset with
  `adhocdata.ReadAllE(bus, alias, obo)` over `adhoc.read`: the service
  resolves the alias and returns the Arrow IPC stream as sealed — the bytes
  every reader of it reads — with its digest, which `ReadAllE` checks
  against what it received (`ErrDigestMismatch`). A reader decodes the
  stream with `ipc.NewReader`; one that hands the bytes on, into a sandbox
  or onto a wire, keeps the digest with them. The request takes no
  statement, so a reader cannot filter, join or aggregate; that is play's
  job, and the result of it is a `publish_result`. The keelson.query
  service (ADR-0253) refuses a statement that reads a table the registry
  marks sealed (`introspect.Registry.IsSealed`), so a grant on
  `keelson.query.<table>` cannot be widened into a query surface over
  datasets. An applet embedded in another app's window is play and reads
  as play does.

- **SD7 — A bundle view is one constructor.** A receiver shows a bundle with
  `sqlapplet.NewBundleView(alias, cfg)` and adds `sqlapplet.BundleViewCaps`
  to its manifest; nothing else. `adhoc.bundle.resolve` answers with the
  document, its digest and the local-name-to-alias mapping, so the view
  needs no second request. The view follows the bundle: a dataset revision
  rebinds as ADR-0240 §SD6's follower does, a document revision re-parses
  and rebuilds the embedded play in place, keeping parameter values whose
  name and type survive; a retract leaves the view saying what it waits
  for. Because an embedded applet's capabilities are the embedder's
  (ADR-0132 §SD8) and a manifest is fixed before any bundle exists, a
  bundle document may ask for nothing beyond `BundleViewCaps`: endpoint
  introspection, reads of its own datasets, no declared capabilities of its
  own. `sqlapplet.PublishBundleE` refuses a document outside that set, so
  every bundle opens in every receiver. The view keeps "Open in
  Playground", which opens the bundle in a full play window.

  **A view does its work on the frame it is drawn.** A receiver that culls
  views out of sight or keeps them in tabs may not call a view's frame for
  many frames. Nothing reaches an undrawn view: the follower's events and
  answers wait in its mailbox, and the frame that next draws the view
  syncs them — a dataset revision, a document revision, a retract —
  before it draws, so the view shows the bundle as it stands, not the
  sequence it missed. An idle view costs only its draw; its frame cost is
  measured (M6) so a receiver can choose between a view and a widget of
  its own.

- **SD8 — A bundle view is plain or operable.** The receiver picks one per
  view at construction (`Operable` in the view's config), and the manifest
  says whether it can be operable.
  - **Plain** is for the person: the view draws, takes the person's input —
    parameters, pane options, selections — and is part of what the window
    shows, so ADR-0269 §SD11's capture includes it, as will an
    accessibility tree when ADR-0269 §SD12's deferral lands. It adds
    nothing to the receiver's catalog; an agent sees it only in a capture.
  - **Operable** is a plain view whose play operations are offered to agents
    through the receiver's catalog. `sqlapplet.BundleViewOps` mounts them
    with `appops.Set.Mount`, prefixed `bundle_`, each taking a `view`
    argument that names the view when a window holds more than one. The
    subset:

    | Offered | Not offered |
    | --- | --- |
    | `get_state`, `describe_result`, `sample_rows`, `list_history` | `set_sql`, `bind_dataset`, `publish_result` — the document is the publisher's; a changed document is a new bundle, made in play |
    | `run`, `cancel_run`, `set_param` | explain, trace, flow and completion operations — authoring tools, which a full play window has |
    | `get_<pane>`, `set_<pane>_options` and a pane's select command, for the panes the document's `tabs:` names | the operations of panes the document does not show |
    | `list_panes` and `get_diagnostics` for those panes: whether a pane can draw what it is fed, the last run's error | the rest of play's diagnostics |

    The catalog is static, so a pane operation the bundle does not show is
    declared and reported unavailable (`appops.Set.Available`) with the
    reason, rather than absent. A run from an operable view is play's run: the
    same agent limits (ADR-0270), the same `keelson-bundle:` destination,
    the same query stamp. Effects are play's for each operation; the person's
    gestures in the view go through the same handlers. appops gains
    mounted commands beside `MountedQuery` for this.

  A receiver that only displays results is plain; one whose agent is
  expected to read and steer what it shows — re-run with another parameter,
  read the drawn chart — is operable. Anything beyond the subset is reached by
  opening the bundle in play.

- **SD9 — Many bundles per receiver.** A receiver may publish a bundle per
  item it shows — a notebook, one per output cell — so the count quota
  has two levels: `MaxDatasets` per process, which bounds the sealed files
  held open, and `MaxDatasetsPerOwner` per owner (the window, or the app
  for what it keeps after close), so one receiver cannot take the
  process's whole count. The byte quota (`StoreMaxBytes`) bounds the store
  whatever the count. A many-bundle receiver keeps one bundle alias per
  item, republishes it when the item changes — a republish replaces, it
  does not add — and retracts it when the item goes; it does not mark
  per-item bundles `KeepAfterClose`, so closing the window releases them
  all.

### Milestones

- **M1 — Bundle records in `adhocdata`:** ✓ atomic publish, republish and
  retract, bundle events, `keelson('adhoc_bundles')`.
- **M1a — Audit:** ✓ `app.CallContextI` on the dispatcher, attestation on
  every bundle request, the `AdhocDataset` archetype on the trail, the
  context columns on both catalogs.
- **M1b — Two-level count quota (SD9):** ✓ `MaxDatasetsPerOwner` beside
  `MaxDatasets`.
- **M2 — `adhoc.read` and the gate:** ✓ `ReadAllE`, keelson.query refusing
  sealed tables, local names on `Follower`.
- **M3 — play:** ✓ `PlayLaunch.Bundle`, `list_bundles`, `open_bundle`,
  `publish_result`, `bind_dataset` with `as`, the `keelson-bundle:`
  destination.
- **M4 — One owner per alias:** ✓ window-scoped publishes, the publishers
  moved to them, `PlayLaunch.DatasetNames`, and the refusal of an alias
  another owner holds.
- **M5 — The headless end-to-end test** ✓ of the Verification plan.
- **M6 — Bundle views:** `NewBundleView`, `BundleViewCaps`, the document
  check in `PublishBundleE`, plain views that sync on the frame they are
  drawn; adhocdemo becomes a receiver; the frame cost of an idle plain
  view measured in a headless scene and recorded.
- **M7 — Operable views:** mounted commands in appops, `BundleViewOps` with
  the subset and its availability.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Capability subjects (ADR-0026) | added: `adhoc.bundle.publish`, `.resolve`, `.retract`; `adhoc.bundle.event.published`, `.retracted` (`adhoc.bundle.event.>`); `adhoc.read` | `adhocdata` service and client; the manifests of apps that publish, open or read bundles |
| Bus codecs (ADR-0240 §SD8) | added to the adhoc request, reply and event kinds: bundle, document, local names, streams, handles, stream digest, on-behalf-of task/epoch/call, window scope, the reply's alias | generated with the existing adhoc codecs; vdd's assignment golden |
| Introspection tables | added: `keelson('adhoc_bundles')`; `keelson('adhoc')` gains `bundle`, `task`, `call`, `conversation`, `turn` | catalog providers; the help pages that list them |
| The trail store (ADR-0277) | added: the `AdhocDataset` component, its archetype, the view `dm_trail_adhoc_bundles` and a timeline branch | `runtime/trail` regeneration; `trailviews` (views v3); the runtime vocabulary's golden |
| `app` exported API | added: `CallContext`, `CallContextI`, implemented by `agent.Service` and wired by hostboot | httpegress may move from `DelegationI` onto it later; not in this decision |
| `adhocdata` exported API | added: bundle publish/resolve/retract, `ReadAllE`, `NewWindowPublisher`, `WindowAlias`, `DatasetAlias`, `IsHandle`, local names on `Follower`, `MaxDatasetsPerOwner`; changed: publish refuses an alias another owner holds, `MaxDatasets` is a process bound | every publisher in the Migration list |
| `launchcfg.PlayLaunch` | added: `Bundle`, `DatasetNames` | leeway codec regeneration |
| play's catalog (ADR-0270) | added: `list_bundles`, `open_bundle`, `publish_result`; changed: `bind_dataset` takes `as`; play's manifest gains the bundle resolve and events and `keelson.query.adhoc_bundles` | play's ops and caps tests; the operation summaries the chat reads |
| play's exported API | added: `AppletDoc`, `SetAppletDocParser`, `DestinationKeelsonBundle` | sqlapplet installs the parser |
| Agent grant destinations | added: `keelson-bundle:<alias>`, host reach | play's agent limits; the chat's guidance and `request_access`'s example |
| keelson.query gate (ADR-0253) | changed: refuses sealed tables | `keelsonquery.Gate` tests |
| `sqlapplet` exported API | added: `NewBundleView`, `BundleViewCaps`, `BundleViewOps`, `PublishBundleE` (M6, M7) | receivers' manifests |
| `appops` exported API | added: mounted commands (M7) | catalogs that mount a component |
| Receivers' catalogs | added, for an operable view: the `bundle_` operations (M7) | the operation summaries the chat reads |

## Alternatives

- **O1, transcription.** Rejected per the QOC: no check on the copy and a
  bound set by the model turn.
- **O2, host result references across windows.** Rejected per the QOC: the
  person cannot open what crossed, and it lives only as long as the task.
  References stay what ADR-0269 made them, a task's handle on a call's
  result.
- **Datasets without a document.** A consumer would have to be told, in
  the model's words, what the table is and how to chart it. The document is
  what lets the person open the bundle and see what the producer meant,
  and what lets play report whether the pane can draw it.
- **Namespacing every alias by its publisher in the service, documents
  rewritten to match.** Rejected: rewriting SQL text is the fragile half,
  and local names (SD3) give the same isolation with the document
  untouched.
- **Letting producers query their own datasets through keelson.query.**
  Rejected: a second query surface over datasets is a second place an
  agent's reads escape play's agent limits and history, and a second dialect
  of what a dataset means.
- **`publish_projection` rebuilt on `publish_result`.** The projection's
  two datasets are bound by handle in the window that computed them, and
  its scaffold names them; a bundle around them adds nothing that window
  needs, and an agent that wants them elsewhere publishes a query over
  them. `publish_projection` keeps its shape and publishes window-scoped.
- **A lone dataset from `publish_result`.** Rejected: what crosses would
  carry no SQL that reads it and no record of the query that made it.
- **`publish_result` as a document-level command.** Rejected: ADR-0269 §SD5
  names publishing as consequential, and a dataset is readable by every
  window in the process, beyond what the task's grant covers. Each publish
  asks the person.

## Consequences

### Positive

- A table crosses from play to another app, and back, as the producer's
  Arrow bytes, at up to ADR-0240's per-dataset quota, and a test can assert
  identity at each hop.
- What an agent published is visible in the person's tools: open the bundle
  in play, read `keelson('adhoc_bundles')`, follow the call to the action
  record and the turn to the chat transcript that asked for it.
- The conversation and turn on an audit row are the dispatcher's, so an app
  cannot attribute a publish to a turn that did not cause it.
- Two publishers can no longer replace each other's dataset by accident.
- Showing a bundle is one constructor and one manifest entry, and the view
  stays current with the bundle's data and document.

### Negative

- A confirmation per `publish_result`. A pipeline of many hops asks the
  person many times.
- A publisher whose app can open two windows publishes window-scoped, and
  whatever opens its data names the window's alias and binds it under the
  base; a plain alias held by another window is refused.
- The document is held in memory per bundle revision, bounded but not
  quota-counted with the datasets.
- A bundle document is limited to what every receiver can grant; a bundle
  that needs another endpoint or capability is opened in play, not
  embedded.
- A receiver that shows many items carries the bookkeeping of one bundle
  alias per item: republish on change, retract on removal.
- An operable view's subset is a second list of play's operations to keep in
  step with play's catalog; a test holds it to play's names and specs.
- A bundle request under an agent's call takes a round to the dispatcher,
  and one whose task has ended is refused even when the publish itself
  would be valid; work that outlives its task publishes as the app's own.
- A truncated result cannot be published; the agent aggregates or raises
  the row limit first.
- Hashing every stream costs a pass over its bytes at publish; the read
  side reuses the stored digest.
- On a host without a durable trail backend the audit rows are lost, as
  every trail row is; `BOXER_TRAIL_REQUIRED` (ADR-0277 §SD3) refuses an
  agent-caused publish there instead.

### Neutral

- Datasets resolve on the introspection endpoint only, so a statement
  cannot join a dataset with a server table (ADR-0145). Moving a server
  result to a dataset is a `publish_result` from a window on the server
  endpoint.

## Migration — Tier 1

- **Breaks.** A publish under a plain alias another owner holds is
  refused. The fixed-alias publishers (2026-10-07) move to window-scoped
  publishes: adhocdemo and the embedded applets bind the window's alias
  under the base through their follower; chat's statistics,
  writingstylescope's handover and the regex explorer's open play with
  the window's aliases in `Datasets` and the base names in
  `DatasetNames`; play's fixtures and projection bind their own handles
  under the base and record the window's alias as its origin. imzrt's
  profiles are kept after close, owned by the app, and unaffected.
  Publishers in downstream modules follow with their pin bump past the
  refusal.
- **Path.** The publishers and the refusal land in one commit.
- **Regeneration.** The adhoc codecs, `launchcfg`'s leeway codec, the trail
  store, and the vocabularies' assignment goldens.
- **Old shape.** Unowned aliases are removed outright; nothing persists an
  alias across processes.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `adhocdata` (atomic publish and republish,
  refusal of a held alias, `ReadAllE` returning the stored bytes, retract
  withdrawing the bundle whole; a request whose call context the dispatcher
  does not confirm — wrong task, stale epoch, a call routed to another
  instance — refused; an audit row per operation carrying the attested
  conversation and turn; window-scoped aliases and the refusal of a held
  one), `keelsonquery.Gate` (a sealed table is refused), the trail's
  cross-lane test (a bundle row joins the action that caused it), play's
  ops tests (a window following a bundle through republish and retract,
  `publish_result` declared consequential and refusing a prefix, its
  document parsed by sqlapplet's parser, `bind_dataset` with `as`, the
  grant naming the bundle, never a local name), and one
  headless task: play `publish_result` → `ReadAllE`, bytes identical; a
  bundle published → `open_bundle` → its datasets bound; the audit
  rows of both joined to the action records on `(task, call)` and agreeing
  on conversation and turn and on the stream digest.
- **Bundle views.** A plain view rebuilt on a document revision keeps
  surviving parameter values and adds nothing to the catalog; an operable view
  offers exactly the subset, reports a pane outside `tabs:` unavailable,
  and its `bundle_run` is refused by play's agent limits as play's `run`
  is; every `bundle_` operation's spec matches play's of the same name
  apart from the `view` argument; `PublishBundleE` refuses a document
  outside `BundleViewCaps`.
- **Receivers.** A publish under a call the window has already answered is
  attested while the task is live; one owner stops at its count while
  another still publishes; a view not drawn for a run of frames shows the
  bundle's current revision on the frame it is next drawn, after a
  republish and after a retract-and-republish in between.
- **What would fail.** A partial bundle live after a failed publish; a
  second owner's publish accepted; bytes differing between publish and
  read; an agent's publish applied without a confirmation; an audit row
  missing, or carrying a turn the dispatcher did not record for its call.
- **Gap.** Whether a pane's verdict on open matches what it draws on screen
  is play's existing `list_panes` contract, not re-tested here.

## Status

Proposed — awaiting review by p@stergiotis.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-07 — M1 bundle records, without the solo-alias refusal

Shipped in `adhocdata`: `Service.PublishBundle`, `ResolveBundle`,
`RetractBundle` and their bus requests on `adhoc.bundle.publish`,
`.resolve` and `.retract`; one request carries the whole bundle (the
wire's list memberships carry the local names and streams), so
atomicity needs no staging step. Every stream is sealed before the commit
lock is taken and admission is checked again under it. A republish
withdraws the previous revision's datasets in two phases and keeps the
bundle's creation instant. `keelson('adhoc_bundles')` lists live bundles;
`keelson('adhoc')` gains `bundle`.

Refinements:

- **Bundle events have their own subjects**, `adhoc.bundle.event.published`
  and `.retracted` under `adhoc.bundle.event.>`, outside `adhoc.event.>`,
  so a consumer that follows datasets never decodes a bundle event. Each
  dataset of a bundle still announces itself on `adhoc.event.*` with the
  bundle named.
- **A bundle's members are touched only through the bundle**: a plain
  retract or republish of a member is refused (`ErrBundleMember`), and a
  plain publish under a member's alias or a bundle's alias is refused
  (`ErrAliasHeld`). A bundle alias may not contain the separator `__`.
- **The refusal of one plain dataset's alias by another owner is not in
  M1.** It breaks every fixed-alias publisher with a second window open,
  and SD3's way out — local names on `Follower` — is M2; the refusal lands
  with M4's migration. Until then a plain alias resolves to the newest
  dataset as before.
- `adhocdata.IsHandle` recognises a handle by shape: the catalog names
  `adhoc` and `adhoc_bundles` share the `adhoc_` prefix.

### 2026-10-07 — M1a audit under an attested call context

Shipped: `app.CallContextI`, implemented by the agent dispatcher and wired
by hostboot into the dataset service; bundle requests carry the task,
epoch and call (`adhocOboTask`, `adhocOboEpoch`, `adhocOboCall`); the
`AdhocDataset` component on the trail with its view
`dm_trail_adhoc_bundles` and a timeline branch; the stream digest
computed while sealing; `task`, `call`, `conversation` and `turn` on both
catalogs.

Refinements:

- **A call is attested from the moment the dispatcher sends it**, not when
  the window has answered: a handler publishes before it replies. The
  dispatcher's record gained a `sent` flag for this; a call turned back
  into a proposal is unsent again.
- **Only an agent-caused resolve is audited.** A bundle view (SD7)
  resolves on every revision and a follower reconciles periodically;
  writing each as a trail row would bury the agents' lookups the trail is
  for. Publishes, republishes, retracts and withdrawals are audited
  whoever caused them.
- **A refused claim is audited without its context.** The row records the
  sender and the dispatcher's reason, and is not attributed to the task
  or turn the sender named.
- The bundle view is general, like the egress fetches: its name carries
  no agentic tag, since the person's publishes land there too.

### 2026-10-07 — M2 the whole-dataset read, the gate, local names

Shipped: `adhoc.read` and `adhocdata.ReadAllE(bus, alias, obo)`;
`keelsonquery.Gate` refusing a sealed table; `FollowerConfig.LocalNames`
and `Follower.FollowAs`, which bind an alias under the name a document
reads.

Refinements:

- **`ReadAllE` returns the stream, not a record reader**: a `ReadResult`
  with the Arrow IPC stream as sealed and its digest, which `ReadAllE`
  checks against the bytes it received (`ErrDigestMismatch`). A reader
  decodes it with `ipc.NewReader`; a consumer that hands the bytes on —
  into a sandbox, onto a wire — keeps the digest with them.
- **Every read is audited**, whoever asks: a read is data leaving play's
  query surface, which is what the audit is for. An agent's read is
  attested first.
- **A follower reports waiting by alias**, what the service knows; only
  the target sees local names.

Raised by the first receiver to plan around bundle views (a notebook that
draws each output cell's table or graph as a view), for M6:

- **Many bundles per window.** A bundle per output cell meets
  `MaxDatasets` (64 live datasets per process) within one window. A
  receiver retracts what it no longer shows, or the quota's unit is
  revisited; M6 states which.
- **Provenance outlives the call.** Attestation holds for a call the
  dispatcher sent while the task is live at that epoch, not only while the
  call is in flight, so an operation that answers at once and publishes
  when its work finishes keeps the call's task, conversation and turn.
- **Views not drawn every frame.** A receiver that culls off-screen views
  or keeps them in tabs calls a view's frame rarely. A view catches up on
  the frame it is next drawn: follower `Sync`, document revisions and
  retracts are pulled then, never pushed into an undrawn frame.
- **Per-view cost.** A view is an embedded play; M6 measures what an idle
  plain view costs per frame, so a receiver can choose between a view and
  a widget of its own.

### 2026-10-07 — the receiver's points folded into the decision

The four points of the M2 entry are now in the body: attribution
outlives the call (SD5), a view syncs on the frame it is drawn and its
idle cost is measured (SD7, M6), and the count quota has two levels
(SD9). The two-level quota shipped as M1b: `MaxDatasets` 1024 per
process, `MaxDatasetsPerOwner` 256, the byte quota unchanged. The
regex explorer's quota test fills one window's share, which it now meets
first.

### 2026-10-07 — M3a play opens bundles

Shipped: `launchcfg.PlayLaunch.Bundle`; a window that follows a bundle
(the document becomes the buffer, its definition and preamble; its
datasets are bound under their local names; a republish reloads, a
retract leaves the window waiting and a publish reopens it);
`list_bundles` and `open_bundle`. The applet parser stays in sqlapplet,
which imports play: sqlapplet hands play the parser at init
(`play.SetAppletDocParser`), and a host without it refuses a bundle by
name.

Refinements:

- **`open_bundle` is asynchronous.** A command runs on the render
  goroutine, where play asks the bus nothing, so the outcome says the
  window follows the bundle and moved to the introspection endpoint;
  the resolve and the apply happen on the following frames, and
  `list_panes` then reports what each pane draws — not the outcome, as
  SD4 had it.
- **A bundle runs on open only if its document is a plain read**, the
  rule ADR-0132 §SD5 applies to an applet; one that writes is applied and
  waits for a run the person or the grant asks for.
- **`list_bundles` reads `keelson('adhoc_bundles')` over keelson.query**
  rather than a list request of its own.
- **A refused resolve says why in the window.** The dataset service's
  reason travels in the reply's text; the request helpers put it in the
  error's message rather than a structured field, so an unattested open
  reads "not attested by the dispatcher" in the window's notice.

### 2026-10-07 — M3b publish_result

Shipped: `publish_result {bundle, local_name?, sql?, tabs?, title?}`,
consequential. The window's main result leaves as a one-dataset bundle;
play composes its applet document — the caller's SQL over
`keelson('<local name>')` on the panes it names, defaulting to `SELECT *`
on the table, and the query that produced the rows as the record of
where they came from. The publish runs off the frame, under the call's
on-behalf-of context, and `list_bundles` reports the window's last
publish.

Refinements:

- **Only a whole main result is published.** A result the row cap cut
  short is refused, naming the cap: a dataset made of a prefix would miss
  rows and nothing downstream could tell. A node's lane does not record
  whether it was cut, so a node is published by running it as the main
  result; SD4's `node` argument is not offered.
- **Always a bundle.** `publish_result` publishes a bundle even for one
  dataset, so what crosses carries the SQL that reads it and its source
  query; a lone dataset without a document is not offered.
- `publish_projection` stays as it is until M4.

### 2026-10-07 — M3c local names in play and the bundle grant

Shipped: `bind_dataset {alias, as}`; the `keelson-bundle:<alias>` grant
destination; the chat's guidance that data moves between windows as
bundles. M3 is complete.

Refinements:

- **A grant never names a local name.** `keelson('orders')` in a bundle
  window and in another window are different datasets, so play keeps,
  per bound name, the dataset's global alias and bundle; a run's check
  accepts `keelson-bundle:<bundle>` or `keelson:<global alias>`, and a
  refusal asks for the bundle. `list_datasets` reports the same
  destination.
- **A bundle destination is host reach.** The dispatcher's ceiling took
  an unknown destination class as network reach, the widest; a bundle is
  datasets in this process, as a keelson table is, and is now host reach.

### 2026-10-07 — M4 one owner per alias; the body revised as built

Shipped: window-scoped publishes (`NewWindowPublisher`, the service
minting `<base>_w<instance>` and answering with it),
`PlayLaunch.DatasetNames`, every fixed-alias publisher moved, and the
refusal of a plain alias another owner holds. M1 is complete with it.

`publish_projection` is not rebuilt on `publish_result`: the projection's
datasets are bound by handle in their own window, and a bundle adds
nothing there (Alternatives). It publishes window-scoped.

The body now states the decision as built through M4 — SD1 to SD6, the
milestones, Surfaces, Migration and the Verification plan; the entries
above record how each part got there.

### 2026-10-07 — M5 the end-to-end lane, and read-before-write

Shipped: `agent.TestABundleCrossesWindowsWithItsProvenance`, in the
dispatcher's own tests so the person's acts stay unexported. Under a
grant the person approved, one play window publishes its result — the
person confirms — another opens the bundle, and a third party reads the
dataset whole; the rows are the producer's, and the publish, the
agent's resolve and the read are trail rows that join the dispatcher's
action rows on `(task, call)`, carry the conversation and turns the
coordinator stated, and name one stream digest. clickhouse-local holds
the trail; the lane skips without it. play gained
`play_headless_testutils.go` for windows driven without a client.

The lane found what the handler tests could not: ADR-0269 §SD1 refuses
a command that writes a resource the task never read, and nothing read
`bundle` or `followed_datasets`. So no agent could call `publish_result`,
`open_bundle` — or, since ADR-0270, `bind_dataset`. `publish_result` now
writes nothing of its window (its effect is outside), and `get_state`
reads both resources and reports them (`Bundle`, `Followed`).

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the applet document format.
- [ADR-0135](./0135-app-launch-requests.md) — launch requests.
- [ADR-0145](./0145-sealed-app-data.md) — sealed data and the placement wall.
- [ADR-0240](./0240-adhoc-datasets-v2-sealed-store-owned-capability.md) — ad-hoc datasets.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — keelson.query.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — app operations and task grants.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's operations catalog and agent limits.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the audit trail and its context components.
