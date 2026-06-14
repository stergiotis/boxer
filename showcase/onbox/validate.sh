#!/usr/bin/env bash
# ADR-0085 deploy-logic validation harness — checks A1..A8 in VALIDATION.md.
#
# Exercises the REAL pipeline (fetch/verify/checkout/build/gate/swap/health/
# rollback/prune) against a THROWAWAY local remote with controlled signed/
# unsigned tags, a temp deploy root, and a `systemctl` shim that backgrounds the
# real run-current.sh — so no real systemd is touched. Run on a build-capable
# box from anywhere in the boxer checkout:
#
#   ./showcase/onbox/validate.sh
#   VALIDATE_ONLY="A2 A1" ./.../validate.sh    # run a subset (each check is self-contained)
#
# Builds are real but incremental: all test tags point at HEAD and the cargo
# headless target/ is seeded (hardlinked). Needs: the toolchain, git, ssh-keygen,
# go (A8), and python3 (A6 only). Checks are independent — each resets the root.
set -u

BOXER="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
# WORK must share a filesystem with BOXER so --local clones + the cargo seed can
# hardlink (cross-device hardlinks fail). Place it beside the repo, not in /tmp.
WORK="$(mktemp -d "$(dirname "$BOXER")/.imz-validate.XXXXXX")"
REMOTE="$WORK/remote.git"; WS="$WORK/ws"; ROOT="$WORK/root"
SHIMDIR="$WORK/bin"; PIDFILE="$WORK/demo.pid"
# LIVE_PORT and SCRATCH_PORT must differ by >=2: each carrier binds P (ws) AND
# P+1 (its viewer page), so adjacent values collide (live's P+1 == scratch's P)
# and the gate would probe the live service instead of the candidate.
LIVE_PORT="${LIVE_PORT:-18091}"; SCRATCH_PORT="${SCRATCH_PORT:-18095}"
# Pick an h264 encoder present on THIS box (the live service sets
# IMZERO2_HEADLESS_ENCODER_ARGS the same way). Fedora ffmpeg ships libopenh264,
# not libx264 (GPL); override with ENC=... to force one.
if [ -z "${ENC:-}" ]; then
  if ffmpeg -hide_banner -encoders 2>/dev/null | grep -q ' libx264 '; then
    ENC="-c:v libx264 -preset veryfast -tune zerolatency -bf 0 -g 100000"
  else
    ENC="-c:v libopenh264 -b:v 4M -bf 0 -g 60"
  fi
fi
KEEP=2
# A static carousel under reactive cadence may emit only the initial keyframe,
# so require just 1 AU — enough to tell a working encoder (>=1) from a dead one (0).
GATE_AUS=1
DEPLOY="$BOXER/rust/imzero2/main_go"
ONLY="${VALIDATE_ONLY:-A2 A3 A1 A4 A6 A7 A5}"

pass=0; fail=0
ok()   { echo "PASS: $*"; pass=$((pass+1)); }
bad()  { echo "FAIL: $*"; fail=$((fail+1)); }
note() { echo; echo "--- $* ---"; }
want() { case " $ONLY " in *" $1 "*) return 0;; *) return 1;; esac; }
curtag() { basename "$(readlink "$ROOT/current" 2>/dev/null || echo none)"; }
mktag()  { local n; n=$(( $(cat "$WORK/.tagn" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$WORK/.tagn"; printf 'v9000.0.%d' "$n"; }  # file counter: survives $(...) subshells
signed()   { local t; t="$(mktag)"; git -C "$WS" tag -s "$t" -m signed >/dev/null && git -C "$WS" push -q origin "$t" && echo "$t"; }
unsigned() { local t; t="$(mktag)"; git -C "$WS" tag    "$t"            >/dev/null && git -C "$WS" push -q origin "$t" && echo "$t"; }

cleanup() {
  # Reap everything this run spawned: the live service AND any gate-candidate
  # carriers all run from under $WORK (a unique path), so match on it. Killing
  # only the PIDFILE leaks gate carriers — they squat the scratch/live ports and
  # make a later run's gate or health probe decode a stranger's stream.
  pkill -KILL -f "$WORK" 2>/dev/null
  [ -f "$PIDFILE" ] && kill -KILL "$(cat "$PIDFILE")" 2>/dev/null
  if [ -n "${KEEP_WORK:-}" ]; then echo "work preserved: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

deploy() { # extra args... ; runs with the systemctl shim on PATH
  # GOWORK=off: WORK sits beside the repo (for hardlinks), which may be under an
  # ambient go.work in a multi-repo checkout; a real box's workspace is not.
  GOWORK=off PATH="$SHIMDIR:$PATH" "$DEPLOY" --logFormat=console --logLevel=info imzero2 deploy \
    --root "$ROOT" --workspace "$WS" --remote origin --service demo.service \
    --live-port "$LIVE_PORT" --scratch-port "$SCRATCH_PORT" --gate-timeout 30s \
    --gate-aus "$GATE_AUS" --keep "$KEEP" --encoder-args "${DEPLOY_ENC:-$ENC}" "$@"
}

setup() {
  note "setup: deploy tool + systemctl shim + throwaway remote"
  # Reap carriers leaked by a prior interrupted run: they squat the live/scratch
  # ports and would poison this run's gate/health probes. Only prior runs match
  # (this run's WORK has no processes yet).
  pkill -KILL -f '\.imz-validate\.' 2>/dev/null && sleep 1 || true
  # Always (re)build: a stale main_go may predate the `deploy` subcommand.
  ( cd "$BOXER/rust/imzero2" && ./build_go.sh ) >"$WORK/build_go.log" 2>&1 || { echo "cannot build main_go (see $WORK/build_go.log)"; exit 1; }
  mkdir -p "$SHIMDIR"
  cat > "$SHIMDIR/systemctl" <<SHIM
#!/bin/sh
# Record the session-leader PID (== PGID) and SIGKILL the whole group, so the
# Rust carrier + ffmpeg die with main_go and release the live port. Killing
# only main_go orphans the carrier (it keeps the port and defeats A6).
case "\$1" in
  restart|stop) [ -f "$PIDFILE" ] && kill -KILL -- -"\$(cat "$PIDFILE")" 2>/dev/null ;;
esac
[ "\$1" = restart ] || exit 0
[ -f "$WORK/.break_restart" ] && { sleep 1; exit 0; }   # A6: failed start; settle so the port frees
setsid sh -c 'echo \$\$ > "$PIDFILE"; exec env IMZERO2_CURRENT="$ROOT/current" IMZERO2_HEADLESS_LISTEN="127.0.0.1:$LIVE_PORT" IMZERO2_HEADLESS_ENCODER_ARGS="$ENC" LIBGL_ALWAYS_SOFTWARE=1 LOG_LEVEL=warn "$BOXER/showcase/onbox/run-current.sh"' >"$WORK/demo.log" 2>&1 &
exit 0
SHIM
  chmod +x "$SHIMDIR/systemctl"
  ssh-keygen -t ed25519 -f "$WORK/signer" -N '' -C release@test -q
  printf 'release@test %s\n' "$(cat "$WORK/signer.pub")" > "$WORK/allowed"
  git clone --quiet --bare --local "$BOXER" "$REMOTE"
  git clone --quiet --local "$REMOTE" "$WS"
  git -C "$WS" config gpg.format ssh
  git -C "$WS" config user.signingkey "$WORK/signer"
  git -C "$WS" config user.email release@test
  git -C "$WS" config user.name validator
  git -C "$WS" config gpg.ssh.allowedSignersFile "$WORK/allowed"
  if [ -d "$BOXER/rust/imzero2/target/headless" ]; then
    mkdir -p "$WS/rust/imzero2/target"
    # copy (not hardlink): the workspace build mutates target/ and must not
    # touch the real repo's cache.
    cp -a "$BOXER/rust/imzero2/target/headless" "$WS/rust/imzero2/target/headless" 2>/dev/null || true
  fi
}

main() {
  setup

  if want A2; then note "A2 — unsigned/newest tag refused BEFORE build (no build)"
    rm -rf "$ROOT"; unsigned >/dev/null
    if deploy >"$WORK/A2.log" 2>&1; then bad "A2: unsigned not refused"; tail -2 "$WORK/A2.log"
    elif grep -q "no valid signature" "$WORK/A2.log" && [ "$(curtag)" = none ]; then ok "A2: refused, current untouched"
    else bad "A2: wrong failure / current=$(curtag)"; tail -3 "$WORK/A2.log"; fi
  fi

  if want A3; then note "A3 — dev escape builds an unsigned tag [REAL BUILD]"
    rm -rf "$ROOT"; t="$(unsigned)"
    if deploy --require-signed-tags=false >"$WORK/A3.log" 2>&1 && [ "$(curtag)" = "$t" ]; then
      grep -q "verification DISABLED" "$WORK/A3.log" && ok "A3: built + warned (current=$t)" || bad "A3: no disabled-warning"
    else bad "A3: did not deploy (current=$(curtag); see $WORK/A3.log)"; fi
  fi

  if want A1; then note "A1+A8 — signed tag deploys, revision agrees [REAL BUILD]"
    rm -rf "$ROOT"; t="$(signed)"
    if deploy >"$WORK/A1.log" 2>&1 && [ "$(curtag)" = "$t" ]; then ok "A1: live, current=$t"
      want_c="$(git -C "$WS" rev-parse "$t^{commit}")"
      got_c="$(go version -m "$ROOT/current/main_go" 2>/dev/null | sed -n 's/.*vcs.revision=\([0-9a-f]*\).*/\1/p')"
      [ -n "$got_c" ] && case "$want_c" in "$got_c"*) ok "A8: vcs.revision matches ($got_c)";; *) bad "A8: want=$want_c got=$got_c";; esac \
        || bad "A8: could not read vcs.revision"
    else bad "A1: failed / current=$(curtag) (see $WORK/A1.log)"; fi
  fi

  if want A4; then note "A4 — no-op when already current [REAL BUILD once]"
    rm -rf "$ROOT"; signed >/dev/null; deploy >/dev/null 2>&1
    if deploy >"$WORK/A4.log" 2>&1 && grep -q "already current" "$WORK/A4.log"; then ok "A4: no-op"
    else bad "A4: did not no-op (see $WORK/A4.log)"; fi
  fi

  if want A6; then note "A6 — rollback when the post-restart service fails to come up [REAL BUILD ×2]"
    rm -rf "$ROOT"; signed >/dev/null; deploy >/dev/null 2>&1; prev="$(curtag)"
    touch "$WORK/.break_restart"   # make the next restart simulate a failed start
    signed >/dev/null
    if deploy >"$WORK/A6.log" 2>&1; then bad "A6: deploy succeeded despite a failed restart"
    elif [ "$(curtag)" = "$prev" ] && grep -qiE "roll(ed|ing) back" "$WORK/A6.log"; then ok "A6: rolled back to $prev"
    else bad "A6: current=$(curtag) prev=$prev (see $WORK/A6.log)"; fi
    rm -f "$WORK/.break_restart"
  fi

  if want A7; then note "A7 — retention keeps K=$KEEP (+ current) [REAL BUILD ×4]"
    rm -rf "$ROOT"; for _ in 1 2 3 4; do signed >/dev/null; deploy >/dev/null 2>&1; done
    n="$(find "$ROOT/releases" -mindepth 1 -maxdepth 1 -type d ! -name '*.staging' 2>/dev/null | wc -l)"
    { [ "$n" -ge 1 ] && [ "$n" -le $((KEEP+1)) ]; } && ok "A7: retained=$n (1..$((KEEP+1)))" || bad "A7: retained=$n (want 1..$((KEEP+1)))"
  fi

  if want A5; then note "A5 — gate blocks a non-streaming build [REAL BUILD]"
    rm -rf "$ROOT"; signed >/dev/null
    DEPLOY_ENC="-c:v not_a_real_codec"   # normal var (env-prefix on a function doesn't propagate)
    if deploy >"$WORK/A5.log" 2>&1; then bad "A5: broken encoder passed the gate"
    elif [ "$(curtag)" = none ]; then ok "A5: gate blocked, current untouched"
    else bad "A5: current changed to $(curtag)"; fi
    DEPLOY_ENC=""
  fi

  echo; echo "==== $pass passed, $fail failed (ran: $ONLY) ===="; [ "$fail" -eq 0 ]
}
main "$@"
