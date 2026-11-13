---
type: adr
status: accepted
date: 2026-11-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-12
---

# ADR-0368: An embedded chat — a chatlet drawn in another app's window, under the host's governance

## Context

A chatlet ([ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md))
is the chat specialised by a committed document. It is minted as an app of its
own, with its own window. A sqlapplet ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md))
is minted the same way, and it has a second shape as well. An app that wants
the applet next to its own content embeds it (§SD8). The embedding app
declares the applet's capabilities in its own manifest, and the applet's runs
carry a composed identity, the embedder's app id plus the applet slug
([ADR-0134](./0134-adhoc-datasets.md) §SD7).

A chat has no second shape. An app that wants "ask about this" beside its
own content can only open a chat window next to its own. That window is a
separate conversation the person has to place, and nothing ties it to the app.

A chat carries more than a play does. It has a model grant, a coordinator
that may ask for access, a run journal, the attention queue, an artefact and
a roster of conversations. Embedding it must not let the host or the
document get around any of that.

## Decision

### SD1 — One constructor; the host drives the pane with its own context

`chat.NewEmbedded(preset, chat.EmbedConfig{Host, Key, Layout})` returns a
`*chat.Pane`. The host calls the pane's `Mount`, `Frame` and `Unmount` from
its own, passing its own context, and draws the pane in whatever region its
frame gives it. The pane takes everything it needs from that context, as a
window does: the host's bus, storage, lifetime scope, tasks and window key.
The host's drain therefore waits for the pane's run to close.

`chatlet.Embed(slug, cfg)` finds a document in a registered book and embeds
it. A host that wants a document only embedded, never in the launcher,
parses its own document with `chatlet.ParseDocSource` and passes the preset
to `chat.NewEmbedded`. Nothing is registered at the host's startup for that.

### SD2 — Governance stays the host's

- The pane's capabilities are the host's to declare: `chat.EmbedCaps()`
  plus `chat.EmbedPersistedKeys(key)`, appended to the host's manifest, as
  `sqlapplet.BundleViewCaps` is. A host that declares fewer has the calls it
  left out refused at its bus. A document widens nothing (ADR-0364 §SD2).
- The bus sender is the host. So the host is the app the model service
  attributes the calls to, the owner the run journal records, and the
  requester the access dialog names. The dialog also shows the document's
  `purpose`. A grant request reaches the person only if the operator
  registered the host as a coordinator (`BOXER_AGENT_COORDINATORS` or its
  predicate, ADR-0269 §SD6). Embedding a chatlet does not make its host a
  coordinator.
- No subject, service or wire field is added. Every check the host
  enforces is already keyed by the sender.

### SD3 — A composed identity, and a key that keeps panes apart

The pane's identity is `<host app id>#<key>`, the shape ADR-0134 §SD7 settled
for an embedded applet. It is attribution, not authority. It stands where
the chat stamps itself: the turn chip's provenance, the artefact's
`chat_app` property, and the id the preset's pin digests. Because the pin
includes it, a run started in a pane is read-only in the chatlet's own
window, and the other way round, as a changed prompt makes it (ADR-0308 §SD5).

The key keeps two panes of one host apart, since the host's runs, storage
and window are shared:

- the run row's settings record `pane`, and a pane lists, resumes and
  deletes only its own runs; a window of its own lists only runs without one;
- its storage keys are prefixed `chat-<key>-`;
- its turn line for the roster is published under `<window>#<key>`;
- its widget ids are scoped by a salt derived from the identity.

The same key in another window of the host is the same pane, and it picks up
that pane's conversations.

### SD4 — Two layouts

`EmbedLayoutFull` draws the window's bar and dock. `EmbedLayoutCompact` is for
a narrow region: the bar, a wrapping row naming the tabs, then the transcript
over the composer, or the one tab picked in their place. Nothing docks and no
divider drags except the composer's. The Artefact tab still comes forward on
the model's first write. Because the host's window may hold another composer,
a pane sends on Ctrl+Enter only while its own composer had the keyboard focus.

## Alternatives

- **Open a chatlet window beside the host** (`windowhost.open`). Rejected:
  that is what exists today. The person places it, and it shares nothing with
  the app.
- **Host identity only, with no key.** Rejected: two panes of one host would
  list, resume and title each other's conversations.
- **A bus sub-identity for the pane, checked by the services.** Rejected for
  now: every service would then have to trust a name the client claims
  inside its own sender. The composed identity is attribution, as it is for
  sqlapplet. The sender stays the only authority.
- **Register each pane as an app.** Rejected: an app is a window in the
  launcher, which is the first shape again.

## Consequences

- A host that embeds a chat carries the chat's capabilities in its manifest,
  and a capability review of the host sees them there.
- The trail rows the model service and the agent service write
  (`llm_calls`, `agent_actions`) name the host. Slicing them by pane goes
  through the conversation id to its run, whose settings name the pane.
- The plain chat and the chatlet's own window do not list a pane's
  conversations, since they are another sender's runs.
- A host's manifest lists its operations; the chat's `turn_state` is not
  among them (see Deferred).

## Verification, recorded 2026-11-10

Unit tests in `apps/chat` and `apps/chatlet` (`chat_embed_test.go`,
`chatlet_embed_test.go`) cover:

- the identity;
- the refusals;
- two panes of one host over one journal;
- the storage prefix;
- mounting on a host's context.

A scratch host that was not committed embedded `audit-trail` twice, once in
each layout, in one window. A headless scene drew both panes, ran a scripted
turn in the compact one while the other stayed idle, and brought its Artefact
tab forward, whose `chat_app` read `<host>#audit-trail`. The host log had no
id warnings. Not driven:

- a host registered as a coordinator, through a granted session;
- a real model.

## Deferred

- **Operations through the host's catalog.** The pane's `turn_state` for an
  agent sharing the host's window, offered the way `BundleViewOps` is.
  Trigger: an agent that needs to read a pane's turn.
- **Help.** The chat's book in the host's F1. Trigger: a host whose
  people ask.
- **Context from the host.** The host telling the model what the pane sits
  beside, or sharing its own window automatically, widens what the model
  sees and is a decision of its own. Trigger: the first host that needs it.
- **A first host.** None ships with this ADR. Trigger: an app that wants
  "ask about this".

## Status

Accepted 2026-11-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — chatlets, the preset.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD8, [ADR-0134](./0134-adhoc-datasets.md) §SD7 — embedding an applet, the composed identity.
- [ADR-0288](./0288-adhoc-bundles-datasets-published-with-the-applet-that-reads-them.md) §SD7 — the bundle view, a caps list a receiver adds.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD6 — coordinators.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD5 — the fingerprint a run is pinned to.
