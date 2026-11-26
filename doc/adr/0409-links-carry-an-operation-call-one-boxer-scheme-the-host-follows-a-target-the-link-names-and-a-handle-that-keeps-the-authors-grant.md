---
type: adr
status: proposed
date: 2026-11-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. Built with the decision (2026-11-26): the leaf `runtime/applink`, the host's `runtime/linkhost`, the dispatcher's minting and click, the surfaces, the hover, and play as the first app whose operations take links; see *Status*. The owner's answers of 2026-11-26 are recorded under *Decided with the owner*.

# ADR-0409: Links carry an operation call — one `boxer:` scheme the host follows, a target the link names, and a handle that keeps the author's grant

## Context

A link in boxer can name a place in another app in exactly one way:
`mdedit://` ([ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md)),
which every markdown surface claims through `markdown.Input.LinkClaims` and
follows with a launch request
([ADR-0135](./0135-app-launch-requests.md)). Each surface names the scheme
itself (`mdeditlink.Claimed`), so a second app's scheme means editing every
surface. A link nobody claims stays an egui hyperlink, which the host hands
to the operating system's URL opener: a link with a scheme boxer does not
know leaves the cell
([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)).

Apps already describe what can be done to them, in a form a link could
carry. An operations catalog
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md))
declares per operation its argument and result types, a version, an effect
class and a one-line summary, and a `Direct` operation already opens a
window of its app when none is open and then calls it. The host resolves a
launch kind to an app ([ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)
§SD2). The clipboard broker hands out opaque handles, `boxer:clip/<id>`,
that resolve only inside boxer and keep their payload's labels
([ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)
§SD3).

What a link adds to the operations contract is a separation the contract
does not have. A call has one of two writers (`app.OperationCall.Writer`):
the person, through the window's own gesture, with no grant and no agent
limits; or a task, through the host's dispatcher, under a grant the person
approved and under the agent limits. A link is triggered by the person and
written by someone else — the person, a shipped help book, a model's reply,
a row in a table, an imported vault. If a click counted as the person's
gesture, every text boxer renders would be a way to act with the person's
authority: a confused deputy. If a model writes a link that runs a
statement and the person clicks it as their own act, the statement runs
outside the read-only bound the agent limits put on it (ADR-0269 §SD6).

## Decision

**A link is an operation call with an author and a target. The host owns
the link's envelope — the scheme, the target, the app, the operation and its
version — and the target app owns how the arguments are written into the
rest of the URL. The host claims every `boxer:` link and follows it under
the author's authority: the grant behind a handle the host minted for a
task, or else the person as the trigger with the author's label on what the
call writes, under rules set by the operation's effect.**

### SD1 — What a link carries

A resolved link has:

- a **target**: an app, or a launch kind, and how to reach a window of it
  (SD4);
- an optional **call** to an app: an operation, its version and its
  arguments. A link without a call reaches the window and does nothing more;
- for a kind, an optional **launch config** (facts-CBOR bytes, as ADR-0135
  delivers them);
- an **author**: the label of the text the link was found in
  ([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
  §SD1), and for a minted handle the grant it was minted under (SD6).

Inside the host the arguments are the operation's CBOR, as every call's are
(ADR-0269 §SD10). The URL is a spelling of them, never their store.

### SD2 — The envelope is the host's, the tail is the app's

```text
boxer:<verb>/app/<alias>[/<operation>@<version>[<tail>]]
boxer:open/kind/<kind>[<tail>]
boxer:l/<handle>
```

- **`<verb>`** is the target (SD4): `open` for a new window, `raise` for any
  open window of the app.
- **`<alias>`** is the app's subject alias (`app.Manifest.SubjectAlias`,
  `play`, `mdedit`), which the registry keeps unique: an app id is a Go
  import path, whose slashes a link's path cannot carry unescaped.
- **`<tail>`** is everything after `@<version>` — further path segments, a
  query, a fragment — and belongs to the codec of that operation and version
  (SD3), or of that kind. The host does not read it. It starts with `/`, `?`
  or `#`, or is empty.
- **Calls are addressed to apps, configs to kinds.** Two apps that take one
  kind may spell the same operation differently, so a call through a kind
  would mean whatever the resolved app's codec makes of it; a launch config
  is the kind's contract (ADR-0361 §SD1), and its codec is the kind's. A
  kind is only opened, because a config is delivered at mount (ADR-0135
  §SD4) and an open window cannot take one.
- **A handle** (`boxer:l/<handle>`) names a record the host keeps (SD7). It
  is the only form that carries a grant and the only form that names one
  window.

The host parses the envelope strictly, as `docref` and `mdeditlink` parse:
an unknown verb, a missing version or a malformed alias is refused. It
refuses a link longer than a bound (8 KiB) before any codec sees it; a call
too large to spell in that is a handle. `boxer:clip/` stays the clipboard
broker's and is not a link.

### SD3 — Each operation that takes links brings its codec

- **The codec is the opt-in.** A command is linkable when its catalog
  entry carries a link codec, `OperationSpec.Link` (`app.LinkCodecI`);
  without one, no link names it. The codec has two functions over the
  operation's argument type, `Format(args) → tail` and `Parse(tail) → args`,
  and at least one example argument value. `applink.Codec[T]` builds one
  over a typed struct; the host sees it over CBOR bytes. Only a command
  takes one: a query's answer would have nowhere to go.
- **The app chooses the spelling.** Path segments, query keys, a fragment,
  a compact form of its own — whatever reads in the app's terms. A codec is
  part of its operation's version: a change to the spelling that an old link
  would parse differently moves the version, and an old link is then refused
  with the reason.
- **Codecs parse text boxer did not write.** A codec is a pure function: no
  I/O, no state, bounded work for a bounded input. Parsing is strict — an
  unknown key or a malformed value is an error, never a default. The leaf
  `runtime/applink` gives codecs the URL pieces (`applink.ParseTail`,
  `applink.Tail`) — segment and query escaping, an escape that decodes to
  bytes that are not UTF-8 refused — so no app writes its own
  percent-encoding. The host calls a codec off the render goroutine and
  treats a panic as a refusal.
- **What a codec cannot spell.** Arguments declared as references — result
  references, data handles (`OperationSpec.Refs`) — name things that live
  with a task or a window; an operation with references takes no codec.
- **Validation.** Catalog validation refuses a codec on a consequential
  operation — nothing that writes outside the app is reachable from a click
  on someone else's text — and a codec that fails its own examples: each
  must format to a tail a link carries, read back to the same CBOR, and
  format to the same tail again. A catalog that fails is withdrawn at
  registration, as any catalog that fails validation is (ADR-0269 §SD2).
  `applink.RegisterKind` runs the same check on a kind's codec.
- **A run operation names how it is staged.** A run-effect operation with a
  codec may name, in `LinkStage`, a document-effect command of the same
  catalog that takes the same argument type and puts the arguments in place
  without running them. It reads the same tail with the run operation's
  codec. An authorless click on the run operation calls the stage instead
  (SD5); without one, an authorless click on a run operation is refused.
- **A kind may bring a codec for its config.** The kind's leaf registers it
  in `runtime/applink`, beside its `kindcheck` probe.
- **Alias schemes.** A codec may also be registered under a scheme of its
  own, which reads as the `boxer:` link it resolves to. `mdedit://` is the
  first: mdedit's launch kind registers the codec, and `mdedit://<mount>/<snapshot>/<path>?mode=…#…`
  resolves to `boxer:open/kind/<mdedit's kind>/<mount>/<snapshot>/<path>?mode=…#…`
  with the grammar of ADR-0347 §SD1–SD3 unchanged, except that a fragment
  holding characters a link writes only escaped is refused: accepted, it
  would read back as another location after `mdeditlink.Format` wrote it.
  Registration refuses a second codec for a scheme, and the schemes `boxer`,
  `http`, `https`, `mailto` and `file`.

`keelson('app_operations')` gains columns saying whether an operation takes
links, its stage and its codec's examples spelled as links;
`keelson('link_codecs')` lists every codec with its operation or kind, its
version, its stage, its alias scheme and its examples. A coordinator's
`describe` carries each linkable operation's examples as links
(`applink.Examples`), which is how a model learns an app's spelling.

### SD4 — The link names its target

| Target | Reaches | When there is none |
| --- | --- | --- |
| `open` | a new window of the app, or of the app the kind resolves to | — |
| `raise` | an open window of the app: the topmost in stacking order among those the link may reach (SD5, SD6) | opens one |
| a window key | that window; handle form only | refused: the window has closed |

A specific window can be named only by a handle because window keys are
counted per process (`windowhost.Inst.nextKey`): a key is never reused
inside a process and means another window in the next one. A handle dies
with the process (SD7), so it cannot outlive what its key means; an inline
link could.

Reaching an existing window raises it. The host raises it, not the app that
rendered the link: the raise service lets an app raise only its own windows,
and following a link is the host acting on the person's click. A click is
the person's act, so the desktop grant agents need to arrange windows
([ADR-0276](./0276-agents-read-and-arrange-windows.md)) is not involved.

An open passes the boundary every launch passes: the launch limit
([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md)), the kind
check, and the refusal of a config for a singleton that already has a window
(ADR-0135, Update of 2026-07-21).

### SD5 — An authorless link: the person triggers, the author is marked

An inline link, or a handle whose grant no longer authorises its call (SD6),
is followed with the person as the trigger. The call's `Writer` is
`person`; it carries a new `OperationCall.Link` naming the author's label and
the surface it was clicked on. What it may do depends on the effect and the
target:

| Effect | `open` | `raise`, a window key |
| --- | --- | --- |
| view | applied | applied |
| document | applied in the new window | refused |
| run | its `LinkStage` applied in the new window | refused |
| consequential | no codec (SD3) | no codec |

- **Existing windows take view effects only.** A document change written
  into the person's open work from text someone else wrote would need a
  proposal without a task behind it, which the operations contract has no
  place for.
- **What a call writes carries the author's label.** An app marks text a
  call wrote whose `Link` label is integrity level 2 or above as it marks
  text a task wrote (ADR-0269 §SD6): until the person edits or runs it.
  Level 0 and 1 — the person's own text, the harness's and the shipped
  books — are not marked.
- **The work is the person's.** The click is the person's act, and nothing
  it starts runs under agent limits; that is why a run is staged rather than
  started and why the person's next gesture, not the link, starts it.
- **No revisions.** An authorless call read nothing, so it expects no
  revision: the operations engine skips the check for a call with a link
  origin and no task, as it does for a gesture. A new window may move its own
  resources while it mounts, which an expectation taken at the open would
  turn into a conflict; in an existing window the call can only change the
  view, and the person editing a resource still wins (ADR-0269 §SD4).
- **Play is the first app whose operations take links.** `set_sql` and
  `query` spell their statement as `?sql=`; `query`'s stage is
  `stage_query`, which puts the statement in the buffer, and play marks it on
  the status line until the person edits or runs it. Live does not run it:
  Live reruns only a statement already run.

### SD6 — A minted link: the author's grant, checked at the click

A task asks for a link through the dispatcher, with a coordinator tool
`make_link` that takes what `call` takes — the arguments as the model's
JSON, the encoding of the tool boundary (ADR-0269 §SD10) — plus a target,
and answers with the link and the markdown to write it with.
The dispatcher checks the call it describes exactly as it would check the
call — entry, operation, mode against effect, destinations, ceiling
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md))
— without running it, and the host mints a handle for a record holding the
resolved link, the grant's id and epoch, the revisions the task had read of
the resources the call writes, the task's conversation, and the label of the
task's context. An operation without a codec can still be minted: the
record holds its CBOR arguments, and only an inline spelling needs the
codec. The grant handle itself never enters the record or the link: it is
bound to the coordinator's window instance (ADR-0269 §SD6), and a link
carrying it would be a bearer token for the whole grant. A minted link
authorises one call, the one checked when it was made.

At the click the dispatcher checks again — grant live, epoch unmoved, entry
still covering the target in a mode that allows the effect, budgets, ceiling
— and stamps the act chain *person clicked → task's grant → target window*.

- **The work is agent-caused.** `OnBehalfOf` is set, so the agent limits
  apply: a run-effect link runs, as a read under `readonly = 2` against a
  destination the grant lists. The call's `Writer` is the task.
- **Each click counts.** A click spends the task's call budget, and an
  `open` its launch count. The call's key is per click, so a second click
  is a second call, not a repeat that returns the first outcome.
- **A click is not a confirmation.** Accepting a proposal and confirming a
  command are the person's acts in host chrome (ADR-0269 §SD3); a link sits
  in an app's content. In suggest mode the call becomes a proposal on the
  window's badge.
- **Stale links conflict.** In an existing window the call expects the
  revisions recorded at minting; if they moved, it is a `conflict` that says
  the window changed since the link was made.
- **`raise` reaches only covered windows.** A minted link picks among the
  windows the grant's entries cover; with none, it opens one if the grant
  names the app for launch.
- **A minted link works on every surface.** Its text may be copied into
  another conversation, a document or a table, and a click there acts under
  the minting task's grant. The hover (SD8) names the task and conversation
  it was minted in.
- **When the grant no longer authorises it** — the task stopped, the epoch
  moved, the entry is gone, the mode is observe, the budget is spent, the
  window is busy or paused for the task — the link is followed as an
  authorless one (SD5), with the record's label as the author's: model
  output, marked. A click never asks the person to widen a grant.

A coordinator that meets a link in what it reads may read it with
`read_link`: the host's reading of the app, operation, arguments and target
it names, and nothing more. The model then opens or finds the window and
makes the call itself, as an ordinary `call` under its own grant. A link's
text never widens a grant, and a handle minted for another task gives the
reader nothing of that task's. A tool that followed a link would have been a
second way to make a call, checked by a second path; reading it keeps one.

### SD7 — The handle store is in memory

- **Lifetime.** Records live in the host process, in a bounded store; the
  oldest is evicted first. Grants, window keys and the store's key all end
  with the process, so no handle outlives what it refers to.
- **The handle** is BLAKE3 in its keyed mode, 256 bits, under a key drawn
  at process start, over the record's deterministic CBOR (RFC 8949
  §4.2) — the arguments as CBOR, not as a codec spelled them, so two
  spellings of one call are one record. The same record gets the same
  handle, a record cannot change under its handle, and nobody outside the
  process can compute one. An unkeyed digest would let anyone who could
  reconstruct a record — an injected model, the author of a row — produce
  the handle of a call another task had minted.
- **Who mints.** The dispatcher, for a task (SD6), and host chrome, for the
  person: *Copy link to this window* in the Window menu mints a handle to
  the active window, no call, the person as author, and puts it on the
  clipboard through the broker.
- **Dead handles.** A handle whose record is gone — evicted, or from an
  earlier process — is still claimed and shows why it does not open. It
  never falls through to the operating system's opener. A conversation
  resumed from its journal shows the handles its text holds as dead and
  mints nothing again; a replay that minted would recreate authority from a
  record.
- **Under test.** The headless host's test flag fixes the key, so a scene
  sees the same handle on every run.

### SD8 — One claim, one follow, and the host's hover

- **One claim.** `applink.Claimed` claims `boxer:` except `boxer:clip/`,
  and every alias scheme a codec registered. It reads only the scheme, so a
  surface claims without loading any app's code. Every surface that renders
  markdown passes it as its link claim, in place of `mdeditlink.Claimed`.
- **One follow.** A surface follows a click by sending the URL, the label of
  the text it was found in and its own identity to the host's
  `runtime.link.follow` subject, off the render goroutine, and declares the
  subject in its manifest. The host parses the envelope, hands the tail to
  the codec, decides under SD5 or SD6, reaches the target, applies the call
  and replies with the outcome, which the surface shows on its action line.
- **The host writes the hover.** A link's label is its author's words. The
  hover shows the host's reading of the link — the app's display name, the
  operation's summary and effect, the target, the arguments as the codec
  parsed them, field by field from the catalog's types, the author, and for
  a handle the task and conversation — from `runtime.link.describe`, asked
  off the render goroutine. A link the codec refuses says so in the hover,
  before anyone clicks it. The markdown widget takes the text as
  `markdown.Input.LinkHover`, under `LinkClaims`'s per-frame purity rule, so
  a surface answers from `applink.Hovers`: the last answer, a line saying
  the host is being asked, and a fresh ask off the render goroutine once an
  answer is older than a few seconds, since a minted link's reading changes
  when its task ends. The paragraph's links are one Rust widget
  (`labelAtomsLinked`), which reports the link under the pointer and shows
  its text through the builder method `linkTips`.

### SD9 — Records

Every follow writes one audit event on the trail
([ADR-0296](./0296-audit-events-as-a-trail-component.md)), domain `link`,
its action the decision (applied, staged, proposed, conflict, refused,
reached): the surface, the app and the window it reached as references, and
as attributes a digest of the resolved link, its form, the operation and
version, the author's label, whether the authorless rules applied, the task
a minted link named, and the reason. The window key joins the window's
lifecycle row. The digest is over the resolved record, so two spellings of
one call are one value; the link itself is not recorded, since its tail may
carry text the trail should not. A minted link's click also writes the
dispatcher's action records (ADR-0269 §SD9), and minting writes one with the
decision `minted`. An audit event needs no new vocabulary term; a row kind
of its own on `boxer.facts` would have, for columns the event already
carries.

### SD10 — Deferred, each with its trigger

- **The operating system's URL handler.** Registering `boxer:` with the
  desktop lets any web page or mail program trigger calls, which makes the
  author of every link an outsider. Trigger: a request to open a boxer link
  from outside the process, together with a threat-model entry for it.
- **The browser-tab host** ([ADR-0278](./0278-tab-mode-for-downstream-apps.md)).
  Its hyperlinks open browser tabs; the claim must reach it before a `boxer:`
  link is rendered there. Trigger: the first tab-mode app that renders
  links.
- **Durable handles.** A store on `boxer.facts` would make minted links
  survive a restart, and would need retention, erasure
  ([ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md))
  and a grant that outlives its window. Trigger: a minted link the person
  needs after a restart.
- **Version upgraders.** An app could register a function that reads an old
  version's tail into the new version's arguments. Trigger: a link kept in
  committed text refused for its version.
- **Authorless proposals into existing windows.** Trigger: a document link
  the person wants applied to the window they are working in.
- **Validation of links in committed text**, as `help/validate` checks
  documentation references. Trigger: the first book or ADR carrying a
  `boxer:` link.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.OperationSpec` | `Link` (`app.LinkCodecI`, over CBOR), `LinkStage`; validation refuses a codec on anything but a command, on a consequential operation or one with references, a codec that fails its own examples, and a stage that is not a document command with the same argument type | the catalogs that take links; `keelson('app_operations')` columns |
| `app.OperationCall`, `opwire.CallRequest` | `Link` (`app.LinkOrigin`: author label, surface, digest); `OperationCall.LinkMarked` | apps that mark task-written text mark link-written text at level ≥ 2 |
| `opengine` | a call with a link origin and no task expects no revision, as a gesture | — |
| `windowhost.Inst.OpsSubmit` | the host's own callers hand a call to a window's engine | — |
| `runtime/applink` (new leaf) | the envelope of SD2, its parser and formatter, `Tail` for codecs, `Codec[T]`, kind codecs and alias schemes, the record and its handle, the follow and describe client, `Hovers` | `keelson('link_codecs')`; codec tests and fuzz targets |
| `runtime/linkhost` (new) | the record store, `runtime.link.follow`, `runtime.link.describe`, *Copy link to this window*, the audit event | hostboot wires it beside the open service and hands it the dispatcher |
| the agent service | `runtime.agent.link` (minting through the dispatcher), `FollowLink` (the click), examples as links in `describe` | the chat's `make_link` and `read_link`; its tool registry, profile fingerprint and preservation goldens |
| `mdeditlink` | the codec of mdedit's launch kind and the `mdedit://` alias; `Open` and `Follow` removed | the surfaces follow through `applink` |
| markdown surfaces (ADR-0347 §SD5's list) | claim with `applink.Claimed`, follow with the author's label, hover with `applink.Hovers` | each manifest declares `runtime.link.follow` and `runtime.link.describe`; the help center no longer declares `runtime.windowhost.open` |
| `markdown.Input` | `LinkHover`, passed through `mdblocks` and `notebookview` | — |
| egui2 IDL | `labelAtomsLinked.linkTips`; `LinkedOut.hovered` in Rust | the generated bindings, the API reference, the browser host digest |
| host chrome | Window ▸ *Copy link to this window* (`ChromeConfig.CopyWindowLink`) | the link service's clipboard capability |
| play | codecs on `set_sql` and `query`; `stage_query`; the status line's link mark | play's operations help |

## Alternatives

- **One generic spelling of the arguments** — the operation's JSON, from the
  catalog's types, in a query parameter. No app writes code for it, but every
  link reads as an escaped JSON blob rather than in the app's terms, and the
  URL's shape follows the argument struct, so a refactor of the struct
  changes links no author meant to change. The owner ruled it out
  (2026-11-26): the target app spells its own arguments.
- **A scheme per app** (`play://`, `map://`), each wholly the app's.
  Deciding authority needs the target, the operation and its effect before
  any app code runs; with the whole URL the app's, the host would have to
  trust a codec to say what the link is. Every surface would also list every
  scheme, and a host without an app's registration hands that app's links to
  the operating system's opener, where another program may own the name.
  Alias schemes keep the readable name where one exists (`mdedit://`) and
  resolve to the host's envelope.
- **Links as launch configs only**, generalising `mdedit://` through kinds.
  A launch config has no effect class or summary and reaches only new
  windows; the operations catalog has both, and an operation reaches an open
  window.
- **A click as the person's gesture, with nothing else.** The author of the
  link would act with the person's authority, and a model's statement would
  run outside the agent limits.
- **The grant handle in the link.** A bearer token for the whole grant,
  valid wherever the text travels; ADR-0269 §SD6 binds the handle to the
  coordinator's instance for this reason.
- **A plain content digest as the handle.** Computable by anyone who can
  reconstruct the record. A random id is unguessable but gives up the
  property that a record cannot change under its handle; the keyed digest
  keeps both.
- **Window keys in inline links.** A key names another window after a
  restart, while the link text persists.
- **A durable store now.** Every minted link would need a grant that outlives
  its window and a retention rule; deferred (SD10) with its trigger.

## Consequences

### Positive

- One claim and one follow for every link family; a new app's links need a
  codec on the operations it offers, and no change to any surface.
- Links read in the app's own terms, and the host still decides what a link
  may do from the envelope and the catalog, without trusting the codec's
  opinion of its own link.
- A task can hand the person a link to a result that runs under the task's
  own limits.
- No link with a boxer scheme reaches the operating system's opener.

### Negative

- Every linkable operation costs a codec, its examples and its tests, and
  every codec is a parser of untrusted text.
- A model writes an inline link only as well as the examples in `describe`
  teach it the app's spelling; `make_link` avoids the question but yields a
  handle that dies with the process.
- A spelling change is a version change, so links kept in text are refused
  until an upgrader exists (SD10).
- Minted links die with the process. A resumed conversation shows its
  handles as dead.
- A minted link copied elsewhere acts under the minting task's grant from
  wherever it is clicked; only the hover tells the person whose grant it is.
- Apps must mark link-written text as they mark task-written text, and a run
  operation must name a stage to be useful from an inline link.
- Two new host subjects, an agent subject, two coordinator tools and an IDL
  builder method, all append-only.

### Neutral

- `mdedit://` keeps its spelling and behaviour; ADR-0347's per-surface claim
  becomes the host's.
- Window reuse, which ADR-0135 §SD5 deferred, is answered for links by the
  `raise` verb; plain launches keep opening a window each.

## Migration — Tier 1

- **Breaks.** `mdeditlink.Open` and `mdeditlink.Follow` are removed: a link
  is followed through the host, which decides what it may do. A caller
  follows with `applink.Follow` or `applink.FollowLogged` and declares
  `applink.FollowCap` in its manifest. `mdeditlink.Parse` refuses a fragment
  holding characters a link writes only escaped.
- **Path.** Claim with `applink.Claimed` where `mdeditlink.Claimed` was
  passed as a link claim; existing `mdedit://` links keep opening as before.
- **Regeneration.** The egui2 bindings, the API reference and the browser
  host digest moved with `linkTips`; the chat's preservation goldens moved
  with its two tools.
- **Old shape.** Removed outright. When this ADR is accepted, ADR-0347 gets
  a dated Update pointing at it for §SD5.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane:
  - `applink` round-trips the envelope and the tail and refuses malformed
    links, unknown verbs, a missing version, an oversized link and escapes
    that are not UTF-8;
  - every operation codec round-trips its examples at registration, so a
    failing codec withdraws its catalog in every test that registers the
    app; `applink.CheckCodec` does the same for a kind's codec, and fuzz
    targets (`FuzzLinkCodec` in `mdeditlink`, `FuzzQueryLinkCodec` in play)
    feed codecs tails nobody wrote under `applink.CheckParsed`: no panic,
    and an accepted tail spells one call;
  - `linkhost` decides by effect and window (`authorless`), and follows
    inline links, staged runs, raises, refusals, eviction and minted links
    whose grant authorises them or no longer does, against a fake host;
  - the dispatcher mints without running or spending, refuses what the grant
    does not run, runs a click under the grant, reports a stale link as a
    conflict, and stops authorising once the mode drops or the task ends;
  - the operations engine applies an authorless link's call without
    expectations and still yields to the person editing;
  - catalog validation refuses each shape of SD3.
- **Scenes.** `apps/mdedit/scenes/boxer-links.scene.md`: the hover of a
  link to play's `query` reads as the host's, a link to no app says it does
  not open, and a click opens a playground with the statement staged and
  marked. `apps/chat/scenes/chat-make-link.scene.md`: a scripted model mints
  a link and writes it into the artefact; the hover names the task, a click
  launches play and sets its buffer as the task, and after the task stops
  the same click is the person's, with the statement marked.
- **What would fail.** A `boxer:` link opened by the operating system; a
  codec that accepts a tail it would not write; link-written text at level
  ≥ 2 left unmarked; an authorless run started; a minted link applied after
  its grant ended with the task as writer.
- **Gap.** The tab host and the operating system's handler are out of scope
  (SD10). Fuzzing bounds what a codec accepts; it does not prove a codec
  spells what its author meant.

## Decided with the owner, 2026-11-26

- An authorless link runs with the person as trigger and the author's label
  on what it writes (SD5).
- The author's grant is traced by a handle the host mints, content-addressed
  over the record (SD6, SD7).
- The link names its target: a new window, any window of the app, or one
  window (SD4).
- Handles are kept in memory only (SD7).
- A minted link is honoured on every surface (SD6).
- A minted link whose grant has ended is followed as an authorless one (SD6).
- `raise` with no matching window opens one (SD4).
- An authorless link applies only view effects in an existing window (SD5).
- The arguments are not bound to JSON: the target app serializes them into
  the URL (SD2, SD3).
- The handle is keyed BLAKE3 at 256 bits (SD7).
- The rest as written: the envelope and its bound, calls to apps and configs
  to kinds, the codec as the opt-in, no codec for references, the authorless
  rules for any grant that no longer authorises a call, `LinkStage`, the
  topmost window for `raise`, *Copy link to this window*, and each click
  charged to the task's budgets.

## Status

Proposed — awaiting review by the owner. Built with the decision
(2026-11-26): every section but SD10, which stays deferred, and play as
the first app whose operations take links.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0135](./0135-app-launch-requests.md) — launch requests; window reuse
  deferred (§SD5).
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  — the operations catalog, the dispatcher, grants and agent limits.
- [ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md)
  — `mdedit://` and the per-surface claim this record moves to the host.
- [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)
  — a kind resolved to an app; the kind's contract in a runtime leaf.
- [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
  — labels and integrity levels.
- [ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)
  — opaque handles inside boxer.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)
  — the cell and its edges.
- [doc/explanation/agentic-layer.md](../explanation/agentic-layer.md) — the
  map of the agentic layer.
