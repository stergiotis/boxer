#!/bin/bash
# The benchmark lane. The default runner (gotest.sh) compiles benchmarks but
# never executes them; this script does, in one of two modes.
#
#   scripts/ci/gobench.sh smoke [pkg...]    # every benchmark once, -short
#   scripts/ci/gobench.sh compare pkg...    # base ref vs working tree, benchstat
#
#   BOXER_BENCH_MATCH=Parse scripts/ci/gobench.sh compare ./public/db/clickhouse/dsl/...
#   BOXER_BENCH_BASE=HEAD~3 BOXER_BENCH_COUNT=10 scripts/ci/gobench.sh compare ./public/caching/
#
# smoke answers "does every benchmark still run": one iteration each
# (-benchtime 1x) under -short, over the packages that declare benchmarks
# (all of them when no pattern is given). A benchmark that skips itself — one
# needing a live /proc or a built wasm bridge — passes without running, so the
# lane lists every skip and its reason.
#
# compare answers "did this change move a number". It builds each package's
# test binary twice — from BOXER_BENCH_BASE checked out in a detached worktree,
# and from the working tree as it stands, uncommitted changes included — then
# runs the two alternately, BOXER_BENCH_COUNT rounds, swapping which goes first
# each round so drift in machine load lands on both sides alike. benchstat
# summarises; it needs at least 6 samples per side for a confidence interval.
# Package patterns are required: a full sweep at the default benchtime runs for
# hours. Both sides build with GOWORK=off, so dependency selection comes from
# go.mod alone and cannot differ between them through a workspace.
#
# The numbers are only as good as the machine is quiet. Concurrent builds, test
# runs or fuzzing on the same host move results more than most changes do; the
# lane prints the load average before it starts and leaves the judgement to
# whoever reads the table.
#
# Knobs (all optional):
#   BOXER_BENCH_MATCH  -bench regex (default .)
#   BOXER_BENCH_BASE   compare: git ref for the base side (default HEAD)
#   BOXER_BENCH_COUNT  compare: rounds per side (default 6)
#   BOXER_BENCH_TIME   compare: -benchtime per run (default the toolchain's 1s)
#   BOXER_BENCH_OUT    output directory (default ${TMPDIR:-/tmp}/boxer-bench)
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(readlink -f "$here/../..")
cd "$root"
tags="$(tr -d "\n" <"$root/tags")"
match="${BOXER_BENCH_MATCH:-.}"
out="${BOXER_BENCH_OUT:-${TMPDIR:-/tmp}/boxer-bench}"

usage() {
  sed -n '5,6p' "$BASH_SOURCE" | sed 's/^#  */usage: /' >&2
  exit 2
}

# bench_packages prints the directory, relative to the tree it runs in, of
# every package matching the patterns whose test files — as compiled under the
# repo's tags — declare a Benchmark function.
bench_packages() {
  go list -tags "$tags" \
    -f '{{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}' \
    "$@" | while read -r dir files; do
    [ -n "$files" ] || continue
    if (cd "$dir" && grep -qE '^func Benchmark[A-Za-z0-9_]*\(' $files); then
      echo "./$(realpath --relative-to=. "$dir")"
    fi
  done
}

loadavg() {
  echo "load average $(cut -d' ' -f1-3 /proc/loadavg) on $(nproc) CPUs"
}

smoke() {
  mapfile -t pkgs < <(bench_packages "${@:-./...}")
  if [ "${#pkgs[@]}" -eq 0 ]; then
    echo "gobench: no packages with benchmarks match" >&2
    exit 1
  fi
  mkdir -p "$out"
  log="$out/smoke.log"
  echo "gobench smoke: ${#pkgs[@]} packages, $(loadavg), log in $log"
  status=0
  # -v is what makes a skipped benchmark print its name and reason.
  go test -tags "$tags" -short -run '^$' -bench "$match" -benchtime 1x -benchmem -v \
    "${pkgs[@]}" >"$log" 2>&1 || status=$?
  echo "gobench smoke: $(grep -cE '^ok ' "$log" || true) of ${#pkgs[@]} packages ok," \
    "$(grep -cE '^Benchmark.*[0-9.]+ ns/op' "$log" || true) benchmark results"
  outcomes FAIL "failed benchmarks"
  grep -E '^(FAIL|panic:)\s' "$log" | sed 's/^/    /' || true
  outcomes SKIP "skipped benchmarks (they did not run)"
  return "$status"
}

# outcomes lists the benchmarks in the smoke log that ended with the given
# verdict, each followed by what it logged. go test -v prints a benchmark's
# log lines, indented, before its "--- SKIP:" / "--- FAIL:" line, so indented
# lines are held until a verdict claims them or a new benchmark starts.
outcomes() {
  local verdict="$1" title="$2" found
  found="$(awk -v verdict="$verdict" '
    $0 ~ "^[ \t]*--- " verdict ": Benchmark" {
      sub(/^[ \t]*--- [A-Z]+: /, ""); print "    " $0; printf "%s", held; held = ""; next
    }
    /^[ \t]+[^ \t]/ && !/^[ \t]*---/ {
      line = $0; sub(/^[ \t]*/, "", line); held = held "        " line "\n"; next
    }
    { held = "" }
  ' "$log")"
  if [ -n "$found" ]; then
    echo "gobench smoke: $title:"
    echo "$found"
  fi
}

compare() {
  [ "$#" -gt 0 ] || usage
  base_ref="${BOXER_BENCH_BASE:-HEAD}"
  count="${BOXER_BENCH_COUNT:-6}"
  benchtime=()
  if [ -n "${BOXER_BENCH_TIME:-}" ]; then
    benchtime=(-test.benchtime "$BOXER_BENCH_TIME")
  fi
  export GOWORK=off

  base_sha="$(git rev-parse --verify "$base_ref^{commit}")"
  mkdir -p "$out"
  run="$out/compare-$(date +%Y%m%dT%H%M%S)"
  worktree="$run/base-tree"
  mkdir -p "$run/bin/base" "$run/bin/head"
  cleanup() {
    git -C "$root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
  }
  trap cleanup EXIT
  git worktree add --quiet --detach "$worktree" "$base_sha"

  mapfile -t pkgs < <(bench_packages "$@")
  if [ "${#pkgs[@]}" -eq 0 ]; then
    echo "gobench: no packages with benchmarks match" >&2
    exit 1
  fi
  echo "gobench compare: base $base_ref ($(git rev-parse --short "$base_sha")) vs working tree," \
    "${#pkgs[@]} packages, $count rounds, -bench '$match'"
  echo "gobench compare: $(loadavg), results in $run"

  # Build both sides first, so compilation does not load the machine while
  # the other side is being measured.
  built=()
  for pkg in "${pkgs[@]}"; do
    bin="$(echo "${pkg#./}" | tr / _).test"
    if ! (cd "$root" && go test -c -tags "$tags" -o "$run/bin/head/$bin" "$pkg"); then
      echo "gobench: $pkg does not build in the working tree" >&2
      exit 1
    fi
    if [ ! -d "$worktree/$pkg" ]; then
      echo "gobench: $pkg is absent at $base_ref; skipped" >&2
      continue
    fi
    if ! (cd "$worktree" && go test -c -tags "$tags" -o "$run/bin/base/$bin" "$pkg"); then
      echo "gobench: $pkg does not build at $base_ref; skipped" >&2
      continue
    fi
    built+=("$pkg")
  done
  if [ "${#built[@]}" -eq 0 ]; then
    echo "gobench: nothing to compare" >&2
    exit 1
  fi

  # A test binary resolves testdata/ against its working directory, so each
  # side runs from the package directory of its own tree.
  runside() {
    local side="$1" tree="$2" pkg="$3"
    local bin
    bin="$run/bin/$side/$(echo "${pkg#./}" | tr / _).test"
    (cd "$tree/$pkg" && "$bin" -test.run '^$' -test.bench "$match" -test.benchmem \
      -test.count 1 "${benchtime[@]}") >>"$run/$side.txt" 2>>"$run/$side.stderr"
  }
  for ((round = 1; round <= count; round++)); do
    echo "gobench compare: round $round of $count"
    for pkg in "${built[@]}"; do
      if ((round % 2)); then
        runside base "$worktree" "$pkg"
        runside head "$root" "$pkg"
      else
        runside head "$root" "$pkg"
        runside base "$worktree" "$pkg"
      fi
    done
  done

  echo
  go tool benchstat base="$run/base.txt" head="$run/head.txt" | tee "$run/benchstat.txt"
}

mode="${1:-}"
shift || true
case "$mode" in
  smoke) smoke "$@" ;;
  compare) compare "$@" ;;
  *) usage ;;
esac
