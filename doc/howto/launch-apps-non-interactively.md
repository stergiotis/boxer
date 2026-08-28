---
type: how-to
audience: engineer or agent who needs a running keelson app without a desktop — to script it, screenshot it, or check what the runtime does
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to launch apps non-interactively and drive them headlessly

You want one or more keelson apps running with no compositor and no
display — to script a scenario, capture a frame, or watch what the runtime
does while a window opens and closes — and you want to query and steer the
running process from a shell. This page is the recipe the `imzero2 demo`
help text refers to. It composes three things that exist separately:
`--launch` ([ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md) M3
for the bare-alias form, [ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)
for the SQL form), the headless carrier ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md))
with its driver ([ADR-0154](../adr/0154-headless-carrier-tree-and-driver.md)),
and the introspection tables over HTTP ([ADR-0094](../adr/0094-keelson-introspection-tables.md)).
The worked example at the end is the one used to verify the closing edge of
[ADR-0188](../adr/0188-app-instance-effect-tracking.md) on 2026-08-15.

Caveats first: the headless Rust client must be built and must not be older
than the last egui2 codegen (a stale client desyncs the FFFI wire on
whichever opcode moved, which reads like an app bug); `POST /query` needs
the `clickhouse` binary on the PATH; and this box routinely runs other sessions'
hosts, so pick your own ports and kill only your own PIDs.

## 1. Build the host into a scratch directory

The host is `./public/thestack/cmd/imzero2/` with the `binary_log` tag —
not `./public/app`, which builds but has no `imzero2` subcommand:

```sh
S=<your scratch dir>
CGO_ENABLED=0 go build -tags "$(tr -d '\n' < ./tags),binary_log" \
  -o "$S/main_go" ./public/thestack/cmd/imzero2/
```

Do not overwrite `rust/imzero2/main_go`: it belongs to whoever is running
the desktop from this checkout. The headless client is
`rust/imzero2/target/headless/release/imzero2`, built by
[rust/imzero2/build_rust_headless.sh](../../rust/imzero2/build_rust_headless.sh);
if `rust/imzero2/src/imzero2/interpreter.rs` or `enums_out.rs` is newer
than it, rebuild it first.

## 2. Pick the apps

```sh
"$S/main_go" imzero2 demo --list                       # every registered app, one row each
"$S/main_go" imzero2 demo --list --list-format Markdown
```

`--launch` takes either a bare alias (`play`) or a SQL `WHERE` clause over
the `--list` table, evaluated with `clickhouse local`:

```sh
--launch play
--launch "subject_alias IN ('play','capdemo','taskdemo')"
--launch "has(topics, 'observability') AND kind = 'app'"
```

Every matching app opens as its own window through the window host, which
is the path that reads the `BOXER_PLAY_*` seed variables
([doc/env-vars.md](../env-vars.md)); the standalone `play` CLI does not.

## 2b. Seed an app's launch config

`--launch` opens each matching app with no config, the way the Apps menu
does. Two ways carry a config into a seeded window:

- **`--launch-config <alias>=<path>`** (repeatable). The file holds the
  config encoded for the app's manifest `LaunchKind` (the same bytes a
  `windowhost.open` request would carry, [ADR-0135](../adr/0135-app-launch-requests.md)
  §SD2); the kind's registered probe validates it at open and the window
  mounts with `LaunchReasonCaller`. A failed open is a boot error, unlike
  an unresolved `--launch` ref, because the config was asked for
  explicitly. This is `hostboot.SeedWindow` at the command line
  ([ADR-0211](../adr/0211-hostboot-runtime-bootstrap.md) §SD3).
- **Env seeds.** An app may register its own seed variables through the
  environment registry ([ADR-0009](../adr/0009-environment-variable-registry.md);
  `env.NewString` / `NewFloat` / `NewPath` with a `CliFlagName`) and read
  them in `Mount` when no caller config arrived — `play` does this with
  `BOXER_PLAY_*`. Precedence is the [ADR-0148](../adr/0148-app-workingsets.md)
  §SD5 order: caller config, then env seeds, then a restored workingset,
  then the app's defaults. `boxer env list` (or the adopter's equivalent)
  documents what a given binary honours.

## 3. Launch headless

```sh
PORT=47311      # carrier: WebSocket on PORT, viewer page on PORT+1
IPORT=47320     # introspection HTTP — must not be PORT or PORT+1
env -u DISPLAY -u WAYLAND_DISPLAY \
  IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" \
  IMZERO2_HEADLESS_DUMP_DIR="$S/out" IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
  IMZERO2_HEADLESS_FPS=20 IMZERO2_SCREENSHOT_SIZE=1400x900 \
  KEELSON_INTROSPECT_HTTP_LISTEN="127.0.0.1:$IPORT" \
  BOXER_COMPONENT=my-scenario \
  BOXER_PLAY_WINDOW_SIZE=900x600 \
  BOXER_PLAY_SQL="SELECT app_id, instance_key, pattern FROM keelson('subscriptions') WHERE NOT is_inbox ORDER BY 1, 2" \
  BOXER_PLAY_AUTORUN=1 \
  "$S/main_go" --logFormat=console --logLevel=info imzero2 demo \
    --clientBinary rust/imzero2/target/headless/release/imzero2 \
    --clientInitialMainWindowWidth 1400 --clientInitialMainWindowHeight 900 \
    --mainFontTTF "$MAIN_FONT" --monoFontTTF "$MONO_FONT" \
    --phosphorFontTTF rust/imzero2/assets/fonts/phosphor/Phosphor.ttf \
    --launch "subject_alias IN ('play','capdemo','taskdemo')" \
  > "$S/host.log" 2>&1 &
echo $! > "$S/host.pid"
```

Fonts: resolve `$MAIN_FONT` / `$MONO_FONT` with `fc-match` the way
[scripts/dev/play-screenshot-tour.sh](../../scripts/dev/play-screenshot-tour.sh)
does (`Noto Sans`, `DejaVu Sans Mono`); the tour script is the maintained
reference for this whole launch line. `IMZERO2_HEADLESS_DUMP_EVERY` is
pushed out of the way so nothing lands in the dump directory except what a
trace asks for. Wait for the carrier before doing anything else:

```sh
for _ in $(seq 1 120); do
  (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
  kill -0 "$(cat "$S/host.pid")" 2>/dev/null || { echo "host died — read $S/host.log"; break; }
  sleep 0.5
done
```

The two failure modes seen so far both surface in `host.log`: `AddrInUse`
from the Rust client (a port collision — the carrier owns `PORT` *and*
`PORT+1`), and `unable to convert from representation` (a stale headless
client).

## 4. Query the running process

The introspection endpoint speaks the ClickHouse HTTP dialect, so a
`FORMAT` clause works:

```sh
q() { curl -sS --max-time 20 "http://127.0.0.1:$IPORT/query" --data-binary "$1"; }
q "SELECT key, app_id, title FROM keelson('windows') FORMAT PrettyCompactMonoBlock"
q "SELECT app_id, instance_key, pattern FROM keelson('subscriptions') WHERE NOT is_inbox ORDER BY 1, 2 FORMAT PrettyCompactMonoBlock"
```

`keelson('tables')` lists everything the host serves. `keelson('windows').key`
is `Int64` while the effect tables' `instance_key` is `UInt64`; cast on the
join (`toUInt64(w.key)`) — the canonical queries are in
[topology-queries § What does an open window hold right now?](./topology-queries.md#what-does-an-open-window-hold-right-now).

## 5. Steer it

The driver reads the accessibility tree and synthesises input, replaying a
JSON Lines trace (one step per line, `#` comments allowed):

```sh
"$S/main_go" imzero2 drive --url "ws://127.0.0.1:$PORT/" --dumpTree     # every node: id, name, role, value, centre
```

Anchor a step by an exact `id` from the dump, or by `name` / `contains`
plus `role`; egui window title bars expose their ✕ as a button named
`Close window`, one per window, so with several windows use the id. Dock
tabs are buttons named by their title (`{"do":"click","name":"Controls",
"role":"button"}` switches to that tab) — egui_dock registers no node for a
tab on its own; the host adds the label from its tab viewer. Ids
print signed in the dump but the trace field is unsigned — convert a
negative id by adding 2⁶⁴ (`python3 -c 'print(2**64 + (ID))'`).

```sh
printf '%s\n' '{"do":"click","name":"Start steps task","settleMs":1500}' > "$S/t1.jsonl"
"$S/main_go" imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$S/t1.jsonl" --settle 500
printf '%s\n' '{"do":"click","id":14343085323791920335,"settleMs":2500}' > "$S/t2.jsonl"   # that window's "Close window"
"$S/main_go" imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$S/t2.jsonl" --settle 500
```

`capture` steps write PNGs into `IMZERO2_HEADLESS_DUMP_DIR`; the full verb
list (`click`, `hover`, `drag`, `type`, `set_value`, `focus`,
`scroll_into_view`, `key`, `scroll`, `wait`, `capture`, `cadence`, `resize`,
`note`, `sleep`) is documented on `carrierclient.Step`. `drag` presses at
`x`,`y`, moves to `toX`,`toY` in `steps` moves over `durationMs` and releases
— the gesture for a map pan, a plot brush or a slider; anchored on a node it
starts at the node's centre and reads `x`,`y` as the delta.

## 6. Worked example — the closing edge leaves nothing behind

Launched as in §3 with `play`, `capdemo`, `taskdemo`, then in order:
`Start steps task` clicked in taskdemo (window key 3), the query below,
taskdemo's `Close window` clicked, the query again. Output on 2026-08-15:

```sh
q "SELECT 'task' AS effect, concat(kind, ' ', task_id, ' [', state, ']') AS what
     FROM keelson('tasks') WHERE owner_instance_key = 3
   UNION ALL SELECT 'subscription', pattern FROM keelson('subscriptions') WHERE instance_key = 3
   UNION ALL SELECT 'cap', concat(pattern, ' declared=', toString(declared)) FROM keelson('client_caps') WHERE instance_key = 3
   FORMAT PrettyCompactMonoBlock"
```

Before the close:

```
subscription │ task.>
subscription │ task.5qZknmZb2wP82MOAB1OBX.cancel
task         │ demo.steps 5qZknmZb2wP82MOAB1OBX [running]
cap          │ task.> declared=true
```

After it: no rows — the host released the subscription the app never
released, the client and its cap, and the cascade-cancelled task announced
its end so the supervisor dropped it. `keelson('windows')` no longer lists
key 3. This is the property the interleaving lane
(`TestClosingEdge_InterleavingLeavesNoTrace`) checks after every step; the
page you are reading is the by-hand version.

## 6b. Worked example — the dataset binder's reconcile, with events lost

The dataset binder (ADR-0188 §SD3) treats the service's events as hints and
a slow reconcile tick as truth, so it survives a transport that loses
events. To watch the tick do the work, drop the events with the
fault-injection knob and shorten the tick:

```sh
BOXER_SQLAPPLET_DATASET_EVENTS=drop BOXER_SQLAPPLET_DATASET_RECONCILE=40s   … --launch "subject_alias IN ('play','imzrt','profile-heap')"
```

`profile-heap` declares `datasets: [pprof_heap]` and opens with the notice
"Waiting for dataset `pprof_heap`…"; imzrt's Profiles tab publishes it. The
dock tab strip is not in the accessibility tree, so the tab is a raw
pointer click (its position from a `capture` step, here 318,149 at
1500×950); the button is anchored by name:

```sh
printf '%s
' '{"do":"click","x":318,"y":149,"settleMs":600}'                '{"do":"click","name":"Capture Heap","settleMs":600}' > "$S/e.jsonl"
"$S/main_go" imzero2 drive --url "ws://127.0.0.1:$PORT/" --trace "$S/e.jsonl" --settle 200
```

Then poll `--dumpTree` for the notice every two seconds. On 2026-08-15:
applet mounted 22:33:26, dataset published 22:33:30, the notice stayed for
38 s, and the applet bound at 22:34:08 — the first tick after mount, the
`sqlapplet: dataset alias bound after open` line in `host.log` giving the
instant. The control run with `BOXER_SQLAPPLET_DATASET_EVENTS=on` bound in
the same second as the publish (22:34:47 → 22:34:47), and a second
`Capture Heap` bumped `keelson('adhoc').revision` to 2, which the applet
takes as a re-run hint. Two earlier attempts landed the publish within a
second of a tick boundary and proved nothing — pick an interval long enough
that the tick is unmistakable.

## 7. Clean up

Kill your host and its Rust child by PID; never by name — several sessions
run the same binaries on this box, and a `pkill` by comm or cmdline takes
someone else's desktop down with yours:

```sh
pid=$(cat "$S/host.pid"); kill "$pid" $(pgrep -P "$pid") 2>/dev/null
```

Wait for `PORT` to free before reusing it: the Rust client owns the carrier
socket and outlives the Go host by a moment; a new launch on a still-bound
port attaches the driver to the old scene.

## Related

- [scripts/dev/play-screenshot-tour.sh](../../scripts/dev/play-screenshot-tour.sh)
  — the maintained, multi-scene form of §3–§5 for play.
- [topology-queries](./topology-queries.md) — the canonical queries over the
  introspection tables, including the effect tables.
- [ADR-0154](../adr/0154-headless-carrier-tree-and-driver.md) — the driver
  and trace format; [ADR-0127](../adr/0127-imzero2-interaction-record-replay.md)
  — the anchor ladder the steps use.
- [ADR-0188](../adr/0188-app-instance-effect-tracking.md) — what §6 shows.
