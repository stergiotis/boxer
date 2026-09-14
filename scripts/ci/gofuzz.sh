#!/bin/bash
# The fuzz lane: runs every Go fuzz target under `go test -fuzz` for a fixed
# time budget each. Plain `go test` (gotest.sh) already replays each target's
# seed corpus as a unit test; only this lane generates new inputs.
#
#   scripts/ci/gofuzz.sh                       # every target, BOXER_FUZZ_TIME each
#   scripts/ci/gofuzz.sh ./public/caching/...  # targets under a package pattern
#   BOXER_FUZZ_MATCH=Canonicalize BOXER_FUZZ_TIME=5m scripts/ci/gofuzz.sh
#
# Knobs (all optional):
#   BOXER_FUZZ_TIME     per-target -fuzztime, a duration or Nx count (default 30s)
#   BOXER_FUZZ_MATCH    extended regex over target names to select a subset
#   BOXER_FUZZ_PARALLEL fuzzing workers per target (default: GOMAXPROCS)
#   BOXER_FUZZ_LOG_DIR  per-target logs (default ${TMPDIR:-/tmp}/boxer-fuzz)
#
# `go test -fuzz` accepts exactly one package and one target per invocation,
# so the lane discovers the targets and runs them one at a time. A
# failing target does not stop the lane; the exit status is non-zero if any
# target failed.
#
# A failure writes its minimised input to <pkg>/testdata/fuzz/<Target>/ — that
# is how Go turns a crasher into a permanent seed, so the lane leaves the file
# in the tree and lists it at the end: commit it with the fix. The generated
# (non-failing) corpus lives in $GOCACHE/fuzz and carries over between runs.
#
# No -race: instrumentation costs throughput, and the lane is looking for
# property violations and panics, not data races.
#
# Long runs of FuzzAstRoundTrip grow ANTLR's process-global DFA cache without
# bound and the worker is eventually OOM-killed; see the note on that target
# before giving it a budget of minutes.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"
fuzztime="${BOXER_FUZZ_TIME:-30s}"
match="${BOXER_FUZZ_MATCH:-.}"
logdir="${BOXER_FUZZ_LOG_DIR:-${TMPDIR:-/tmp}/boxer-fuzz}"
mkdir -p "$logdir"

parallel=()
if [ -n "${BOXER_FUZZ_PARALLEL:-}" ]; then
  parallel=(-parallel "$BOXER_FUZZ_PARALLEL")
fi

# `go list` resolves the test files each package compiles under the repo's
# tags without building anything; the targets are the Fuzz functions declared
# in them. A `go test -list` would be exact too, but links every test binary
# in the tree first.
listing="$(go list -tags "$tags" \
  -f '{{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}' \
  "${@:-./...}")"
targets=()
while read -r dir files; do
  [ -n "$files" ] || continue
  for name in $(cd "$dir" && sed -nE 's/^func (Fuzz[A-Za-z0-9_]*)\(.*/\1/p' $files); do
    if [[ "$name" =~ $match ]]; then
      targets+=("./$(realpath --relative-to=. "$dir") $name")
    fi
  done
done <<<"$listing"
if [ "${#targets[@]}" -eq 0 ]; then
  echo "gofuzz: no fuzz targets match" >&2
  exit 1
fi

# Only the selected packages' corpora are compared, so a concurrent fuzz run
# elsewhere in a shared working tree is not reported as this lane's.
corpora=()
for entry in "${targets[@]}"; do
  corpora+=("${entry% *}/testdata/fuzz")
done
mapfile -t corpora < <(printf '%s\n' "${corpora[@]}" | sort -u)
fuzzstatus() {
  git status --porcelain --untracked-files=all -- "${corpora[@]}" 2>/dev/null || true
}
before="$(fuzzstatus)"
echo "gofuzz: ${#targets[@]} targets, $fuzztime each, logs in $logdir"

failed=()
for entry in "${targets[@]}"; do
  dir="${entry% *}"
  name="${entry#* }"
  log="$logdir/$(echo "${dir#./}" | tr / _)__$name.log"
  t0=$SECONDS
  if go test -tags "$tags" -run '^$' -fuzz "^${name}\$" -fuzztime "$fuzztime" \
      "${parallel[@]}" "$dir" >"$log" 2>&1; then
    status=PASS
  else
    status=FAIL
    failed+=("$dir $name")
  fi
  # The last progress line carries the exec count and corpus size; its rate is
  # the final tick's, usually 0/sec, so it is dropped.
  stats="$(grep -E '^fuzz: elapsed' "$log" | tail -n 1 \
    | sed -E 's/^fuzz: elapsed: [^,]+, //; s# \([0-9]+/sec\)##' || true)"
  if [ "$status" = PASS ]; then
    printf '%-4s %4ds  %-72s %s\n' "$status" $((SECONDS - t0)) "$dir $name" "$stats"
  else
    printf '%-4s %4ds  %s\n' "$status" $((SECONDS - t0)) "$dir $name"
    grep -vE '^fuzz: elapsed' "$log" | tail -n 30 | sed 's/^/    /' || true
  fi
done

after="$(fuzzstatus)"
new="$(comm -13 <(echo "$before" | sort) <(echo "$after" | sort))"
if [ -n "$new" ]; then
  echo
  echo "gofuzz: new failing inputs (commit them with the fix):"
  echo "$new" | sed 's/^/    /'
fi

if [ "${#failed[@]}" -gt 0 ]; then
  echo
  echo "gofuzz: ${#failed[@]} of ${#targets[@]} targets failed:"
  printf '    %s\n' "${failed[@]}"
  exit 1
fi
echo "gofuzz: all ${#targets[@]} targets passed"
