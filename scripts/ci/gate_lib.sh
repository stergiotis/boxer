# Sourced, not run: the step wrapper release.sh and nightly.sh share
# (ADR-0390). Modelled on scripts/dev/updatedeps.sh's gate().
#
#   source scripts/ci/gate_lib.sh
#   gate_logdir=DIR                  where each step's full output goes
#   gate LABEL CMD...                run a step
#   not_run LABEL REASON             record a step that could not start
#   ensure_clickhouse START DIR      a server on 127.0.0.1:8123, or not
#
# A step's output goes to its own log and is printed only when the step
# fails. A step whose output reports a skipped part — the Rust and font
# scripts print "skipped:" or "skipped (" and exit 0 when a toolchain is
# missing — fails, as does a step that could not start: callers of this file
# report "everything ran and passed", not "nothing failed". gate_failed holds
# the labels that did not pass, gate_results one "status seconds label" line
# per step, for a summary.

gate_logdir="${gate_logdir:-${TMPDIR:-/tmp}}"
gate_failed=()
gate_results=()

gate() {
    local label="$1" log t0 code=0 status
    shift
    log="$gate_logdir/$(tr -c 'A-Za-z0-9._-' '_' <<<"$label").log"
    t0=$SECONDS
    "$@" >"$log" 2>&1 || code=$?
    if [ "$code" -ne 0 ]; then
        status=FAILED
        printf 'gate %-28s FAILED (%ds)\n' "$label" $((SECONDS - t0))
        tail -n 60 "$log" | sed 's/^/    /'
    elif grep -qE 'skipped( \(|:)' "$log"; then
        status=SKIPPED
        printf 'gate %-28s SKIPPED, counts as failure (%ds)\n' "$label" $((SECONDS - t0))
        grep -E 'skipped( \(|:)' "$log" | sed 's/^/    /'
    else
        status=ok
        printf 'gate %-28s ok (%ds)\n' "$label" $((SECONDS - t0))
    fi
    [ "$status" = ok ] || gate_failed+=("$label")
    gate_results+=("$(printf '%-8s %6ds  %s' "$status" $((SECONDS - t0)) "$label")")
}

not_run() {
    printf 'gate %-28s NOT RUN, counts as failure: %s\n' "$1" "$2"
    gate_failed+=("$1")
    gate_results+=("$(printf '%-8s %6s   %s (%s)' "NOT-RUN" - "$1" "$2")")
}

# ensure_clickhouse START DIR: succeed when a server answers on
# 127.0.0.1:8123. A server already answering is used as it is. Otherwise, with
# START=1, the pinned throwaway from clickhouse_server.sh is installed and
# started under DIR (and gate_clickhouse_dir set, for gate_stop_clickhouse);
# with START=0 nothing is started.
gate_clickhouse_dir=""
ensure_clickhouse() {
    local start="$1" dir="$2"
    if curl -fsS http://127.0.0.1:8123/ping >/dev/null 2>&1; then
        echo "using the ClickHouse server already answering on 127.0.0.1:8123"
        return 0
    fi
    [ "$start" = 1 ] || return 1
    if scripts/ci/clickhouse_server.sh install "$dir" >"$gate_logdir/clickhouse.log" 2>&1 &&
        scripts/ci/clickhouse_server.sh start "$dir" >>"$gate_logdir/clickhouse.log" 2>&1; then
        echo "started the pinned throwaway ClickHouse server under $dir"
        gate_clickhouse_dir="$dir"
        export PATH="$dir:$PATH"
        return 0
    fi
    return 1
}

gate_stop_clickhouse() {
    if [ -n "$gate_clickhouse_dir" ]; then
        scripts/ci/clickhouse_server.sh stop "$gate_clickhouse_dir" >/dev/null 2>&1 || true
        gate_clickhouse_dir=""
    fi
}
