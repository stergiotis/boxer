# Sourced, not run: the step wrapper release.sh and nightly.sh share
# (ADR-0390). Modelled on scripts/dev/updatedeps.sh's gate().
#
#   source scripts/ci/gate_lib.sh
#   gate_logdir=DIR                  where each step's full output goes
#   gate LABEL CMD...                run a step
#   not_run LABEL REASON             record a step that could not start
#   clickhouse_point_private         point every ClickHouse client at the
#                                    run's own server's address
#   clickhouse_private DIR           the same, and start that server under DIR
#   clickhouse_running               point them at the server already
#                                    answering on 127.0.0.1:8123 (opt-in)
#   gate_stop_clickhouse             stop what clickhouse_private started
#
# A step's output goes to its own log and is printed only when the step
# fails. A step whose output reports a skipped part — the Rust and font
# scripts print "skipped:" or "skipped (" and exit 0 when a toolchain is
# missing, gotest-integration.sh --strict-skips prints "skipped:" per skipped
# test — fails, as does a step that could not start: callers of this file
# report "everything ran and passed", not "nothing failed". gate_failed holds
# the labels that did not pass, gate_results one "status seconds label" line
# per step, for a summary.

gate_logdir="${gate_logdir:-${TMPDIR:-/tmp}}"
gate_failed=()
gate_results=()
_gate_lib_dir=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")

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

# ---- ClickHouse ---------------------------------------------------------------
#
# Every ClickHouse client in the tree resolves its server through
# chclient.ConfigFromEnv: CLICKHOUSE_ENDPOINT, then CLICKHOUSE_URL, then
# http://localhost:8123/. Both functions below set both variables, so neither an
# ambient value nor the built-in default decides where a lane's queries go.

# The ports clickhouse_private tries first; it takes the first free one at or
# above each. Both are away from ClickHouse's defaults (8123, 9000), so the
# private server runs beside a workstation's own.
gate_clickhouse_http_port_base="${gate_clickhouse_http_port_base:-18123}"
gate_clickhouse_native_port_base="${gate_clickhouse_native_port_base:-19000}"
gate_clickhouse_dir=""
gate_clickhouse_url=""

_gate_port_in_use() {
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

_gate_free_port() {
    local p="$1" skip="${2:-}" n
    for n in $(seq 1 200); do
        if [ "$p" != "$skip" ] && ! _gate_port_in_use "$p"; then
            echo "$p"
            return 0
        fi
        p=$((p + 1))
    done
    return 1
}

# _gate_point_clickhouse_at URL: the CLICKHOUSE_* consumers reach URL, and no
# other variable can send a client elsewhere. Credentials are left to the
# caller: the private server takes the default user with no password, a
# running one may need the ambient ones.
_gate_point_clickhouse_at() {
    gate_clickhouse_url="$1"
    export CLICKHOUSE_ENDPOINT="$1"
    export CLICKHOUSE_URL="$1"
    # A different server for the hosts' log lines, the anchor store or a
    # jackstay target: none of them belongs in a check run.
    unset BOXER_LOG_FACTS_URL BOXER_ANCHOR_STORE_URL BOXER_ANCHOR_STORE_USER \
        BOXER_ANCHOR_STORE_PASSWORD BOXER_JACKSTAY_TARGET_ENDPOINT \
        BOXER_JACKSTAY_TARGET_USER BOXER_JACKSTAY_TARGET_PASSWORD
}

# clickhouse_point_private: pick free ports at or above the bases and point
# the CLICKHOUSE_* variables at the HTTP one, as the default user with no
# password, without starting anything. Until clickhouse_private starts the
# server, a client finds nothing there.
gate_clickhouse_http_port=""
gate_clickhouse_native_port=""
clickhouse_point_private() {
    gate_clickhouse_http_port=$(_gate_free_port "$gate_clickhouse_http_port_base") ||
        gate_clickhouse_http_port="$gate_clickhouse_http_port_base"
    gate_clickhouse_native_port=$(_gate_free_port "$gate_clickhouse_native_port_base" "$gate_clickhouse_http_port") ||
        gate_clickhouse_native_port="$gate_clickhouse_native_port_base"
    _gate_point_clickhouse_at "http://127.0.0.1:$gate_clickhouse_http_port/"
    export CLICKHOUSE_USER=default
    unset CLICKHOUSE_PASSWORD CLICKHOUSE_PASSWORD_FILE CLICKHOUSE_DATABASE
}

# _gate_clickhouse_provision DIR: give the fresh server what a host's first
# run leaves on one — the facts table with its id lease table and id directory
# (chstore's setup DDL, as `keelsonddl --with-id-tables` prints it) and
# leeway's SQL read surface — built from the tree in the
# current directory. The live-server tests read and write boxer.facts and
# call the surface's functions; on a server that never ran a host they fail
# on a missing table rather than on what they test.
#
# It also gets a table of rows, gate.rows. Some live tests read "a table of
# the server" without owning one — they create nothing but session temporary
# tables, so that they can run against a person's server — and on a server
# that holds no data they would skip.
_gate_clickhouse_provision() {
    local dir="$1"
    ./boxer.sh keelsonddl --with-id-tables |
        "$dir/clickhouse" client --host 127.0.0.1 --port "$gate_clickhouse_native_port" --user default --multiquery
    ./boxer.sh leeway sqlsurface install --url "$gate_clickhouse_url" --user default
    "$dir/clickhouse" client --host 127.0.0.1 --port "$gate_clickhouse_native_port" --user default --multiquery \
        -q "CREATE DATABASE IF NOT EXISTS gate; CREATE TABLE IF NOT EXISTS gate.rows ENGINE = MergeTree ORDER BY n AS SELECT number AS n, toString(number) AS label FROM numbers(5000)"
}

# clickhouse_private DIR: clickhouse_point_private, then install the pinned
# server from clickhouse_server.sh under DIR (the binary is kept between runs;
# data and logs are not), start it on those ports, listening on 127.0.0.1
# only, and provision it as a host's first run would. The variables stay where they point if the server does not start, so
# a client finds either this server or nothing — never another one. The
# binary's directory goes first on PATH, and BOXER_CLICKHOUSE_LOCAL and
# BOXER_CLICKHOUSE_BINARY_PATH name it, so `clickhouse local` is the pinned
# build too — the worker pool's as well, which takes the override ahead of a
# packaged /usr/bin/clickhouse. Succeeds when the server answers;
# gate_clickhouse_dir is then set, for gate_stop_clickhouse.
clickhouse_private() {
    local dir="$1" srv="$_gate_lib_dir/clickhouse_server.sh"
    local log="$gate_logdir/clickhouse.log"
    clickhouse_point_private
    export PATH="$dir:$PATH"
    export BOXER_CLICKHOUSE_BINARY_PATH="$dir/clickhouse"
    export BOXER_CLICKHOUSE_LOCAL="$dir/clickhouse"
    mkdir -p "$dir"
    rm -rf -- "$dir/data" "$dir/log"
    gate_clickhouse_dir="$dir"
    if "$srv" install "$dir" >"$log" 2>&1 &&
        "$srv" start "$dir" "$gate_clickhouse_http_port" "$gate_clickhouse_native_port" >>"$log" 2>&1 &&
        _gate_clickhouse_provision "$dir" >>"$log" 2>&1; then
        echo "ClickHouse: the run's own server on 127.0.0.1:$gate_clickhouse_http_port (native $gate_clickhouse_native_port), under $dir"
        return 0
    fi
    gate_stop_clickhouse
    echo "ClickHouse: the run's own server did not start (see $log); clients point at $gate_clickhouse_url, where it would have answered"
    return 1
}

# clickhouse_running: the opt-in to the server already answering on
# 127.0.0.1:8123. Every lane then creates and drops its scratch databases on
# that server and writes to its system tables. Credentials stay as the
# environment has them.
clickhouse_running() {
    _gate_point_clickhouse_at "http://127.0.0.1:8123/"
    if curl -fsS http://127.0.0.1:8123/ping >/dev/null 2>&1; then
        echo "ClickHouse: using the server already answering on 127.0.0.1:8123 (opted in)"
        return 0
    fi
    return 1
}

gate_stop_clickhouse() {
    if [ -n "$gate_clickhouse_dir" ]; then
        "$_gate_lib_dir/clickhouse_server.sh" stop "$gate_clickhouse_dir" >/dev/null 2>&1 || true
        gate_clickhouse_dir=""
    fi
}
