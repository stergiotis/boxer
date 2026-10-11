---
type: explanation
audience: whoever decides how boxer, as a library, hands configuration to the agent harness — which values an integrating application supplies, and what boxer must guarantee about them; a problem statement, read before any ADR on the seams
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A problem statement, not a decision.
> Written 2026-10-11 from how the agent harness (ADR-0307, ADR-0310,
> ADR-0312) and the chat profile read their configuration. It names the
> seams an option would need; it does not choose one.

# The harness reads configuration it should be handed

## The problem in short

Boxer is a library. An integrating application boots a host from it — through
`hostboot.Boot`, or by assembling the bus, the window host and the services
itself — and owns its deployment: where its settings live, how many profiles it
keeps, how a person picks one. Parts of the agent harness ignore that. They
read the environment-variable registry (ADR-0009) directly, at a point of their
own choosing, so a value the integrator supplies does not reach them, and a run
cannot say which values it ran under.

The registry is not the problem. It is process-global by design, and a
process-global default is right for a library. What is missing is the
alternative to it: a seam through which the integrator passes a value, and a
rule that boxer's own code reads the value it was handed rather than the
registry behind it.

## Where boxer reads instead of being handed

- **The cell declaration has two copies in a hostboot host.** Hostboot builds
  one from `BOXER_CELL_*` and shares it as `Runtime.Cell` with the model
  service, the egress service and the clipboard broker; its comment says the
  services "never read the environment again". The chat app reads it a second
  time (`confinedTierNow`, via `cell.FromEnv(nil)`), because `MountContextI`
  carries no declaration. The two are not equal by construction — the host's
  copy carries the trusted hosts and the egress destinations, the app's does
  not — and only the confined tier is read from the app's copy.
- **There is no way to supply a declaration at boot.** `hostboot.Options` lets
  an integrator replace env-derived values elsewhere (`Facts` replaces the
  store chosen from `CLICKHOUSE_*`; `ScreenshotDir` exists because env values
  are cached process-wide), but not the cell. A host assembled without
  hostboot can pass one to the model service (`llm.Config.Cell`), but
  `llm.ConfigFromEnv` fills it from the environment by default.
- **The profile describes values that are not in force.** `ProfileT` holds
  rounds, pixels, image budgets and operation tools, and `Fingerprint` hashes
  them, but `defaultProfile` fills them from compile-date constants and
  nothing reads them back: the window and the coordinator read the env
  variables (`RoundsEnv`, `PixelsMaxEnv`, `ImageBudgetBytes` and their
  neighbours) directly. A run started under `BOXER_CHAT_ROUNDS_MAX=8` and
  resumed under `96` fingerprints alike, so ADR-0308's read-only resume does
  not fire. The journal's settings (`journalSettings`) record keep, apps,
  questions, artefact and the ceiling, not the rest, nor the confined tier.
- **Limits and starting values are mixed.** The chat's env variables are of
  two kinds. Some are *limits* the person cannot move — the rounds range,
  the pixels ceiling and whether pixels must stay local, the image budgets.
  Others are *starting values* for a setting the person moves in the chat's
  Settings — the rounds, the pixels, the operation tools, apps, questions,
  artefact. A limit is a property of the profile; a starting value is only
  where a conversation's own setting begins, and what governs a run is the
  setting, not its seed. The profile holds both kinds, and the journal
  records some of the settings.
- **There is one profile, and only boxer can add one.** Profiles register in
  the chat package's own `harness.Registry`; `BOXER_CHAT_PROFILE` admits
  `"default"` only. An integrator that wants a second profile, or wants
  profiles kept as data, has nowhere to put it.

## What an integrator should be able to do

- Supply the cell declaration at boot, or let boxer build it from the
  registry when it supplies none.
- Supply profiles — from code, a record store, anywhere — and choose one per
  run, with the registry's `BOXER_CHAT_PROFILE` as the default selector.
- Read back, for a run, the values it ran under, so it can show or keep them.

## What boxer must guarantee regardless

These cannot be delegated, because an integrator who gets them wrong weakens
the cell rather than its own application:

- **One value, once.** A value supplied at a seam is the one every service
  and app sees. No boxer package reads the registry behind a seam; otherwise
  an integrator's declaration is silently ignored wherever that happens — the
  chat app's confined tier is the instance this would create first.
- **The fingerprint covers the limits in force.** The profile carries the
  limits as resolved — supplied, or the registry's — and the chat reads them
  from the profile, so a changed limit changes the fingerprint and a resume
  under it is read-only.
- **The journal carries the settings.** A conversation's settings are fixed
  on the run at the first send and restored on resume; a changed starting
  value changes only where a new conversation begins, and neither strands a
  run nor is fingerprinted.
- **Policy is checked on resume, never restored.** A run may resume with the
  preferences it started under (rounds, pixels, budgets). It may not resume
  with the policy it started under: a declaration tightened since must hold,
  and a changed declaration is detected — the run resumes read-only, or the
  stricter value wins — rather than overlaid from the run's record.

## What stays out of scope

- **Changing the registry.** No scoped, run-level or persisted layers in
  `public/config/env`; ADR-0009 stands, including its rejection of a file
  loader. The registry remains the default a seam falls back to, and the
  `os.Getenv` ban (codelint CS011) the only gate.
- **Storing profiles.** Where a profile lives and how it is named or listed
  is the integrator's decision; boxer needs only a way to ask for one by id.
- **Hot reload.** Nothing in the harness needs a value to change mid-run; a
  changed value mid-run is what the fingerprint exists to refuse.
- **Structured specs.** Approval policies and canary lists are strings with
  hand-written parsers (`cell.ParseApprovalPolicy`, `cell.ParseCanaries`).
  A typed spec would help, but it is independent of the seams and belongs in
  its own note.

## Open questions

- Settled 2026-10-11: the declaration reaches an app on its mount context
  (ADR-0307 Update, §SD1), and the run's fingerprint folds in the confined
  tier, not a digest of the whole declaration (ADR-0308 Update, SD5).
- How an integrator supplies profiles to the chat, and selects one per
  run.
- Whether the model service's trail rows should name the declaration a call
  was checked against, not only its tier and route. (Not verified against
  the trail's row shape.)

## What to read

- `public/config/env/env.go`, `resolved.go`; ADR-0009
- `public/keelson/runtime/hostboot/hostboot.go` (`Options`, `Runtime.Cell`,
  where the declaration is built)
- `public/keelson/runtime/cell/decl.go` (`FromEnv`, `FromEnvLogged`)
- `public/keelson/runtime/llm/service.go` (`Config.Cell`, `ConfigFromEnv`)
- `public/keelson/runtime/app/app.go` (`MountContextI`)
- `apps/chat/chat_profile.go` (`ProfileT`, `Fingerprint`, `defaultProfile`,
  `ProfileEnv`), `chat_journal.go` (`confinedTierNow`, `journalSettings`),
  `chat_resume.go` (the fingerprint check), `chat_coordinator.go`
  (`RoundsEnv`)
- `public/keelson/runtime/harness/profiles.go` (the profile registry)
- ADR-0308 §SD5 (profile and fingerprint), ADR-0310 (seams and profiles)
