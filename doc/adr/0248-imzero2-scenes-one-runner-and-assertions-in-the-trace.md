---
type: adr
status: accepted
date: 2026-09-19
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-19
---

# ADR-0248: imzero2 scenes — one runner, a scene document, and assertions in the trace

**In one paragraph:** a *scene* — launch an app headless, drive it, assert,
capture — is today a bash script per scene, each carrying its own copy of the
harness and reaching for scene-specific Go commands whenever it has to compare
a number. This ADR makes the scene a markdown document, gives it one runner
(`imzero2 scene`), adds `read` and `expect` to the step vocabulary so the
common assertions stay in the trace, and sends what no declarative step should
express — a map projection, a period computed from a readout — to Go tests in
the integration lane, over the same harness as a library.

## Context

[ADR-0154](./0154-headless-carrier-tree-and-driver.md) gave the headless host a
driver and a trace vocabulary; its 2026-09-18 Update made that driver the
agent-facing surface too. What grew around it was not designed. Measured on
2026-09-19:

- **The harness is copied, and the copies disagree.** Eight scene scripts and
  the play tour each carry the host build, the client copy, font resolution,
  the launch environment, the wait for the carrier, the `drive` wrapper and
  the teardown. Of the eight scripts' lines roughly one in eight is a trace
  step. The copies have drifted where it matters: the stale-client guard is
  fatal in five, a warning in the tour and absent in two; only two install a
  cleanup trap, so a failure mid-run elsewhere leaks a host; ports are
  hand-assigned and collide (three scripts default to the same one); each
  scene builds its own host binary.
- **Assertions leave the trace.** `wait` is the only check a step can make.
  Anything numeric — a zoom within tolerance, a delta between two readings, a
  counter at zero — is done by dumping the tree, parsing label text and
  comparing in a scene-specific Go command invoked from the shell
  (`portolan-cam`, `waveform-scene`). The same gap is why scenes carry fixed
  pauses where they mean "until this reads X".
- **Inputs are computed from readings.** A painter-only canvas has no node, so
  a scene derives pointer coordinates from text the demo prints (a canvas
  origin) or from neighbouring nodes, again in shell plus a helper command,
  with one `drive` invocation per gesture because the arithmetic sits between
  them.
- **One launch is one unit.** Seeded state (`BOXER_PLAY_SQL`, a focus knob, a
  demo file) is launch state, so scripts that look like one scene are several
  launches, and the tour is 73.

The pressure is ordinary: every new widget that wants a scene copies the
largest script that looks similar. The agent surface sharpens it — a sequence
of steps an agent found to work should become a maintained scene by being
saved, not by being wrapped in two hundred lines of bash.

## Design space (QOC)

**Question.** Where do a scene's assertions and computed inputs live, given
that the harness moves into one runner either way?

**Options.**

- **O1** — trace only, with an expression language: bindings, arithmetic and
  functions in steps.
- **O2** — trace only, with a fixed `read` / `expect` pair and no arithmetic
  beyond adding a captured number to a coordinate; whatever that cannot say
  stays in shell.
- **O3** — Go only: every scene is a Go test over a harness library.
- **O4** — two tiers over one harness: O2's verbs for declarative scenes, Go
  tests in the integration lane for computed ones.

**Criteria.** C1 a scene stays a reviewable data file, and an agent's working
steps become one by being saved; C2 every assertion observed today is
expressible somewhere without shell; C3 the vocabulary stays small enough to
execute on a second seam ([ADR-0127](./0127-imzero2-interaction-record-replay.md),
proposed) and to teach in a page; C4 implementation weight.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | −− | ++ |
| C2 | ++ | −  | ++ | ++ |
| C3 | −− | ++ | +  | ++ |
| C4 | −− | ++ | +  | +  |

**Resolution.** O4. Kill reasons in [§Alternatives](#alternatives).

## Decision

### SD1 — `read` and `expect`: assertions that stay in the trace

Two verbs join the step vocabulary of ADR-0154 §SD6.

`read` resolves an anchor like any other step, matches a regular expression
against the node's value (or name), and binds each **named** capture group for
the rest of the run. A capture that parses as a number is a number. No match
is a failure, as an unresolved anchor is.

`expect` compares a bound name — or the difference of two, which is also how
one reading is compared with another — with a constant: equal, within a
tolerance, at least, at most, or a string match. It fails the run with what
was read and what was expected in the message.

```
{"do":"read","valueContains":"zoom","role":"label","pattern":"zoom (?P<z0>[\\d.]+)"}
{"do":"click","name":"+"}
{"do":"read","valueContains":"zoom","role":"label","pattern":"zoom (?P<z1>[\\d.]+)"}
{"do":"expect","of":"z1","minus":"z0","approx":1,"tol":0.011}
```

`read` polls like `wait` until the pattern matches or the timeout runs out, so
"until this reads X" is one step rather than a pause and a hope.

### SD2 — Computed coordinates: a bound name as an origin, nothing more

Pointer steps take `xFrom` / `yFrom`: the name of a bound number added to `x`
/ `y` (and to `toX` / `toY` of a drag). That covers "the canvas origin the demo
prints, plus an offset into the canvas" without a second `drive` invocation.
There is no arithmetic beyond this addition and no functions. A scene that
needs more is an SD5 scene.

### SD3 — A scene is one markdown document, and one launch

The document follows the applet book's convention
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD1): frontmatter, prose,
role-marked fences.

- **Frontmatter** is the launch spec: `launch` (the app alias), `size`, `env`,
  `needs` (what the scene requires of the client — `raster` for a capture),
  `requires` (preconditions, SD6), `services` (SD6), `settle`.
- **The prose** is the scene's description and becomes its gallery entry.
- **The first `sql` fence**, when present, seeds the editor buffer of an app
  that reads one. Multi-line SQL is the reason the spec is not a JSON header:
  the tour's queries are paragraphs.
- **The `jsonl` fence** is the trace. Absent, the scene is a single capture
  named after the file.

One document is one launch, because seeded state is launch state. A script that
relaunches becomes several documents; a directory of them is a tour, ordered
by file name. Scene documents live beside what they exercise.

### SD4 — One runner: `imzero2 scene`

`imzero2 scene <doc-or-dir>…` runs each scene: starts the host as a child of
its own executable, waits for the carrier, runs the trace, tears down, and
writes a gallery index — the document's prose, its captures and its trace —
beside the PNGs. The runner owns what the copies disagree on today:

- **Ports** are allocated, not assigned.
- **The client** is chosen by capability against `needs`; a client older than
  the generated interpreter sources is fatal, always.
- **Teardown** runs on every exit path and kills by PID.
- **One host binary.** The runner *is* the host binary, so nothing is built per
  scene; a thin launcher script builds it once into the per-checkout cache
  [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md)'s launcher
  uses.
- **Dry run** resolves every anchor of every scene and captures nothing.
- **Exit status** is the assertion. A precondition that does not hold is a
  *skip*, reported as one, and distinct from a pass.

### SD5 — Computed scenes are Go tests over the same harness

The harness of SD4 is a library first and a command second. A scene whose
oracle is a computation — a web-mercator shift between two readings, a press
position derived from a hover readout and a burst period — is a Go test in the
`//go:build integration` lane that launches through the library, runs trace
fragments with the same executor, and does its arithmetic in Go, next to the
widget it checks. The scene-specific commands under `public/app/commands` go
away; their arithmetic stays, as test helpers.

### SD6 — Preconditions and services are named, not scripted

`requires` names checks the runner knows how to make: a reachable ClickHouse,
a non-empty table, an executable on the path. `services` names helpers it
knows how to start and reap; the map tile stub is the one that exists. Both are
small registries in the runner, not shell hooks in the document — a scene
document that can run arbitrary commands is a shell script again.

Fixture *generation* (synthesising audio, taking a snapshot into the store)
stays outside: it mutates the environment, runs rarely and is not what a scene
asserts. It remains a script the scene's prose points at.

### SD7 — Deferrals

- **Input acknowledgement** (ADR-0154, 2026-09-18 Update) stays deferred;
  `read`'s polling is what scenes use instead of a pause.
- **Anchored `hover`.** The tour parks the pointer by coordinate before a
  scroll. Letting `hover` take an anchor is a small vocabulary addition that
  rides with SD1 but is not required by it.
- **play's `BOXER_PLAY_SCREENSHOT` family** is not retired here. Nothing
  scripted uses it, but the desktop tab-walk uses its SVG path as a visual
  record; retiring it needs that record to come from somewhere else first.
- **CI.** No scene runs in CI today. The runner makes it possible; which scenes
  are cheap and hermetic enough is a separate call.

### Milestones

- **M1 — `read`, `expect`, `xFrom`/`yFrom`** ✓ in the trace executor, with the
  skill page updated.
- **M2 — The harness library and `imzero2 scene`** ✓, proven on the two
  single-launch scenes with no computed inputs.
- **M3 — The computed scenes as integration tests** ✓; the scene-specific
  commands removed.
- **M4 — The remaining scripts and the play tour** ✓ as scene documents; the
  scripts removed.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Trace step vocabulary (`carrierclient.Step`) | `read`, `expect`, `xFrom`, `yFrom` added | the imzero2-drive skill; ADR-0127's executor, if built, inherits them |
| `imzero2` subcommands | `scene` added beside `drive` | the launch how-to; AGENTS.md's screenshot ladder |
| `public/app` `dev` subcommands | `portolan-cam`, `waveform-scene` removed (M3) | the two scene scripts that call them |
| `scripts/dev/*-scene.sh`, `play-screenshot-tour.sh` | removed (M4) | every doc that names them as the maintained example |

## Alternatives

- **An expression language in steps (O1).** It expresses everything and that is
  the problem: bindings plus arithmetic plus functions is a scripting language
  with no debugger, a second executor would have to implement it identically,
  and the one page that teaches the vocabulary becomes a manual.
- **`read` / `expect` only, the rest in shell (O2).** Leaves the harness copies
  alive exactly where the scenes are hardest, and keeps Go commands whose only
  caller is a bash script.
- **Go tests only (O3).** Uniform, but a scene stops being a file an agent can
  write, a reviewer can read without the harness API, and the gallery can
  render. Most scenes are a query, a focus knob and a capture; making those Go
  is weight without a payoff.
- **A JSON header line instead of a markdown document.** One format to parse,
  but the tour's SQL is multi-line and a description wants prose; both are
  what the applet book already solved.
- **Shell hooks in the scene document.** Would absorb fixture generation and
  every odd precondition at once, and with them the portability and review
  problems this ADR exists to remove.
- **A shared bash library.** Removes the copying and none of the rest: no
  assertions in the trace, no dry run across scenes, no library for the
  computed scenes, and bash remains the language of port allocation and
  process supervision.

## Consequences

### Positive

- A scene is a document: prose, a query, a trace. An agent's working steps
  become one by being saved.
- The decisions the copies disagree on are made once, and a failure no longer
  leaks a host.
- Assertions report what was read and what was expected, from the executor
  that read it.
- Computed oracles sit next to the widget they check, in the lane built for
  tests that need a live dependency.

### Negative

- The step vocabulary grows by two verbs and two fields, and a second executor
  would owe all of them.
- Two ways to write a scene. The line between them — "is the oracle a
  computation" — is a judgement, and a declarative scene can outgrow its tier.
- The runner is a process supervisor in Go: ports, children, timeouts,
  teardown. Less code than the copies, but it is the code that must not leak.
- Scene documents are a third markdown-with-fences dialect beside applet books
  and help books, sharing a convention rather than a parser contract.

### Neutral

- Scenes remain best-effort across UI change: an anchor that stops resolving
  fails loudly, which is the intent.
- `imzero2 drive` is unchanged and remains the tool for a host that is already
  running; `scene` is `drive` plus the launch.

## Migration — Tier 1

- **Breaks.** Nothing until M3, which removes two `dev` subcommands, and M4,
  which removes the scene scripts.
- **Path.** Each script is replaced by its documents in the commit that removes
  it; the tour's scene functions translate mechanically (its per-scene
  variables are the frontmatter fields of SD3).
- **Regeneration.** None.
- **Old shape.** Removed outright, per script, once its replacement has run
  green on the same host.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the executor's new verbs, the document
  parser and the runner's supervision logic against a fake carrier. The
  integration lane for SD5 scenes. The migrated scenes themselves, run through
  the runner, for M2 and M4.
- **What would fail.** A migrated scene whose capture or assertion differs from
  its script's; a runner test that finds a child alive after a failed run.
- **Gap.** Scenes that need a provisioned store cannot run everywhere; the
  runner reports them as skipped, and a skip is not evidence.

## Status

Accepted 2026-09-19.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-09-19 — M1–M4 built; three scenes translated but never run

What runs: the tree-widget scene and the play tour as scene documents (56 of
the tour's 73 pass on the machine this was built on, the other 17 skip for an
empty fixture table and 16 of those pass with `--ignoreRequires`; the one that
does not needs a control the fixture's data draws); the portolan camera and
both waveform scenes as integration tests. The scene-specific `dev`
subcommands and eight scripts are gone.

Refinements the build forced, none of them changing a decision:

- **The spec sits under a `scene:` frontmatter key**, parsed strictly, beside
  the repository's own `type` / `audience` / `status`, which doclint requires
  of every markdown file. A misspelt spec key is an error; the keys outside
  are not this parser's.
- **`stepSettleMs`** joined the spec. A scene tuned against one default pause
  keeps it in its own document, where a change to the runner's default cannot
  move it.
- **`--ignoreRequires`** runs a scene whose precondition does not hold — what
  the tour's old fixture-check override did, and how a skip is examined.
- **`expect` compares with a constant only.** Two readings are compared through
  `minus`, which SD1 now says.
- **A failure is rendered with its fields.** What a step read and what it
  expected are fields of the error, so the runner and the gallery print the
  rendered error rather than its message.

One defect found in the driver, and fixed: a read deadline that expired while
a large video frame was still arriving truncated the frame and left the
connection unusable. `wait` and `read` poll on a short deadline, so a scene
that streams big frames and polls often — the portolan one, at 60 fps — hit it
reliably. A frame that has started is now read to its end under its own
deadline; the caller's deadline bounds only the wait for one.

Not done, and why:

- **Three documents have never run.** `tally`, `tally-audio` and the files-pane
  `lading` scene need a lading store, and the machine the migration was done
  on has none. The migration rule above would have kept `tally-scene.sh` until
  its replacement ran green beside it; it was removed with the rest on the
  maintainer's call, so the first run of `apps/tally/scenes` on a provisioned
  machine is a check of the translation as much as of the app. One thing did
  not carry over: the script took its mount, directory and file names from
  `TALLYSCENE_*` variables, and the document names the defaults
  (`scripts/dev/lading-demo.sh` provisions exactly those). The tally-audio
  script survives as the part a document cannot hold — generating the fixture,
  and checking the staging directory afterwards.
- **The completion pane's `kinds` scene fails**, in the document and in the
  script it replaced alike: no `SysMem` row appears for the buffer
  `SELECT LW_COMPONENT('Sys`. Carried over as found; it is the pane's to
  explain, not the runner's.

## References

- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the carrier, the
  driver and the step vocabulary this extends.
- [ADR-0127](./0127-imzero2-interaction-record-replay.md) (proposed) — owns the
  vocabulary's first definition and a second executor, if built.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the one-document
  convention a scene reuses.
- [ADR-0179](./0179-downstream-consumption-gate-and-skeleton.md) — the
  per-checkout launcher cache.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the isolated-widget tour,
  which this does not replace.
- [imzero2-drive skill](../skills/imzero2-drive/SKILL.md) — the page the new
  verbs are taught on.
