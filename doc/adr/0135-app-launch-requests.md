---
type: adr
status: proposed
date: 2026-07-20
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0135: App-launch requests with leeway-modelled arguments

## Context

Apps cannot open other apps. `windowhost.Open(appId)` exists and is the
mechanism behind the launcher surfaces — it opens a window, mints the
per-app bus client, and emits app-lifecycle audit facts — but no bus
subject reaches it, `MountContextI` carries no arguments, and `Manifest`
declares nothing launch-related. Parameterizing an app today means env
vars at process start ([ADR-0009](./0009-environment-variable-registry.md);
play's `BOXER_PLAY_SQL`/`BOXER_PLAY_AUTORUN` incantations), persisted
state, or per-app request subjects an app services after it is already
mounted.

The demand is recorded in several places:

- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD3 deferred exactly
  this route — "bus-delivered `ReplaceSql` into a full play window via the
  `app.{id}.request.{name}` taxonomy" — until the Copy SQL escape hatch
  demonstrably failed someone. The "open this buffer in the full
  playground" gesture is that trigger firing.
- The help facility wants open-at-a-page (the recorded "per-doc Help
  narrowing" deferral); the demo registry opens a specific demo; the
  remote-access and automation lines want a deep-link primitive.

Two constraints shape the answer. First, hygiene (ADR-0026): opens must be
capability-declared and audited, and the target app is *not yet mounted*
when the request arrives — so per-app request subjects have a
bootstrapping problem; the window host is the actor. Second, a
data-engineering constraint: the runtime already has exactly one
sanctioned CBOR dialect — the runtime.facts DML module
(`factsschema/dml_cbor`) with `codec/factswrapper` generating per-DTO
`Marshal`/`Unmarshal` and a `buscodec.CodecI` bridge — and the runtime's
request/reply payloads are already codec modules under `runtime/codec/`
(`grantrequest`, `watchrequest`, `persistreply`, …; the capbroker
flattens its API types into these wire DTOs). A launch-arguments feature
must not introduce a second, schema-less dialect beside that.

A framing observation: a SQL applet is play with **frozen, committed**
arguments — a curated partial application whose manifest is the argument
record. Launch arguments are the runtime-dynamic point on the same axis.
The ADR-0132 registry-hygiene concern does not arise: opens create
windows, not manifests, so no durable name is minted.

## Design space (QOC)

**Question.** How does a running app open another app's window, optionally
with typed arguments — audited, capability-gated, and without a second
wire dialect?

**Options.**

- **O1 — Per-app ad-hoc request subjects.** Each app that wants to be
  launchable-with-arguments invents its own subject and wire shape.
- **O2 — Host-level open subject + leeway-modelled launch configs.** One
  audited `windowhost` request; per-app launch configs are leeway-declared
  DTOs with factswrapper-generated codecs, kinds registered in the
  runtime.facts vocabulary.
- **O3 — Host-level open subject + opaque arguments.** Same routing, but
  the payload is a schema-less CBOR bag the runtime never validates.
- **O4 — Status quo.** Env vars at process start plus the clipboard.

**Criteria.**

- **C1 — Uniformity**: one mechanism, one audit surface, no N protocols.
- **C2 — Schema discipline**: arguments are declared, versioned, refusable
  at the boundary.
- **C3 — Hygiene fit**: caps + audited request/reply per ADR-0026.
- **C4 — Marginal cost** for an app to adopt.
- **C5 — Fail-closed boundary**: malformed or mistargeted payloads refused
  visibly, before an app sees them.
- **C6 — Introspectability**: launches and launchability are queryable.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 per-app | O2 codec configs | O3 opaque args | O4 status quo |
|----|-----------|------------------|----------------|---------------|
| C1 | −−        | ++               | ++             | n/a           |
| C2 | − (per app) | ++             | −−             | −             |
| C3 | − (bootstrapping) | ++       | +              | +             |
| C4 | −         | + (DTO + kind + golden) | ++      | ++            |
| C5 | −         | ++               | −−             | n/a           |
| C6 | −         | ++ (facts-native) | − (blob)      | −−            |

## Decision

Adopt **O2**.

- **SD1 — One host-level open subject.** The window host services an
  audited request/reply subject, `windowhost.open`. The request wire shape
  is a codec module (`runtime/codec/launchrequest`; reply
  `launchreply`) carrying the target app id, the config kind, and the
  config as facts-CBOR bytes. Callers declare the subject in their
  manifest `Caps`; the target app is resolved, validated, and opened by
  the host — the per-app taxonomy is not used because the target is not
  mounted yet. Refusals (unknown app, kind mismatch, oversize, malformed
  envelope) are replies with named errors, never silent drops.

- **SD2 — A launch config is a leeway-declared DTO with a generated
  facts-codec.** An app that accepts launch arguments ships one config
  DTO in an app-owned package, declared in the codec grammar
  (`kind:"…"` tag, `lw:` columns) and generated via the `keelsoncodec`
  path (`factswrapper.FactsWrapper{}.Generate`), with the kind registered
  in the runtime.facts vocabulary and the module's golden pinning byte
  stability. This enforces the dialect rule **by construction**: a kind
  absent from the vocabulary has no codec, so a free-form payload is not
  representable, not merely rejected.

- **SD3 — `Manifest.LaunchKind`.** One optional manifest field naming the
  config kind the app accepts (empty = the app takes no arguments; an
  argument-carrying open targeting it is refused). The field is the
  boundary gate and makes launchability introspectable; a
  `keelson('apps')` column is a recorded follow-up, not built here.

- **SD4 — Delivery at Mount, frozen per window.**
  `MountContextI.LaunchConfig() []byte` returns the raw facts-CBOR (nil
  when the window was opened plainly). The app decodes with its generated
  `Unmarshal` in `Mount`; a decode failure is a visible mount-error label
  (the window host's existing failed-mount behaviour), never a silent
  default. Post-mount parameter changes remain per-app ops
  (`ReplaceSql` et al.) — launch config is an opening intent, not a
  channel.

- **SD5 — Window policy: always a new window.** Multi-instance already
  works (the per-instance widget-id salt). A manifest reuse/focus policy
  ("focus the existing window and deliver") is deferred with a trigger:
  the first app that genuinely needs it — help is the likely witness.

- **SD6 — The request is the audit record.** Because the request wire
  shape is itself a runtime.facts DTO, the host persists it as a fact
  beside the app-lifecycle "started" row it already emits, with the
  caller identity the audited-request path attributes. Launches — which
  SQL was opened in play, by which app, when — become ordinary facts
  queries. The config size cap (64 KiB at the boundary) bounds the row.

- **SD7 — play adopts, resolving ADR-0132 §SD3.** A `PlayLaunch` config
  (`Sql` text, `AutoRun`/`Live` bools, `BandsSql` text, `Tab` symbol);
  play's Mount seeding priority becomes: launch config (for windows
  opened with one) > `BOXER_PLAY_SQL` > persisted session buffer —
  per-window intent beats process-wide defaults, and plainly-opened
  windows behave exactly as today. Implementation lands a dated Update on
  ADR-0132 recording the §SD3 deferral resolved by this mechanism.

- **SD8 — Naming.** Subject `windowhost.open`; codec modules
  `launchrequest`/`launchreply` under `runtime/codec/`; manifest field
  `LaunchKind`; play's kind `playLaunch`. Open to review at acceptance;
  the constraint is only vocabulary hygiene (kinds are append-only).

## Alternatives

- **Per-app launch subjects (O1).** Killed: N wire shapes and audit
  surfaces, and a bootstrapping hole — the app that would service the
  subject is not mounted when the request arrives. Post-mount per-app
  subjects remain the right tool for what they already do.

- **Opaque argument bytes (O3).** Killed — and recorded honestly as the
  first sketch of this design. A schema-less bag cannot be validated at
  the boundary, audits as an inert blob, and quietly becomes a second
  wire dialect. The codec-module route costs each adopting app one DTO,
  one vocabulary entry, and one golden — deliberate friction that keeps
  the launch surface enumerable and queryable.

- **Status quo (O4).** Env vars cover process start only; the clipboard
  path is manual by design. It was the recorded v1 answer of ADR-0132
  §SD3, and this ADR is its recorded trigger firing.

- **URL scheme / deep links.** Deferred to the remote-access line
  (ADR-0024 territory); if it comes, it should compose *onto*
  `windowhost.open` rather than beside it.

- **Content-based routing (a rules layer choosing the target).** The
  Plan 9 plumber and Android implicit intents route by message content
  under user- or system-owned rules, so the sender need not name an app.
  Deferred, not killed: a router would be an ordinary caller layered
  *over* `windowhost.open`. v1 composition is explicit — the caller
  names the target.

- **Reuse/focus window policy now.** Deferred (SD5) — speculative until a
  concrete app needs it, and it complicates delivery semantics (a reused
  window has already consumed its Mount).

## Consequences

### Positive

- Apps compose: "open this in the full playground", help-at-a-page, and
  gallery-style deep opens all ride one audited primitive.
- One CBOR dialect; launch configs join the codec corpus with goldens,
  vocabulary governance, and byte-stable wire forms.
- Launches are queryable facts; the launch surface is enumerable from
  manifests.
- The ADR-0132 §SD3 gesture lands as ~30 lines of play Mount code once
  the chassis exists.

### Negative

- Contract growth: `MountContextI` gains a method (one implementer today,
  `StaticMountContext`, plus test fakes) and `Manifest` a field.
- Every argument-accepting app pays a vocabulary entry, a codec module,
  and a golden — the friction is the point, but it is real cost.
- The runtime.facts vocabulary becomes load-bearing for UI composition;
  kind hygiene (append-only, no renames) now guards launch compatibility
  too.

### Neutral

- Hygiene-not-security unchanged: caps and audit, not an in-process
  security boundary.
- Env-var parameterization remains; the launch config only outranks it in
  windows explicitly opened with one.
- More consumers on the interim hand-coded facts encoder strengthens the
  eventual general Leeway-CBOR codec case
  ([ADR-0010](./0010-leeway-cbor-rpc-codec.md)) without gating on it.

## Status

Proposed (2026-07-20).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

Internal:

- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — manifests, caps, audited request/reply, hygiene-not-security.
- [ADR-0132 — sqlapplet](./0132-sqlapplet-sql-defined-applets.md) — the
  §SD3 deferred route this resolves; the frozen-arguments framing.
- [ADR-0042 — keelson leeway codec SoA generator](./0042-keelson-leeway-codec-soa-generator.md)
  and the `runtime/codec/` corpus — the generator, the wire-DTO
  precedent (`grantrequest`), the goldens discipline.
- [ADR-0010 — Leeway-CBOR codec (deferred)](./0010-leeway-cbor-rpc-codec.md)
  — the interim hand-coded facts encoder this adds consumers to.
- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md)
  — the process-start parameterization this coexists with.
- [ADR-0134 — ad-hoc datasets](./0134-adhoc-datasets.md) — the sibling
  capability pattern (audited subjects, in-process custody).

External prior art:

- **Plan 9 plumbing** (Pike, *Plumbing and Other Utilities*) — the
  sharpest ancestor: six-field messages with attributes delivered to
  app-declared **ports** (the `LaunchKind` analog), the plumber starting
  the target when no reader exists and holding the message for
  delivery-at-start — SD1/SD4's shape, two decades earlier. Its
  user-programmable content routing is the layer recorded-deferred in
  Alternatives.
- **Android explicit Intents** — caller-named target, delivery at
  component start. Instructively *not* typed: `Bundle` extras are
  schema-less, and Android's own URI permission grants cannot ride them
  — boundary mediation fails exactly where typing stops, an independent
  kill-reason for O3.
- **Apple App Intents** — the closest modern analog: `@Parameter`-typed
  intents over a registered entity vocabulary (`AppEntity`), enumerable
  by system surfaces (Shortcuts, Spotlight); validates SD3's declared,
  introspectable launch surface.
- **Fuchsia component manifests (CML)** — `use`/`offer`/`expose`
  capability routing declared in build-compiled manifests, statically
  walkable; the whole-OS version of manifest-declared reach.
- **Web Intents** (deprecated 2012) — the recorded failure: its
  post-mortem names over-broad, developer-extensible intent types as a
  root cause, supporting SD2's bounded append-only vocabulary.
- **D-Bus service activation** — a host starting the target to deliver a
  typed request; the same bootstrapping resolution as SD1.
- **x-callback-url / desktop URL schemes** — the schema-less contrast
  case O3 declines to become.
