---
type: adr
status: proposed
date: 2026-09-23
---

> **Status: proposed — pre-human-review.** Decision under consideration; do
> not implement as if accepted.

# ADR-0254: Model inference as a keelson capability — `llm.<verb>`, and what a model may touch

## Context

Three records describe apps calling a language model, and none of them
names a capability. [ADR-0216](./0216-mdedit-llm-transformations.md)
(proposed, implemented) has mdedit run text transformations through
`public/llm/openaichat`, kept two packages away so that capslock attributes
the network egress to the transform package "rather than smearing it across
everything mdedit touches"; the feature is gated on `BOXER_MDEDIT_LLM_*`
environment variables. [ADR-0120](./0120-play-natural-language-ask-panel.md)
(proposed, unbuilt) planned the same shape for a play Ask panel: a sibling
package, an endpoint variable as the egress gate, the endpoint host shown
beside the gesture. [ADR-0139](./0139-semantic-layer-text2dsl.md) (proposed,
unbuilt) added an agentic loop in which the model calls tools that read the
introspection tables.

What that shape leaves out is the thing the capability model of
[ADR-0026](./0026-app-runtime-and-capability-subjects.md) exists to make
explicit. No manifest declares that an app sends text to a model; the
broker has nothing to show; no audit row names the app; and the egress is
deliberately placed where the static gate sees the least of it. It is the
position the introspection reads were in before
[ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md), with
three properties that make it worse:

- **What leaves is context, not bytes.** The operability requirements
  ([aiops-operability R14](../explanation/aiops-operability.md)) say it
  directly: model context is an egress, and the sensitivity mechanism must
  be the single policy point through which anything reaches a model. Today
  every consumer assembles its own prompt and calls its own client; there
  is no point at which a rule about sealed data
  ([ADR-0145](./0145-sealed-app-data.md)) could be applied.
- **The provider is not always a socket.** ADR-0216 was verified against
  LM Studio and Ollama on loopback; an in-process model is a plausible
  future provider. A grant phrased as network access would be wrong for
  one and over-broad for the other.
- **Tool calling inverts the direction.** Once a model can ask the runtime
  to do things (ADR-0139 §SD8/§SD9), whoever executes those calls does so
  with someone's grants. A service that runs them with its own would be a
  confused deputy holding everyone's capabilities.

A generic HTTP egress facility has been planned as a successor boundary
since [ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) O3 and
[ADR-0204](./0204-leaflet-map-core-port.md) §SD4 Q5 and is still not drawn.
Inference does not wait for it: the subject taxonomy already gates by
resource — `ch.*`, `kafka.*` and `fs.*` are not `net.*` — and a model is a
resource with semantics of its own.

## Design space (QOC)

**Q1 — Where does the gate on model inference sit?**

- *O1 — per-app environment gate, in-app client* (ADR-0120 §SD3, ADR-0216
  §SD3; the status quo). Killed: undeclared, unprompted, unattributed, and
  every consumer repeats the provider configuration and has its own place
  to get the sensitivity rule wrong.
- *O2 — the generic HTTP facility, once drawn.* Killed for inference: it
  gates bytes to a host, not context to a model; it has nothing to say to
  a loopback or in-process provider; and it is not drawn.
- *O3 — a declared `llm.*` cap with the client still in the app.* The
  ADR-0253 O4 shape. Killed: a grant that enforces nothing only turns the
  gate green.
- *O4 — a request/reply family served by a host-side service holding the
  one client.* Chosen.

**Q2 — Who executes a model's tool calls?**

- *The service, with its own grants.* Killed: the confused deputy. The
  service would need every grant any consumer might want a model to use.
- *The service, impersonating the caller.* Killed: the bus has no
  delegation primitive, and inventing one for this is a larger decision
  than the one at hand.
- *The caller, under its own grants, with the service stateless per
  turn.* Chosen — SD5. The model's tool calls come back in the reply; the
  app runs each through the bus client it already holds.

| criterion | O1 | O2 | O3 | O4 |
| --- | --- | --- | --- | --- |
| the app is the audited, prompted sender | − | + | + | + |
| one policy point for what reaches a model (R14) | −− | − | −− | ++ |
| covers loopback and in-process providers | + | −− | + | ++ |
| provider configured once | −− | + | −− | ++ |
| new surface | none | a facility | a label | one family, two codecs, one service |

## Decision

### SD1 — One family: `llm.describe`, `llm.complete`

`llm.<verb>` is a request/reply family. `describe` answers what the host
offers — model id, the endpoint's host, whether the provider supports tool
calls and structured output — so an app renders its surface only where a
model exists and can show the host beside the gesture, which is ADR-0216
§SD3's visibility without its environment variable. `complete` is one
chat completion: messages in, one answer out, with tool definitions and
tool calls carried as `openaichat` already models them.

An app declares `llm.ClientCaps()`: the two verbs, publish direction,
**not sticky**. Sending text off the box is a per-session consent the day
a Mount-time prompt exists; ADR-0253's live check recorded that the host
mints manifest caps without one today, so the flag is a statement of
intent, not a behaviour, exactly as ADR-0185's is. The `Reason` names the
purpose ("mdedit: transform the selection through a model").

A streaming verb is deferred (SD7): a bus request is one reply, and a
streamed completion would ride the frame contract of
[ADR-0144](./0144-query-engine-adapters.md) or the task progress subjects,
which is a decision for the first consumer that needs it.

### SD2 — One service, one client, configured once

`runtime.llm` (a new package beside the runtime's other services, working
name `llm`) holds the
repository's one `openaichat.ClientI`, built from a host-level registration
under the ADR-0009 registry: `BOXER_LLM_ENDPOINT`, `BOXER_LLM_MODEL`,
`BOXER_LLM_APIKEY` (sensitive), `BOXER_LLM_MAXTOKENS`, `BOXER_LLM_TIMEOUT`
— ADR-0216's five, lifted from the app to the host and renamed. An
unconfigured host runs the service anyway and answers `describe` with
"no model configured" and `complete` with a refusal, the appstate posture:
a consumer is told rather than left to a timeout, and the surface it
would have rendered stays absent because `describe` said so. Neither the
endpoint nor the model has a default (ADR-0216 §SD3's reason stands: a
wrong default model is worse than a refusal).

The service is where `net/http` lives, which is host-side code where a
network capability is expected. An app that talks to a model shows no
network capability to capslock at all and holds an `llm.*` grant instead,
which is the honest picture.

### SD3 — The sensitivity policy point

Every completion passes through one function before anything leaves, and
it is the only such function in the tree. Its first rule is the sealed-data
wall: a request carries the `queryengine` sensitivity label of the content
it was composed from — `confined` when any of it derives from a sealed
dataset (ADR-0145 §SD3) — and a confined request is refused unless the
provider is exempt by locality, the same rule the query dispatcher applies
(loopback endpoint, or a provider this process started). The label is the
caller's declaration: the service cannot see provenance, so a consumer
that composes from query results is responsible for carrying the run's
label forward, and the play consumer does (SD6).

Attribute masking of `sysmetrics.sensitive`-tagged values
([ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) §SD8) is the second
rule this point is built to hold and is deferred with it; the point exists
so that adding the rule is one change.

### SD4 — Every call is a record

The service records one row per completion: app, instance, purpose,
model, endpoint host, sensitivity, sizes, prompt and completion token
counts, latency, truncation, the refusal or error reason. Message bodies
are not kept by default; `BOXER_LLM_KEEP_MESSAGES` opts a deployment in.
`keelson('llm_calls')` serves the rows (ADR-0094 §SD1), so cost, refusal
rate and who-sent-what are queries; the
[ADR-0239](./0239-play-chat-panel-and-chatview-widget.md) chat pane is the
natural viewer of a stored session once bodies are kept. The bus audits the
request as well, because it is a request.

The prompts a model may be asked to run are a table too:
`keelson('llm_prompts')`, one row per registered prompt document with its
book, slug, scope, knobs and system text — a document that failed to parse
as a row carrying the error, so a drifted book is visible rather than
short. `llm_calls.purpose` is spelled `book/slug`, so the two join exactly.

*Built as an in-process bounded record (the last thousand calls), not a
facts-store kind.* A durable kind is a generated record store
([ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md)), and
the row above is the DTO it would carry; the write is deferred (SD7) so
the capability did not wait on a store.

### SD5 — A model's tool calls run under the caller's grants

When a request carries tool definitions, the service forwards them and
returns the model's tool calls unexecuted. The loop is the caller's:
ADR-0139 §SD9's in-conversation protocol lives in the orchestrator, in the
app's process, and each call the model makes is executed through the bus
client the app holds — an introspection read through the app's own
`keelson.query.<table>` grants (ADR-0253), a validate tool that is pure
nanopass and needs no grant. The service is stateless per turn and can
never exercise a capability the app lacks. What a model may touch is
therefore exactly what the app declared, which is the answer to Q2 and to
ADR-0139 §SD8's "guarded executor": the guard is the manifest.

A keelson tool surface for *external* agents — the introspection tables
and the validate tool exposed over MCP on stdio — is the same tool set
under a different transport and is deferred to its own ADR (SD7).

### SD6 — The consumers

- **mdedit** (ADR-0216) keeps its prompt book, preview-then-apply and
  one-attempt posture; its transform package swaps the `openaichat` client
  for the `llm` client, drops the five `BOXER_MDEDIT_LLM_*` variables, and
  renders the surface when `describe` reports a model.
- **play** gets its model affordance as a *transformation book* in
  ADR-0216 §SD2's shape rather than ADR-0120's bespoke panel: `explain`,
  `fix this error` and `ask` as prompt documents over the editor buffer,
  one `bgjob`-backed flow with preview and Insert / Replace actions, never
  running generated SQL unseen. ADR-0120's surviving constraints hold
  unchanged: compile-only through the text2sql2 orchestrator with the
  editor-delivery ops (its SD1), grounding owned by the ADR-0139 semantic
  layer (SD4), the nanopass canonical dialect as the target (SD7). The
  orchestrator's `LLMClientI` is implemented over the bus client, and the
  play consumer forwards the sensitivity label of the results a question
  was composed against (SD3).
- The `boxer text2sql` CLI is not an app and keeps calling `openaichat`
  directly; the CLI is host-side code.

### SD7 — Deferred, recorded

A streaming verb; the durable `llm_calls` kind (SD4); long completions as
a watchbill job kind
([ADR-0223](./0223-watchbill-durable-work-on-facts.md)) so a run survives
the window; attribute masking at the SD3 point; the external tool surface
over MCP; a Mount-time prompt so non-sticky means something; per-purpose
grants (`llm.complete.<purpose>`) if a deployment ever wants to allow
transformation but not free-form asking.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `llm.>` subject family | new, request/reply (SD1) | ADR-0026 §SD3 taxonomy; capinspector's registry, classifier and help page |
| Wire forms | versioned CBOR structs through `buscodec`, the chlocal broker's shape, carrying `openaichat`'s message model as is — a nested request would flatten into a dozen parallel facts columns for no reader's benefit | nothing generated; the call record (SD4) is the flat row |
| Env registry (ADR-0009) | `BOXER_LLM_ENDPOINT/MODEL/APIKEY/MAXTOKENS/TIMEOUT/KEEP_MESSAGES` added; `BOXER_MDEDIT_LLM_*` retired | `doc/env-vars.md` regeneration; any launch script setting the old names |
| `Manifest.Caps` of mdedit and play | +`llm.ClientCaps()`, non-sticky | the capslock app set (mdedit's transform package loses its network capability) |
| `llm/promptbook` | the ADR-0216 §SD2 book mechanism, lifted from `apps/mdedit/transform`, with three scopes added for play | mdedit's package becomes its book plus aliases; the corpus gate tests |
| play's tab set | +`model` (dock id 34), an editor tool beside Snippets | the tab-count pins; the panes menu |
| `text2sql2/orchestrator` | +`ToolClientI`, `ToolExecutorI`, `ToolObserverI`, `Config.Tools`, `Validate` | the openaichat adapter (now a ToolClientI); the ollama adapter stays single-shot |
| `Manifest.Caps` of play | +`keelson.query.sql_passes`, sticky, for the model's reads | the cap-count pin |
| `keelson()` table set | +`llm_calls`, +`llm_prompts` (SD4) | the introspection table docs |
| the `llm` runtime package | new service and client | hostboot wiring |

## Alternatives

The QOC section carries the killed options. Two more were weighed:

- **Keep ADR-0120's panel and add the capability under it.** Rejected:
  the panel's decisions that were about the *shape* of an app-side LLM
  surface (sibling package, env gate, config names, bespoke UI) are all
  replaced here, and the ones about *generation* (compile-only, grounding,
  DSL target) are constraints on a consumer, not a panel. A transformation
  book already exists as a shipped shape.
- **Fold this into ADR-0139.** Rejected: the semantic layer is engine-side
  content with no dependency on the runtime; the capability is runtime
  policy. They meet at SD5 and nowhere else.

## Consequences

### Positive

- A model call is a declared, audited request with the app as sender, and
  the provider is configured once per host.
- One place exists where "may this reach a model" is decided, with the
  sealed-data rule on day one.
- A model's tools are bounded by the app's manifest without a delegation
  primitive.

### Negative

- One transport bound: a completion is one reply under a request timeout
  the caller must set long enough for a local model (ADR-0216 chose 120 s),
  and a mid-flight cancel does not reach the provider.
- The tool loop crosses the bus once per model turn; an agentic question
  with a dozen introspection calls is a dozen round trips. Loopback bus
  cost, but recorded.
- The sensitivity label is declared, not derived; a consumer that forgets
  to forward it sends confined content as ordinary. SD3 makes that a
  consumer bug with one place to look, not a new class of bug.

### Neutral

- mdedit's shipped behaviour does not change from the user's side; its
  configuration names do.
- Without a Mount-time prompt the non-sticky flag is inert, as it is for
  ADR-0185.

## Migration — Tier 1

mdedit's transform package is the one shipped consumer. Its five
environment variables are retired in favour of `BOXER_LLM_*`; a host that
still sets the old names sees no transform surface and a registry
diagnostic naming the new ones. `ADR-0120` is withdrawn and `ADR-0139`
is revised in place; nothing built cites either.

## Verification plan — Tier 1

- **Lane: default `go test`.** The service over a fake `openaichat.ClientI`:
  `describe` reports the configured model and refuses when none; a
  confined request is refused against a non-loopback endpoint and served
  against a loopback one; a completion lands one fact row; tool calls come
  back unexecuted. mdedit's transform tests run against the bus client with
  a stub service.
- **Lane: capslock gate.** mdedit reports no `CAPABILITY_NETWORK`; the
  `llm` runtime package is host-side and outside the app set. Green on
  2026-09-23.
- **Live.** A transformation in mdedit against LM Studio or Ollama through
  the host configuration; `keelson('llm_calls')` shows the row.

## Milestones

- **M1 — the family and the service.** ✓ `llm.describe` / `llm.complete`,
  the wire, `BOXER_LLM_*`, hostboot wiring, capinspector entry; mdedit
  migrated.
- **M2 — the record.** ✓ `llm_calls` provider over the in-process record;
  the durable kind deferred.
- **M3 — play's transformation book.** ✓ `explain`, `fix this error`, `ask`
  over the orchestrator with `LLMClientI` on the bus. Built 2026-09-23:
  the prompt book lifted out of mdedit into `llm/promptbook` (mdedit keeps
  its book and the names it reads them by), play's three documents under
  its Model tab, an explain as one completion rendered as markdown, a fix
  and an ask compiled through the orchestrator with the T0 schema harvest
  of the pinned endpoint as grounding until the ADR-0139 layer exists,
  Insert / Replace over the delivery ops, and the buffer's dispatch label
  forwarded as the request's sensitivity.
- **M4 — the tool loop.** ✓ ADR-0139 §SD9 in the orchestrator, tools
  executed through the app's grants. Built 2026-09-23: `ToolClientI` and
  `ToolExecutorI` beside `LLMClientI`, a per-question call budget, the
  tool history kept across repair attempts, an optional `ToolObserverI`;
  play's executor offers `list_tables`, `describe_table`, `validate_sql`
  (pure nanopass) and `keelson_query` over the introspection tables the
  manifest grants (`sql_passes`), every call run in play's process.

## Status

Proposed 2026-09-23. Consolidates the app-side half of ADR-0120 (withdrawn
the same day; its evidence and generation constraints survive here and in
ADR-0139) and takes over ADR-0139 §SD8's guarded executor as SD5.

M1, M2 and M3 built the same day: the family, the service under
`runtime.llm` with `BOXER_LLM_*`, the wire, hostboot and capinspector
wiring, `keelson('llm_calls')` over the in-process record, mdedit migrated
— its transform package completes through the bus client, its five
variables are gone, and `llm.describe` gates the surface — and play's
Model tab over the shared prompt book; M4, the tool loop, the same day.
Two deviations from the text as first proposed are recorded in SD2's wire
row and SD4. Neither consumer has been checked live against a model.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD3 the taxonomy this family joins, §SD7 the broker, §SD10 capslock.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — the same move for table reads; the grants a model's tools run under.
- [ADR-0216](./0216-mdedit-llm-transformations.md) — the shipped consumer; the transformation-book shape play adopts.
- [ADR-0120](./0120-play-natural-language-ask-panel.md) (withdrawn) — the evidence review and the generation constraints SD6 keeps.
- [ADR-0139](./0139-semantic-layer-text2dsl.md) (proposed) — grounding, and the tool protocol SD5 places.
- [ADR-0145](./0145-sealed-app-data.md) — the sensitivity label and the locality rule SD3 copies.
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) §SD8 — the masking rule SD3 is built to hold.
- [ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) O3, [ADR-0204](./0204-leaflet-map-core-port.md) §SD4 — the HTTP facility this does not wait for.
- [aiops-operability](../explanation/aiops-operability.md) R14 — model context is an egress.
- `public/llm/openaichat` — the client the service wraps; `public/db/clickhouse/text2sql2/orchestrator` — the loop SD5 places.
