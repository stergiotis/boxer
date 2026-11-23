#!/bin/bash
# The integration lane (ENGINEERING_PRACTICES §4). Members carry
# `//go:build integration`, so the default `go test ./...` neither compiles nor
# runs them; this script is the only thing that does.
#
#   scripts/ci/gotest-integration.sh [--strict-skips] [<go test arguments>...]
#
# The arguments default to ./... and go to `go test` as they are.
#
# Two kinds of test live here, for two different reasons:
#
#   - heavy dependency — the testcontainers / Moby / OCI chain the Kafka test
#     needs, kept out of every developer's build;
#   - shared live server — the ClickHouse tests, which reach one server, the
#     one CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL names (localhost:8123 when
#     neither is set), and share system.query_log with each other.
#
# The second reason is why this runs with `-p 1`. Those tests create and drop
# scratch databases and read back rows they just wrote, bounded by wall-clock
# polls; run in parallel with each other (or with the rest of the suite) they
# contend for the same server and fail on timing rather than on behaviour.
# `-count=1` keeps a cached PASS from hiding a server that has since changed.
#
# Members skip themselves when their server or tool is unavailable, so running
# this without ClickHouse reports skips rather than failures, and tparse's
# summary does not name them. --strict-skips, which release.sh and nightly.sh
# pass, compares the skipped tests with integration-skips-baseline.txt beside
# this script and prints one "skipped: <package> <test>: <reason>" line per
# skip the baseline does not list; gate_lib.sh's gate() counts such a line as
# a failure, so a lane whose server was never reached cannot pass as green.
#
# `-race` is on, as in the default runner. It was off for a while because the
# queryrunsvc pipeline test could not finish under it — root-caused since to a
# first-boot backfill of the host's whole query_log retention, not to anything
# racy: the extract drains oldest-first at a batch cap per refresh, so the test
# inherited a workload proportional to the machine's history, and race
# instrumentation slowed the drain ~20x. The test now starts its capture at
# `now` (Config.BackfillFrom), which makes it O(1) in host history, and the
# whole lane passes under -race.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n"),integration"

strict=0
args=()
for a in "$@"; do
    case "$a" in
        --strict-skips) strict=1 ;;
        *) args+=("$a") ;;
    esac
done
[ ${#args[@]} -gt 0 ] || args=(./...)

if [ "$strict" = 0 ]; then
    go test -race -json -count=1 -p 1 -tags "$tags" "${args[@]}" \
      | go tool tparse -progress -trimpath auto -slow 20
    exit
fi

events=$(mktemp "${TMPDIR:-/tmp}/gotest-integration.XXXXXX")
trap 'rm -f -- "$events"' EXIT
rc=0
# The copy is awk's, not tee's: the tee of uutils coreutils 0.10 aborts on an
# assertion in its pipe-to-pipe path when the reader falls behind, which ends
# the lane with the events it had.
go test -race -json -count=1 -p 1 -tags "$tags" "${args[@]}" \
  | awk -v out="$events" '{ print > out; print; fflush() }' \
  | go tool tparse -progress -trimpath auto -slow 20 || rc=$?

# One "<package> <test>\t<reason>" line per skipped test, the package relative
# to the module and the reason the first line the test logged (its t.Skip
# message). Lines that are not JSON — build output — are passed over.
mod="$(go list -m)/"
skips=$(jq -R -r -n --arg mod "$mod" '
    [inputs | fromjson? | select(type == "object" and .Test != null)] as $ev
    | ($ev | map(select(.Action == "output"))
           | group_by(.Package + " " + .Test)
           | map({key: (.[0].Package + " " + .[0].Test),
                  value: ([.[].Output | gsub("^\\s+|\\s+$"; "")
                           | select(. != "" and (startswith("=== ") | not) and (startswith("--- ") | not))]
                          | first // "")})
           | from_entries) as $why
    | $ev[] | select(.Action == "skip")
    | "\(.Package | ltrimstr($mod)) \(.Test)\t\($why[.Package + " " + .Test] // "")"' "$events")
tested=$(jq -R -r -n --arg mod "$mod" '[inputs | fromjson? | select(type == "object" and .Package != null) | .Package | ltrimstr($mod)] | unique | .[]' "$events")

# The baseline: skips the lane is expected to have on a release machine (see
# the file's header). A skip outside it is printed as "skipped: …", which
# gate() fails; an entry whose package ran and whose test did not skip is
# reported, so the file only shrinks.
baseline="$here/integration-skips-baseline.txt"
declare -A expected=() seen=() pkgs=()
if [ -f "$baseline" ]; then
    while read -r p t _; do
        [ -n "$p" ] && [ "${p:0:1}" != "#" ] && expected["$p $t"]=1
    done <"$baseline"
fi
while read -r p; do [ -n "$p" ] && pkgs["$p"]=1; done <<<"$tested"
unexpected=0
while IFS=$'\t' read -r key why; do
    [ -n "$key" ] || continue
    seen["$key"]=1
    if [ -z "${expected[$key]:-}" ]; then
        [ "$unexpected" -gt 0 ] || echo
        echo "skipped: $key: $why"
        unexpected=$((unexpected + 1))
    fi
done <<<"$skips"
for key in "${!expected[@]}"; do
    if [ -n "${pkgs[${key%% *}]:-}" ] && [ -z "${seen[$key]:-}" ]; then
        echo "integration: $key ran; remove it from $(basename "$baseline")"
    fi
done
if [ "$unexpected" -gt 0 ]; then
    echo "integration: $unexpected test(s) did not run and are not in $(basename "$baseline")"
fi
exit "$rc"
