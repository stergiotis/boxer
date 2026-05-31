---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to triage a frozen imzero2 demo (FFFI2 deadlock)

Diagnose a hung `./rust/imzero2/hmi.sh` (or any imzero2-based app — imztop,
regex_explorer, leewaywidgets) when the Go server and the Rust client
have both gone idle. The recipe pinpoints the offending FFFI2 opcode
without bisecting widget code. Out of scope: render-side hangs where a
Rust thread is still busy (use puffin or `gdb -p` for those).

## When to use this recipe

The hmi window has stopped updating. `top` shows both `main_go` and
`pebble2_rust` alive but using 0 % CPU. The Go process exposes
`localhost:6060/debug/pprof/`. Symptoms that indicate this is the
right recipe rather than a render hang:

- `pprof/profile?seconds=5` reports `Total samples = 0` (Go is fully
  idle, not spinning).
- `pprof/goroutine?debug=2` shows goroutine 1 deep inside
  `(*Fetcher).FetchR*` → `readU64h`/`readU32h` →
  `Unmarshaller.readBuf`, blocked for many minutes.
- Every Rust thread is in `futex_do_wait` / `ep_poll` *except* the
  main thread, which is in `anon_pipe_read` (syscall 0).

That is the classic Go↔Rust FFFI2 protocol-desync deadlock: both
processes are blocked on opposite-direction `read()` calls. The bug is
on the Go write side — some widget under-supplied bytes for an opcode,
and Rust is patiently waiting for them.

## Prerequisites

- The Go process was launched with `--pprofHttpListenAddress
  localhost:6060` (hmi.sh sets this by default).
- Bash, `curl`, `awk`, and `go tool pprof` available.
- Read access to `/proc/<pid>/{maps,task,wchan}` (the running user's
  own processes — no extra privileges needed).

## Steps

1. **Identify the two processes.** They share a parent shell (hmi.sh);
   the Go server is the parent of the Rust client.

   ```bash
   ps aux | grep -E "main_go|pebble2_rust" | grep -v grep
   ```

   Capture both PIDs into `GO_PID` and `RUST_PID`.

2. **Confirm the deadlock shape via Go's goroutine dump.** Goroutine 1
   should be inside `Unmarshaller.readBuf` called from a `FetchR*`
   inside `StateManager.Sync` for many minutes. CPU profile should
   show zero samples.

   ```bash
   curl -s "http://localhost:6060/debug/pprof/goroutine?debug=2" -o /tmp/goroutines.txt
   grep -B1 -A12 "Unmarshaller\|FetchR\|StateManager.*Sync" /tmp/goroutines.txt

   timeout 7 curl -s "http://localhost:6060/debug/pprof/profile?seconds=5" -o /tmp/cpu.pprof
   go tool pprof -top -nodecount=10 /tmp/cpu.pprof    # expect: Total samples = 0
   ```

3. **Confirm the Rust client is also idle.** All render / disk / GL
   threads should be sleeping; main should be reading the pipe.

   ```bash
   for tid in $(ls /proc/$RUST_PID/task); do
       printf "tid=%s name=%s wch=%s\n" "$tid" \
           "$(cat /proc/$RUST_PID/task/$tid/comm)" \
           "$(cat /proc/$RUST_PID/task/$tid/wchan)"
   done | head -20
   ```

   If any thread is busy (not `futex_do_wait` / `ep_poll` /
   `anon_pipe_read`), this recipe does not apply — it's a render-side
   hang, not an FFFI2 deadlock.

4. **Look for a sentinel-sized virtual mapping in the Rust process.**
   This is the highest-value step and the one a generic pprof
   workflow would miss. A mapping near 4 GiB, 8 GiB, 16 GiB, or 32 GiB
   is a near-certain fingerprint of the nil-slice sentinel bug.

   ```bash
   awk '/^[0-9a-f]+-[0-9a-f]+/{
     split($1, a, "-");
     size=(strtonum("0x"a[2]) - strtonum("0x"a[1]))/1024/1024;
     if (size > 1024) print size " MB " $0
   }' /proc/$RUST_PID/maps | sort -rn | head
   ```

   Decode the mapping size into the implicated element type:

   | Mapping size | Element type             | Implicated Rust reader                  |
   |--------------|--------------------------|------------------------------------------|
   | ~4100 MB     | `u8` / `i8` (1-byte)     | `read_plain_u8h` / `read_plain_i8h`     |
   | ~8200 MB     | `u16` / `i16` (2-byte)   | `read_plain_u16h` / `read_plain_i16h`   |
   | ~16400 MB    | `u32`/`i32`/`f32` (4-byte) | `read_plain_u32h` / `read_plain_f32h` |
   | ~32800 MB    | `u64`/`i64`/`f64` (8-byte) | `read_plain_u64h` / `read_plain_f64h` |

   The arithmetic: Go's `WriteNilSlice()` writes
   `math.MaxUint32 = 0xFFFFFFFF`. Rust's `read_plain_*h` reads it as a
   length and calls `Vec::with_capacity(0xFFFFFFFF)` — which succeeds
   on virtual memory (RSS stays small) but then blocks reading 4
   billion elements that will never come.

5. **Localise the offending opcode.** Each `read_plain_*h` callsite
   in the interpreter belongs to one `FuncProcId::*` arm. Find the
   candidates for the implicated element type:

   ```bash
   grep -n "read_plain_u8h" rust/imzero2/src/imzero2/interpreter.rs    # adjust suffix
   ```

   For each hit, read ~20 lines above to see which `FuncProcId::*`
   arm it sits in. Then find the Go factory:

   ```bash
   grep -n "WriteOpCode.*FuncProcIdDockAreaRaw\b" \
       public/thestack/imzero2/egui2/bindings/factories.out.go
   ```

   The factory's parameter list tells you the arg names. The nil-prone
   arg is the slice whose Rust counterpart is `read_plain_*h` of the
   element type you matched.

6. **Find the Go callsite that passed nil.** Most user-facing widgets
   wrap the raw factory through a fluent helper (e.g. `DockArea` wraps
   `DockAreaRaw`). Inspect the helper for an early-return that hits a
   named-return slice without assigning. The canonical pattern (fixed
   in commit `b47f5458`):

   ```go
   func (inst *Foo) encodeBlob() (out []byte) {
       if /* nothing to encode */ {
           return    // ← out is nil, sentinel-bound on the wire
       }
       ...
   }
   ```

7. **Apply the fix.** Two equally valid approaches:

   - **Single callsite** (preferred for hot-path widget code):

     ```go
     if /* nothing to encode */ {
         out = []byte{}    // explicit empty, length-0 prefix on the wire
         return
     }
     ```

   - **Defensive Rust** (broader, but a wire-format change): make
     `read_plain_*h` treat `0xFFFFFFFF` as empty. Do not land this
     unilaterally — coordinate first; the asymmetry is currently
     intentional.

## Verification

After applying the fix, kill both processes and relaunch:

```bash
kill $GO_PID $RUST_PID
./rust/imzero2/hmi.sh
```

The Go binary rebuilds incrementally; Rust does not need a rebuild if
only the Go-side write was changed (the protocol contract is
unchanged — Go just stops emitting the sentinel). The carousel should
advance past the demo that previously triggered the hang.

To regression-guard, add a frame-budget test that constructs the
suspect widget with the empty-input shape and asserts the wire encodes
a length-0 prefix for the implicated slice arg.

## Troubleshooting

- **Symptom:** no mapping >1024 MB appears in step 4.
  **Cause:** the desync is not a nil-slice sentinel — likely either
  codegen drift (Go and Rust binaries built from different
  `enums.out.go` / `enums_out.rs` revisions) or a length-prefixed
  string whose body got truncated mid-write.
  **Fix:** check `stat -c %y enums.out.go enums_out.rs` — they should
  share a mtime. If not, re-run `./generate.sh` and rebuild both
  sides. Otherwise, see `reference_fffi_desync_diagnostic` for the
  frame-trail ring + read_plain_s hex dump that surfaces the other
  desync classes.

- **Symptom:** Rust thread is busy (CPU profile non-zero, render or
  GL thread not in futex).
  **Cause:** This is a render-side hang, not an FFFI2 deadlock.
  **Fix:** Out of scope for this recipe — use puffin (`puffin-server`
  thread is already running) or `gdb -p $RUST_PID` to inspect the
  busy thread's stack.

- **Symptom:** Go goroutine 1 is in `select` or a logbridge call
  rather than `Unmarshaller.readBuf`.
  **Cause:** Go-side bug independent of the FFFI2 protocol — for
  example a blocking channel send with no receiver.
  **Fix:** Out of scope — apply ordinary goroutine-dump triage.

## See also

- `feedback_fffi2_nil_slice_sentinel` (in `MEMORY.md`) — the
  underlying footgun this recipe detects.
- `reference_fffi_desync_diagnostic` — frame_trail ring +
  read_plain_s hex dump for the other desync class.
- `.claude/skills/triage-imzero2-hang/SKILL.md` — Claude Code skill
  form of this recipe (invocable as `/triage-imzero2-hang`).
