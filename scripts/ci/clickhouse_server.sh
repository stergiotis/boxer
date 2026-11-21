#!/bin/bash
# A throwaway ClickHouse server for the integration lane.
#
#   scripts/ci/clickhouse_server.sh install DIR                fetch the pinned binary into DIR
#   scripts/ci/clickhouse_server.sh start DIR [HTTP [NATIVE]]  start a server on 127.0.0.1
#   scripts/ci/clickhouse_server.sh stop DIR                   stop it
#   scripts/ci/clickhouse_server.sh url DIR                    print the started server's HTTP URL
#
# start listens on 127.0.0.1 only, on the HTTP port HTTP (default 8123) and the
# native port NATIVE (default 9000), and opens no other port. It refuses to
# start when either port already answers, so it never reports a server it did
# not start as its own. release.sh and nightly.sh pass free ports away from the
# defaults (gate_lib.sh), so the server runs beside one a workstation already
# has; the defaults are for a machine with none. Without the arguments the
# ports come from CLICKHOUSE_SERVER_HTTP_PORT and CLICKHOUSE_SERVER_TCP_PORT. A
# binary already at DIR/clickhouse is used as it is, so a machine with
# ClickHouse installed can link its own instead of running install.
#
# A started server carries an empty database named boxer_throwaway_server.
# The tests that need the runtime's real names — boxer.facts as a view over a
# deployment's table, a host booted against it — look for it and skip on any
# server without it, so they cannot run against a server that holds data.
#
# Tests reach the server through CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL as the
# default user with no password, and read system.query_log; `clickhouse local`
# covers the rest of the tree. The version is pinned at or above the README's
# floor (26.8), and the download is pinned by its SHA-256 as well as its
# version: the static build's tarball, of which only usr/bin/clickhouse is
# kept. Everything the server writes — data, logs, the pid file — stays under
# DIR.
set -euo pipefail

CH_VERSION=26.8.37.168
CH_TAG=v26.8.37.168-lts
CH_SHA256=b05ddbea1938e9f53decbfae8b2c26748ffad2981b3634f2b2098566266c53c3
CH_ASSET="clickhouse-common-static-${CH_VERSION}-amd64.tgz"
CH_URL="https://github.com/ClickHouse/ClickHouse/releases/download/${CH_TAG}/${CH_ASSET}"

cmd="${1:-}"
dir="${2:-}"
http_port="${3:-${CLICKHOUSE_SERVER_HTTP_PORT:-8123}}"
tcp_port="${4:-${CLICKHOUSE_SERVER_TCP_PORT:-9000}}"
[ -n "$cmd" ] && [ -n "$dir" ] || { sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
case "$http_port$tcp_port" in
    *[!0-9]*) echo "clickhouse_server: ports must be numbers" >&2; exit 2 ;;
esac
mkdir -p "$dir"
dir=$(readlink -f "$dir")
bin="$dir/clickhouse"
marker_db=boxer_throwaway_server

install() {
    if [ -x "$bin" ] && "$bin" --version 2>/dev/null | grep -qF "$CH_VERSION"; then
        echo "clickhouse $CH_VERSION already in $dir"
        return
    fi
    [ "$(uname -m)" = x86_64 ] || { echo "clickhouse_server: the pin is the amd64 build" >&2; exit 1; }
    local tgz="$dir/$CH_ASSET"
    curl -fsSL --retry 3 -o "$tgz" "$CH_URL"
    echo "$CH_SHA256  $tgz" | sha256sum -c -
    # --no-same-owner: run as root (a CI container, a user namespace), tar
    # would give the file the archive's owner, which may not exist there.
    tar -xzf "$tgz" -C "$dir" --no-same-owner --strip-components=3 --wildcards '*/usr/bin/clickhouse'
    rm -f -- "$tgz"
    chmod +x "$bin"
    "$bin" --version
}

# port_in_use PORT: something accepts a connection on 127.0.0.1:PORT.
port_in_use() {
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

start() {
    [ -x "$bin" ] || { echo "clickhouse_server: run install first" >&2; exit 1; }
    local p
    for p in "$http_port" "$tcp_port"; do
        if port_in_use "$p"; then
            echo "clickhouse_server: 127.0.0.1:$p is already in use; not starting" >&2
            exit 1
        fi
    done
    mkdir -p "$dir/data" "$dir/log"
    echo "$http_port" >"$dir/http_port"
    cat >"$dir/config.xml" <<EOF
<clickhouse>
    <logger>
        <level>warning</level>
        <log>$dir/log/clickhouse-server.log</log>
        <errorlog>$dir/log/clickhouse-server.err.log</errorlog>
    </logger>
    <listen_host>127.0.0.1</listen_host>
    <http_port>$http_port</http_port>
    <tcp_port>$tcp_port</tcp_port>
    <path>$dir/data/</path>
    <tmp_path>$dir/data/tmp/</tmp_path>
    <user_files_path>$dir/data/user_files/</user_files_path>
    <format_schema_path>$dir/data/format_schemas/</format_schema_path>
    <mark_cache_size>268435456</mark_cache_size>
    <query_log>
        <database>system</database>
        <table>query_log</table>
        <flush_interval_milliseconds>1000</flush_interval_milliseconds>
    </query_log>
    <asynchronous_metric_log>
        <database>system</database>
        <table>asynchronous_metric_log</table>
        <flush_interval_milliseconds>1000</flush_interval_milliseconds>
    </asynchronous_metric_log>
    <users>
        <default>
            <password></password>
            <networks><ip>127.0.0.1</ip><ip>::1</ip></networks>
            <profile>default</profile>
            <quota>default</quota>
            <access_management>1</access_management>
        </default>
    </users>
    <profiles><default/></profiles>
    <quotas><default/></quotas>
</clickhouse>
EOF
    (cd "$dir" && nohup "$bin" server --config-file="$dir/config.xml" --pid-file="$dir/clickhouse.pid" \
        >"$dir/log/stdout.log" 2>&1 &)
    local i base="http://127.0.0.1:$http_port"
    for i in $(seq 1 60); do
        if curl -fsS "$base/ping" >/dev/null 2>&1; then
            echo "clickhouse server up on 127.0.0.1:$http_port (native $tcp_port): $(curl -fsS "$base/?query=SELECT%20version()")"
            # The marker a test checks before it uses the runtime's own
            # database and table names, which it must never do on a server
            # that holds someone's data.
            curl -fsS "$base/" --data-binary \
                "CREATE DATABASE IF NOT EXISTS $marker_db COMMENT 'a throwaway server started by scripts/ci/clickhouse_server.sh: tests may drop and create any database here'" >/dev/null
            return
        fi
        sleep 1
    done
    echo "clickhouse_server: no answer on 127.0.0.1:$http_port after 60s" >&2
    tail -n 50 "$dir/log/stdout.log" "$dir/log/clickhouse-server.err.log" 2>/dev/null >&2 || true
    exit 1
}

stop() {
    [ -f "$dir/clickhouse.pid" ] || { echo "clickhouse_server: no pid file in $dir"; return; }
    local pid i
    pid=$(cat "$dir/clickhouse.pid")
    kill "$pid" 2>/dev/null || true
    for i in $(seq 1 30); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 1
    done
    if kill -0 "$pid" 2>/dev/null; then
        echo "clickhouse_server: $pid still running after 30s; sending KILL" >&2
        kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f -- "$dir/clickhouse.pid" "$dir/http_port"
}

url() {
    [ -f "$dir/http_port" ] || { echo "clickhouse_server: no server was started in $dir" >&2; exit 1; }
    echo "http://127.0.0.1:$(cat "$dir/http_port")/"
}

case "$cmd" in
    install) install ;;
    start) start ;;
    stop) stop ;;
    url) url ;;
    *) echo "clickhouse_server: unknown command $cmd" >&2; exit 2 ;;
esac
