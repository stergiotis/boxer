#!/bin/bash
# The integration lane (ENGINEERING_PRACTICES §4). Members carry
# `//go:build integration`, so the default `go test ./...` neither compiles nor
# runs them; this script is the only thing that does.
#
# Two kinds of test live here, for two different reasons:
#
#   - heavy dependency — the testcontainers / Moby / OCI chain the Kafka test
#     needs, kept out of every developer's build;
#   - shared live server — the ClickHouse tests, which reach one server at
#     localhost:8123 and share system.query_log with each other.
#
# The second reason is why this runs with `-p 1`. Those tests create and drop
# scratch databases and read back rows they just wrote, bounded by wall-clock
# polls; run in parallel with each other (or with the rest of the suite) they
# contend for the same server and fail on timing rather than on behaviour.
# `-count=1` keeps a cached PASS from hiding a server that has since changed.
#
# Members skip themselves when their server is unreachable, so running this
# without ClickHouse reports skips rather than failures.
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
go test -race -json -count=1 -p 1 -tags "$tags" "${@:-./...}" \
  | go tool tparse -progress -trimpath -slow 20
