#!/usr/bin/env bash
# ADR-0085 deploy-logic validation harness — checks A1..A8 in VALIDATION.md.
#
# !!! DRAFTED, NOT YET RUN !!!  Expect to debug it on first use, like any new
# harness. It exercises the REAL pipeline (fetch/verify/checkout/build/gate/
# swap/health/rollback/prune) against a THROWAWAY local remote with controlled
# signed/unsigned tags, a temp deploy root, and a `systemctl` shim that
# backgrounds the real run-current.sh — so no real systemd is touched. Run on a
# build-capable box from anywhere in the boxer checkout:
#
#   ./rust/imzero2/deploy/onbox/validate.sh
#
# Builds are real. All test tags point at HEAD, so with a warm cargo target/
# (seeded below) the per-tag builds are incremental, not the cold 663-crate slog.
# Needs: the toolchain (rust/go/ffmpeg+x264/mesa-sw/fonts), git, ssh-keygen,
# python3 (only for the A6 port-occupy), and Go on PATH (for the A8 check).
set -u

BOXER="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/imz-validate.XXXXXX")"
REMOTE="$WORK/remote.git"; WS="$WORK/ws"; ROOT="$WORK/root"
SHIMDIR="$WORK/bin"; PIDFILE="$WORK/demo.pid"
LIVE_PORT="${LIVE_PORT:-18091}"; SCRATCH_PORT="${SCRATCH_PORT:-18092}"
ENC="${ENC:--c:v libx264 -preset veryfast -tune zerolatency -bf 0 -g 100000}"
KEEP=2; GATE_AUS=5
DEPLOY="$BOXER/rust/imzero2/main_go"

pass=0; fail=0
ok()   { echo "PASS: $*"; pass=$((pass+1)); }
bad()  { echo "FAIL: $*"; fail=$((fail+1)); }
note() { echo; echo "--- $* ---"; }
curtag() { basename "$(readlink "$ROOT/current" 2>/dev/null || echo none)"; }
sign()   { git -C "$WS" tag -s "$1" -m signed && git -C "$WS" push --quiet origin "$1"; }

cleanup() { [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT

deploy() { # extra deploy args... ; runs with the systemctl shim on PATH
  PATH="$SHIMDIR:$PATH" "$DEPLOY" --logFormat=console --logLevel=info imzero2 deploy \
    --root "$ROOT" --workspace "$WS" --remote origin --service demo.service \
    --live-port "$LIVE_PORT" --scratch-port "$SCRATCH_PORT" \
    --gate-aus "$GATE_AUS" --keep "$KEEP" --encoder-args "$ENC" "$@"
}

setup() {
  note "setup: deploy tool"
  [ -x "$DEPLOY" ] || ( cd "$BOXER/rust/imzero2" && ./build_go.sh ) || { echo "cannot build main_go"; exit 1; }

  note "setup: systemctl shim (backgrounds run-current.sh on the live port)"
  mkdir -p "$SHIMDIR"
  cat > "$SHIMDIR/systemctl" <<SHIM
#!/bin/sh
case "\$1" in
  restart)
    [ -f "$PIDFILE" ] && kill "\$(cat "$PIDFILE")" 2>/dev/null
    IMZERO2_CURRENT="$ROOT/current" \\
    IMZERO2_HEADLESS_LISTEN="127.0.0.1:$LIVE_PORT" \\
    IMZERO2_HEADLESS_ENCODER_ARGS="$ENC" \\
    LIBGL_ALWAYS_SOFTWARE=1 LOG_LEVEL=warn \\
      "$BOXER/rust/imzero2/deploy/onbox/run-current.sh" >"$WORK/demo.log" 2>&1 &
    echo \$! > "$PIDFILE" ;;
  stop) [ -f "$PIDFILE" ] && kill "\$(cat "$PIDFILE")" 2>/dev/null ;;
esac
exit 0
SHIM
  chmod +x "$SHIMDIR/systemctl"

  note "setup: signing key + throwaway remote (HEAD-pointing test tags)"
  ssh-keygen -t ed25519 -f "$WORK/signer" -N '' -C release@test -q
  printf 'release@test %s\n' "$(cat "$WORK/signer.pub")" > "$WORK/allowed"
  git clone --quiet --bare --local "$BOXER" "$REMOTE"
  git clone --quiet --local "$REMOTE" "$WS"
  git -C "$WS" config gpg.format ssh
  git -C "$WS" config user.signingkey "$WORK/signer"
  git -C "$WS" config user.email release@test
  git -C "$WS" config user.name validator
  git -C "$WS" config gpg.ssh.allowedSignersFile "$WORK/allowed"
  # Warm the cargo cache so per-tag builds are incremental (hardlink copy).
  [ -d "$BOXER/rust/imzero2/target/headless" ] && \
    { cp -al "$BOXER/rust/imzero2/target" "$WS/rust/imzero2/target" 2>/dev/null || true; }
}

main() {
  setup

  note "A2 — unsigned/newest tag refused BEFORE build (fast, no build)"
  git -C "$WS" tag v-test-2-unsigned && git -C "$WS" push --quiet origin v-test-2-unsigned  # lightweight
  if deploy >"$WORK/a2.log" 2>&1; then bad "A2: unsigned tag was NOT refused"; else
    grep -q "no valid signature" "$WORK/a2.log" && [ "$(curtag)" = none ] \
      && ok "A2: unsigned refused, current untouched" || { bad "A2: wrong failure / current=$(curtag)"; sed -n '$p' "$WORK/a2.log"; }
  fi

  note "A3 — dev escape (--require-signed-tags=false) builds it [REAL BUILD]"
  if deploy --require-signed-tags=false >"$WORK/a3.log" 2>&1; then
    grep -q "verification DISABLED" "$WORK/a3.log" && ok "A3: dev escape built + warned" || bad "A3: missing disabled-warning"
  else bad "A3: dev escape did not deploy (see $WORK/a3.log)"; fi

  rm -rf "$ROOT"; git -C "$WS" push --quiet origin :v-test-2-unsigned >/dev/null 2>&1
  note "A1 — signed tag deploys [REAL BUILD]"
  sign v-test-1
  if deploy >"$WORK/a1.log" 2>&1 && [ "$(curtag)" = v-test-1 ]; then ok "A1: live, current=v-test-1"
  else bad "A1: failed / current=$(curtag) (see $WORK/a1.log)"; fi

  note "A8 — revision agreement (binary stamps the tag commit)"
  want="$(git -C "$WS" rev-parse v-test-1^{commit})"
  got="$(go version -m "$ROOT/current/main_go" 2>/dev/null | sed -n 's/.*vcs.revision=\([0-9a-f]*\).*/\1/p')"
  [ -n "$got" ] && case "$want" in "$got"*) ok "A8: vcs.revision==tag commit ($got)";; *) bad "A8: want=$want got=$got";; esac \
    || bad "A8: could not read vcs.revision from the binary"

  note "A4 — no-op when already current"
  if deploy >"$WORK/a4.log" 2>&1 && grep -q "already current" "$WORK/a4.log"; then ok "A4: no-op"; else bad "A4: did not no-op"; fi

  note "A6 — rollback when the live port is occupied [REAL BUILD of a new tag]"
  sign v-test-5
  python3 -c "import socket,os,time;s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('127.0.0.1',$LIVE_PORT));s.listen(1);time.sleep(180)" &
  occ=$!; sleep 1; prev="$(curtag)"
  if deploy >"$WORK/a6.log" 2>&1; then bad "A6: deploy succeeded despite a blocked live port"; else
    [ "$(curtag)" = "$prev" ] && grep -qiE "roll(ed|ing) back" "$WORK/a6.log" \
      && ok "A6: rolled back to $prev" || bad "A6: current=$(curtag) prev=$prev (see $WORK/a6.log)"
  fi
  kill "$occ" 2>/dev/null

  note "A7 — retention keeps K=$KEEP (+ current)"
  rm -rf "$ROOT"
  for t in v-test-6 v-test-7 v-test-8 v-test-9; do sign "$t"; deploy >/dev/null 2>&1; done
  n="$(find "$ROOT/releases" -mindepth 1 -maxdepth 1 -type d ! -name '*.staging' 2>/dev/null | wc -l)"
  [ "$n" -le $((KEEP+1)) ] && ok "A7: retained=$n (<= K+1)" || bad "A7: retained=$n > $((KEEP+1))"

  note "A5 — gate blocks a non-streaming build"
  rm -rf "$ROOT"; sign v-test-10
  if deploy --encoder-args "-c:v not_a_real_codec" >"$WORK/a5.log" 2>&1; then bad "A5: broken encoder passed the gate"; else
    [ "$(curtag)" = none ] && ok "A5: gate blocked, current untouched" || bad "A5: current changed to $(curtag)"
  fi

  echo; echo "==== $pass passed, $fail failed ===="; [ "$fail" -eq 0 ]
}
main "$@"
