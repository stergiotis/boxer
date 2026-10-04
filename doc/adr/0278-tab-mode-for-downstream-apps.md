---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0278: Tab mode as a library feature — `tabhost`, embedded assets, a content-addressed browser host

## Context

[ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md)
runs a keelson app in a browser tab: a wasip1 Go module and the Rust browser
host share one Web Worker, and the viewer page paints the mesh. It works for
boxer's own apps, but only boxer can build it. A repository that consumes boxer
as a library and wants its apps in a tab has to copy, and then keep in step:

- the tab binary, `cmd/imzero2tab` — a `package main` whose app list is boxer's
  and whose body (transport swap, mount, reactor hand-off, `serve`) is the part
  every tab needs;
- `scripts/dev/build_tab_bundle.sh`, which addresses boxer's own tree for the Go
  package, the Rust crate, the page and the worker;
- a Rust toolchain with the `wasm32-unknown-unknown` target, to build the
  browser host from `rust/imzero2`;
- the page, worker and bridge files, which the script copies off disk.

Two of these are not merely inconvenient. The Rust host must match the Go
module's FFFI2 opcode table exactly: when a fetcher was added to the IDL the
table shifted, and a bundle whose Rust half predated the change drew the wrong
widgets without any error. And the build flags a shipped binary depends on
already live in two files so that no two scripts can disagree
([ADR-0215](./0215-retire-mimalloc-reproducible-builds.md)); a copied bundle
script would be a third.

The native host had the same problem and solved it as a library:
[ADR-0211](./0211-hostboot-runtime-bootstrap.md)'s `hostboot` boots the runtime
for any registry, and [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md)
keeps the files a consumer must copy reconciled. This ADR applies the same
treatment to the tab.

## Design space (QOC)

**Question.** How does a consumer obtain the Rust browser host that matches the
boxer version it pins?

**Options.**

- **O1** — **Commit the `.wasm` and embed it.** The artifact (about 8 MB, 2.6 MB
  compressed) lives in the tree and reaches the consumer with the module pin.
- **O2** — **Build from the module's source.** The consumer's build runs cargo
  on the `rust/imzero2` sources the module download already contains.
- **O3** — **Content-addressed artifact, digest in the tree.** The tree carries
  the SHA-256 of the reproducible build of the browser host; CI publishes the
  artifact under that digest; the bundler fetches it by digest and checks it,
  and builds from source (O2) when it cannot. (chosen)

**Criteria.**

- **C1** — No Rust toolchain needed by a consumer.
- **C2** — Repository size and history stay unaffected by IDL churn.
- **C3** — Works offline once fetched, and on a consumer that pins a commit
  rather than a tag.
- **C4** — A mismatch between the Go and Rust halves cannot pass silently.

| | C1 | C2 | C3 | C4 |
|---|---|---|---|---|
| O1 | + | − (8 MB per IDL or Rust change) | + | + |
| O2 | − | + | + | + |
| O3 | + (fallback to O2 needs it) | + | ± (first fetch needs network; cached by digest after) | + |

O3 trades one network fetch for keeping binaries out of history. It depends on
the browser host building byte-identically on CI and on a developer machine,
which ADR-0215 set out to make true; the verification plan below is what keeps
it true. Keying by digest rather than by tag is what makes C3 hold for a
commit pin: boxer is consumed by pseudo-version far more often than by release.

## Decision

### SD1 — `tabhost`: the tab binary's body as a library

A package `browserhost/tabhost` holds what `cmd/imzero2tab` does today: the
urfave/cli app with logging and version wiring, the host transport install,
`browserhost.Mount` of the selected app, the reactor hand-off under wasip1, the
native pipe loop, and the `serve` and `bundle` subcommands (SD4). A tab binary
is a `package main` of blank imports — the apps it may open — a package-level
`tabhost.New(options, cliApp)`, and a `main` that calls the result's `Main`.
boxer's `cmd/imzero2tab` is such a binary.

The cli.App is the binary's own: its name and version are the binary's, and
the entry-point standard (urfave/cli, `logging.Apply`, `vcs.BuildVersionInfo`)
is checked in the main package, so that is where they stay; `New` adds the
flags, the action and the subcommands. `New` runs at package initialisation
because a c-shared wasip1 module runs inits but never `main`; it registers the
reactor's setup there (`browserhost.SetMain`), so a consumer writes no
build-tagged file.

### SD2 — Tab services are opt-in, as in `hostboot`

The options carry a `Services` struct of booleans with the same meaning as
ADR-0211 §SD2: the zero value is the bus and the mounted app, nothing else, and
each true field adds one in-tab service. The struct starts empty of fields;
services are added to it one per later decision (persist on browser storage,
the LLM service behind a `serve`-side proxy that holds the key), so every
consumer gains each one by setting a field and none re-implements it.

### SD3 — The page, worker and bridge are embedded

`browserhost/web` embeds the viewer page, `worker.mjs` and `bridge.mjs` with
`go:embed`. `serve` and `bundle` take them from the binary, so a page can no
longer be paired with a worker from another boxer version. `serve` prefers a
file of the same name in the bundle directory, which keeps a measurement
harness that brings its own page working; a bundle written by `bundle` carries
none, so the embedded ones are what it serves.

The viewer page stays where it is: it is the Rust carrier's page too, compiled
in with `include_str!`. `go:embed` reads only files beside the embedding
package, so a one-file Go package sits next to the page and embeds it. Moving
the page into the Go tree instead would have pointed the carrier's
`include_str!` across trees and changed its path in ten documents.

### SD4 — `bundle` replaces the shell script

`tabhost`'s `bundle` subcommand builds a servable directory from any module:

- it builds the named package of the calling module for wasip1 as a c-shared
  reactor, with the flags `scripts/dev/go-build-env.sh` sets, held in Go so the
  shell file and the command read one definition (the shell file is reduced to
  printing them, or kept as the single source the Go side parses; either way,
  one place);
- it runs `wasm-opt` when installed, as the script does;
- it obtains the browser host per SD5;
- it writes the embedded assets (SD3) and the fonts, from a flag naming a
  directory or from the existing font resolver.

`scripts/dev/build_tab_bundle.sh` becomes one invocation of it.

### SD5 — The browser host is content-addressed

The tree carries a small text file beside `browserhost` with the SHA-256 of the
browser host built from the same tree by `build_rust_browser.sh` under
`rust-repro-env.sh`. A CI workflow builds it on every change to its inputs and
publishes it under its digest when not yet published. `bundle` resolves the
host in this order: a path given on the command line; a local cache keyed by
digest; a fetch by digest from the published location; a build from the
module's own `rust/imzero2` sources when cargo and the target are present. Every
path but the first is checked against the digest, and a mismatch is an error.

### SD6 — The two halves prove they match

Both modules expose a hash of the opcode table the IDL generator emits, the Go
module through a reactor export and the Rust host through its C ABI. The
worker compares them before the first frame and refuses to start on a
mismatch, naming both. This holds even for a bundle assembled by hand.

### SD7 — A consumer can see what its apps get in a tab

Two checks, both runnable by a consumer through `gov`:

- **compile gate** — every app package a tab binary links builds for wasip1;
- **tab report** — for each linked app, the bus subjects its manifest declares
  that the tab's configured services do not serve. The report is information,
  not a failure: an app may degrade by design.

### SD8 — Adoption is documented, not generated

`doc/howto/adopting-boxer-downstream.md` gains a tab section: the
`package main`, the `bundle` and `serve` commands, and the services. `gov
skeleton` emits nothing new: the entry point is code the consumer owns, and the
commands come with the module pin.

### Milestones

- **M1 — `tabhost` and embedded assets.** ✓ SD1, SD3; `cmd/imzero2tab` on them;
  no behaviour change.
- **M2 — `bundle` and the opcode handshake.** SD4 with a source build of the
  host, SD6; the shell script reduced to a call.
- **M3 — The content-addressed host.** SD5: digest file, CI workflow, fetch and
  cache.
- **M4 — Gates.** SD7.
- **M5 — Adoption text.** SD8, verified against a consumer repository.

## Surfaces — Tier 1

- `public/thestack/imzero2/browserhost` — gains `tabhost` and the embedded
  `web` assets; `Serve` reads assets from the embed.
- `public/thestack/cmd/imzero2tab` — becomes a blank-import binary over
  `tabhost`.
- `rust/imzero2/src/imzero2/viewer` — gains a one-file Go package embedding the
  viewer page (SD3).
- The FFFI2 IDL generator — emits the opcode-table hash on both sides (SD6).
- `rust/imzero2/browser` — exports the hash.
- `scripts/dev/build_tab_bundle.sh`, `scripts/dev/go-build-env.sh` — reduced
  per SD4.
- `.github/workflows` — a workflow that builds and publishes the browser host
  (SD5).
- `gov` — the compile gate and the tab report (SD7).

## Alternatives

- **O1, O2** — see the QOC above.
- **A `boxer tab` subcommand instead of commands carried by the consumer's
  binary.** Rejected: the consumer would need boxer's CLI built from the same
  pin to bundle its own module, and its native `serve` would come from a
  different binary than its tab. Carrying the commands in `tabhost` gives every
  tab binary its own bundler and server.
- **A bundle per app group shipped by boxer** (play, the demos, the editors).
  Rejected as the mechanism: it serves boxer's apps, not a consumer's. It
  stays possible on top of SD1 for boxer's own deployment.
- **Generate the consumer's `main` with `gov skeleton`.** Rejected: three lines
  that name the consumer's apps are not a reconciliation problem.

## Consequences

### Positive

- A consumer's tab is a `package main` and two commands; the flags, assets and
  browser host follow its boxer pin.
- A stale or mixed bundle fails at start (SD6) instead of drawing wrong widgets.
- Services added to the tab later reach every consumer through one struct.

### Negative

- SD5 adds a CI workflow and a published artifact store, and its premise —
  byte-identical builds of the browser host across machines — becomes a
  maintained property rather than a property observed once.
- The first bundle on a machine without cargo needs network access.
- Consumers who link many apps get large modules (play alone is about a third
  of the current 104 MB); this ADR does not address module size.

### Neutral

- Tab mode remains ADR-0263's demo and per-user tier; nothing here changes its
  isolation posture.

## Migration — Tier 1

Nothing outside boxer depends on `cmd/imzero2tab` or the bundle script, so
nothing breaks. boxer's own bundle directories are rebuilt once with `bundle`;
their layout is unchanged.

## Verification plan — Tier 1

- **Handshake** — a test in the browser host's native lane and one in the Go
  reactor's assert both sides compute the same hash for the generated table;
  the worker's refusal is exercised by the existing trial harness with a
  deliberately mismatched pair.
- **Reproducibility** — the SD5 workflow rebuilds the host and fails when the
  digest it computes differs from the committed one.
- **Consumer path** — an integration test builds a minimal tab binary from a
  temporary module that imports `tabhost` and one app, bundles it with a source
  build of the host, and serves it.

## Deferred

- The in-tab services themselves (persist, the LLM service and its `/llm/`
  proxy, adhocdata): each is its own decision added to SD2's struct.
- More than one app per tab, and the window host in the tab.
- Module size: splitting or trimming what a tab links.

## Status

Proposed 2026-10-04. Awaiting owner review.

## References

- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — the tab this makes reusable.
- [ADR-0211](./0211-hostboot-runtime-bootstrap.md) — the native host as a library; SD2 mirrors its services struct.
- [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md) — the consumer adoption path.
- [ADR-0215](./0215-retire-mimalloc-reproducible-builds.md) — reproducible builds, the premise of SD5.
- [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) — the egress rules the tab's direct fetches are an exception to.
- [imzero2-in-the-browser](../howto/imzero2-in-the-browser.md) — the how-to that SD4 and SD8 rewrite.
