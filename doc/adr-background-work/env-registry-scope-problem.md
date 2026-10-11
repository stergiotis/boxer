---
type: explanation
audience: whoever decides whether the environment-variable registry (`public/config/env`, ADR-0009) gains scoped, persisted or structured values — a problem statement, read before any ADR that would add them
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A problem statement, not a decision.
> Written 2026-10-11 from how the agent harness (ADR-0307, ADR-0310,
> ADR-0312) and the chat profile read their configuration. Nothing here
> proposes an option.

# The env registry is process-global and the harness needs per-run values

## The registry today

A variable is a package-level `env.New<Type>(env.Spec{Name, Default,
Description, Category, Sensitive, ...})`, registered once per process and
panicking on a duplicate name (ADR-0009). A value is read lazily on the first
`Get()` and cached for the process. The layering is Override, Flag, Env,
Default; `ValueSource()` says which tier supplied a variable's value. There is
no file layer (ADR-0009 rejected a multi-source loader), no scope (the ADR
says so: "a process-global singleton; specs cannot be locally scoped"), and no
structured type: approval policies and canary lists are strings parsed by
hand (`cell.ParseApprovalPolicy`, `cell.ParseCanaries`). `Category` is a
documentation tag. `go generate` renders `doc/env-vars.md` from the registry.

This is the single choke point the repo wants, and it holds: a lint test bans
`os.Getenv` outside an allowlist.

## What the harness needs from configuration

A run is the unit the harness reasons about (ADR-0308): it has a profile, a
fingerprint, settings, a cell declaration in force, and it can be resumed in
a later process. Three needs follow:

1. **Values resolved once and shared.** The cell declaration is built from
   nine `BOXER_CELL_*` variables by `cell.FromEnv`. Hostboot builds it once
   ("the services read it and never the environment again"), the llm
   service builds it again in `ConfigFromEnv`, and the chat app builds it a
   third time (`confinedTierNow` in `chat_journal.go`) because an app has
   no handle on the host's copy. Three reads, no shared source record, and
   nothing that says they agree.

2. **Values bound to a run and recorded.** The run row carries a profile
   id, a fingerprint and journaled settings (`journalSettings`), and a
   resume strands the run when the fingerprint moved. But the fingerprint
   names compile-date constants only (`ProfileT`). The env-derived seeds
   and caps that govern a chat (rounds, pixels, image budgets, operation
   tools, the cell's confined tier) are read at window or coordinator
   construction, process-cached, and not in the fingerprint. A run resumed
   under different `BOXER_CHAT_ROUNDS` or a different confined tier is not
   detected as changed.

3. **Values that outlive the environment.** A profile is a Go literal plus
   env seeds; the only persisted form is the journal's copy of the settings
   the chat chose to write. There is no place to store a profile as data, so
   there is one profile (`BOXER_CHAT_PROFILE` allows `"default"` only), and
   a person cannot keep two.

## Where it hurts today

- **Resume is less honest than it claims.** ADR-0308 says a resume under a
  changed profile is read-only. The check sees the code, not the values.
- **The cell declaration can disagree with itself.** Nothing stops the
  host's and the app's copies from differing when an override is set after
  boot, and no record says which copy a given exit was decided under. The
  trail row for an llm call carries the tier and route but not the
  declaration it was checked against.
- **Structured policy as strings** means the approval map and the canary
  list have their own parsers, their own error paths and no doc rendering
  of their shape beyond the description text.
- **Tests set process state.** `SetForTest` and `Override` are process-wide,
  so tests that need two configurations serialise or fork.

## What an extension would have to provide

Stated as requirements, not as a design:

- A **resolved snapshot**: a value type built once from the registry at a
  known point (host boot), immutable, fingerprintable, carried to apps and
  services on the mount context or the bus rather than re-read. The cell
  declaration and the chat seeds are its first contents. `ValueSource` per
  entry travels with it, so a run's journal can record not only the values
  but where each came from.
- **A run-scoped layer** above Env: a resolved snapshot can be overlaid by
  values a run carries (its journaled settings), so a resumed run sees the
  values it was started under, and the fingerprint covers them.
- **A persisted layer**: a profile as a record (the journal, or a
  recordstore kind), named, listable, chosen per run. The registry's
  `Default` stays the compiled fallback.
- **Structured specs**: a typed `New<Struct>` that parses once at
  resolution, validates, and renders its shape into `doc/env-vars.md`.
- **No second choke point.** Whatever is added resolves through the same
  registry; the `os.Getenv` lint stays the only gate.

## What stays out of scope

- Hot reload. Nothing in the harness needs a value to change mid-run, and a
  changed value mid-run is what the fingerprint exists to refuse.
- A file loader. ADR-0009's rejection stands; the persisted layer above is a
  record, not a config file.

## What to read

- `public/config/env/env.go`, `resolved.go`, `envdoc/render.go`; ADR-0009
- `public/keelson/runtime/cell/env.go`, `decl.go` (`FromEnv`,
  `FromEnvLogged`); `hostboot.go` where the declaration is built
- `public/keelson/runtime/llm/service.go` (`ConfigFromEnv`)
- `apps/chat/chat_profile.go` (`ProfileT`, `Fingerprint`), `chat_journal.go`
  (`confinedTierNow`, `journalSettings`), `chat_resume.go` (the fingerprint
  check)
- `public/keelson/runtime/harness/profiles.go` (the published profile table)
- ADR-0308 §SD5 (profile and fingerprint), ADR-0310 (seams and profiles)
