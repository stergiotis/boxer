---
type: adr
status: proposed
date: 2026-07-14
---

> **Status: proposed — pre-human-review.** Package `public/extbin` built, all
> in-tree call sites migrated, and the CS012 enforcement rule added (2026-07-14)
> — see the Status section. Awaiting review before acceptance.

# ADR-0118: a single audited chokepoint for external-binary resolution

## Status

Proposed. Implemented ahead of acceptance so the decision is reviewed against
working code:

- `public/extbin` — the `Program` registry, `Kind`-based resolution, and the
  `Command`/`Output`/`CombinedOutput`/`Run` surface, with tests.
- Every `os/exec` **resolution** site in the tree routed through it (the one
  exemption is `observability/eh`, below `extbin` in the import graph — see
  Consequences). Process *spawning* deliberately stays with callers; §Scope
  below states exactly what that does and does not buy.
- `codelint` rule **CS012** bans direct `os/exec` outside `extbin`; it reports
  zero findings across `./public/...`. What that attests to is narrower than it
  looks — again, §Scope.
- A `keelson.extbin` introspection table (ADR-0094) exposes the registry: each
  program's name, kind, module, override_env, install_hint, its live resolution
  on this host (`resolved_path`, `available` via `Program.Resolve`), and a
  cached blake3 digest of the resolved binary. This turns "what external
  programs can this box run, where do they resolve, and is each the binary I
  expect" into a query. The digest is computed lazily (only when the column is
  projected on the in-process path) and cached per (path, size, mtime).

## Context

boxer shells out to a dozen external programs — `git`, `clickhouse-local`,
`tinygo`, `rustfmt`, `pijul`, `scc`, the `go` toolchain, profiler wrappers, and
built release artifacts — from ~35 call sites across a dozen subsystems. Each
site reinvented resolution (`exec.LookPath`, a hardcoded name, a config path)
and its own not-found error.

For a toolkit that ships airgapped and pins its own toolchain, *which host
binaries can this thing execute, and where does it find them* is a supply-chain
question. Scattered `exec.Command` calls make it a grep, not an answer. The
immediate trigger was narrower — `scctree` ran `go tool scc` with no fallback to
a PATH `scc` — but the fix generalises: a **pinned** source (`go tool`, version-
matched in `go.mod`) should be preferred over an **ambient** one (`$PATH`), and
that policy belongs in one place.

Two facts shaped the design, both established by inspection rather than
assumption:

- `go tool` lists module tools by **full module path** (`github.com/boyter/scc/v3`),
  so an upfront "is `go tool X` available" probe cannot cheaply match the short
  name `X`. A missing tool instead fails fast (`go: no such tool`) *without*
  doing the tool's work.
- Of the call sites, ~32 are simple capture (`Output`/`Run`), but four stream:
  the `chlocalpool` worker (stdin/stdout pipes, `SIGTERM`→`SIGKILL` reap), the
  imzero2 FFI client (long-lived pipe handshake, no kill), the `gov/repo` git
  log iterator (`StdoutPipe` + kill-on-early-break), and the deploy gate
  (`Setpgid` process group, `syscall.Kill(-pid)`). A wrapper that owned the
  process lifecycle would break all four.

## Decision

Introduce `public/extbin`: one package every external-process invocation flows
through.

**A declared registry.** Each program is a package-level `Program` value
(`extbin.Git`, `extbin.ClickHouseLocal`, `extbin.SCC`, …). `extbin.Registry()`
enumerates the lot — the machine-readable audit surface. A `Program` carries its
`Kind`, an optional `Module` (cross-references the SBOM for `go` tools), an
optional `OverrideEnv` (an absolute-path env override — the hook a future
hermetic mode can *require*), and an `InstallHint`.

**Resolution, not ownership.** `Program.Command(ctx, Opts, args…)` resolves the
executable and returns a configured `*exec.Cmd` the caller still drives — stdio,
`SysProcAttr`, signals, `Start`/`Wait`/`Kill`. This is what preserves the four
streaming sites unchanged: extbin answers *which binary*, the caller keeps the
lifecycle. `Output`/`CombinedOutput`/`Run` are conveniences over `Command` for
the capture majority.

**Four resolution kinds**, in priority order `Opts.Path` → `OverrideEnv` →
kind-specific:

- `Host` — PATH lookup (the ambient majority).
- `GoTool` — `go tool <Name>`, falling back to a PATH `<Name>` when the module
  tool is unavailable. The fallback is a *run-time* behaviour on the capture
  methods (the primary fails fast, so there is no redundant work); `Command`
  returns the pinned form. This subsumes the original scctree fix.
- `GoToolchain` — the `go` binary itself.
- `Local` — a caller-supplied path (`Opts.Path` required): built artifacts and
  configured clients, declared in their consuming packages so the role still
  shows up in the registry.

**Enforcement.** codelint CS012 (severity Error) flags
`exec.Command`/`CommandContext`/`LookPath` outside `extbin`. Referencing the
`*exec.Cmd` *type* is fine; only the resolving calls are banned.

## Scope: what is centralised, and what is not

The distinction between resolution and ownership above is a design choice with a
consequence worth stating plainly, because the phrase "chokepoint" invites a
stronger reading than the design supports. Added 2026-07-15, after a source
census of the call sites made the boundary measurable.

**Centralised: resolution.** *Which* binary runs, found *how* — PATH lookup vs
pinned `go tool` vs an `OverrideEnv` absolute path vs a caller-supplied `Local`
path — happens in exactly one package, for every external program in the tree.
`extbin.Registry()` enumerates the full set, and `keelson.extbin` makes it a
query. This is the property the ADR actually delivers, and CS012 does enforce it:
constructing an `exec.Cmd` requires importing `os/exec`, which CS012 denies.

**Not centralised: spawning.** `Program.Command` hands back a live `*exec.Cmd`
and the caller invokes it. **Eleven packages** outside `extbin` call `.Run()`,
`.Start()` or `.CombinedOutput()` on a Cmd they were handed:

```
public/algebraicarch/pushout/pijul      public/keelson/data/chlocalpool
public/app/commands/adr                 public/storage/recordstore/chexec
public/db/clickhouse/cli                public/thestack/imzero2/application
public/gov/callsites                    public/thestack/imzero2/egui2/demo/carousel
public/gov/commitdigest                 showcase/deploy
public/gov/repo
```

This is intended — it is what keeps the four streaming sites (pipes, process
groups, signals) expressible, and rejecting the lifecycle-owning wrapper was a
considered choice (see Alternatives). The point is only that "every external
process is spawned from one place" is **not** among this ADR's claims, and should
not be cited as though it were.

**CS012 cannot see the spawn.** Calling a method on a value you were handed needs
no import, so **eight of those eleven do not import `os/exec` at all**. The other
three import it solely for type references (`exec.Cmd`, `exec.ExitError`), which
the rule permits by design. CS012 therefore flags none of the eleven — correct
behaviour for what it checks, and precisely the point: its zero-findings result
attests that nothing *resolves* an external binary outside `extbin`, and nothing
more. Two further gaps follow from the same fact:

- The returned `*exec.Cmd` is mutable. A caller can reassign `cmd.Path` or
  `cmd.Args` after resolution and before `Start`, and nothing in the registry,
  the lint, or the `keelson.extbin` digest would notice. The audit surface is
  *what extbin resolved*, not *what actually ran*. No call site does this today;
  nothing prevents one.
- `keelson.extbin`'s blake3 digest attests the binary at the resolved path at
  query time — not the one a given process actually exec'd, which may have been
  a different file at a different moment (TOCTOU).

**Corroboration, and its limits.** capslock (ADR-0026 §SD10), run over the tree,
independently attributes direct `exec` to six of the eleven, plus `extbin` itself
and the exempt `eh`. It finds six rather than eleven because its call graph is
built by VTA and links only calls whose receiver it can resolve: `pijul`'s runner
is a method on an unexported type that nothing outside a test constructs, so the
concrete type never flows to the interface and its `cmd.Run()` is invisible to
the analysis. A capability verdict is therefore a **lower bound**, and the eleven
above come from the source census, not from capslock. Capability analysis is a
useful cross-check on this boundary; it is not the authority for it.

## Consequences

- One place to read, grep, or lint the host-binary surface; one uniform
  override + install-hint path; the pinned-over-ambient policy applied
  everywhere. All three are properties of *resolution* — see §Scope for what
  this does not cover.
- **`observability/eh` is exempt.** `extbin` imports `eh`, so `eh`'s own
  `go env GOROOT` call (stack-trace path shortening) cannot route through
  `extbin` without a cycle. CS012 exempts `eh` by prefix, as CS001 already does.
  `eh` sits below the chokepoint by construction.
- Test files are exempt: fixtures may shell out freely and are not shipped
  runtime.
- `showcase/deploy` is migrated too, though it sits outside CS012's default
  `./public/...` scope; its generic `run`/`step` helpers now take a `*Program`.

## Alternatives considered

- **A thin `Command(name, …)` constructor, no registry.** Centralises
  resolution but leaves the program set discoverable only by grepping call
  sites — it doesn't produce the single auditable list, which was the point.
- **An upfront availability probe to choose `go tool` vs PATH.** Rejected: the
  `go tool` listing is by full module path (fragile short-name matching), and a
  `--version` probe spawns an extra process. Try-then-fallback on the capture
  path is cheaper and simpler because the primary fails fast.
- **A wrapper that owns process lifecycle (`Run` only, no `*exec.Cmd`).**
  Rejected: it cannot express the four streaming sites' pipes, process groups,
  and signal handling without re-exposing all of `exec.Cmd` anyway.

## Deferred

- `OverrideEnv` is populated only for `clickhouse-local` (the one site with a
  real path-override need today); other programs can gain it without touching
  call sites.
- A hermetic/airgap mode that *requires* `OverrideEnv` (denies ambient PATH) is
  designed-for but not built.
- CS012 covers `os/exec` only; `syscall.Exec` / `os.StartProcess` are not in
  use and not yet gated.
- **Gating the spawn is not attempted** (§Scope). Making `extbin` a chokepoint
  for spawning too — rather than only for resolution — would need something CS012
  cannot express as an import rule: a check that `(*exec.Cmd).Start`/`Run` is
  only called on a Cmd obtained from `extbin` and unmutated since. An opaque
  handle type (`extbin.Cmd` wrapping `*exec.Cmd`, exposing the lifecycle
  explicitly) would make it structural instead, at the cost of re-exposing the
  streaming surface the Alternatives section rejected doing. Worth revisiting
  only if a hermetic mode needs to attest what ran, not just what resolved.
