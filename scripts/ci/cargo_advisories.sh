#!/bin/bash
# Known advisories against the crates.io packages in the Rust lockfiles, from
# OSV.dev, which carries the RustSec and GitHub advisories (ADR-0362).
#
#   cargo_advisories.sh [Cargo.lock ...]
#
# Lockfile paths are relative to the repository root; with none, every
# rust/*/Cargo.lock is checked. Only crate names and versions are sent.
#
# Exits non-zero on any advisory that is not informational. Informational ones
# (unmaintained, unsound, notice) are listed and pass, because most have no
# version to move to. A scan that cannot finish fails: an unreachable OSV is
# not a clean result. Path and git dependencies, which includes the vendored
# crates, are not covered.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
cd "$here/../.."

shopt -s nullglob
if [ $# -gt 0 ]; then
    locks=("$@")
else
    locks=(rust/*/Cargo.lock)
fi
if [ ${#locks[@]} -eq 0 ]; then
    echo "cargo_advisories: no Cargo.lock found; refusing to report clean" >&2
    exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() { # URL OUTFILE [curl args...]
    local url="$1" out="$2"
    shift 2
    curl -fsS --retry 3 --max-time 120 -A 'boxer-vuln-scan' -o "$out" "$@" "$url"
}

# "name<TAB>version<TAB>lockfile" for every crates.io package.
awk -v OFS='\t' '
    function flush() { if (n != "" && s ~ /crates\.io-index/) print n, v, f; n = v = s = "" }
    FNR == 1 { flush(); f = FILENAME }
    /^\[\[package\]\]/ { flush() }
    /^name = /    { n = $3; gsub(/"/, "", n) }
    /^version = / { v = $3; gsub(/"/, "", v) }
    /^source = /  { s = $3 }
    END { flush() }
' "${locks[@]}" | LC_ALL=C sort -u >"$tmp/pkgs"
cut -f1,2 "$tmp/pkgs" | LC_ALL=C sort -u >"$tmp/uniq"

# "name<TAB>version<TAB>advisory id" for every match.
: >"$tmp/hits"
split -l 500 "$tmp/uniq" "$tmp/chunk."
for c in "$tmp"/chunk.*; do
    jq -Rn '{queries: [inputs | split("\t") | {package: {name: .[0], ecosystem: "crates.io"}, version: .[1]}]}' <"$c" >"$c.q"
    fetch https://api.osv.dev/v1/querybatch "$c.r" -H 'Content-Type: application/json' --data "@$c.q"
    jq -r --slurpfile q "$c.q" '
        .results as $r
        | range(0; $r | length) as $i
        | if $r[$i].next_page_token then error("result truncated for \($q[0].queries[$i].package.name)") else . end
        | $q[0].queries[$i] as $k
        | ($r[$i].vulns // [])[]
        | [$k.package.name, $k.version, .id] | @tsv
    ' "$c.r" >>"$tmp/hits"
done

advisories=0
informational=0
while IFS=$'\t' read -r name ver id; do
    f="$tmp/v-$id.json"
    [ -s "$f" ] || fetch "https://api.osv.dev/v1/vulns/$id" "$f"
    IFS=$'\t' read -r kind fixed summary < <(jq -r --arg n "$name" '[
        (if .withdrawn then "withdrawn"
         else ([.database_specific.informational?, (.affected[]?.database_specific.informational?)]
               | map(select(.)) | first // "vulnerability") end),
        ([.affected[]? | select(.package.name == $n) | .ranges[]?.events[]? | .fixed? // empty] | unique | join(",")
         | if . == "" then "none" else . end),
        ((.summary // "") | if . == "" then "-" else . end)
    ] | @tsv' "$f")
    [ "$kind" != withdrawn ] || continue
    where=$(awk -F'\t' -v a="$name" -v b="$ver" '$1 == a && $2 == b { c = split($3, p, "/"); print p[c - 1] }' "$tmp/pkgs" | paste -sd, -)
    if [ "$kind" = vulnerability ]; then
        advisories=$((advisories + 1))
        printf 'ADVISORY  %s %s  %s  fixed in: %s  [%s]  %s\n' "$name" "$ver" "$id" "$fixed" "$where" "$summary"
    else
        informational=$((informational + 1))
        printf 'notice    %s %s  %s (%s)  fixed in: %s  [%s]\n' "$name" "$ver" "$id" "$kind" "$fixed" "$where"
    fi
done <"$tmp/hits"

echo "cargo_advisories: $(wc -l <"$tmp/uniq") crate versions in ${#locks[@]} lockfile(s), $advisories advisories, $informational informational"
if [ "$advisories" -gt 0 ]; then
    echo "cargo_advisories: run scripts/dev/updatedeps.sh on the crate; --cooldown-days 0 if the fix is too recent to pass the cooldown" >&2
    exit 1
fi
