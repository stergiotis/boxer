#!/bin/bash
# Dedicated binary_log (CBOR zerolog) test lane.
#
# The default gotest.sh lane builds with JSON logging (the ./tags set omits
# binary_log). zerolog's CBOR encoder — and the keelson logbridge that decodes
# it — only compile/behave under the binary_log build tag, so their tests are
# `//go:build ... && binary_log` and never run in the default lane. This lane
# appends binary_log and exercises exactly those CBOR-logging packages.
#
# Scope note: observability/eh is intentionally NOT in this lane. Its
# MarshalZerologObject tests json.Unmarshal zerolog output and only hold under
# JSON; the default gotest.sh lane covers them. Keep this list in sync with the
# packages carrying binary_log-gated tests.
#
# --check-drift fails the run when the tests left the tree dirty; see gotest.sh.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n"),binary_log"
check_drift=0
case "${1:-}" in
  --check-drift) check_drift=1 ;;
  "") ;;
  *) echo "gotest_binary_log.sh: unknown argument $1" >&2; exit 2 ;;
esac
rc=0
go test -race -json -short -cover -tags "$tags" \
  ./public/observability/logging/... \
  ./public/keelson/runtime/logbridge/... \
  ./public/thestack/imzero2/egui2/demo/apps/logdemo/... \
  | go tool tparse -progress -trimpath auto -slow 20 || rc=1
if [ "$check_drift" = 1 ]; then
  "$here/check_drift.sh" || rc=1
fi
exit "$rc"
