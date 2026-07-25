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
# scripts/ci/gotest.sh. These tests bound their end-to-end waits by wall clock
# (the queryrunsvc pipeline polls for a fact for 20s, and takes 17-21s to get
# it), and race instrumentation inflates exactly those timings: under -race the
# same test blows its own budget and reports a behaviour failure for a
# scheduling cost. Race detection belongs in the default lane, whose tests are
# hermetic and fast; here it would only manufacture noise.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n"),integration"
go test -json -count=1 -p 1 -tags "$tags" "${@:-./...}" \
  | go tool tparse -progress -trimpath -slow 20
