---
type: adr
status: accepted
date: 2026-11-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-22
---

# ADR-0391: What a tab link may decide

## Context

A browser tab ([ADR-0077](./0077-keelson-browser-wasm-execution.md),
[ADR-0278](./0278-tab-mode-for-downstream-apps.md)) is configured entirely by
its page link: `?worker=worker.mjs?…` names the app, the module environment
(`env=NAME=value`), the two wasm modules (`host=`, `module=`), the fonts and
the cadence. A link is something anyone can write and send. Before this
decision the worker fetched the modules and fonts from whatever URL the link
named and copied every `env=` entry into the module's environment, including
`CLICKHOUSE_URL`, `BOXER_PLAY_ALLOW_WRITES`, `BOXER_PLAY_SQL` and
`BOXER_PLAY_AUTORUN`. Where a bundle is served beside a same-origin `/ch/`
proxy, a crafted link could therefore run chosen SQL with writes enabled under
the visitor's ClickHouse authority, point the tab at another server, or load
foreign code into the deploying origin.

The published demo depends on links: the README's and the demo page's links
pass `CLICKHOUSE_URL=http://keelson.invalid/query` (the in-process endpoint),
seed SQL with autorun, focus and layout variables, a theme, and an applet
document path. Those links must keep working unchanged.

## Decision

A link decides **what to show and how**, never **what may be written, with
which credentials, which server is reached, or where code comes from**.

- **Code is same-origin.** The worker resolves `host=`, `module=` and every
  `<slot>Font=` against its own URL and refuses one on another origin: a
  module named elsewhere stops the tab with the reason in the worker's log; a
  font named elsewhere is refused with a log line and the bundle's default is
  used. The page served by `imzero2tab serve` carries a Content-Security-Policy
  with `worker-src 'self'`, so the worker itself cannot come from a `data:`
  URL either.
- **The environment is an allowlist, decided in Go.** The tab module, before
  anything reads its environment, keeps only the names on one reviewed list in
  `tabhost` (`linkEnvNames`, `linkEnvPrefixes`) plus the names a tab binary
  adds through `Options.LinkEnv`, and unsets the rest with a logged reason.
  The list holds display, theme, layout, focus and seed variables, an applet
  document path (which ADR-0299 confines to the page's origin), a user name,
  and the trial knobs the committed measurement scripts pass. It holds no
  write switch, no credential, no endpoint.
- **`CLICKHOUSE_URL` is kept only when it cannot reach a server other than the
  page's:** its host is under the reserved `.invalid` domain, or it has the
  page's origin, where a deployment's `/ch/` proxy sits. It never carries
  userinfo.
- **`BOXER_PLAY_AUTORUN` is kept only when the seeded `BOXER_PLAY_SQL`, if
  any, classifies `read`** (`analysis.ClassifyQuerySecurity`, the same
  classification play applies). A link may show a query's result; it does not
  run a write or an egress on the visitor's authority.
- **`BOXER_TAB_BASE` is the worker's alone.** The worker drops a link's value
  and sets the page directory itself, since the endpoint rule reads the
  page's origin from it.
- **`imzero2tab serve` sends a policy.** Every response carries
  `X-Content-Type-Options: nosniff` and a Content-Security-Policy; the page's
  names its inline blocks by digest, allows `'wasm-unsafe-eval'` for the two
  modules, and sets `frame-ancestors 'self'`.

## Alternatives

- **Decide the allowlist in the worker (JavaScript).** Killed: the rule needs
  the SQL classifier, which exists only in Go, and the repository has no
  JavaScript test lane; the worker is kept to what only it can enforce (where
  bytes are fetched from).
- **Derive the allowlist from a registry attribute.** Killed for now: no
  `env.Spec` field says "a link may set this", and adding one is a registry
  change for one consumer. A reviewed list in one place is auditable and the
  test pins every published link against it.
- **Drop `env=` and `autorun` from links altogether.** Killed: the published
  demo is built from them.
- **Refuse `CLICKHOUSE_URL` from links entirely.** Killed: the demo's
  in-process endpoint is named that way, and a same-origin proxy path is the
  deployment shape ADR-0077 SD9 describes.

## Consequences

### Positive

- A crafted link can no longer enable writes, swap the endpoint, name a
  credential, or load code from elsewhere.
- The rule lives in Go beside a test that decodes every link in the README and
  the demo page and asserts nothing in them is refused.

### Negative

- A downstream tab whose links set its own seed variables must list them in
  `Options.LinkEnv`; until it does, they are dropped with a log line.
- `arg=` still passes module flags through; the flags a tab binary carries are
  its app id, the client binary (unused under the reactor) and logging.
- The policy is sent by `imzero2tab serve` only. A static host (GitHub Pages)
  sets its own headers, so the page and worker enforcement are what hold
  there.

### Neutral

- A variable read during package initialisation, before the reactor's setup
  runs, would see the unfiltered value. None of the listed seed or endpoint
  variables is read there.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `tabhost`'s link-environment tests, including
  the one over the README's and the demo page's links, and `browserhost`'s
  header test.
- **What would fail.** A link variable added to a published page but not to
  the list; an endpoint or autorun rule that admits a write or a foreign host.
- **Gap.** The worker's same-origin checks are JavaScript with no test lane;
  they are short and read in review.

## Choices for review

- The list's members, notably `CLICKHOUSE_USER` (a name, not a secret),
  `BOXER_LOG_LEVEL`, the map and window-size variables, and the audio-lane
  trial knobs.
- Autorun with no seeded SQL is kept (it runs the app's own default).
- `connect-src` and `img-src` allow `https:` so an app that fetches map tiles
  keeps working under `imzero2tab serve`; `frame-ancestors 'self'` rather than
  `'none'`.
- A cross-origin font falls back to the default rather than stopping the tab.

## Status

Accepted 2026-11-22.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0077 — keelson browser-wasm execution](./0077-keelson-browser-wasm-execution.md) — SD9, the same-origin data plane.
- [ADR-0278 — tab mode for downstream apps](./0278-tab-mode-for-downstream-apps.md) — the tab binary and `imzero2tab serve`.
- [ADR-0299 — SQL applets a tab loads from its origin](./0299-sql-applets-a-tab-loads-from-its-origin.md) — the applet document rule this list relies on.
- [ADR-0132 — SQL applets](./0132-sqlapplet-sql-defined-applets.md) — §SD5, the security classes.
