---
name: imzero2-drive
description: "Use when operating or verifying a running imzero2 app from a shell — read its accessibility tree, click, type, wait and capture through `imzero2 drive` against a headless host. Covers the tree line format, the step vocabulary, anchoring rules and the pitfalls that make a step miss."
type: how-to
audience: agent or engineer driving a headless imzero2 host from a shell
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Driving an imzero2 app with `imzero2 drive`

`imzero2 drive` connects to a headless host's carrier, reads the accessibility
tree and sends input ([ADR-0154](../../adr/0154-headless-carrier-tree-and-driver.md)).
It is a command, not a session: each invocation connects, runs its steps, prints
what it was asked to print and exits. Put several steps in one invocation
rather than one step in several.

It does not reach the desktop host — that seam is
[egui-mcp](../../howto/egui-mcp.md). Getting a headless host running, with its
ports and its dump directory, is
[launch-apps-non-interactively](../../howto/launch-apps-non-interactively.md);
this page starts once the carrier answers. Below, `drive` stands for
`<host binary> --logLevel=warn imzero2 drive --url ws://127.0.0.1:<port>/`.

## The loop

Look, act and look again in one connection:

```sh
drive --dumpTree --treeText rows                 # what is there
drive --step '{"do":"click","name":"Run","role":"button"}' \
      --step '{"do":"wait","name":"Run","role":"button"}' \
      --dumpTree --treeText rows                 # act, then what it left
```

`--step` repeats and runs in order, after the steps of `--trace <file>` if both
are given. `--dumpTree` prints after the last step has settled. The tree goes to
stdout and the log to stderr, so `2>/dev/null` leaves only the answer.
`--dryRun` resolves every anchor and sends nothing — the cheap check that a
trace still matches the app.

## Reading the tree

```
# tree pass=1714 nodes=3
window "SQL Playground" #6652989512278090663 @566,366
  button "Run" #1892535899870274819 @54,114 [disabled]
  label ="5 rows · 12ms" #10343111975910720320 @237,694
```

Role, then the name in quotes, then the value after `=`, then `#id`, then `@`
the bounds centre in logical points, then flags (`disabled`, `hidden`,
`focused`, `selected`). Everything is spelled the way a step takes it: the
quoted text is a JSON string, the id is what `"id"` reads. Indentation is
nesting among the printed nodes.

Filters, all optional: `--treeText` (name or value contains, case ignored),
`--treeRole`, `--treeUnder <id>` (that node and below), `--treeLimit`,
`--treeHidden`. Unnamed containers and `text_run` duplicates of a label are left
out. The header says `nodes=200 of 640` when the limit cut the list — narrow
the filter rather than reading a partial scene as the whole one.

A script that parses the dump wants `--treeFormat jsonl` instead: one JSON
object per node (`id`, `role`, `name`, `value`, `cx`, `cy`, `x`, `y`, `w`, `h`,
`flags`, `depth`), nothing clipped or rounded, no header, and no limit unless
one is given. The lines format is for reading and may change shape; the JSONL
fields are the ones to build on.

## Steps

One JSON object per step. The anchor fields are `id`, `name`, `contains` (name
substring), `value`, `valueContains`, `role`, `nth`.

| `do` | Reads | Does |
| --- | --- | --- |
| `click` | anchor, or `x`,`y`; `button`, `count`, `modifiers`, `pointer` | AccessKit click on the node; a pointer press when `pointer`, `button` or `count` is set, or when there is no anchor |
| `type` | anchor, `text` | focuses the node, then sends the text |
| `set_value` | anchor, `text` | sets a slider or drag value |
| `focus`, `scroll_into_view` | anchor | |
| `key` | `text` (`Enter`, `Escape`, `ArrowDown`, `A`…), `modifiers` | key down and up to whatever is focused |
| `hover` | `x`,`y` | moves the pointer |
| `drag` | `x`,`y`,`toX`,`toY`; anchored: `x`,`y` is the delta | press, moves, release |
| `scroll` | `x`,`y` as the wheel delta | scrolls under the pointer — `hover` first |
| `wait` | anchor | polls until the node is present and enabled |
| `read` | anchor, `pattern`, `on` | polls until the node's value (or name, with `"on":"name"`) matches the regular expression, then binds every named group `(?P<zoom>[\d.]+)` for the rest of the run |
| `expect` | `of`, `minus`; `eq`, `approx`+`tol`, `min`, `max`, `is`, `matches` | compares a bound name — less another, with `minus` — with a constant; fails with what was read and what was expected |
| `tree` | `text`, `role`, `id` (as *under*) | prints matching nodes mid-run |
| `capture` | `text` as the file name | PNG into the host's `IMZERO2_HEADLESS_DUMP_DIR` |
| `resize`, `cadence`, `sleep`, `note` | see `carrierclient.Step` | |

Coordinate steps (`click`, `hover`, `drag`) also take `xFrom` / `yFrom`: the
names of bound numbers added to `x`,`y` (and `toX`,`toY`). That is how a trace
aims into a painter-only canvas whose origin the app prints — `read` the
origin, then give offsets into the canvas:

```
{"do":"read","valueContains":"canvas at","role":"label","pattern":"canvas at (?P<ox>[\\d.]+),(?P<oy>[\\d.]+)"}
{"do":"click","x":360,"y":230,"xFrom":"ox","yFrom":"oy"}
{"do":"read","valueContains":"zoom","role":"label","pattern":"zoom (?P<z>[\\d.]+)"}
{"do":"expect","of":"z","approx":13,"tol":0.011}
```

Addition is all the arithmetic there is. A check that needs more — a
projection, a period — is a Go test over the same executor (see *Scenes*).

`settleMs` on any step overrides `--settle` (default 250): a pause after the
step, or before it for `capture` and `tree`. `modifiers` is a bitmask: 1 alt,
2 ctrl, 4 shift, 8 mac_cmd, 16 command.

## Anchoring

- **An anchor must match exactly one node.** Several matches is an error that
  lists the candidates; add `role`, use the `id`, or say which with `nth`.
  `tree` and `--dumpTree` are the exception — their fields are a filter.
- **Static text has no name.** egui puts a label's text in the value, so anchor
  it with `value` / `valueContains` plus `"role":"label"`. Buttons, check boxes
  and text inputs carry a name.
- **Ids are stable.** A node id is a function of the widget's path, the same
  across runs and window sizes. Prefer it once you have it, and whenever two
  windows hold the same button (`Close window` is one per window).
- **Text cut with `…` in the tree is cut in the listing only.** Anchor on a
  prefix with `contains` / `valueContains`, not on the clipped string.
- **A node that shows but does not react** is usually text sitting over the
  real sense region (tree rows are the known case). Add `"pointer":true` to
  press where it was drawn.
- **Painter-only widgets have no nodes** — a plot, a map, a timeline's
  interior. Take a `capture`, read positions off it (pixels are logical points
  at scale 1), and use `x`,`y`. These coordinates go stale silently when the
  layout moves; an anchor fails loudly. Use coordinates only where no node
  exists.

## What makes a step miss

- **Looking too early.** Input is not acknowledged. An action's effect reaches
  the tree a pass or two after it is applied, which the settle pause covers for
  anything immediate. For anything slower — a query, a fetch — `wait` on a node
  that marks completion (play disables `Run` while a query is in flight) rather
  than raising `settleMs` until it happens to work.
- **Not being the active connection.** The host honours input and tree requests
  from the active connection only, and the first connection is admitted
  active. Against a host somebody is watching in a browser, the driver does
  nothing.
- **No dump directory.** A `capture` against a host started without
  `IMZERO2_HEADLESS_DUMP_DIR` is ignored and the step times out. The PNG lands
  on the host's filesystem; the driver reports the path.
- **Typing into the wrong widget.** `key` goes to whatever holds focus. `type`
  focuses its anchor first; `key` after a `click` elsewhere does not.
- **A stale headless client.** If the host log shows `unable to convert from
  representation`, the Rust client predates the last codegen; rebuild it before
  suspecting the app.

## Scenes: keeping what worked

`drive` needs a host that is already running. A *scene* is `drive` plus the
launch ([ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md)):
one markdown document, one launch.

````
---
type: reference
audience: contributor
status: draft
scene:
  launch: play                 # the app alias
  size: 1600x1000
  env: {BOXER_PLAY_FOCUS_TABLE: "1", BOXER_PLAY_AUTORUN: "1"}
  requires: [clickhouse, "table:default.some_table"]   # unmet → skipped, not failed
---

# What the scene shows

Prose: it becomes the gallery entry.

```sql
SELECT 1 AS a          -- seeds the app's buffer (BOXER_PLAY_SQL unless `sqlEnv` says otherwise)
```

```jsonl trace
{"do":"wait","name":"Run"}
{"do":"capture","text":"my-scene"}
```
````

```sh
scripts/dev/scene.sh apps/play/scenes                  # a directory is a tour, in name order
scripts/dev/scene.sh --only history apps/play/scenes
scripts/dev/scene.sh --dryRun path/to/x.scene.md       # resolve anchors, capture nothing
```

The runner allocates ports, picks a client that can do what the scene needs,
refuses one older than the generated interpreter, tears the host down on every
exit path, and writes `index.md` with each scene's prose, captures and trace.
Exit status is the assertion. So: steps that worked under `drive` become a
maintained scene by being pasted into a `jsonl trace` fence.

Other spec keys: `fps`, `needs: [raster]` (implied by a `capture`), `services:
[tilestub]` for a map scene, `settleMs` (held before the first step),
`stepSettleMs` (the default pause after a step). A document cannot run
commands; preconditions and services are names the runner knows.

A scene whose oracle is a computation is a Go test instead, in the
`//go:build integration` lane, over the same harness: `scenetest.Launch`, trace
fragments through `scenetest.Run`, readings back out through
`scenetest.Number`. The portolan and waveform widget packages hold the examples.
