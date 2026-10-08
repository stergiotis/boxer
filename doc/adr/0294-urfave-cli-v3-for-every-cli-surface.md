---
type: adr
status: proposed
date: 2026-10-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0294: urfave/cli v3 for every CLI surface

## Context

[CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points) makes
`github.com/urfave/cli` mandatory for every command line in the tree, and the
environment-variable registry ([ADR-0009](./0009-environment-variable-registry.md))
hands out its flags as `cli.Flag` values. The standard named v2. v3 has been the
maintained major line since 2025; v2 receives fixes only.

The two lines differ in shape, not just in names. v3 has one type, `Command`,
where v2 had `App` and `Command`; an action receives the `context.Context` as a
parameter instead of reading it from a `*cli.Context`; a `Before` hook returns
the context the rest of the run sees; flags are inherited by subcommands unless
marked local; an environment variable is one `ValueSource` among others.

Because the registry's flags and the shared `Before: logging.Apply` wiring are
exported from boxer, a module that builds against boxer cannot stay on v2 once
boxer moves.

## Decision

We move every CLI surface to `github.com/urfave/cli/v3` — boxer and the
modules that build against it in the same workspace, in one step, with no v2
code left behind.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `env` registry handles' `AsCliFlag` | returns a v3 `cli.Flag`; a flag `Action` takes `(ctx, cmd, value)` | every module that mounts registry flags |
| `logging.Apply` | has the v3 `BeforeFunc` shape, `(context.Context, error)`, so `Before: logging.Apply` stays the wiring | every main |
| Helpers that took `*cli.Context` | take `(ctx context.Context, cmd *cli.Command)` | their callers |
| Entry-points gate | checks for the v3 import path | consumer repositories running the gate |
| CODINGSTANDARDS and the downstream skeleton | name v3 | — |

## Alternatives

- **Stay on v2.** Rejected: it no longer receives features, and every year on it
  adds call sites to the eventual move.
- **Move boxer first and let downstream modules follow later.** Rejected:
  boxer's exported flags and hooks are v2 or v3 types, not both, so a
  downstream module would have to bump boxer and migrate in the same change
  anyway.
- **A compatibility layer translating between the two.** Rejected: the
  standard rules out compatibility shims, and the two lines disagree on where
  the context lives, which a wrapper can only paper over.

## Consequences

### Positive

- Actions receive the context as their first parameter, matching the
  standard's context-first rule (codelint CS002) without a `c.Context` detour.
- One command type at every level of a command tree.

### Negative

- A source-breaking change for every module that pins boxer.
- Flags defined on a parent are now visible to its subcommands unless marked
  `Local`, so a parent's flag can be given after the subcommand name.
- A flag value keeps its parsed state after a run: run the same flag instances
  twice in one process and the second run skips their environment sources and
  sees the first run's values. A binary runs once per process and is not
  affected; a test or a host that runs a command repeatedly needs fresh flags
  per run, which is why `logging.NewLoggingFlags` exists beside
  `logging.LoggingFlags`.
- Accessors no longer convert. `cmd.String` on a float flag, or `cmd.Int` on an
  `Int64Flag`, returns the zero value where v2 returned the converted value.
  Generic code that reads flags of several types goes through `cmd.Value`.

### Neutral

- v3 requires a newer `testify` than the tree pinned; the minimum moves with it.

## Migration — Tier 1

- **Breaks.** `cli.App`, `*cli.Context`, `Subcommands`, `EnvVars`, `FilePath`,
  `PathFlag`, `HasBeenSet`, `HelpName`, slice-constructor defaults
  (`cli.NewStringSlice`), and `TimestampFlag.Layout`.
- **Path.** `cli.App` becomes the root `cli.Command`; an action
  `func(c *cli.Context) error` becomes
  `func(ctx context.Context, cmd *cli.Command) error`, with `c.Context` read as
  `ctx` and `c.App` as `cmd.Root()`; `Subcommands` becomes `Commands`;
  `EnvVars: []string{X}` becomes `Sources: cli.EnvVars(X)`; a `Before` hook
  returns `(context.Context, error)`, where a nil context keeps the incoming
  one; `app.Run(os.Args)` becomes `app.Run(ctx, os.Args)`.
- **Regeneration.** None.
- **Old shape.** Removed outright.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane, plus the entry-points gate, which
  fails a main that does not import the v3 path.
- **What would fail.** A command whose flags collide or whose hooks have the
  wrong shape fails when its help is rendered; walking `--help` over every
  command path of each binary exercises that.
- **Gap.** The help walk is a manual check, not a test in the lane. Nothing
  checks that an accessor's type matches its flag's, which v3 turns into a
  silent zero; when the move was made every accessor called with a constant
  name matched, and the few with computed names were read by hand.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0009](./0009-environment-variable-registry.md) — the environment-variable registry whose flags this reshapes.
- [CODINGSTANDARDS § Entry Points](../../CODINGSTANDARDS.md#entry-points)
- urfave/cli v3 migration guide: <https://cli.urfave.org/migrate-v2-to-v3/>
