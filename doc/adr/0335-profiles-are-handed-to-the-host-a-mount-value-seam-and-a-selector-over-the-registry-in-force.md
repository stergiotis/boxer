---
type: adr
status: accepted
date: 2026-10-25
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-25
---

# ADR-0335: Profiles are handed to the host — a mount-value seam, and a selector over the registry in force

## Context

Boxer is a library; an integrating application boots a host from it and
owns its deployment. The background note
[env-registry-scope-problem](../adr-background-work/env-registry-scope-problem.md)
found two places where the agent harness read the environment registry
([ADR-0009](./0009-environment-variable-registry.md)) instead of being
handed a value, so an integrator's value could not reach it. The first,
the cell declaration, was settled on 2026-10-11: one declaration per host,
supplied through `hostboot.Options.Cell` or built from `BOXER_CELL_*`, and
handed to every app on its mount context
([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)
§SD1 Update, `app.MountValuesI`, `cell.OfMount`); the chat's remaining
`cell.FromEnv` is the fallback for a host that hands none. The limits a
profile carries were split from the starting values a person moves the
same day ([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
Update of 2026-10-11).

The second place is the profile. The chat's profiles live in a registry
compiled into the chat package (`harness.Registry` over the chat's
`ProfileT`), the one entry is `default`, and `BOXER_CHAT_PROFILE` admits
that one id. An integrator that wants a second profile, or profiles kept as
data, has nowhere to put one; ADR-0310 §SD8 defers per-conversation
selection until a second profile exists, which the seam's absence prevents.
The note's open question — how an integrator supplies profiles and selects
one per run — is what this record answers. What it must keep: one value,
once (no boxer package reads the registry behind the seam); the fingerprint
covers the limits in force; the journal, not the profile, carries a
conversation's settings.

## Design space (QOC)

**Question.** How does a profile reach the chat from outside the chat?

**Options.**

- **O1** — No seam: the compiled registry is the registry; an integrator
  adds a profile by registering it from its own `init`, or by forking the
  chat.
- **O2** — A mount-value seam: the host hands the registry it wants in
  force to the apps it mounts, as it hands the cell declaration; the chat
  reads it from its mount context and falls back to the compiled registry.
  `BOXER_CHAT_PROFILE` selects an id from whichever registry is in force.
- **O3** — Profiles as data the registry loads: a file or a record store
  the chat reads at mount, found through the environment.

**Criteria.**

- **C1** — One value, once: the chat sees the integrator's profile and
  reads nothing behind it.
- **C2** — The integrator's effort and freedom: where profiles live and
  how they are named is theirs.
- **C3** — Fits the cell's seam: the same shape as ADR-0307 §SD1, so a
  reader of one knows the other.
- **C4** — Leaves ADR-0009 alone: no scoped or persisted layer in the
  registry, no file loader.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −  | ++ | +  |
| C2 | −− | ++ | −  |
| C3 | −  | ++ | −  |
| C4 | +  | ++ | −− |

O1 makes a profile a build artefact and ties every integrator to the
chat's package initialisation order. O3 chooses the integrator's storage
for it and reopens the file loader ADR-0009 rejected. O2 is the cell's
seam again.

## Decision

The host hands the chat the profile registry in force as a mount value,
and the chat selects a profile from that registry; the compiled registry is
what a host that hands none falls back to.

### SD1 — The seam

`hostboot.Options` gains `MountValues`, a map from a key to a value the
window host hands every app it mounts (`SetMountValue`), so a host built
from the library supplies app-owned values without hostboot naming their
types. The chat exports the key and the typed accessor, as `cell` does:
`chat.ProfilesMountKey` and `chat.ProfilesOf(mount)`, which returns the
handed `*harness.Registry[ProfileT]` or the compiled one. Every profile a
handed registry holds is validated (`ProfileT.Validate`) when the chat
mounts; an invalid one refuses the mount with its reason, since a profile
that fails validation would otherwise be chosen silently later. The chat
reads profiles through `ProfilesOf` alone.

### SD2 — Selection

`BOXER_CHAT_PROFILE` stays the selector and names an id in the registry in
force; its allowed set is no longer fixed to `default` at compile time but
checked against that registry at the first send. An id the registry does
not hold is a refusal of the send naming the ids it does, not a fall-back:
a run that silently ran under another profile than the one named would
defeat the fingerprint. The run row's `profile` records the id used, as
now. Per-conversation selection — a picker, or a selector an integrator
supplies per conversation — stays deferred as ADR-0310 §SD8 has it, until
a second profile exists to select; this record removes the reason none
could.

### SD3 — Read-back

`keelson('harness_profiles')` publishes every profile of the registry in
force, not only the compiled default, at mount; the run row's `profile`
and `fingerprint` say which one a run ran under and with which limits. An
integrator reads both from the journal views
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
Update of 2026-10-13).

### SD4 — Not taken: the trail names the declaration

The note asked whether a model call's trail row should name the cell
declaration it was checked against. It does not: the row carries the tier
the wall saw and the route, the declaration is one per host (ADR-0307
§SD1), and the host's identity is the row's origin. A declaration digest,
if a deployment ever needs to prove which declaration a host ran under,
belongs on the host's run record (`runinfo`), once, not on every call.

### SD5 — Deferred, recorded

- Per-conversation profile selection (ADR-0310 §SD8), with a second
  profile.
- Profiles as data gate-tested like the prompt book (ADR-0310 §SD7): the
  seam carries a registry, and how one is built from data is the
  integrator's until a second consumer wants the same loader.
- A profile's label and the cell kind it is for (ADR-0310 §SD7).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `hostboot.Options` | `MountValues` added | the window host's `SetMountValue` loop at boot |
| `apps/chat` | `ProfilesMountKey`, `ProfilesOf` added; `ProfileEnv` loses its compiled allowed set | `profileNow`, the first-send check, the mount's validation |
| `keelson('harness_profiles')` | publishes the registry in force | nothing: additive rows |

## Alternatives

- **A `harness.ProfilesI` interface on `hostboot.Options`.** Rejected:
  hostboot cannot name the chat's profile type without importing the chat,
  and an untyped interface would push the type assertion into every
  consumer; a map of mount values keeps hostboot ignorant of app types.
- **Falling back to `default` on an unknown selector.** Rejected: the run
  would carry a fingerprint the person did not ask for; a refusal that names
  the ids in force is cheaper to understand than a run under the wrong
  limits.
- **A selector per conversation now.** Rejected for the reason ADR-0310
  §SD8 gives: with one profile there is nothing to select.

## Consequences

### Positive

- An integrator supplies profiles from wherever it keeps them, and the
  chat's fingerprint covers the limits they carry.
- The cell and the profile reach an app through one mechanism.

### Negative

- `ProfileEnv` can no longer be validated at registration; a typo is found
  at the first send rather than at boot.
- A handed registry that fails validation refuses the chat's mount, which
  an integrator meets as a window that does not open, with the reason in
  the log.

### Neutral

- The compiled `default` profile stays and stays first; nothing changes
  for a host that hands none.

## Migration — Tier 1

Nothing to migrate: the seam is additive, and a host that supplies no mount
values runs as before.

## Verification plan — Tier 1

- **Lane.** Default `go test`: a mount that hands a registry with a second
  profile fingerprints under its limits and the run row names it; a
  selector naming an id the registry lacks refuses the first send naming
  the ids it holds; a handed profile that fails validation refuses the
  mount; `keelson('harness_profiles')` lists both profiles.
- **What would fail.** A chat reading a profile from anywhere but
  `ProfilesOf`; a run whose fingerprint does not move when the handed
  profile's limits do.
- **Gap.** Whether a second profile does better work is a trial, not a
  test (ADR-0310).

## Status

Accepted 2026-10-25. Nothing built: the design follows the cell's seam,
and the first integrator with a second profile is the trigger to build it.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [env-registry-scope-problem](../adr-background-work/env-registry-scope-problem.md) — the problem statement.
- [ADR-0009](./0009-environment-variable-registry.md) — the registry this record leaves alone.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) §SD1 Update of 2026-10-11 — the cell's seam this one copies.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) §SD7, §SD8 — the profile, its limits, and the deferred selection.
