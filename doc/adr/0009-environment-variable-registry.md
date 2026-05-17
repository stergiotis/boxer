---
type: adr
status: accepted
date: 2026-05-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-13
---

# ADR-0009: Environment variable registry under `public/config/env`

## Context

A survey of the boxer codebase finds **24 distinct environment variables read across 9 packages**, through **three different mechanisms**:

- **CLI flag framework** (urfave/cli/v2 `EnvVars`): 13 `BOXER_*` vars declared in per-package flag struct files (`public/observability/logging/flags.go`, `public/observability/tracing/flightrecorder.go`, `public/dev/debugger.go`, `public/docgen/docflags.go`).
- **Direct `os.Getenv` / `os.LookupEnv`**: 11 vars, including credentials (`GEMINI_API_KEY`, `GOOGLE_API_KEY`), system vars consumed for path shortening (`GOPATH`, `HOME`), and the ClickHouse test integration (`CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DATABASE`, `CLICKHOUSE_ENDPOINT`, lowercase `clickhouse` for the binary path).
- **`os.Environ` manipulation**: `GOWORK` in two test setups.

The fragmentation has produced concrete defects already visible in the survey:

- A lowercase env var name (`clickhouse` in `lw_ddl_clickhouse_testutils.go:11`) when every other declared name is uppercase.
- A naming typo: `BOXER_LOG_MODULE_INFO_IN_START` versus the four sibling `..._ON_START` flags.
- Inconsistent default-declaration patterns (`Value`, `DefaultText`, inline post-`Getenv` fallback, file fallback, test-skip).
- No discoverability surface: no `.env.example`, no markdown index, no runtime introspection. The only documentation is the inline `Usage` strings on `cli.Flag` declarations, which only cover the 13 `BOXER_*` vars.

Two additional pressures push beyond "tidy what's there":

- **Cross-project sharing.** Pebble2impl also reads `CLICKHOUSE_USER` / `CLICKHOUSE_PASSWORD` / `CLICKHOUSE_ENDPOINT`, plus its own `PEBBLE_*` family (`PEBBLE_CIPHER_KEY_HEX`, `PEBBLE_ALGORITHM`, `PEBBLE_N_ANCHOR_BYTES`, `PEBBLE_MAX_HAMMING_DIST_PER_BYTE_INCL`). A third consumer of boxer (not in this repo) also consumes `public/config` and would benefit from the same registry. Whatever boxer adopts has to work as a stable, shared API surface, not a boxer-internal helper.
- **Coexistence with the Configer pattern.** `public/config/config.go` defines `Configer` (`ToCliFlags(nameTransf, envVarNameTransf)`, `FromContext(...)`, `Validate(...)`) and `NameTransformFunc`. Configer is actively used in pebble2impl for flag-name composition (`imzero2/application/config.go` composes `ImZeroClientConfig` under a `clientPrefixNameTransf`). It is **not** used to declare env vars — across both projects, no Configer impl populates `EnvVars` on its `cli.Flag`. The `envVarNameTransf` parameter is held for the unnamed third consumer.

The question is how to introduce env-var declaration as a first-class concern without disturbing Configer, without forcing a rewrite of the `BOXER_*` flag struct files, and while extending naturally to downstream consumers.

## Design space (QOC)

**Question.** How should environment variable declarations in boxer (and consumers of `public/config`) be unified so that (a) every read is discoverable from a central registry, (b) defaults, types, descriptions, and sensitivity are declared in one place per variable, (c) the existing `Configer` flag-composition pattern is unaffected, and (d) downstream projects (pebble2impl and a third unnamed consumer) participate in the same registry without coordination?

**Options.**

- **O1** — Status quo: `os.Getenv` + `cli.Flag.EnvVars` literals + ad-hoc `os.LookupEnv`. No registry.
- **O2** — Manual markdown index in `doc/env-vars.md` plus a CI grep test that fails when undocumented `os.Getenv` calls appear. No code change to reads.
- **O3** — Struct-tag decoding (Kelsey Hightower `envconfig` / `koanf` style): each subsystem declares a struct with `env:"X"` and `default:"…"` tags; a library walks the struct and populates fields.
- **O4** — Multi-source config object (Viper-style): env vars are one layer behind a unified configuration object that also pulls from CLI, file, and remote sources.
- **O5** — Registry + typed declarative globals (Tailscale `envknob`, CockroachDB `pkg/util/envutil` lineage): each variable is declared once as a package-level `var X = env.NewString(env.Spec{...})`; the act of declaration registers it; reads happen through `X.Get()`; CLI flags are derived via `X.AsCliFlag()`. *(chosen)*
- **O6** — Registry + functional accessor (`env.String("X", "default", "desc")` at the read site, no named value): terser than O5 but provides no value to pass around.

**Criteria.**

- **C1 — Discoverability:** can the full set of env vars boxer (and linked consumers) read be enumerated at runtime and rendered as documentation?
- **C2 — Single read path:** do all env reads go through one typed API, so type parsing, defaults, and caching live in one place?
- **C3 — Cross-project participation:** does a downstream project (pebble2impl, third consumer) get its declarations into the same registry without coordination?
- **C4 — Configer coexistence:** does the option leave the existing `Configer` flag-composition pattern intact, including the `envVarNameTransf` parameter that the unnamed third consumer relies on?
- **C5 — Per-spec metadata:** can each variable carry type, default, description, category, sensitivity (for redaction), and origin (which module declared it)?
- **C6 — Lint enforceability:** can a CI test prevent new stray `os.Getenv` / `os.LookupEnv` calls outside the registry?
- **C7 — Migration cost:** how disruptive across boxer + pebble2impl?
- **C8 — API stability for downstreams:** does the API have a small enough surface that downstream projects can depend on it long-term?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 |
|----|----|----|----|----|----|----|
| C1 | −− | +  | +  | ++ | ++ | ++ |
| C2 | −− | −− | +  | ++ | ++ | ++ |
| C3 | −  | −  | +  | +  | ++ | ++ |
| C4 | ++ | ++ | +  | −− | ++ | ++ |
| C5 | −− | −  | +  | ++ | ++ | +  |
| C6 | −− | +  | −  | −  | ++ | ++ |
| C7 | ++ | ++ | −  | −− | −  | −  |
| C8 | n/a | +  | +  | −  | ++ | +  |

O5 dominates O6 on C5 (the `*StringVar` value carries metadata that the bare accessor can't expose) and C8 (a named value is a stable target for downstreams to consume; an accessor name is a function-call site that can't be passed around). O5 dominates O3 on C2/C6 because struct-tag decoding leaves the read site implicit and grep-unfriendly, which weakens lint enforcement. O4 is rejected on C4/C7: Viper-style multi-source config replaces, rather than coexists with, urfave/cli + Configer.

## Decision

We introduce **`public/config/env`** as a typed env-var registry. Each variable is declared as a package-level `var X = env.New*(env.Spec{...})` in the package that owns it; declaration registers the spec globally; reads go through `X.Get()` / `X.Lookup()`; CLI flags are constructed by `X.AsCliFlag()`. `ConfigerI` is left unchanged. A lint test bans `os.Getenv` / `os.LookupEnv` outside an allowlist from day one.

The decision has six parts.

### 1. Package layout

```
public/config/
├── config.go        // existing Configer interface — unchanged
└── env/
    ├── env.go       // Spec, registry, base Var, AsCliFlag
    ├── string.go    // *StringVar
    ├── bool.go      // *BoolVar
    ├── int.go       // *IntVar
    ├── duration.go  // *DurationVar
    ├── path.go      // *PathVar (FS paths; allows ~ expansion)
    ├── lint_test.go // bans os.Getenv outside allowlist
    └── doc_gen.go   // go:generate target → doc/env-vars.md
```

### 2. Spec and constructors

```go
package env

type CategoryE string
const (
    CategoryObservability    CategoryE = "observability"
    CategoryDev              CategoryE = "dev"
    CategoryDocgen           CategoryE = "docgen"
    CategoryLLM              CategoryE = "llm"
    CategoryDatabase         CategoryE = "database"
    CategorySystem           CategoryE = "system"            // HOME, GOPATH, GOWORK
    CategoryTestIntegration  CategoryE = "test-integration"  // CLICKHOUSE_*
)

type Spec struct {
    Name        string    // canonical, fully-qualified env var name; immutable
    Default     string    // string form; type-specific parsing happens in the var
    Description string
    Category    CategoryE
    Sensitive   bool      // redact from runtime dumps and generated docs
    CliFlagName string    // if non-empty, AsCliFlag() emits a cli.Flag with this name
    // Origin and Type are filled at registration time, not by the caller:
    Origin      Origin
    Type        TypeE     // string|bool|int64|duration|path; set by NewXxx
}

type TypeE string
const (
    TypeString   TypeE = "string"
    TypeBool     TypeE = "bool"
    TypeInt64    TypeE = "int64"
    TypeDuration TypeE = "duration"
    TypePath     TypeE = "path"
)

type Origin struct {
    Module string  // e.g. "github.com/stergiotis/boxer"
    Package string // e.g. "github.com/stergiotis/boxer/public/observability/logging"
}

func NewString(s Spec) *StringVar
func NewBool(s Spec) *BoolVar
func NewInt(s Spec) *IntVar
func NewDuration(s Spec) *DurationVar
func NewPath(s Spec) *PathVar
```

`Origin` is auto-derived at registration via `runtime.Caller(2)` and `runtime.FuncForPC`, then mapped to module + package path. Callers do not set it. Rationale: no coordination needed when a new project (boxer, pebble2impl, third) adopts the package; the registry can answer "which module declared this var?" without per-spec annotation.

### 3. Var API

```go
func (v *StringVar) Get() string                       // cached after first read
func (v *StringVar) Lookup() (val string, set bool)    // set=true iff env var is non-empty
func (v *StringVar) Spec() Spec                        // for inspection/docs
func (v *StringVar) AsCliFlag(opts ...FlagOption) cli.Flag

// Test helper:
func (v *StringVar) SetForTest(t testing.TB, value string) // uses t.Setenv + cache reset on t.Cleanup
```

`AsCliFlag` constructs a `cli.StringFlag` with `Name = spec.CliFlagName`, `EnvVars = []string{spec.Name}`, `Value = spec.Default`, `Usage = spec.Description`. The flag's `Action` writes the parsed value back through the var's cache, so `X.Get()` reflects the post-flag-parsed value uniformly regardless of whether the user supplied the flag, the env var, or relied on the default.

`FlagOption` exists to support the Configer composition path:

```go
type FlagOption func(*flagOptions)
func WithCliFlagName(name string) FlagOption
```

A `Configer` impl that wants to attach the existing name-transform pattern calls:

```go
spec.AsCliFlag(env.WithCliFlagName(nameTransf(spec.CliFlagName)))
```

The Spec's `Name` (env var side) is **canonical and not transformable**. This is consistent with current practice: no Configer impl in either project sets `EnvVars`, so freezing the env name introduces no regression. `envVarNameTransf` on `Configer.ToCliFlags` stays in the interface for the unnamed third consumer; the env package simply ignores it.

### 4. Registry introspection

```go
func All() []Spec                          // every spec registered process-wide
func ByCategory(c CategoryE) []Spec
func ByOrigin(modulePath string) []Spec    // e.g. boxer's specs vs pebble2impl's
func ByPrefix(prefix string) []Spec        // e.g. ByPrefix("BOXER_")
```

Surfaces:

- **`boxer env list` subcommand** — table output filterable by `--category`, `--origin`, `--prefix`. Respects `Sensitive` by displaying `<redacted>` for the current value.
- **`go generate ./public/config/env/...`** — emits `doc/env-vars.md`. Same redaction rules.

### 5. Lint enforcement from day one

`public/config/env/lint_test.go` walks the boxer module's Go files, parses each, and fails the test if any `os.Getenv` / `os.LookupEnv` / `syscall.Getenv` call appears in a non-allowlisted file. The allowlist is the env package itself plus a small documented set of legitimate exceptions (e.g., a test that exercises stdlib `os.Environ` semantics directly).

Downstream consumers (pebble2impl, third) can adopt the same test in their own modules if they wish; boxer cannot enforce against external modules.

### 6. Migration scope

**In scope for an initial migration PR**:

- Move the 13 `BOXER_*` cli.Flag declarations to spec-derived form. Fix `BOXER_LOG_MODULE_INFO_IN_START` → `BOXER_LOG_MODULE_INFO_ON_START` in passing.
- Migrate the 11 direct `os.Getenv` / `os.LookupEnv` call sites. Fix the lowercase `clickhouse` to a properly-named `BOXER_CLICKHOUSE_BINARY_PATH` (or similar) in passing.
- Register the system vars (`HOME`, `GOPATH`, `GOWORK`) with `CategorySystem`. Reads of these go through `env.Home.Get()` etc., even though the *defaults* are owned by the OS.
- `GEMINI_API_KEY` and `GOOGLE_API_KEY` register with `Sensitive: true`. The existing `LoadGeminiApiKey` composite stays: it reads through both specs and the `~/.config/gemini/api_key` file fallback.

**Out of scope for the initial PR**:

- Migrating pebble2impl. That's a follow-up coordinated with the pebble2impl owner (the user), once the boxer-side API has been exercised.
- Migrating Configer impls to use spec-derived flags. They currently expose zero env vars, so there is no pressure.

## Alternatives

- **O1 (status quo).** Rejected: the fragmentation is what the survey was prompted to address.
- **O2 (manual markdown + CI grep).** Rejected: cheap but inevitable drift, no read-path unification, no type/default centralization.
- **O3 (struct-tag decoding).** Rejected on C2/C6: reads via reflection-populated struct fields are grep-unfriendly, weakening lint enforcement, and duplicate the Configer struct-field pattern without adding value.
- **O4 (Viper-style multi-source).** Rejected on C4/C7: replaces rather than coexists with the existing CLI + Configer machinery, and brings file-source / remote-source mechanisms boxer does not currently need.
- **O6 (functional accessor).** Rejected on C5/C8: the lack of a named value to pass around removes the cleanest path for `AsCliFlag` composition and weakens the downstream API surface.

## Consequences

### Positive

- All env-var reads become enumerable at runtime via the registry; documentation is generated, not maintained by hand.
- Type parsing, default handling, and caching live in one place — three latent defects identified in the survey (lowercase `clickhouse`, `IN_START` typo, mixed default-declaration patterns) are fixed mechanically during migration and prevented going forward.
- Downstream consumers (pebble2impl, third) opt in by importing `public/config/env` and using `env.New*` for their own variables; their specs join the same registry automatically. `CLICKHOUSE_*` becomes a single shared spec instead of duplicated string literals.
- `Configer` is unaffected. The `envVarNameTransf` parameter that the third consumer depends on stays in the interface.
- Day-one lint catches future drift before it lands.

### Negative

- Every existing `os.Getenv` / `os.LookupEnv` site must migrate. The boxer-internal count is small (~13 read sites across 24 specs), but the change touches packages that don't otherwise change often.
- `Origin` auto-derivation uses `runtime.Caller` + `runtime.FuncForPC` at registration time. This is mildly reflective and ties registration to Go's runtime symbol tables. The alternative (explicit `Origin` per Spec) was rejected because it requires every adopting project to remember to set it.
- The registry is a **process-global singleton**. Binaries that link both boxer and pebble2impl share one registry. This is the intended outcome (single `CLICKHOUSE_USER` spec across both projects), but it means specs cannot be locally scoped, and a misregistration in one module is visible to all.
- `AsCliFlag`'s write-back into the var's cache via `Action` introduces a small piece of mutable global state per spec. Reads after CLI parsing return the parsed value; reads before parsing return env-or-default. Documented; not problematic in single-binary CLI use, but worth noting if the package is ever used outside a CLI process.

### Neutral

- The package is process-global by design; this is captured under "Negative" only because of the visibility implication, not because it's avoidable.
- System vars (`HOME`, `GOPATH`, `GOWORK`) appear in the registry alongside boxer-owned vars. The `Category: CategorySystem` field makes the distinction explicit for documentation rendering and filtering.
- Sensitive vars (`GEMINI_API_KEY`, `GOOGLE_API_KEY`) are redacted in dumps and generated docs but otherwise behave identically. Test helpers respect redaction.

## Status

Accepted 2026-05-13.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

- **2026-05-17 — M1/M2/M3 shipped; scaffold signature refinements
  during implementation.** Implementation across boxer and pebble2impl
  revealed five design gaps; this entry records them in one place
  rather than threading rationale through three more commit messages.

  1. **Per-type `WithXxxAction` options (added).** §3 specified
     `FlagOption` as a single type but did not anticipate
     Action-bearing flags whose cache write-back must run *after* a
     caller-supplied validator. Six flags in the boxer migration
     (`logLevel`, `logFormat`, `flightRecorder`,
     `flightRecorderOutputFile`, `waitForDebugger`, `markdownEcho`)
     carried such Actions. The scaffold gained `WithStringAction` /
     `WithBoolAction` / `WithInt64Action` / `WithDurationAction` /
     `WithPathAction`, each typed to the matching `cli.*Flag`
     signature. The chained Action runs the user function first; the
     cache is written only when the user function returns nil. The
     pebble2impl migration exercised the same options (e.g.
     `BOXER_FLIGHT_RECORDER` retains its trace-recorder bootstrap
     Action).

  2. **`IntVar` is 64-bit.** §3's signature
     `NewInt(s Spec) *IntVar` did not specify bit width. The boxer
     coding standard mandates sized integers on fields; the scaffold
     uses `int64` internally and emits `cli.Int64Flag`. Existing
     consumers reading via `ctx.Int(...)` migrated to
     `int(env.X.Get())`. The two pebble2impl `findAnchor` flags
     exercise this path; no consumer regressions surfaced.

  3. **`Lookup()` shape uniform across typed vars.** §3 named the
     StringVar signature `Lookup() (val string, set bool)` and left
     the others implicit. All typed vars expose
     `Lookup() (raw string, set bool)` — the raw env value as a
     string, regardless of internal type. This keeps the diagnostic /
     introspection surface trivially shared between the future doc
     generator (§4) and call sites that want "is this set" vs.
     "relying on the default" (`SPINNAKER_PLAY_SQL`'s
     persisted-session-vs-env-override branch in
     `play/app_register.go` is the canonical user).

  4. **Parse-failure semantics: env falls back, default panics.**
     §3 did not specify behaviour when `Bool` / `Int` / `Duration`
     fail to parse the env-supplied value, nor when the Spec's
     `Default` itself is malformed. The scaffold treats env-side
     parse failure as user error (silent fall-back to the parsed
     default) and default-side parse failure as programmer error
     (panic at first read). Both are documented on the
     `parseDefault` helpers.

  5. **`Category` is an open string.** §2 enumerated seven category
     constants. Consumers introduced five more by literal
     `env.Category("anchor")` / `"krypto"` / `"spinnaker-play"` /
     `"swisstopo"` / `"runinfo"` at declaration sites where the
     enumerated set didn't fit and adding a constant would have
     meant a boxer-side change for every new domain. The
     open-string design (`type Category string`) was already in §2
     — this entry confirms the pattern as deliberate. The boxer
     migration also reused the original `cli.Flag.Category` strings
     ("logging", "tracing", "doc") to avoid CLI-help UX churn.

  Three deviations from the §6 migration scope proved benign:

  - `CLICKHOUSE_PASSWORD` (both boxer nanopass test and pebble2impl
    clickhouseenv) and `PEBBLE_CIPHER_KEY_HEX` (pebble2impl
    encryptedHash) marked `Sensitive: true` even though §6 only
    named the Gemini/Google keys explicitly; all three are
    credentials and the redaction policy is "redact in dumps and
    generated docs".
  - `flightRecorderOutputFile` module-level mirror in
    `public/observability/tracing/flightrecorder.go` dropped —
    `FlightRecorderOutputFile.Get()` is now the single source of
    truth. The mirror existed only because the original code lacked
    a typed handle.
  - Pebble2impl mirrored the §5 lint test rather than depending on
    boxer's `_test.go` helper. The mirror at
    `src/go/public/config/envlint/envlint_test.go` skips nested
    `go.mod` sub-modules (the `scripts/dev/sponsor_deps` standalone
    tool, the `whole_program_fixture` test fixtures) by
    `os.Stat`-checking for `go.mod` in each directory. ADR §5 already
    noted "boxer cannot enforce against external modules"; within-repo
    sub-modules are equivalent for this purpose.

  **Milestones shipped.**

  - **M1 scaffold** — boxer `77e89dd`. `public/config/env/` with
    `Spec`, `Origin` (auto-derived via `runtime.Caller`), registry,
    five typed vars, `AsCliFlag`, `SetForTest`, the AST-based lint
    test (initially `t.Skip`-ped), and `Snapshot` / `FormatValue`
    helpers for the doc generator.
  - **M2 boxer-internal migration** — boxer `5400809`. The 14
    `BOXER_*` flags and 11 direct `os.Getenv` reads folded;
    `BOXER_LOG_MODULE_INFO_IN_START` → `_ON_START` typo fixed;
    lowercase `clickhouse` → `BOXER_CLICKHOUSE_BINARY_PATH`;
    `TestNoStrayOsGetenv` un-skipped.
  - **M3 pebble2impl migration** — pebble2impl `06bfb90c`. 7
    cli.Flag declarations + 28 direct reads folded across 24 files;
    three new shared spec packages
    (`db/clickhouse/clickhouseenv`, `thestack/imzero2/imzero2env`,
    `config/envlint`); mirror walker active.

  **Still open.** The `boxer env list` runtime subcommand
  (§4 introspection) has not been built.

  The core decision (typed declarative globals + process-global
  registry, Configer untouched, day-one lint) is unchanged.

- **2026-05-17 — M4 shipped: cmd/envgen + Spec.Type.** The doc
  generator §4 landed at `internal/cmd/envgen/main.go` (boxer
  precedent for `cmd` placement: `internal/cmd/licensegate`). It
  side-effect imports every boxer-owned env declaration site, calls
  `env.Snapshot()`, and renders a Diátaxis-typed reference markdown
  (`doc/env-vars.md`) with one H2 per category, an Origins lookup
  table, and YAML front-matter marking the file generated. The
  `//go:generate` directive in `doc_gen.go` uses
  `sh -c "go run -tags=\"$(cat ../../../tags)\" ..."` so the boxer
  build tags travel through to envgen without hardcoding them.

  Two scaffold additions enable the generator:
  - `Spec.Type TypeE` — filled at registration time by every
    `NewXxx` constructor. The generator reads it as the "Type"
    column; without it the renderer would have to type-switch on
    the registered `VarI` concrete type. The field is documented
    as set-by-constructor, like `Spec.Origin`.
  - `Category` renamed to `CategoryE` per the boxer coding
    standard's enum-suffix convention. The pre-existing constants
    (`CategoryObservability`, …) keep their names. The ADR §4
    `ByCategory(c Category)` signature is corrected in passing.
    Pebble2impl's ten `env.Category("…")` consumer sites migrate
    in a follow-up.

  Initial generated `doc/env-vars.md` covers 21 variables across 6
  categories (`dev`, `docgen`, `llm`, `observability`, `system`,
  `test-integration`). The nanopass `passes_test` ClickHouse vars
  are out of scope because they live in a `_test.go` file that
  envgen does not link in.

## References

- `public/config/config.go` — existing `Configer` interface, unchanged by this ADR.
- ADR-0006 — first-class pass with declared metadata; same "value carries its own metadata" pattern at a different layer.
- Tailscale `envknob` (`tailscale.com/envknob`) — closest prior art for the declarative-global + registry pattern.
- CockroachDB `pkg/util/envutil` — prior art for the lint-enforced single-read-path model.
- Kelsey Hightower `envconfig` (`github.com/kelseyhightower/envconfig`) — struct-tag option (O3) for reference.
