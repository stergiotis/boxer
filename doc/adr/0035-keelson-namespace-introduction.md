---
type: adr
status: accepted
date: 2026-05-15
reviewed-by: "@spx"
reviewed-date: 2026-05-15
---

> **Status: accepted 2026-05-15 by @spx.** Implementation landed in 6 commits on `main` the same day; see *Implementation notes* below for deviations from the plan as recorded.

# ADR-0035: Keelson — namespace for the platform spine

## Context

`pebble2impl` has grown to host four distinct concerns that until now share a single namespace prefix (`src/go/public/thestack/`):

- A **Go monolith runtime** ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)): `AppI`/`Manifest`/`Registry` plus the in-proc bus and brokers (`capbroker`, `persist`, `windowhost`, `fsbroker`, `factsstore`, `logbridge`, `logviewer`, `audit`, `vocab`, `heartbeat`, `runinfo`, `factsschema`).
- A **data-centrality layer**: ClickHouse client + low-latency local-CH pool ([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) — `chclient`, `chlocalbroker`, `chlocalpool` — plus the leeway columnar protocol (currently staged in `boxerstaging/leeway/` pending upstream into boxer).
- A **security model**: the capslock-cross-checker at `src/go/cmd/capslock-check/` enforces ADR-0026 §SD10 (Manifest.Caps must match the static call graph) and is the visible surface of the cap-as-subject design.
- A **GUI framework + apps**: ImZero2 + FFFI2 (which will move to `../boxer` in a separate refactor) plus a growing set of `AppI` implementations (`imztop`, `capinspector`, `capdemo`, demo carousel apps, `boxerstaging/spinnaker/hmi/play`).

Three forces make the current naming untenable:

- **No visible boundary.** A new contributor cannot see in `ls src/go/public/thestack/` where the platform spine ends and the GUI tech / apps begin. The historical name `thestack` is a placeholder that conveys nothing; the repo name `pebble2impl` is even less specific (it dates back to a build-system experiment).
- **ImZero2 is leaving for boxer.** Once `thestack/imzero2/` and `thestack/fffi2/` upstream, the residue is a platform with no name. We want to settle the residue's identity *before* the move so the path migration there is a one-step rename rather than two.
- **Apps need a home outside the framework directory.** Today `AppI` implementations are intermixed with framework code under `thestack/`. There is no top-level `apps/` directory; even the three already-standalone apps (`imztop`, `capdemo`, `capinspector`) live under `thestack/`, where they are indistinguishable from imzero2 internals.

The user proposed the name **keelson** (nautical: the internal timber running along the keel, tying the floor frames to it). The metaphor captures the role precisely — a structural spine *inside* the hull, invisible to passengers (apps), distributing loads from above (apps) to below (data). The repo already uses nautical naming for adjacent concerns (`leeway` — sideways drift of a ship under wind/current — for the columnar protocol; `anchor` for the leeway anchor type).

## Design space (QOC)

**Question.** How do we give the platform spine a distinct, discoverable name without forcing a repo or Go module rename that ripples through every import statement and every downstream consumer?

**Options.**

- **O1 — Repo + module rename.** `pebble2impl → keelson` everywhere: repo name, `go.mod` path (`github.com/stergiotis/keelson`), import paths across all 392 affected files.
- **O2 — Subdirectory namespace (chosen).** Repo and `go.mod` stay `pebble2impl`. `src/go/public/keelson/` becomes a subdirectory that gathers platform packages under three pillars (data / runtime / security). Apps live at a sibling top-level `apps/<name>/`. ImZero2 / FFFI2 stay at `src/go/public/thestack/` until they upstream into boxer.
- **O3 — Branding and docs only.** The name appears in README, ADRs, package docs, and CLI banners. No code paths change.
- **O4 — Module sub-path.** Keep `github.com/stergiotis/pebble2impl` but introduce `github.com/stergiotis/pebble2impl/keelson` as a separate Go module (its own `go.mod`).

**Criteria.**

- **C1 — Discoverability.** Can a reader skimming `ls` see the platform/apps/GUI boundary?
- **C2 — Migration cost.** How many `.go` files change? How many import-path strings move? How much is purely mechanical sed vs. requiring judgment?
- **C3 — Reversibility.** If the name does not stick, how expensive is the rollback?
- **C4 — Future-fit.** Does the layout survive the (separate) imzero2 → boxer move without needing another reshuffle?
- **C5 — Module-graph hygiene.** Does the choice introduce dependency-direction headaches, `go.work` complexity, or cross-module surfaces that need to stay in sync?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 rename | O2 subdir | O3 branding | O4 sub-module |
|----|:--:|:--:|:--:|:--:|
| C1 | ++ | +  | −− | +  |
| C2 | −− | −  | ++ | −  |
| C3 | −− | +  | ++ | −  |
| C4 | +  | +  | −  | +  |
| C5 | ++ | ++ | ++ | −  |

## Decision

We introduce **`src/go/public/keelson/`** as a subdirectory namespace gathering the platform spine under three pillars:

- **`keelson/runtime/`** — the ADR-0026 app runtime and adjacent brokers. Every package previously under `thestack/runtime/` moves here: `app`, `audit`, `buscodec`, `capbroker`, `factrpc`, `factsschema` (+ `codegen/`, `dml/`), `factsstore` (+ `chstore/`), `fsbroker` (+ `pickerbridge/`), `heartbeat`, `inprocbus`, `logbridge`, `logviewer`, `persist`, `runinfo`, `vocab`, `windowhost`. Plus `widgethandle`, promoted out of `thestack/internal/` (see *Implementation notes*).
- **`keelson/data/`** — ClickHouse client glue lifted one level (formerly `thestack/runtime/{chclient,chlocalbroker,chlocalpool}`). The lift is intentional: these packages are consumed by both runtime and app code and read more clearly as a sibling pillar than as runtime internals.
- **`keelson/security/capslock/`** — the cap-cross-checker package (formerly `src/go/cmd/capslock-check/main.go`). The `src/go/cmd/capslock-check/main.go` shim is retained as the binary entrypoint, importing the new package.

Standalone apps move to a sibling top-level **`apps/<name>/`**: initially `imztop`, `capdemo`, `capinspector`. The carousel-embedded demo apps (`thestack/imzero2/egui2/demo/apps/*`) and `boxerstaging/spinnaker/hmi/play` stay at their current locations until imzero2 moves to boxer — disentangling them from the imzero2 host topology before that move is more churn than payoff.

ImZero2 and FFFI2 stay at `src/go/public/thestack/` for now. `boxerstaging/`, `src/go/public/storage/`, and `src/go/public/db/` stay put — the data pillar covers the *runtime-side* ClickHouse glue, not the pebble-CLI-side factories.

Repository name and Go module path stay `github.com/stergiotis/pebble2impl`. Keelson is a **directory boundary, not a module boundary**.

## Alternatives

- **O1 repo + module rename.** Rejected — 392 files (53% of the tree) change, downstream consumers (any boxer sibling or external clone) must rebuild, and the reversibility is poor. The discoverability win is also available under O2 with a fraction of the cost.
- **O3 branding and docs only.** Rejected — the name stays soft. New contributors will still see `thestack/` competing with `keelson` in the file tree. The historical-name carry forward is precisely the problem we want to address.
- **O4 module sub-path.** Rejected — multi-module repos add `go.work` complexity, introduce a versioning surface between the modules, and constrain refactors that span the boundary. The pillars are tightly coupled (the runtime imports the data pillar; the security pillar reads the runtime's registry); a single module is the right grain.

## Migration plan

The migration ships as 7 commits behind one ADR, executable autonomously. Build-tag rule: `tags=$(cat tags | tr -d $'\n')`; every `go build` invocation uses `-tags "$tags"`. After each step: `go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh`. Steps marked `(+test)` additionally run `bash scripts/ci/gotest.sh`. Steps marked `(+capslock)` additionally run `bash scripts/ci/capslock.sh`.

### Step 0 — Pre-flight baseline

`go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh && bash scripts/ci/gotest.sh`. Capture `go vet -tags "$tags" ./src/go/...` output for delta comparison. Confirm `go.work` includes `../boxer`. Do not proceed if the baseline is dirty.

### Step 1 — Skeleton and ADR (this commit + EXPLANATION.md)

- `mkdir -p src/go/public/keelson/{runtime,data,security/capslock} apps`.
- Seed `src/go/public/keelson/EXPLANATION.md` via `scripts/new-doc.sh explanation`; fill the three-pillars rationale (substantive content, not a placeholder — Step 7 then has no re-flesh to do).
- `doc.go` stubs are *not* placed at `keelson/`, `keelson/runtime/`, or `keelson/data/`. These are container directories with no `.go` files of their own (`thestack/runtime/` followed the same convention); a phantom `package runtime` declaration would create an empty importable package. `keelson/security/capslock/` is the only pillar that becomes a real Go package, and it gets its `doc.go` via the moved-package's existing comment in Step 2.
- This ADR itself (`doc/adr/0035-keelson-namespace-introduction.md`) is the first file of the step.
- Gate: `bash scripts/ci/doclint.sh src/go/public/keelson/ doc/adr/0035-keelson-namespace-introduction.md`.

### Step 2 — Security pillar: `capslock-check` → `keelson/security/capslock`

- `git mv src/go/cmd/capslock-check src/go/public/keelson/security/capslock`.
- Rename `main.go` → `check.go`; change `package main` → `package capslock`; expose entry as `Run(args []string) (exitCode int)`. Move tests with the package.
- Create a new `src/go/cmd/capslock-check/main.go` (~10 lines) that imports `keelson/security/capslock` and calls `capslock.Run(os.Args)`.
- Leave `trustBoundaryPackages` constants pointing at `thestack/runtime/...` for now; Step 3 sweeps them.
- Gate: `go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh && bash scripts/ci/capslock.sh`.

### Step 3 — Runtime pillar: `thestack/runtime` → `keelson/runtime` (+test, +capslock)

The largest single step: ~93 files reference the moving paths.

- `git mv src/go/public/thestack/runtime src/go/public/keelson/runtime`.
- Mechanical sweep (the sed itself self-edits these very commands when run; re-run is safe because it becomes a no-op):
  ```
  find src/go -name '*.go' -print0 | xargs -0 sed -i \
    -e 's|github.com/stergiotis/pebble2impl/src/go/public/thestack/runtime|github.com/stergiotis/pebble2impl/src/go/public/keelson/runtime|g'
  find doc scripts -type f \( -name '*.md' -o -name '*.sh' \) -print0 | xargs -0 sed -i \
    -e 's|github.com/stergiotis/pebble2impl/src/go/public/thestack/runtime|github.com/stergiotis/pebble2impl/src/go/public/keelson/runtime|g' \
    -e 's|src/go/public/thestack/runtime|src/go/public/keelson/runtime|g'
  ```
- `goimports -l ./src/go/...` must return empty.
- Regenerate `runtime/factsschema/dml/runtime_facts_dml.out.go`: `go run -tags "$tags" ./src/go/cmd/runtimecodegen`.
- Update `keelson/security/capslock/check.go` `trustBoundaryPackages` (formerly lines 117-121) to point at `keelson/runtime/{fsbroker,inprocbus,persist}` (the chclient/chlocalbroker/chlocalpool entries are updated in Step 4).
- Update `imzero2_demo_resolve.go` `legacyCodeToId` map entries and any `keelson/runtime/...` side-effect imports.
- Manifest.Id literals in app_register.go files are rewritten to match the new import paths (per ADR-0026's identity rule). Historical persisted state keyed by old AppIdT is orphaned; the runtime is pre-stable so no live operators are affected. Document this break in the **Consequences** section below.
- Amend [ADR-0020](./0020-imzero2-imztop-resource-monitor.md), [ADR-0026](./0026-app-runtime-and-capability-subjects.md), [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) with a single `## Updates` H3 per ADR pointing at ADR-0035; **do not** change `status` or `reviewed-date` (the decisions did not change, the paths reflect the new namespace).
- Gate: `go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh && bash scripts/ci/gotest.sh && bash scripts/ci/capslock.sh`.

### Step 4 — Data pillar: lift `chclient`, `chlocalbroker`, `chlocalpool` out of `runtime/`

- `git mv src/go/public/keelson/runtime/chclient src/go/public/keelson/data/chclient` (and `chlocalbroker`, `chlocalpool`). The destination `keelson/data/` must exist before the moves; if it has been recursively removed (e.g., by an earlier `rmdir`), recreate it first with `mkdir -p src/go/public/keelson/data`.
- Sweep (the sed itself self-edits the lines below; re-run is a no-op):
  ```
  find src/go doc scripts -type f \( -name '*.go' -o -name '*.md' -o -name '*.sh' \) -print0 | xargs -0 sed -i \
    -e 's|keelson/runtime/chclient|keelson/data/chclient|g' \
    -e 's|keelson/runtime/chlocalbroker|keelson/data/chlocalbroker|g' \
    -e 's|keelson/runtime/chlocalpool|keelson/data/chlocalpool|g'
  ```
- Update `keelson/security/capslock/check.go` `trustBoundaryPackages` for `chlocalbroker` and `chlocalpool`.
- Amend ADR-0028 path references.
- Gate: `go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh && bash scripts/ci/capslock.sh`.

### Step 5 — Apps: hoist `imztop`, `capdemo`, `capinspector` to `apps/` (+test, +capslock)

For each app:

- `git mv src/go/public/thestack/<app> apps/<app>`.
- Sweep:
  ```
  find src/go apps doc scripts -type f \( -name '*.go' -o -name '*.md' -o -name '*.sh' \) -print0 | xargs -0 sed -i \
    -e "s|github.com/stergiotis/pebble2impl/src/go/public/thestack/<app>|github.com/stergiotis/pebble2impl/apps/<app>|g" \
    -e "s|src/go/public/thestack/<app>|apps/<app>|g"
  ```
- Rewrite `Manifest.Id` literals and any `const ManifestId` definitions.
- Update `imzero2_demo_resolve.go` `legacyCodeToId` map entries and side-effect imports (formerly `_ "thestack/<app>"`).
- Update `scripts/ci/capslock.sh` `packages=(...)` list.
- Update `keelson/security/capslock/check.go` `widgetsPkgPath` constant and the side-effect-import block.
- Amend ADR-0020 (`imztop`-specific) and ADR-0026 path references.
- Gate: `go build -tags "$tags" ./src/go/... && bash scripts/ci/lint.sh && bash scripts/ci/gotest.sh && bash scripts/ci/capslock.sh`.
- Smoke test: `./thestack.sh imzero2 demo --launch imztop` runs to first paint without crashing.

### Step 6 — Regenerate generated headers (cosmetic)

`./generate.sh` and `./egui2gen.sh` refresh the generator-path comment headers in `.out.go` and `src/rust/src/imzero2/*.rs`. Bodies unaffected. Gate: build + lint clean (no semantic change expected).

### Step 7 — Docs

- Flesh out `src/go/public/keelson/EXPLANATION.md` with the three-pillars rationale, what moved and from where, what didn't and why.
- Update root `CLAUDE.md` to add a "keelson namespace" item under *pebble2impl-local supplement*: keelson is the platform namespace (data + runtime + security); `apps/` is the top-level home for standalone AppIs; `thestack/` continues to host imzero2/fffi2 pending the boxer-upstream move.
- After human review, flip ADR-0035 `status: proposed → accepted` and add `reviewed-by` / `reviewed-date`.

### Step 8 — Follow-up tickets (filed, not executed)

- Move imzero2 demo apps and `spinnaker/hmi/play` into `apps/` once imzero2 lands in `../boxer`.
- Upstream `boxerstaging/` content into boxer.
- Decide `adversarial-review` and the `pebble` CLI placement (currently `src/go/cmd/` and `src/go/app/` respectively — both out of keelson scope for this round).
- Rename packages with non-conforming symbols (interfaces missing `I` suffix, etc.) — explicitly **deferred** per CLAUDE.md "move not rewrite" rule.

## Implementation notes

Recorded retrospectively (2026-05-15) to capture deviations from the plan as written above. The plan was largely faithful; six points worth noting:

- **Widgethandle promotion (Step 3).** The plan did not anticipate that `keelson/runtime/windowhost` would import `thestack/internal/widgethandle`, blocked by Go's `internal/` mechanism after the runtime move. The package was promoted to `keelson/runtime/widgethandle/` (not flagged as a public-API decision elsewhere) and opacity is now enforced by an unexported `secret` + unexported `Resolve()` rather than by `internal/`. Cross-tree direction is `thestack → keelson`, matching the intended imzero2-to-boxer arrow.
- **Sed self-edit hazard.** Steps 3 and 4 both sweep `thestack/runtime → keelson/runtime` and `keelson/runtime/ch* → keelson/data/ch*` across all `.md` files including this ADR. The sed commands embedded in the migration plan above match their own arguments, so the documented `git mv` and `sed -i` examples got over-swept (both sides ended at the destination). Each step's commit restored them. Lesson recorded above as "the sed itself self-edits these very commands when run; re-run is safe because it becomes a no-op." Future plan-as-code documents inside this repo should expect the same hazard.
- **Doc.go stubs not created.** The plan called for `doc.go` stubs at `keelson/`, `keelson/runtime/`, `keelson/data/`. These are container directories with no `.go` files (matching the pre-migration `thestack/runtime/` shape); stubs would create empty importable packages with no purpose. Only `keelson/security/capslock/doc.go` exists, and that comes from the moved package's existing comment header. This is the intended end state.
- **Step 6 was a no-op.** No `.out.go` headers or `.rs` headers turned out to be stale post-sweep: the sed sweep already updated the import-path strings inside generator-emitted files (they are themselves `.go` source), and `src/rust/src/imzero2/*.rs` did not reference any of the moving paths. Running `./generate.sh` would have been a cosmetic no-op. Recorded so a future cosmetic-regen step in a similar ADR can short-circuit when no stale headers are found.
- **EXPLANATION.md done early.** The plan paired `EXPLANATION.md` flesh-out with Step 7. In practice the file was written substantively in Step 1 (the placeholder template otherwise blocks doclint on the same step's `mkdir -p` because empty container directories are fine but a draft EXPLANATION.md with placeholder links to non-existent packages is not). Step 7 ended up scoped to the `CLAUDE.md` update.
- **CLAUDE.md missed the path sweep.** It lives at repo root, not under `src/go/` or `doc/`; the sed `find` regex excluded it. Step 7 fixed the one stale `thestack/internal/widgethandle` reference manually and added the new keelson-namespace bullet. If a future refactor reorganises any path called out in `CLAUDE.md`, sweep the file explicitly.

Final commit chain on `main`: `be6377ac` (Step 1), `32ed685f` (Step 2), `b3ae8ced` (Step 3), `6dafb5e9` (Step 4), `4d39ceaa` (Step 5), `e975c633` (Step 7). Step 6 ships as a no-op subsumed by Step 3's sed. Final gate: build clean, `gotest.sh` exit 0 with zero FAILs, `capslock.sh` exit 0 with finding counts identical to baseline (8 ok / 16 missing-cap / 36 hard-fail / 6 investigate), `lint.sh` exit 1 due to pre-existing `vocab/vocab.go` DL009 doc-comment-phrasing warnings (same as baseline; not introduced by this refactor).

## Consequences

### Positive

- `ls src/go/public/keelson/` displays the platform spine in one view: `runtime/`, `data/`, `security/`. `ls apps/` displays the standalone apps. `ls src/go/public/thestack/` collapses to the GUI framework + carousel-coupled apps awaiting their own move.
- The three-pillar layout survives the imzero2 → boxer move without reshuffle. When imzero2 leaves, `thestack/` shrinks to its remaining residue without affecting keelson.
- `src/go/cmd/capslock-check/main.go` is now a 10-line shim, freeing the security package to grow library API (`Run`, `Check`, etc.) without coupling to `os.Args` / `os.Exit`.
- `Manifest.Id` literals match the new import paths — no AppId/import-path drift in the registry.
- Sed sweep is the right tool for a directory move with identical symbol names; `gopls rename` is *wrong* (it operates on symbols, not modules). Confirmed up front, no false starts.

### Negative

- Wire-incompatible AppId migration: facts persisted under old `Manifest.Id` strings (`github.com/stergiotis/pebble2impl/apps/imztop` etc.) are orphaned. The runtime is pre-stable; no live operators retain history; documented here so the choice is auditable. Mitigation (a legacy-alias table in `keelson/runtime/app/legacy.go`) is explicitly **not** taken — the indirection cost outweighs the value.
- ~93 files touched in Step 3; ~18 files touched in Step 5. The sweep is mechanical but the volume is real — every commit must pass the gate before the next begins.
- Three ADRs (0020, 0026, 0028) gain an entry under `## Updates`. Path-only sweeps would arguably qualify as Tier 1 (in-place edits) under boxer's three-tier policy, but recording them as a dated Tier 2 entry preserves the audit trail of the namespace cutover for the cross-ADR sweep.
- The carousel host (`thestack/imzero2/egui2/demo/carousel/`) ends up importing `keelson/runtime/...` from `thestack/`, creating a thestack → keelson cross-tree dependency. This is acceptable (and inevitable until imzero2 moves) but noted.

### Neutral

- `boxerstaging/`, `storage/`, `db/`, `observability/`, `identity/`, `krypto/`, and the various small utility packages stay at their current locations. Reclassifying them is out of scope and would be re-churned when boxerstaging upstreams.
- `logviewer` stays in `keelson/runtime/` (next to `logbridge.Sink`) rather than moving to `apps/`. It is both a runtime observability tool and an `AppI`; the runtime location reflects the tighter coupling.
- `utfsafe` (consumed only by `boxerstaging/spinnaker/hmi/play`) stays put.

## Status

Accepted 2026-05-15 by @spx. Implementation landed on `main` the same day (6 commits, `be6377ac..e975c633`).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop app (subject to Step 3 / Step 5 amendments).
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — app runtime; Manifest.Id identity rule; capslock §SD10 (subject to amendments).
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — chlocal data-pillar consumer (subject to amendments).
- [ADR-0036](./0036-runtime-buscodec.md) — buscodec; example of a path that *does not* move (lives at `keelson/runtime/buscodec/` post-Step-3).
- Memory `project_keelson_namespace` — naming decision (2026-05-15), three-pillar scope, deferral choices recorded out of conversation.
