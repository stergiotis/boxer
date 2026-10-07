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
  has an alias of its own; the datasets' global aliases are minted by the
  service as `<bundle>__<local>`, so a document never needs rewriting when
  it is published under another bundle alias. The document is held in
  process memory beside the records, never on disk, bounded at 256 KiB.

- **SD2 — Publish is atomic and format-agnostic.** A new request
  `adhoc.bundle.publish` carries the bundle alias, the document bytes, the
  local datasets as Arrow IPC streams, `KeepAfterClose`, and the provenance
  of SD5. The service checks the quotas and every stream, then makes the
  datasets and the bundle record live together; a failure leaves nothing
  live. A republish swaps the document and every dataset under one lock, and
  the previous revision's files retire as ADR-0240 §SD4 retires them. A
  retract, or the publishing window closing (ADR-0240 §SD5), withdraws the
  bundle whole. The service stores the document without parsing it: it
  lives under `public/keelson/runtime` and the applet parser lives in
  `apps/sqlapplet`. The checked constructor is
  `sqlapplet.PublishBundleE`, which parses the document and refuses one
  whose `keelson('…')` references name anything outside the bundle's local
  datasets; play parses it again on open.

- **SD3 — An alias has one owner.** A publish of a dataset or bundle alias
  that is live under another owner is refused with a typed error naming the
  owner. Owner is ADR-0240 §SD2's: the publishing instance, or the app when
  `KeepAfterClose` is set. Resolve keeps answering the newest live dataset,
  which under this rule is the owner's only one. A window binds a dataset
  under a **local name** that may differ from its alias
  (`adhocdata.Follower` gains the mapping; play's `bind_dataset` gains
  `as`), so an app whose windows each publish under an instance-qualified
  alias keeps a document that names one local name.

- **SD4 — play opens bundles and publishes results.**
  - `launchcfg.PlayLaunch` gains `Bundle`: the window loads the document
    — buffer, parameters, the panes `tabs:` names — binds the bundle's
    datasets under their local names, and follows the bundle: a republish
    reloads and reruns, a retract leaves the window saying what it waits
    for, as a single followed dataset does.
  - Operations, under ADR-0270's catalog:

    | Operation | Class | Effect |
    | --- | --- | --- |
    | `list_bundles` | query | none |
    | `open_bundle {alias}` | command | document — replaces the buffer; its outcome reports, per pane the document names, whether the pane can draw what it is fed and why not, as `list_panes` does |
    | `publish_result {node?, alias, bundle?}` | command | **consequential** — the main result, or one node's, as a dataset; with `bundle`, the buffer as the document of a one-dataset bundle |

    `publish_result` is consequential as ADR-0269 §SD5 defines publishing:
    the person confirms each one, in act mode as in suggest. The person's
    own Publish button is not asked. `publish_projection` keeps its effect
    and becomes `publish_result` over the projection's two results.
  - An agent's run reading a bundle needs `keelson-bundle:<alias>` in the
    grant, which covers the bundle's datasets; `keelson:<alias>` keeps
    covering one dataset.

- **SD5 — Every bundle operation is audited, under a context the host attests.**
  A request that an agent's call caused — publish,
  republish, retract, read — carries the `(task, epoch, call)` of the
  `app.OperationCall.OnBehalfOf` it runs under, as httpegress requests carry
  theirs. The dataset service does not take the rest from the publisher: it
  asks the host's dispatcher through a new `app.CallContextI` (wired by
  hostboot as `app.DelegationI` is), which answers for a call only when the
  call is live in that task at that epoch and was routed to the requesting
  sender and instance, and returns what the dispatcher recorded for it:
  the **conversation** (the chat session the task was requested from), the
  **turn**, the model call, the provider's tool-call id and index, the
  operation and the coordinator. A context the dispatcher does not confirm
  refuses the request; a request with no context is the person's or the
  app's own and is audited as such. Turn and conversation are therefore
  what the coordinator stated to the dispatcher (ADR-0277 §SD1), never what
  the publishing app claims.

  The audit is a new archetype on ADR-0277's trail, written through the
  host's `trail.Recorder` like every other row there, one row per operation
  and outcome: publish, republish, retract, withdraw-on-close, read,
  resolve, refused. It carries `Origin` (stamped by the recorder from the
  envelope), `Delegation`, `Conversation` and `Cause` from the attested
  context, and a new domain component `AdhocDataset`: operation, outcome and
  reason, bundle alias, dataset alias and handle, revision, rows, bytes, and
  BLAKE3 digests of the stored Arrow stream and of the document — the digest
  ADR-0277 §SD2 uses for message content. Because the dispatcher supplies
  `Conversation` and `Cause` here, these rows are vouched for one step more
  strongly than ADR-0277 §SD1's table says those components usually are.
  The stream digest is what lets a reader, or a test, show that the bytes a
  consumer read are the bytes a producer published, without either party's
  word for it. The live state stays in `keelson('adhoc_bundles')` — alias,
  owner, local names and the dataset aliases they map to, `tabs:`, digests,
  revision, created, and the context of the publish that made the live
  revision — and `keelson('adhoc')` gains `bundle`, `task`, `call`,
  `conversation` and `turn`. A row joins the agent action rows on
  `(task, call)` and the model-call and message rows on
  `(conversation, turn)`.

- **SD6 — Only play queries a dataset.** Any other app reads a dataset with
  `adhocdata.ReadAllE(bus, alias)` over a new request `adhoc.read`: the
  service resolves the alias and returns the sealed Arrow IPC stream as it
  was stored — the producer's bytes, without an engine between them. The
  request takes no statement, so a reader cannot filter, join or aggregate;
  that is play's job, and the result of it is a `publish_result`. The
  keelson.query service (ADR-0253) refuses a statement that reads a table
  the registry marks sealed (`introspect.Registry.IsSealed`), so a grant
  on `keelson.query.<table>` cannot be widened into a query surface over
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

### Milestones

- **M1 — Bundle records in `adhocdata`:** atomic publish, republish and
  retract, owned aliases, `keelson('adhoc_bundles')`.
- **M1a — Audit:** `app.CallContextI` on the dispatcher, attestation on
  every bundle request, the `AdhocDataset` archetype on the trail, the
  context columns on `keelson('adhoc')`.
- **M2 — `adhoc.read` and the gate:** `ReadAllE`, keelson.query refusing
  sealed tables, local names on `Follower`.
- **M3 — play:** `PlayLaunch.Bundle`, `list_bundles`, `open_bundle`,
  `publish_result`, `bind_dataset` with `as`, the `keelson-bundle:`
  destination.
- **M4 — Publishers migrated;** `publish_projection` over `publish_result`.
- **M5 — The headless end-to-end test** of the Verification plan.
- **M6 — Bundle views:** `NewBundleView`, `BundleViewCaps`, the document
  check in `PublishBundleE`, plain views; adhocdemo becomes a receiver.
- **M7 — Operable views:** mounted commands in appops, `BundleViewOps` with
  the subset and its availability.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Capability subjects (ADR-0026) | added: `adhoc.bundle.publish`, `adhoc.bundle.retract`, `adhoc.bundle.resolve`, `adhoc.read`; `adhoc.event.published` / `retracted` carry the bundle alias | `adhocdata` service and client; the manifests of apps that publish or read bundles |
| Bus codecs (ADR-0240 §SD8) | added: bundle request/reply and read reply memberships under `runtime/codec/` | generated with the existing adhoc codecs |
| Introspection tables | added: `keelson('adhoc_bundles')`; `keelson('adhoc')` gains `bundle`, `task`, `call`, `conversation`, `turn` | catalog providers; the help pages that list them |
| The trail store (ADR-0277) | added: the `AdhocDataset` component and its archetype | `runtime/trail` regeneration; the trail views; the facts claims golden |
| `app` exported API | added: `CallContextI`, implemented by `agent.Service` and wired by hostboot | httpegress may move from `DelegationI` onto it later; not in this decision |
| `adhocdata` exported API | added: bundle publish/resolve/retract, `ReadAllE`, local-name binding on `Follower`; changed: publish refuses an alias another owner holds | every publisher in the Migration list |
| `launchcfg.PlayLaunch` | added: `Bundle` | leeway codec regeneration |
| play's catalog (ADR-0270) | added: `list_bundles`, `open_bundle`, `publish_result`; changed: `bind_dataset` takes `as` | play's ops tests; the operation summaries the chat reads |
| Agent grant destinations | added: `keelson-bundle:<alias>` | play's agent limits; the request dialog |
| keelson.query gate (ADR-0253) | changed: refuses sealed tables | `keelsonquery.Gate` tests |
| `sqlapplet` exported API | added: `NewBundleView`, `BundleViewCaps`, `BundleViewOps`, `PublishBundleE` | receivers' manifests |
| `appops` exported API | added: mounted commands | catalogs that mount a component |
| Receivers' catalogs | added, for an operable view: the `bundle_` operations | the operation summaries the chat reads |

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
- Every fixed-alias publisher whose app can open two windows is refused on
  the second window's publish until it moves to instance-qualified aliases
  and local names.
- The document is held in memory per bundle revision, bounded but not
  quota-counted with the datasets.
- A bundle document is limited to what every receiver can grant; a bundle
  that needs another endpoint or capability is opened in play, not
  embedded.
- An operable view's subset is a second list of play's operations to keep in
  step with play's catalog; a test holds it to play's names and specs.
- A bundle request under an agent's call takes a round to the dispatcher,
  and a request whose call has ended — the task stopped, the epoch moved —
  is refused even when the publish itself would be valid. An app that
  publishes after its call has finished publishes as itself.
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

- **Breaks.** A publish under an alias another owner holds is refused. The
  fixed-alias publishers (2026-10-07): imzrt profiles (keep-after-close,
  owned by the app, unaffected), writingstylescope, chat stats, adhocdemo,
  play's series and card-grid fixtures, play's projection and
  regex_explorer. Each moves to an instance-qualified alias and binds its
  document's local name to it. Publishers in downstream modules follow
  with their pin bump past the refusal.
- **Path.** Publishers first, in the same commit as the refusal; then
  `publish_projection` onto `publish_result`.
- **Regeneration.** The adhoc codecs and `launchcfg`'s leeway codec.
- **Old shape.** Unowned aliases are removed outright; nothing persists an
  alias across processes.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `adhocdata` (atomic publish and republish,
  refusal of a held alias, `ReadAllE` returning the stored bytes, retract
  withdrawing the bundle whole; a request whose call context the dispatcher
  does not confirm — wrong task, stale epoch, a call routed to another
  instance — refused; an audit row per operation carrying the attested
  conversation and turn), `keelsonquery.Gate` (a sealed table is
  refused), play's ops tests (`open_bundle` verdicts, `publish_result`
  proposed for confirmation in act mode, `bind_dataset` with `as`), and one
  headless task: play `publish_result` → `ReadAllE`, bytes identical; a
  bundle published → `open_bundle` → a pane that can draw it; the audit
  rows of both joined to the action records on `(task, call)` and agreeing
  on conversation and turn and on the stream digest.
- **Bundle views.** A plain view rebuilt on a document revision keeps
  surviving parameter values and adds nothing to the catalog; an operable view
  offers exactly the subset, reports a pane outside `tabs:` unavailable,
  and its `bundle_run` is refused by play's agent limits as play's `run`
  is; every `bundle_` operation's spec matches play's of the same name
  apart from the `view` argument; `PublishBundleE` refuses a document
  outside `BundleViewCaps`.
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
