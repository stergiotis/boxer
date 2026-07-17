---
type: adr
status: accepted
date: 2026-06-20
reviewed-by: "@spx"
reviewed-date: 2026-06-20
---

# ADR-0090: System metrics as a unidirectional pub/sub data plane

## Context

`imztop` ([ADR-0020](./0020-imzero2-imztop-resource-monitor.md)) samples the host via the `sysmetrics` collectors ([ADR-0019](./0019-observability-sysmetrics-linux-collector.md)), which read `/proc` and `/sys` directly. Its `Sampler` fuses two roles in one struct: *producing* snapshots (`bundle.Sample`) and *consuming* them (sliding-window history, per-process EWMA, MiB/s derivation, the published `PublishedSnapshot`). Three forces force a change:

- **imztop is dead on arrival under the hardened headless deployment.** The ADR-0085 sandbox drop-in (`showcase/onbox/20-hardening.conf`) sets `ProcSubset=pid` (hides every non-PID `/proc` file — `cpuinfo`, `stat`, `meminfo`, `loadavg`, …) and `ProtectProc=invisible`. `cpu.New()` reads `/proc/cpuinfo` at *construction*, gets ENOENT, and `NewSampler` treats CPU as mandatory — so the app aborts before its first frame. (Under Docker it doesn't crash but misleads: host-wide cgroup-unaware aggregates beside a container-only process table.)
- **`/proc` is the one capability class with no broker.** Every other resource routes through a trust boundary (`fsbroker`, `clipboardbroker`, `persist`, `inprocbus`; [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD7). `sysmetrics` has none: imztop declares zero `Caps` yet reaches `CAPABILITY_FILES` + `CAPABILITY_READ_SYSTEM_STATE` directly. ADR-0026 §SD10 already flags this as a `// nolint:cap-bypass` IOU — imztop is the bill.
- **boxer is data-centric.** A monitor that scrapes `/proc` privately inside one GUI process is the least data-centric shape possible: the metrics exist only as pixels.

NATS is not yet in the repo (ADR-0026 reserves it for phase M4); `inprocbus.Client` already does subject-filtered `Publish`/`Subscribe` with an audit sink; the `sysmetrics.Domain` enum and `BundleSnapshot.Errors map[Domain]error` already exist.

## Design space (QOC)

**Question.** How should imztop obtain host metrics headless so that (a) the carrier sandbox stays maximal, (b) metrics become reusable data, and (c) each component holds the smallest capability surface?

**Options.** **O1** in-process scrape behind a capability ("scoped reader handle"); **O2** relax the carrier sandbox so the GUI can read `/proc`; **O3 (chosen)** a standalone scraper service publishes `sysmetrics.*` one-way and the GUI subscribes with no fs/system-state cap (NATS core in prod, `inprocbus` co-located); **O4** deployment-only read-only host-`/proc` bind-mount, no bus.

**Criteria.** C1 carrier sandbox tightness; C2 data-centricity; C3 capability hygiene; C4 transport uniformity (local == prod); C5 multi-host/consumer fan-out; C6 cost & new deps.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | −− | ++ | +  |
| C2 | −  | −  | ++ | −− |
| C3 | +  | −− | ++ | −  |
| C4 | +  | +  | ++ | −  |
| C5 | −− | −− | ++ | −  |
| C6 | +  | ++ | −  | +  |

Only O3 turns "read `/proc`" from a GUI syscall into *data on a bus* — emptying the GUI's capability surface (C3), keeping the carrier locked (C1), and making the metrics reusable by other consumers and other boxes (C2/C5). It loses only on cost (C6: a new service + NATS core). O1 keeps `/proc` inside the GUI (sandbox must loosen; metrics never become data); O2 abandons the sandbox the exposed carrier exists for; O4 unbreaks one shape with no local analogue and no data-centricity.

## Decision

Model system metrics as a **unidirectional publish/subscribe data plane**:

1. **A standalone scraper service is the sole `/proc` reader**, publishing per-domain snapshots to `sysmetrics.{host}.{domain}`. It is the relocated trust boundary for `READ_SYSTEM_STATE` + `FILES` — like `fsbroker`, but a separately-deployable *publisher*.
2. **Strictly one way: scraper → bus → consumers.** No backchannel, no request/reply on the plane. imztop is a pure **subscriber** with no fs/system-state capability.
3. **One dataflow, two transports behind `BusI`.** Canonical: separate process → **NATS core** (no JetStream/KV) → sandboxed carrier. Special case (co-located, e.g. desktop): the identical pub/sub over `inprocbus`. Subjects and payloads are unchanged across the swap.

This extends ADR-0026 — adds `sysmetrics.*` to the §SD3 taxonomy, exercises the §SD4/§SD5 NATS↔inprocbus swap for the first cross-process consumer, relocates the §SD7 boundary, resolves the §SD10 bypass — and is the concrete first driver for its phase M4.

## Subsidiary design decisions

### SD1 — Subjects, identity, availability

`sysmetrics.{host}.{domain}`; `{domain}` reuses the `sysmetrics.Domain` tokens verbatim (`cpu mem disk net proc sensors battery gpu psi container`). `{host}` is a configured node id (default: sanitised hostname) so one plane carries many boxes — `sysmetrics.demo-box-1.>` or `sysmetrics.*.cpu` — and the monitored host is an explicit token, not "whoever's `/proc` we opened"; the UI labels which box it shows. Per-domain `BundleSnapshot.Errors` rides the wire so a consumer can distinguish **denied-by-policy** / **unsupported-on-host** / **collector-failed** / **withheld-by-config** and render *"process table withheld"* rather than an empty panel. `proc` is its own subject because it is a distinct *data kind* (a column-major process table), not a security boundary — sensitivity is a membership tag (SD8); subject-level grants are the coarse control (grant `cpu` without `proc`).

### SD2 — The scraper is the relocated trust boundary

A single-purpose service (working name `sysmetricsd`, under `public/keelson/runtime/`) owns the collectors and a publish loop — `fsbroker`'s `Service` shape inverted to *originate* messages. It is the only component holding `CAPABILITY_FILES` + `CAPABILITY_READ_SYSTEM_STATE`, justified by its `sysmetrics.*` publish declaration (SD6); every consumer's metric capability collapses to a subscription. Collectors already accept an injectable root (`Options.Proc = procfs.New(root)`), so one binary reads the real `/proc`, a bind-mounted host `/proc`, or a test fixture.

### SD3 — Metrics are leeway facts; reuse the facts pipeline

A metric sample *is* a leeway fact, modelled with the `runtime.facts` machinery (`factsschema` + `marshall`/`marshallgen`/`mappingplan`, a `<Kind>Columns` SoA generated from one `TableDesc`). Per domain an SoA columns type (`CPUColumns`, `ProcColumns`, …) fills a batch per tick — a process table is column-major by nature, the snug fit ADR-0089 identified. We **reuse the existing facts bus codec, not a bespoke one**: ADR-0089 settled that the SoA is the unification point and the wire is not, so metrics inherit the path verbatim (encode `dml_cbor` + `arrowrowcbor`; decode `cborarrow` + `ra`). The scraper publishes **raw counters**; rates, history, and EWMA are consumer-side views (SD5). *Schema sub-decision:* reuse the `runtime.facts` generic schema (a metric sample = a fact with domain memberships on the typed sections — zero new codegen) is recommended over a parallel `sysmetrics` schema; either is an in-memory/wire shape, not a stored table (SD4).

### SD4 — Transports, and no persistence

Production transport is **NATS core** — pub/sub only, **no JetStream/KV** — boxer's first NATS client dependency (external server per ADR-0026 §SD4; not embedded or supervised). Co-located runs keep `inprocbus.Client` (already subject-authz'd + audit-sinked); with no reply subjects the two are interchangeable behind `BusI`. **v1 persists nowhere** — not NATS, not ClickHouse. This is possible because facts are not CH-bound today: the bus codec above imports no ClickHouse (it builds Arrow records → CBOR bytes), and `FactsStoreI` ships an `InMemoryFactsStore` with `chstore` as merely one backend. Because the payload is already facts-shaped, persistence stays a free future add — a `chstore` subscriber could tee the same bytes into a table (ADR-0089's CH projection) without touching producer or consumer — but it is deliberately not built; no CH instance enters the metric path.

### SD5 — imztop bisects at the snapshot boundary

The `Sampler` splits along the seam it already has: the **producer half** (`bundle.Sample`) moves to the scraper; the **consumer half** (windows, EWMA, MiB/s, the published `PublishedSnapshot`) stays in imztop, fed by a subscription instead of a direct `Sample()`. The co-located case reconnects them through `inprocbus` — the same path prod runs over NATS. Strict unidirectionality costs imztop its "set interval" backchannel: the scraper owns cadence; a consumer downsamples locally. A control plane, if ever needed, is a separate bidirectional subject family, not a weakening of this one.

### SD6 — Capabilities resolve the §SD10 bypass

The scraper declares `sysmetrics.>` (`CapDirectionPub`); capslock's `READ_SYSTEM_STATE → sysmetrics.*` / `FILES → fs.*` mappings now have a real declarant. imztop declares the domains it consumes (`sysmetrics.*.cpu`, … `CapDirectionSub`) and no `fs.*`/system-state cap — capslock reports it clean. `Publish`/`Subscribe` is not audited per-message (only request/reply is; a 1 Hz stream must not emit a record per sample); access control is coarse subject-level grants. Attribute-level masking is deferred (SD8).

### SD7 — Deployment topology: three scoped units

- **scraper** — narrow sandbox: read `/proc` (+ `/sys` where needed) and connect to the bus; no `/dev`, no write, no exec. Small and single-purpose, so a tight profile is easy.
- **GUI carrier** (`imzero2-demo`) — keeps the **full** ADR-0085 sandbox (`ProcSubset=pid`, `ProtectProc=invisible`, `PrivateDevices=true`); it no longer needs `/proc`, so nothing has to give. This is the payoff: the exposed component stays maximally confined.
- **NATS core** — pub/sub only, localhost-bound (carrier + scraper are co-resident).

### SD8 — Sensitivity is a leeway membership; masking deferred

v1 exposes **every** attribute, including `argv`/usernames. Sensitive attributes carry a `sysmetrics.sensitive` membership (the mechanism that already discriminates `runtime.kind.*`/`runtime.app.*`); the tag travels with the data, so a later **"untrusted" switch** masks tagged attributes at one policy point rather than via scattered redaction. Until then the assumption is the single-tenant, localhost-bound bus (SD7); exposing `argv` there is an accepted, documented gap. Subject-level granularity (SD1) is the coarse interim control.

### SD9 — Phasing

Independently shippable: **P1** this ADR; **P2** bisect `Sampler`, pub/sub over `inprocbus` (no new dep); **P3** scraper as its own process + NATS core (first NATS dep; ADR-0026 M4); **P4** capability declarations + capslock flip (`READ_SYSTEM_STATE` bypass → declared); **P5 (optional)** a `chstore` persistence tee, only if history is ever wanted.

## Alternatives

- **O1 — in-process scoped reader handle.** Keeps `/proc` in the GUI (sandbox loosens) and metrics never become data; a handle suits a *pull* resource, but metrics are a *push* stream.
- **O2 — relax the carrier sandbox.** Abandons the sandbox on the network-exposed component to benefit one app.
- **O4 — host-`/proc` bind-mount.** Unbreaks one shape with no local analogue, no data-centricity; leaves `/proc` an un-brokered bypass. (May survive as a scraper-side implementation detail.)
- **JetStream/KV for the plane.** Live metrics aren't replayed; durability is a future tee's concern, not the broker's.
- **Per-message audit.** Auditing a 1 Hz broadcast is noise; the meaningful audited event is a sensitive-subscription grant.
- **Derived rates on the wire.** Rates are a view; raw counters keep the fact minimal and let each consumer pick its window.
- **Bidirectional control plane.** Breaks strict unidirectionality; revisit as a separate subject family if a real need appears.
- **A bespoke metric codec (CBOR-of-structs, protobuf, …).** A second serialization plus a persistence mapping step; modelling as facts reuses the existing pipeline (SD3).
- **Masking at v1 / a per-subject sensitivity split.** Masking is deferred to a membership-keyed switch (SD8); `proc` is split by data-kind, not as a security boundary — the boundary protects attributes, not whole subjects.

## Consequences

**Positive.** The carrier keeps the full ADR-0085 sandbox and needs no `/proc`; imztop works headless by consuming data. imztop's metric capability drops to a subscription; the sole `/proc` reader is small and separately confined. Metrics are first-class data with no bespoke serialization — the `<Domain>Columns` SoA *is* the bus payload, `play`/ClickHouse-queryable the moment persistence is wanted, with nothing stored in v1; fan-out to other consumers/boxes is free. The §SD10 bypass is resolved; imztop lands ADR-0026 M4 against a real need.

**Negative.** New moving parts: NATS core + the scraper (first NATS dep). Unidirectionality removes cadence control (interval → local downsample). Cross-process adds serialise/transport latency (imperceptible at ≤1 Hz, but real). The `sysmetrics.*` taxonomy and `<Domain>Columns` schema become public-stability surfaces. v1 puts `argv`/usernames on the bus — safe only under the single-tenant localhost assumption (SD7) until the masking switch (SD8) lands.

**Neutral.** The scraper is reusable (its output contract now serves more than one reader). Two deployment shapes (NATS / inprocbus) share code but both must stay tested (the §SD5 parity ADR-0026 already commits to).

## Status

Accepted on 2026-06-20 by @spx. Implementation begins at P2 (SD9).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. Post-acceptance edits follow [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) tiers (Tier 2 dated `## Updates`).

## Updates

### 2026-06-21 — P2→M4 implemented; headless metric plane via a NATS→in-proc bridge

P2–P4 and the headless deployment shipped. `sysmetricsbus` (Producer/Consumer/Codec/`StartScraper`) and `sysmetrics.DefaultBundleOptions` (shared collector wiring, GPU included) are in; `imztop` is a pure `MountCtx.Bus()` consumer (declares a `sysmetrics.>` Sub cap, no collectors/producer — SD6 for the production App, modulo the screenshot-tour harness which still scrapes in-package for live capture); the carousel host runs the scraper co-located; a standalone `sysmetricsd` CLI command publishes over NATS. SD4's NATS↔inprocbus swap is realised at the host via `app.BusProvider` (`inprocbus.Inst` and `natsbus.Provider`) threaded through `windowhost.SetBus`.

The headless-sandboxed case (the original problem) is solved **not** by SD4's "all capabilities over NATS" but by a narrower `sysmetricsbus.Bridge`: an external `sysmetricsd` reads `/proc` in its own sandbox and publishes to NATS; the carrier connects to NATS and republishes only the metric plane onto its in-proc host bus, so the carrier never reads `/proc` while the UI-coupled fs/clipboard/persist brokers stay co-located on inprocbus (they remain hardwired to `inprocbus.Inst`). Full SD4 — migrating every broker off `inprocbus.Inst` — is left as a larger future milestone; the `BusProvider` foundation is in place for it.

### 2026-06-21 — Review follow-up: observed cadence; SD6 correction; data/collector decoupling scheduled

Self-review fixes (commit `42bfa7c8`): the per-process CPU EWMA and the topbar/heatmap now use the *observed* sample cadence (the delta between consecutive bundle timestamps) rather than a configured interval — imztop no longer sets the rate, the scraper does — so the now-dead `SetInterval` and the topbar ± control are removed. `windowhost` copies the manifest caps before minting a client.

Correction to the SD6 note in the entry above: imztop is **not** capability-clean even setting the screenshot tour aside — `imztop_panel_topology.go` reads the CPU topology from sysfs directly (`cpu.ReadTopology`) in a production panel. Full SD6 needs either publishing CPU topology on the metric plane or accepting that read as a documented exception.

Scheduled focused follow-up: extract the per-domain data types plus `BundleSnapshot`/`Domain` into a data-only package so consumers (`sysmetricsbus`, `imztop`) stop importing the collector packages, and resolve the topology fork above. The bus-data extraction alone decouples 9/10 collectors; `cpu` stays coupled via topology until the fork is decided.

### 2026-06-21 — Data/collector decoupling done; topology fork resolved (publish on the plane)

The scheduled follow-up landed. The per-domain snapshot types, `BundleSnapshot`, and `Domain` moved into a new data-only package, `observability/sysmetrics/sysmsnap` (stdlib-only, no `/proc`/`/sys` code); each collector imports it and returns its types, and `sysmetrics` keeps only the orchestrating `Bundle`/`BundleOptions`/`DefaultBundleOptions`. `sysmetricsbus` is now collector-free too: the Producer takes a small `BundleSampler` interface instead of a concrete `*sysmetrics.Bundle`, and the collector-wiring `StartScraper` moved to a new scraper-side package, `keelson/runtime/sysmscrape` — the one place that imports both the collectors and the bus. A subscriber importing `sysmetricsbus` for its Consumer now pulls in no `/proc` reader.

The topology fork is resolved by **publishing CPU topology on the plane** (the consistent, data-centric option). The scraper's `Bundle` reads the static topology once (best-effort, when the CPU domain is wired) and stamps the same `*sysmsnap.Topology` onto every `BundleSnapshot`; the consumer builds its treemap from `PublishedSnapshot.Topology`. The recursive tree survives the CBOR codec, so a late subscriber still receives it and the production panel reads no sysfs.

Net effect: the production `imztop` App imports zero collector packages and reads neither `/proc` nor `/sys` — both §SD6 production gaps (the panel's sysfs read and the data-type coupling) are closed. The last in-package reach was the screenshot-tour harness (`imztop_tour.go`, ADR-0057 Demo enrollment), which used to start an in-proc scraper for live capture; it now feeds the consumer a synthetic, live-looking `BundleSampler` through the same `sysmetricsbus.Producer` instead, so `go list -deps ./apps/imztop` shows **zero** collector packages — the whole package, not just the production App, is collector-free. (The `BundleSampler` seam introduced for the decoupling is what makes the synthetic feed a drop-in.)

### 2026-07-18 — ADR-0126: the `sockets` domain, proc identity fields, per-domain cadence

[ADR-0126](./0126-appliance-topology-as-data.md) extends the plane with
observed topology. The SD1 domain token list gains `sockets` (the
listening-socket table, pid-attributed via the fd socket-inode walk);
the `proc` domain's rows gain two additive identity fields, `Component`
(the supervisor-injected `BOXER_COMPONENT` mark from the exec-frozen
environ, read once per process instance) and `CgroupUnit` (the owning
systemd unit per `/proc/[pid]/cgroup`). Both reads stay inside the SD2
capability envelope. Cadence remains scraper-owned per SD5, and is now
per-domain: the sockets collector samples on its own slower default
(15 s) internally and serves its cached snapshot between due times, so
the bundle tick loop is unchanged. Marked processes are exempt from the
proc collector's `MaxProcs` cap so an idle appliance daemon stays
visible. The SD8 sensitivity posture covers the new fields unchanged.

## References

- [ADR-0019](./0019-observability-sysmetrics-linux-collector.md) sysmetrics collector · [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) imztop · [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) remote access · [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) auth/TLS
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects (§SD3 taxonomy, §SD4/§SD5 transports, §SD7 boundary, §SD10 capslock)
- [ADR-0085](./0085-imzero2-demo-pull-build-atomic-deploy.md) — the sandbox drop-in that breaks in-process scraping
- [ADR-0042](./0042-keelson-leeway-codec-soa-generator.md) + [ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md) — `<Kind>Columns` SoA + sparse-CBOR bus codec; "SoA is the unification point"
- Code: `keelson/runtime/{inprocbus,factsschema,factsstore,vocab}`, `semistructured/leeway/{marshall,mappingplan}`, `keelson/security/capslock/check.go`
