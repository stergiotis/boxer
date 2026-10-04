---
type: adr
status: accepted
date: 2026-10-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-04
---

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

- it builds a main package of the calling module — by default the binary's own,
  read from its build information — for wasip1 as a c-shared reactor;
- the build flags are the ones `scripts/dev/go-build-env.sh` sets for every
  shipped binary. The shell file stays their source for shell builds, the Go
  side holds them as values, and a test fails when the two differ; the tags
  and the toolchain pin are read from the module being built, not from boxer;
- it runs `wasm-opt` when installed, as the script did;
- it obtains the browser host per SD5;
- it writes the fonts, from a flag naming a directory or from boxer's font
  resolver, run from boxer's module directory;
- it writes the page, worker and shim only when asked, for a bundle served by
  something other than the tab binary; `serve` has them embedded (SD3).

`scripts/dev/build_tab_bundle.sh` becomes one invocation of it, asking for the
assets so that its output stays servable by any static host.

### SD5 — The browser host is content-addressed

`tabhost/browserhost.sum` records the SHA-256 of the browser host that
`build_rust_browser.sh` builds from the same tree under the pinned toolchain
and `rust-repro-env.sh`, and the IDL fingerprint (SD6) it was generated for.
`tabhost` embeds the file. A test fails when the bindings' fingerprint differs
from the recorded one, which catches the common way the file goes stale — a
regeneration — without building Rust; `hostdigest --write` refreshes it.

The `tab-host` workflow builds the host on every push to `main` that touches
its inputs, fails when the digest differs from the recorded one, and publishes
the file on GitHub Pages as `tabhost/<sha256>.wasm` with `tabhost/index.txt`
listing every published digest. Nothing published is removed, since a consumer
pinned to an older commit needs an older digest: each deployment is assembled
from everything the live site lists plus the new file, and a run that cannot
read back the whole of it fails instead of deploying a smaller site. When this was
built, one commit gave the same digest from two checkouts on one machine and
from the workflow's runner; the workflow's first run failed on a digest
recorded before a Rust change from another session landed, which is the check
working as meant, and AGENTS.md now carries the refresh rule.

`bundle` obtains the host per `--hostFrom`: `auto`, the default, builds from
source when the module being bundled is boxer itself — a developer changing the
host wants that build, not the published one — and otherwise fetches it by the
recorded digest, from a cache keyed by digest or from `BOXER_TAB_HOST_URL`
(the Pages site unless set; a mirror for an offline build), building from
source when the fetch fails; `fetch` and `source` force one way. A fetched host
that does not match the digest is refused. A host built from source that
differs from it is used and reported: the tree may be ahead of its recorded
digest, and SD6 still refuses a host for another IDL. `--host` names a file to
use as given.

### SD6 — The two halves prove they match

The FFFI2 code generator digests the IR it generates from — the top-level nodes
in opcode order, each factory's methods in method-id order, and the names and
types of everything that crosses the wire — into a 64-bit fingerprint
(`ir.Fingerprint`), and writes it into both halves' generated enums. The Go
module exports it from the reactor and the Rust host from its C ABI. The
worker compares the two before `setup` and refuses to start on a mismatch,
naming both values; a pair in which only one module has the export is a mixed
pair and refused too, while two modules that both predate it are let through.
This holds for a bundle assembled by hand.

A fingerprint rather than a hash of the generated files: the two halves'
generated code differs by language, and a reordering or a changed reply shape
must change the value while a change to a code template, which alters what a
call does but not what crosses, need not.

### SD7 — A consumer can see what its apps get in a tab

Two checks, both run by the gate's `tab` step for each package named with
`--tab-pkg` (a repository without a tab binary gets a skip):

- **compile gate** — the tab binary builds for wasip1 as `bundle` builds it,
  which compiles exactly the apps it links, those it reaches transitively
  included; a failure names the packages the compiler reported, most often a
  dependency of one app rather than the app;
- **tab report** — for each linked app, the bus subjects its manifest declares
  that the tab's configured services do not serve, with the manifest's reason.
  Apps register at initialisation, so only the binary knows what it links: it
  carries a `tabreport` subcommand and the step runs it. A subject the tab
  makes unnecessary rather than serves — the basemap fetching its own tiles —
  is listed as answered in the tab. The report is information, not a failure:
  an app may degrade by design.

Adding the step changes the gate's published step list (ADR-0179); it skips
unless configured, so a consumer without a tab sees one more `skip` line.

### SD8 — Adoption is documented, not generated

`doc/howto/adopting-boxer-downstream.md` gains a tab section: the
`package main`, the `bundle` and `serve` commands, and the services. `gov
skeleton` emits nothing new: the entry point is code the consumer owns, and the
commands come with the module pin.

### Milestones

- **M1 — `tabhost` and embedded assets.** ✓ SD1, SD3; `cmd/imzero2tab` on them;
  no behaviour change.
- **M2 — `bundle` and the opcode handshake.** ✓ SD4 with a source build of the
  host, SD6; the shell script reduced to a call.
- **M3 — The content-addressed host.** ✓ SD5: digest file, CI workflow, fetch
  and cache.
- **M4 — Gates.** ✓ SD7.
- **M5 — Adoption text.** ✓ SD8, verified against a consumer repository.

## Surfaces — Tier 1

- `public/thestack/imzero2/browserhost` — gains `tabhost` and the embedded
  `web` assets; `Serve` reads assets from the embed.
- `public/thestack/cmd/imzero2tab` — becomes a blank-import binary over
  `tabhost`.
- `rust/imzero2/src/imzero2/viewer` — gains a one-file Go package embedding the
  viewer page (SD3).
- The FFFI2 IR and both code generators — `ir.Fingerprint`, emitted as
  `IdlFingerprint` and `IDL_FINGERPRINT` into the generated enums (SD6).
- `rust/imzero2/browser` and the wasip1 reactor — export the fingerprint; the
  worker's shim compares them.
- `public/extbin` — declares `wasm-opt`; `bundle` runs go, bash and wasm-opt
  through it.
- `IMZERO2_BROWSER_TARGET_DIR` — a registered variable, read by `bundle` and by
  `build_rust_browser.sh`.
- `scripts/dev/build_tab_bundle.sh`, `scripts/dev/go-build-env.sh` — reduced
  per SD4.
- `.github/workflows` — a workflow that builds and publishes the browser host
  (SD5).
- `gov gate` — a `tab` step and its `--tab-pkg` option (SD7); boxer's own lint
  runs it on `imzero2tab`.

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

- **Handshake** — a bindings test reads the Rust client's generated
  `IDL_FINGERPRINT` and fails when it differs from the Go side's, which is the
  shape of regenerating only one side; an `ir` test pins that the fingerprint
  follows opcode order, method order and reply shapes. The worker's refusal of
  a mixed pair was observed in a browser when M2 was built.
- **Reproducibility** — the `tab-host` workflow rebuilds the host and fails
  when the digest it computes differs from the committed one; a tabhost test
  fails when the recorded IDL fingerprint is not the bindings'.
- **Consumer path** — an integration test builds a minimal tab binary from a
  temporary module that imports `tabhost` and one app, bundles it with a source
  build of the host, and serves it.

## Deferred

- The in-tab services themselves (persist, the LLM service and its `/llm/`
  proxy, adhocdata): each is its own decision added to SD2's struct.
- More than one app per tab, and the window host in the tab.
- Module size: splitting or trimming what a tab links.

## Status

Accepted 2026-10-04. M1–M5 were built the same day.
Two consumer shapes were exercised when M5 was: a repository consuming boxer
through a Go workspace, whose tab binary the gate refused for two apps (an
embedded key-value store that maps memory, an in-memory audio file) and passed
without them, and a fresh module pinning a pushed boxer commit, which bundled
from the module cache with the host fetched from GitHub Pages; an app of each
ran in a browser tab.

## References

- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — the tab this makes reusable.
- [ADR-0211](./0211-hostboot-runtime-bootstrap.md) — the native host as a library; SD2 mirrors its services struct.
- [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md) — the consumer adoption path.
- [ADR-0215](./0215-retire-mimalloc-reproducible-builds.md) — reproducible builds, the premise of SD5.
- [ADR-0262](./0262-http-egress-as-a-keelson-capability.md) — the egress rules the tab's direct fetches are an exception to.
- [imzero2-in-the-browser](../howto/imzero2-in-the-browser.md) — the how-to that SD4 and SD8 rewrite.
