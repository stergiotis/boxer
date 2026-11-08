#!/bin/bash
# Update one dependency target, gate it, and optionally commit it (ADR-0362).
#
#   updatedeps.sh [--cooldown-days N] [--dry-run] [--commit] go
#   updatedeps.sh [--cooldown-days N] [--dry-run] [--commit] rust/<crate>
#
# Only compatible versions move: `go get` within a major, `cargo update`
# without touching Cargo.toml. No version published fewer than N days ago
# (default 5) lands. Every version that changed is checked, not only the ones
# the script asked for. Bumps cargo update cannot reach are listed, not
# applied. A gate that prints "skipped" counts as a failure here.
set -euo pipefail

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
repo=$(readlink -f "$here/../..")
cd "$repo"
tags="$(tr -d '\n' <tags)"
# The target is boxer's own module graph, as CI sees it: a go.work above the
# checkout would resolve versions, and feed the license gate, from modules
# that are not boxer's.
export GOWORK=off

cooldown=5
dry=0
commit=0
target=""
while [ $# -gt 0 ]; do
    case "$1" in
        --cooldown-days) cooldown="$2"; shift 2 ;;
        --dry-run) dry=1; shift ;;
        --commit) commit=1; shift ;;
        -h|--help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) [ -z "$target" ] || { echo "updatedeps: one target per run" >&2; exit 2; }; target="${1%/}"; shift ;;
    esac
done
[ -n "$target" ] || { echo "updatedeps: name a target: go | rust/<crate>" >&2; exit 2; }

die() { printf 'updatedeps: %s\n' "$*" >&2; exit 1; }
cutoff=$(date -u -d "-${cooldown} days" +%s)
aged() { [ "$(date -u -d "$1" +%s)" -le "$cutoff" ]; } # $1: RFC 3339 time

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# gate LABEL CMD...: run CMD, print its output only on failure. With
# GATE_NOSKIP=1, output containing "skipped" fails too: the Rust CI scripts
# exit 0 when a toolchain is missing, which is not a pass for an update.
gate() {
    local label="$1" out
    shift
    if out=$("$@" 2>&1); then
        if [ "${GATE_NOSKIP:-0}" = 1 ] && grep -qi 'skipped' <<<"$out"; then
            printf 'gate %s -- SKIPPED, counts as failure\n%s\n' "$label" "$out"
            return 1
        fi
        echo "gate $label: ok"
    else
        printf 'gate %s -- FAILED\n%s\n' "$label" "$out"
        return 1
    fi
}

# require_clean PATH...: the target's own files must have no uncommitted change.
require_clean() {
    git diff --quiet HEAD -- "$@" || die "uncommitted changes in: $* — commit or restore them first"
}

moves=()  # "name old -> new", what landed
held=()   # "name new (published T)", held back by the cooldown
manual=() # "name current -> available", behind latest and out of `cargo update`'s reach

report() {
    local x
    echo
    echo "target: $target  cooldown: ${cooldown}d"
    echo "moves (${#moves[@]}):";  for x in "${moves[@]}";  do echo "  $x"; done
    echo "held by cooldown (${#held[@]}):"; for x in "${held[@]}"; do echo "  $x"; done
    if [ ${#manual[@]} -gt 0 ]; then
        echo "not reachable by cargo update — a manifest edit or a dependent's pin (${#manual[@]}):"; for x in "${manual[@]}"; do echo "  $x"; done
    fi
}

do_commit() {
    local msg body
    msg="chore(deps): update $target (cooldown ${cooldown}d, ADR-0362)"
    body=$(printf '%s\n' "${moves[@]}")
    git commit --quiet -m "$msg" -m "$body" -- "$@"
    git log -1 --format='committed %h %s'
}

# ---- Go -------------------------------------------------------------------

update_go() {
    local files=(go.mod go.sum)
    require_clean "${files[@]}"
    cp go.mod "$tmp/go.mod.before"

    local req path old new t want=()
    mapfile -t req < <(go mod edit -json | jq -r '.Require[]?.Path')
    while IFS=$'\t' read -r path old new t; do
        if aged "$t"; then
            want+=("$path@$new"); moves+=("$path $old -> $new")
        else
            held+=("$path $new (published $t)")
        fi
    done < <(go list -m -u -json "${req[@]}" | jq -r 'select(.Update) | [.Path, .Version, .Update.Version, .Update.Time] | @tsv')

    if [ "$dry" = 1 ] || [ ${#want[@]} -eq 0 ]; then report; return; fi

    go get "${want[@]}"
    go mod tidy

    # Every require that changed, including ones MVS raised on its own.
    local young=()
    while IFS=$'\t' read -r path new; do
        t=$(go list -m -json "$path@$new" | jq -r '.Time // empty')
        if [ -z "$t" ] || ! aged "$t"; then young+=("$path $new (published ${t:-unknown})"); fi
    done < <(LC_ALL=C comm -13 \
        <(go mod edit -json "$tmp/go.mod.before" | jq -r '.Require[]? | [.Path, .Version] | @tsv' | LC_ALL=C sort) \
        <(go mod edit -json | jq -r '.Require[]? | [.Path, .Version] | @tsv' | LC_ALL=C sort))
    report
    local before_go after_go
    before_go=$(go mod edit -json "$tmp/go.mod.before" | jq -r '"\(.Go) \(.Toolchain.Name // "")"')
    after_go=$(go mod edit -json | jq -r '"\(.Go) \(.Toolchain.Name // "")"')
    [ "$before_go" = "$after_go" ] || echo "note: go directive / toolchain moved: $before_go -> $after_go"
    if [ ${#young[@]} -gt 0 ]; then
        printf '  %s\n' "${young[@]}"
        die "versions above are inside the cooldown; restore with: git restore ${files[*]}"
    fi

    gate "go mod verify" go mod verify
    gate "go build" go build -tags="$tags" ./...
    gate "gotest" scripts/ci/gotest.sh
    gate "govulncheck" scripts/ci/govuln.sh
    gate "license gate" scripts/ci/license_gate.sh

    [ "$commit" = 1 ] && do_commit "${files[@]}"
    return 0
}

# ---- Cargo ----------------------------------------------------------------

# index_path NAME: the crate's path in the crates.io sparse index.
index_path() {
    local n="${1,,}"
    case ${#n} in
        1) echo "1/$n" ;;
        2) echo "2/$n" ;;
        3) echo "3/${n:0:1}/$n" ;;
        *) echo "${n:0:2}/${n:2:2}/$n" ;;
    esac
}

# pubtime NAME VERSION: when VERSION was published. Prints "old" for versions
# that predate the index's pubtime field, and fails for a crate not on crates.io.
pubtime() {
    local f="$tmp/idx-${1,,}"
    [ -s "$f" ] || curl -fsS -A 'boxer-updatedeps' "https://index.crates.io/$(index_path "$1")" >"$f" || return 1
    jq -r --arg v "$2" 'select(.vers == $v) | .pubtime // "old"' "$f" | grep . || return 1
}

# lock_pkgs FILE: "name<TAB>version" for every crates.io package in a Cargo.lock.
lock_pkgs() {
    awk '
        /^\[\[package\]\]/ { if (n != "" && s ~ /crates\.io-index/) print n "\t" v; n = v = s = "" }
        /^name = /    { n = $3; gsub(/"/, "", n) }
        /^version = / { v = $3; gsub(/"/, "", v) }
        /^source = /  { s = $3 }
        END { if (n != "" && s ~ /crates\.io-index/) print n "\t" v }
    ' "$1" | LC_ALL=C sort
}

within_cooldown() { # NAME VERSION -> 0 if it may land
    local t
    t=$(pubtime "$1" "$2") || return 1
    [ "$t" = old ] || aged "$t"
}

# step_back_target NAME VERSION: the version to step a too-young VERSION back
# to: the newest stable, unyanked release older than it that is past the
# cooldown, or that the lockfile already held before this run.
step_back_target() {
    local f="$tmp/idx-${1,,}"
    pubtime "$1" "$2" >/dev/null || return 1
    {
        jq -r --argjson cut "$cutoff" '
            select((.yanked | not)
                and (.vers | split("+")[0] | contains("-") | not)
                and (.pubtime == null or (.pubtime | fromdateiso8601) <= $cut))
            | .vers' "$f"
        lock_pkgs "$tmp/Cargo.lock.before" | awk -F'\t' -v n="$1" '$1 == n { print $2 }'
        echo "$2"
    } | sort -Vu | awk -v y="$2" '$0 == y { if (p != "") print p; exit } { p = $0 }' | grep .
}

# young_in_lock FILE: "name<TAB>version" for each crates.io package FILE gained
# over the starting lockfile that may not land yet.
young_in_lock() {
    local name ver
    while IFS=$'\t' read -r name ver; do
        within_cooldown "$name" "$ver" || printf '%s\t%s\n' "$name" "$ver"
    done < <(LC_ALL=C comm -13 <(lock_pkgs "$tmp/Cargo.lock.before") <(lock_pkgs "$1"))
}

# lock_moves FILE: one "name old -> new" line per package that differs from
# the starting lockfile.
lock_moves() {
    awk -F'\t' '
        NR == FNR { if ($1 in r) r[$1] = r[$1] "," $2; else r[$1] = $2; next }
        { print $1, (($1 in r) ? r[$1] : "(new)"), "->", $2; seen[$1] = 1 }
        END { for (n in r) if (!(n in seen)) print n, r[n], "-> (removed)" }
    ' <(LC_ALL=C comm -23 <(lock_pkgs "$tmp/Cargo.lock.before") <(lock_pkgs "$1")) \
      <(LC_ALL=C comm -13 <(lock_pkgs "$tmp/Cargo.lock.before") <(lock_pkgs "$1")) | LC_ALL=C sort
}

update_cargo() {
    local dir="$target"
    [ -f "$dir/Cargo.lock" ] || die "$dir has no Cargo.lock"
    local files=("$dir/Cargo.lock")
    local hostsum=public/thestack/imzero2/browserhost/tabhost/browserhost.sum
    [ "$dir" = rust/imzero2 ] && files+=("$hostsum")
    require_clean "${files[@]}" "$dir/Cargo.toml"
    cp "$dir/Cargo.lock" "$tmp/Cargo.lock.before"

    local plan name old new t want=()
    plan=$(cd "$dir" && cargo update --dry-run --verbose 2>&1)
    while read -r name old new; do
        old="${old#v}"; new="${new#v}"
        if t=$(pubtime "$name" "$new") && { [ "$t" = old ] || aged "$t"; }; then
            want+=("$name $old $new")
        else
            held+=("$name $new (published ${t:-unknown})")
        fi
    done < <(sed -nE 's/^ *Updating ([^ ]+) (v[^ ]+) -> (v[^ ]+)$/\1 \2 \3/p' <<<"$plan")
    while read -r name old new; do
        manual+=("$name $old -> $new")
    done < <(sed -nE 's/^ *Unchanged ([^ ]+) (v[^ ]+) \(available: (v[^)]+)\)$/\1 \2 \3/p' <<<"$plan")

    if [ "$dry" = 1 ]; then
        for t in "${want[@]}"; do read -r name old new <<<"$t"; moves+=("$name $old -> $new"); done
        report; return
    fi
    # Update everything, then step back what landed inside the cooldown. Applying
    # versions one at a time does not work on a large graph: each --precise drags
    # its neighbours forward, to versions the cooldown would refuse. A step back
    # can be refused until a dependent has stepped back too, hence the rounds.
    held=()
    (cd "$dir" && cargo update --quiet)
    local round progress target_ver young=()
    for round in $(seq 1 25); do
        mapfile -t young < <(young_in_lock "$dir/Cargo.lock")
        [ ${#young[@]} -gt 0 ] || break
        progress=0
        for t in "${young[@]}"; do
            IFS=$'\t' read -r name new <<<"$t"
            target_ver=$(step_back_target "$name" "$new") || continue
            if (cd "$dir" && cargo update --quiet -p "$name@$new" --precise "$target_ver" 2>/dev/null); then
                held+=("$name $new -> stepped back to $target_ver")
                progress=1
            fi
        done
        [ "$progress" = 1 ] || break
    done
    mapfile -t moves < <(lock_moves "$dir/Cargo.lock")
    report
    [ ${#moves[@]} -gt 0 ] || return 0

    # Whatever is still young could not be stepped back.
    mapfile -t young < <(young_in_lock "$dir/Cargo.lock")
    if [ ${#young[@]} -gt 0 ]; then
        printf '  %s\n' "${young[@]}"
        die "versions above are inside the cooldown and could not be stepped back; restore with: git restore ${files[*]}"
    fi

    case "$dir" in
        rust/imzero2)
            GATE_NOSKIP=1 gate "rust/imzero2 check" scripts/ci/rust_imzero2_check.sh
            # ADR-0278 SD5: the host digest moves in the same commit.
            gate "browser host digest" go run -tags="$tags" ./public/thestack/cmd/imzero2tab hostdigest --write ;;
        rust/watermark)      GATE_NOSKIP=1 gate "watermark" scripts/ci/watermark_test.sh ;;
        rust/h3bridge)       GATE_NOSKIP=1 gate "h3 wasm parity" scripts/ci/h3_wasm_parity.sh ;;
        rust/imzero2-viewer) GATE_NOSKIP=1 gate "viewer" scripts/ci/viewer_test.sh ;;
        *)                   gate "cargo test" bash -c "cd '$dir' && cargo test --locked --quiet" ;;
    esac
    gate "license gate" scripts/ci/license_gate.sh

    [ "$commit" = 1 ] && do_commit "${files[@]}"
    return 0
}

case "$target" in
    go) update_go ;;
    rust/*) update_cargo ;;
    *) die "unknown target '$target': go | rust/<crate>" ;;
esac
