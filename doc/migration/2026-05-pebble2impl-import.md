---
type: explanation
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-28
---

# Migration: `pebble2impl` → `boxer`

Tracking doc for importing the curated `pebble2impl_oss` snapshot into boxer.

## Scope

**Full migration**, including ImZero **v2** (`imzero2` / `fffi2`), `keelson`, the
demo `apps/`, and the Rust `imzero2_egui` renderer. ImZero **v1** was previously
extracted out of boxer (into `imzero_imgui`) specifically to make room for v2's
return — so the README's "imzero/fffi extracted" note refers to v1, and v2 is
in-scope here.

- **Commit strategy:** fresh import (the snapshot has no `.git`), clean
  per-package commits in boxer's conventional style
  (`feat(<scope>): …`, `refactor(<scope>): …`, `chore(<scope>): …`).
- **Source:** `../pebble2impl_oss`, module `github.com/stergiotis/pebble2impl`
  (~887 Go files / ~220k LOC under `src/go/`, plus `apps/`, `src/rust/`, `doc/`).
- **`email/` was deleted** from the snapshot → out of scope (former P4).

## P1–P3 status (2026-05-28, branch `import/pebble2impl-p1-p3`)

Landed (9 fresh-import commits, no AI-attribution trailer):
- `parsing/antlr4utils` — antlr→cbor converter + s-expr serializer.
- `science/geo/swisstopo`; `hmi/progressbar`.
- `identity` — vlq, identcontainer, fibonacci(code). identcontainer set-ops
  fixed during import (pointer params; DifferenceCardinality returned the
  intersection count — corrected, per the previously non-compiling test).
- `observability/sysmetrics` — Linux collectors (+ gpu_intel/nvml/rocm opt-in).
- `storage/blob/chunked`.
- `semistructured/leeway` — anchor, card, dql, schema, marshallreflect_test,
  + additive cbor mapping.
- `chore(deps)` — `go mod tidy` promotions (golang-lru, go-isatty, purego,
  colormap, umap-go, gonum, x/sys, x/term).

Full module builds with `-tags="$(cat ./tags)"`; all migrated packages test
green; no regression to boxer's existing leeway suite.

Deferred out of P1–P3: `external/tools` (missing `codegen/fluidcli`),
`identity/identgen` (needs `cbor/kvh` + `storage/redis`), `db` additions and all
`*/cli` subcommands (→ P9 entry-point consolidation), and the leeway `cli` merge
(card/id/ir collide with boxer's canonical ddl/dml → P9). The
`ident_tag_impl_fib` alternative belongs in the `identifier` package.

## Layout mapping

| Source | Boxer destination |
|---|---|
| `src/go/public/X` | `public/X` (drop `src/go/`) |
| `src/go/public/swisstopo` | `public/science/geo/swisstopo` |
| `src/go/public/boxerstaging/storage` | `public/storage` |
| `src/go/public/boxerstaging/leeway` | `public/semistructured/leeway` (merge) |
| `src/go/app/commands/*` | `public/app` subcommands (`NewCommand()` → `cli2.CommandsNilRemoved(...)`) |
| `src/go/cmd/<x>` | `public/app` subcommand **or** `public/gov/<x>` (no ad-hoc `main()`) |
| `src/rust/imzero2_egui` | `rust/imzero2_egui` (joins `rust/h3bridge`) |
| `doc/adr-p/*` | `doc/adr/*` (renumber from 0011+, dedup against existing) |
| `doc/{skills,design-system,…}` | `doc/*` (merge) |
| `licenses/`, `doc/legal/` | `THIRD_PARTY_NOTICES.md` + `NOTICE` |

## Mechanical transforms (per package)

1. **Import rewrite** (apply specific moves before the general rule):
   - `…/pebble2impl/src/go/public/swisstopo` → `…/boxer/public/science/geo/swisstopo`
   - `…/pebble2impl/src/go/public/boxerstaging/storage` → `…/boxer/public/storage`
   - `…/pebble2impl/src/go/public/boxerstaging/leeway` → `…/boxer/public/semistructured/leeway`
   - `…/pebble2impl/src/go/public/` → `…/boxer/public/`
2. **File naming** → `LowerSnakeCase` (boxer `gov filename` lint; ADR-0048).
   Leave `.out.go` / `.gen.go` generated files as-is.
3. **Build tags** preserved (`llm_generated_opus47/46`, `gemini3pro`; already in
   `./tags`). The README AI-codegen declaration must cover the ~600 tagged files.
4. **go.mod**: add the 65 new modules + `go mod tidy -tags="$(cat ./tags)"`.

## Standards reconciliation (must resolve before the relevant phase)

- `urfave/cli/v3` → **v2** (boxer standard; `public/app` is v2).
- `go.uber.org/zap` / `zerozap` → **zerolog**.
- `gofrs/uuid` / `google/uuid` → **`matoous/go-nanoid`** where it's ID generation.
- `os.Getenv` → **`public/config/env`** registry (lint-enforced, ADR-0009).

## Phase sequence (dependency-ordered)

Each phase: copy → rewrite imports → snake_case rename → `go build`/`go test`
with `-tags="$(cat ./tags)"` → `gov` lints → commit.

### P1 — overlap reconciliation  *(done)*
- **`parsing/antlr4utils`** — additive: `antlr_tree_to_cbor.go`, `parse_tree_sexpr.go`.
- **SKIP** `compiletimeflags`, `semistructured/cbor` — boxer is ahead (boxer already
  adopted snake_case filenames per source's own ADR-0048).
- **SKIP** `db` core — boxer far ahead on `clickhouse/dsl`. `db/{badger,clickhouse}/cli`
  → P9; `clickhouse/{clickhouseenv,dsl/funccharacterize}` → deferred additive (API
  divergence risk against boxer's reworked dsl).

### P2 — clean net-new leaves  *(done)*
- **`swisstopo`** → `public/science/geo/swisstopo`.
- **`hmi/progressbar`** → `public/hmi/progressbar`.
- **`identity`**: `fibonacci`, `fibonaccicode`, `vlq`, `identcontainer`.
  **DEFER** `identgen` (+`seq`, `internalized`) — `fromconfig.go` needs `cbor/kvh`
  (not in boxer) + `storage/redis` (not in snapshot).
- **`observability/sysmetrics`** → `public/observability/sysmetrics` (self-contained;
  `//go:build linux`; `gpu/{intel,nvml,rocm}` may need vendor-lib build tags).
- **DROP `external/tools`** — dangling dep on `codegen/fluidcli` (absent from snapshot).

### P3 — boxerstaging  *(done)*
- **`storage/blob/chunked`** → `public/storage/blob/chunked` (leaf).
- **`boxerstaging/leeway`** → `public/semistructured/leeway`:
  net-new `anchor`, `card`, `dql`, `schema`; additive merges into existing
  `cli`, `mapping`, `marshallreflect_test`. Depends on `identity/fibonacci` (P2),
  boxer's `leeway`, and `storage/blob` (P3).

### P4 — email  *(dropped: source `email/` deleted)*

### P5 — fffi2  *(done)*
Framed-FFI `ir` (+`idl`), `runtime`, `typed`, `compiletime`
(`docgen`/`goclient`/`rustclient`) + `utfsafe`, under `public/thestack/`
(ADR-0049). Pulled `keelson/runtime/widgethandle` (a stdlib-only leaf) in early
as a prerequisite for `fffi2/typed`. No new go.mod deps (purego/xxh3/x-exp
already present; gostackparse not used here).

**Deferred to P6:** `compiletime/goserver` (imports `imzero2/egui2/widgets/color`,
which depends on `fffi2` — lands with imzero2). Fixed two mis-gated `ir`
extrachecks tests to honour `compiletimeflags.ExtraChecks` (skip under default
tags; pass under `-tags=…,extrachecks`).

### P6 — imzero2 foundation + rust  *(foundation done; widgets/apps → P7)*
Landed the **keelson-free egui2 foundation**: `bindings`, `definition`, `driver`,
`widgets/color`, `metrics`, `imzero2env`, `application` (under `public/thestack/imzero2`)
— and re-added the deferred `fffi2/compiletime/goserver` (color now present). New
deps: `golangci/gofmt`, `valyala/fasttemplate` (codegen). Fixed a flaky `metrics`
timing test. Rust renderer workspace imported to **`rust/imzero2`** (unverified
build; root crate still `pebble2_rust`; 5 unembedded IDSMono variants + build
artifacts excluded; build scripts reference the old `../go` layout — reconcile later).

**Deferred to P7:** imzero2 `egui2/widgets/*` and `egui2/demo/apps/*` — they reach
~29 keelson packages (`designsystem/styletokens`, `runtime/app`, `runtime/icons`,
`data/chlocal*`, …), and keelson depends back on `egui2/bindings`, so the
imzero2↔keelson cycle forces them to migrate **together**. `egui2gen`/`iconsgen`
cmd wiring → P9.

### P7 — keelson + imzero2 widgets/apps  *(done; capslock/carousel → P8)*
Ported the mutually-coupled cluster in one pass: keelson
`runtime`/`data`/`designsystem`/`vdd` + imzero2 `egui2/widgets/*` and
`egui2/demo/apps/*` (~540 files). All build+test green under default tags; whole
module also builds under `binary_log`. New direct deps: `BurntSushi/toml`,
`dustin/go-humanize`, `hishamk/statetrooper`.

Fixes surfaced by the port:
- `styletokens` drift test repointed to `rust/imzero2` (layout change).
- `logbridge`/`logdemo` CBOR-sink tests gated on `binary_log` (the bridge decodes
  CBOR zerolog output, only emitted under that tag — matches boxer's
  `observability/logging`; they pass under `-tags=…,binary_log`). **Resolved:** a
  dedicated `gotest (binary_log)` CI job + `scripts/ci/gotest_binary_log.sh` run
  these CBOR-logging packages. `binary_log` is deliberately **kept out of `./tags`**
  — adding it to the default flips zerolog to CBOR globally and breaks
  `observability/eh`'s `MarshalZerologObject` tests, which `json.Unmarshal` zerolog
  output and only hold under JSON. So the default lane stays JSON; the new lane is
  scoped to the CBOR-logging packages (eh excluded).
- `pickerbridge` HOME tests use `env.Home.SetForTest` (memoized registry, ADR-0009).

**Deferred to P8** (depend on top-level `apps/`): `keelson/security/capslock`
(blank-imports `apps/imztop`,`apps/play`) and the imzero2 demo `carousel`
(composes `apps/capinspector`). `keelsoncodec`/`runtimecodegen` cmd wiring → P9.

### P8 — apps  *(done)*
`play`, `imztop`, `capdemo`, `capinspector`, `taskdemo` → `boxer/apps/` (top-level,
sibling to `public/`). No `main()` — they register into keelson's `AppI` registry
via `init()` and are launched through a host, so they're packages (AppId = boxer
package path), not commands. Landed `db/clickhouse/clickhouseenv` as a prerequisite
(apps/play needs it; clean single-file env-registry pkg, no dsl coupling — the P1
deferral was overcautious). **Re-added** the P6/P7-deferred app-coupled packages
now that apps exist: `keelson/security/capslock` (blank-imports `apps/imztop`,
`apps/play`) and the imzero2 demo `carousel` (composes `apps/capinspector`). No new
deps. Whole module: 218 pkgs green under default tags. ADRs 0008/0020.

> **MIGRATION COMPLETE — merged to `main` + pushed 2026-05-29** (34 commits,
> `3fb2094..6ecffc5`). P9 leftovers below are now all done.

### P9 — entry-point consolidation  *(done)*
No cli v2/v3 conflict after all — the source `app/commands/*` are cli **v2** (the
go.mod v3 was the dropped external-tools). Migrated the command tree to
`public/app/commands/` and wired **12** subcommands into `public/app/main.go`
(`capslock`, `codedriven`, `compression`, `datasource`, `designsystem`,
`findAnchor`, `http`, `key`, `runtimecodegen`, `sample`, `swisstopo`, `watch`).
219 pkgs green. (`compression` briefly pulled `fatih/camelcase`; since `naming`
validates/normalizes identifiers and can't replace a raw case-boundary tokenizer
that also counts `_`/`-`/`.`, it was inlined as `splitCamelCase` and the dep dropped.)

- **Dropped as dups** (boxer wires from home pkgs): `cbor`, `leeway`,
  `observability`, `dev`, `env`(=`envgen`), `gov`.
- **Dropped — absent/unmigrated deps:** `adversarialreview` + `clarityrate`
  (import the `cmd/adversarial-review` tree, absent from the snapshot — never
  built), `config`/`app/config` (pull absent `identgen/internalized` + `cbor/kvh`),
  `encryptedHash` (absent `krypto`), `spinnaker` (unmigrated `spinnaker` pkg).
- **Deferred:** the leeway `cli` merge (`card`/`id`/`ir` collide with boxer's
  `ddl`/`dml`); `db/*/cli`; the `cmd/*` standalone tools (boxer forbids ad-hoc
  `main()` — `designsystem`/`runtimecodegen`/`capslock` functionality is already
  covered by the wired subcommands; `envgen`→`env`, `designlint`→`gov/codelint`
  are dups; `iconsgen`/`keelsoncodec` are codegen tools whose outputs are checked
  in — re-home as app subcommands if needed).

### P10 — docs / compliance / publish gate  *(docs done; gate-run + rust-rename remain)*
**Done:**
- **Skills** — 9 ported (`leeway-*`, `nanopass-sql`, `canonicaltypes`, `imzero2*`,
  `fffi2`, `rerunmapping`).
- **ADRs** — 47 ported into `doc/adr/`. Numbering reconciled: source `0012-0049`
  keep their numbers (no collision with boxer `0001-0011`); source `0001-0011`
  renumbered to `0050-0060`; content-dups dropped (`0015` kafka → boxer `0005`,
  `0017` membership → boxer `0007`). Cross-refs + the 54 `ADR-00xx` citations in
  migrated code remapped; boxer-core ADRs/citations untouched.
- **THIRD_PARTY_NOTICES** — §1.6 Grafana TimePicker (Apache-2.0, pre-AGPL pin),
  §1.7 fatih/camelcase inline splitter (MIT), §2.2 the three `include_bytes!`
  fonts (IDS Mono + Phosphor licensed in-tree).
- **README** — new subsystems in "What's inside"; corrected the stale
  "imzero/fffi extracted" line (ImZero v2 is back in-module).

**Remaining:**
- **License gate run** — `cyclonedx-gomod` isn't installed locally; CI runs it.
  Policy analysis: the 14 new direct deps are permissive or reciprocal
  (`golang-lru` = MPL-2.0 = *reciprocal* = allowed; gate blocks only
  forbidden+restricted), so no expected violation.
- ~~Iosevka Aile OFL text~~ — **done**: OFL-1.1 vendored next to the `.ttf`
  (upstream Renzhi Li / Belleve Invis copyright); THIRD_PARTY_NOTICES §2.2 links it.
- ~~Rust build-script reconcile~~ — **done**: imzero2 host migrated
  (`public/thestack/cmd/imzero2`), `build_go.sh` appends `binary_log`, and
  `hmi.sh` builds again (Rust `cargo build` + Go `main_go` both verified; the
  `imzero2 demo` command resolves with hmi.sh's client/font flags — actual GUI
  launch needs a display). Optional `pebble2_rust`→`imzero2` crate rename stays
  deferred (hmi.sh references `pebble2_rust`).
- **P9 leftovers** — leeway `cli` merge (card/id/ir vs ddl/dml), `db/*/cli`,
  `cmd/*` standalone tools.

## Known deferrals / blockers discovered

| Item | Reason | Resolution |
|---|---|---|
| `external/tools` | imports `codegen/fluidcli`, absent from snapshot | obtain `codegen/fluidcli` (markbates-based) or drop the feature |
| `identity/identgen` | needs `cbor/kvh` + `storage/redis` | land after `cbor/kvh` (additive) + `storage/redis` are migrated |
| `db/clickhouse/dsl/funccharacterize` | boxer dsl reworked; API divergence | reconcile against boxer's `dsl` (P9/P10) |
| ~~`db/clickhouse/clickhouseenv`~~ | ~~deferred P1~~ | **resolved** — landed in P8 (no dsl coupling) |
| cli v2 vs v3, zap vs zerolog | boxer-standard conflicts | port down (decision pending) |
