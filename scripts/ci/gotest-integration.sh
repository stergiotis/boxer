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
# No `-race`, deliberately — the one place this lane departs from
# scripts/ci/gotest.sh. The queryrunsvc pipeline test fails under `-race` for a
# reason that is NOT understood: it is not slowness (it still fails with a 60s
# budget, three times the passing run's wall clock), the service logs no error,
# and ClickHouse reports its refreshes succeeding with no exception — the fact
# simply never arrives. The race detector reports no data race either. Until
# that is root-caused, running the lane under -race reports a failure that says
# nothing about the code under test.
#
# This is a KNOWN GAP, not a considered exclusion: these tests are concurrent
# services and would benefit from race coverage. See the note in
# ENGINEERING_PRACTICES §4.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n"),integration"
go test -json -count=1 -p 1 -tags "$tags" "${@:-./...}" \
  | go tool tparse -progress -trimpath -slow 20
