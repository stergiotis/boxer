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
# pass, prints one "skipped: <package> <test>: <reason>" line per skipped test
# after the run; gate_lib.sh's gate() counts such a line as a failure, so a
# lane whose server was never reached cannot pass as green.
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
go test -race -json -count=1 -p 1 -tags "$tags" "${args[@]}" \
  | tee "$events" | go tool tparse -progress -trimpath auto -slow 20 || rc=$?

# One line per skipped test, with the first line it logged (its t.Skip
# message). Lines that are not JSON — build output — are passed over.
skips=$(jq -R -r -n '
    [inputs | fromjson? | select(type == "object" and .Test != null)] as $ev
    | ($ev | map(select(.Action == "output"))
           | group_by(.Package + " " + .Test)
           | map({key: (.[0].Package + " " + .[0].Test),
                  value: ([.[].Output | gsub("^\\s+|\\s+$"; "")
                           | select(. != "" and (startswith("=== ") | not) and (startswith("--- ") | not))]
                          | first // "")})
           | from_entries) as $why
    | $ev[] | select(.Action == "skip")
    | "skipped: \(.Package) \(.Test): \($why[.Package + " " + .Test] // "")"' "$events")
if [ -n "$skips" ]; then
    echo
    echo "$skips"
    echo "integration: $(wc -l <<<"$skips") test(s) did not run"
fi
exit "$rc"
