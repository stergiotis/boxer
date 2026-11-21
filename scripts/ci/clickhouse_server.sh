#!/bin/bash
# A throwaway ClickHouse server for the integration lane.
#
#   scripts/ci/clickhouse_server.sh install DIR   fetch the pinned binary into DIR
#   scripts/ci/clickhouse_server.sh start DIR     start a server on 127.0.0.1
#   scripts/ci/clickhouse_server.sh stop DIR      stop it
#   scripts/ci/clickhouse_server.sh url DIR       print the started server's HTTP URL
#
# Ports: CLICKHOUSE_SERVER_HTTP_PORT (default 8123) and
# CLICKHOUSE_SERVER_TCP_PORT (default 9000), read by start. A binary already
# at DIR/clickhouse is used as it is, so a machine with ClickHouse installed
# can link its own instead of running install.
#
# The lane's ClickHouse tests reach one server at localhost:8123 as the default
# user with no password, and read system.query_log; `clickhouse local` covers
# the rest of the tree. The version is pinned at or above the README's floor
# (26.8), and the download is pinned by its SHA-256 as well as its version: the
# static build's tarball, of which only usr/bin/clickhouse is kept. Everything
# the server writes — data, logs, the pid file — stays under DIR.
#
# For CI and release runs on a machine with no ClickHouse of its own. On the
# default ports it cannot run beside a server that already binds them; on
# other ports it can, and the tests are pointed at it with
#
#   export CLICKHOUSE_URL="$(scripts/ci/clickhouse_server.sh url DIR)"
#
# Only the tests that read CLICKHOUSE_URL follow; the ones that name
# localhost:8123 themselves still reach whatever answers there.
set -euo pipefail

CH_VERSION=26.8.37.168
CH_TAG=v26.8.37.168-lts
CH_SHA256=b05ddbea1938e9f53decbfae8b2c26748ffad2981b3634f2b2098566266c53c3
CH_ASSET="clickhouse-common-static-${CH_VERSION}-amd64.tgz"
CH_URL="https://github.com/ClickHouse/ClickHouse/releases/download/${CH_TAG}/${CH_ASSET}"

cmd="${1:-}"
dir="${2:-}"
[ -n "$cmd" ] && [ -n "$dir" ] || { sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
mkdir -p "$dir"
dir=$(readlink -f "$dir")
bin="$dir/clickhouse"
http_port="${CLICKHOUSE_SERVER_HTTP_PORT:-8123}"
tcp_port="${CLICKHOUSE_SERVER_TCP_PORT:-9000}"
case "$http_port$tcp_port" in
    *[!0-9]*) echo "clickhouse_server: ports must be numbers" >&2; exit 2 ;;
esac

install() {
    if [ -x "$bin" ] && "$bin" --version 2>/dev/null | grep -qF "$CH_VERSION"; then
        echo "clickhouse $CH_VERSION already in $dir"
        return
    fi
    [ "$(uname -m)" = x86_64 ] || { echo "clickhouse_server: the pin is the amd64 build" >&2; exit 1; }
    local tgz="$dir/$CH_ASSET"
    curl -fsSL --retry 3 -o "$tgz" "$CH_URL"
    echo "$CH_SHA256  $tgz" | sha256sum -c -
    tar -xzf "$tgz" -C "$dir" --strip-components=3 --wildcards '*/usr/bin/clickhouse'
    rm -f -- "$tgz"
    chmod +x "$bin"
    "$bin" --version
}

start() {
    [ -x "$bin" ] || { echo "clickhouse_server: run install first" >&2; exit 1; }
    if curl -fsS "http://127.0.0.1:$http_port/ping" >/dev/null 2>&1; then
        echo "clickhouse_server: something already answers on 127.0.0.1:$http_port; set CLICKHOUSE_SERVER_HTTP_PORT and CLICKHOUSE_SERVER_TCP_PORT" >&2
        exit 1
    fi
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
    local i
    for i in $(seq 1 60); do
        if curl -fsS "http://127.0.0.1:$http_port/ping" >/dev/null 2>&1; then
            echo "clickhouse server up on 127.0.0.1:$http_port: $(curl -fsS "http://127.0.0.1:$http_port/?query=SELECT%20version()")"
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
    kill "$(cat "$dir/clickhouse.pid")" 2>/dev/null || true
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
