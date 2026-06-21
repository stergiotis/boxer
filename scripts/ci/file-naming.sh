#!/bin/bash
# boxer Go file / package naming linter (ADR-0048).
#
# Enforces three rules within ./public/ and ./apps/:
#   N1  file basename matches ^[a-z][a-z0-9_]*\.go$
#   N6  package name matches  ^[a-z][a-z0-9]*$   (external <pkg>_test exempt)
#   N7  files directly under apps/<n>/ start with <n>_
#       (exemptions: main.go, doc.go, app_register.go, *_test.go, <n>.go)
#
# Out-of-scope: experiments/*, scripts/dev/*, attic/, *.out.go, *.gen.go.
#
# Output format (one violation per line):
#   file:<path>           rule N1
#   package:<dir>         rule N6
#   app-prefix:<path>     rule N7
#
# --baseline FILE subtracts lines matching the same format (blank lines
# and # comments ignored). --strict makes non-zero exit if there are
# new violations *or* baseline lines that no longer violate (forces
# baseline hygiene; mirrors scripts/ci/entry-points-baseline.txt).

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

baseline=""
strict=0

while [ $# -gt 0 ]; do
    case "$1" in
        --baseline) baseline="$2"; shift 2 ;;
        --strict)   strict=1; shift ;;
        -h|--help)
            sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

audit_paths='public apps'

ls_go_files() {
    find $audit_paths -type f -name '*.go' \
        -not -path '*/attic/*' \
        -not -name '*.out.go' \
        -not -name '*.gen.go' \
        2>/dev/null
}

violations=$(mktemp)
baseline_clean=$(mktemp)
trap 'rm -f $violations $baseline_clean' EXIT

# N1 — file basenames must be snake_case lowercase.
ls_go_files | while read -r f; do
    base=$(basename "$f")
    case "$base" in
        [a-z]*) ;;
        *) echo "file:$f"; continue ;;
    esac
    if ! printf '%s\n' "$base" | grep -qE '^[a-z][a-z0-9_]*\.go$'; then
        echo "file:$f"
    fi
done >> "$violations"

# N6 — package name lowercase, no underscores.
# Per dir: take the first non-_test package decl; check its form.
# External <pkg>_test packages are Go-standard and exempt.
ls_go_files | xargs -n1 dirname 2>/dev/null | sort -u | while read -r d; do
    # `|| true`: a dir containing only external test packages (<pkg>_test)
    # makes the `grep -vE _test$` stage exit 1, which pipefail would
    # propagate; we want pkg="" handled by the next line, not a kill.
    pkg=$(grep -hE '^package [a-zA-Z0-9_]+' "$d"/*.go 2>/dev/null \
        | awk '{print $2}' \
        | grep -vE '_test$' \
        | head -1) || true
    if [ -z "$pkg" ]; then continue; fi
    if ! printf '%s\n' "$pkg" | grep -qE '^[a-z][a-z0-9]*$'; then
        echo "package:$d"
    fi
done >> "$violations"

# N7 — app-prefix for files directly under apps/<n>/.
if [ -d apps ]; then
    find apps -mindepth 2 -maxdepth 2 -type f -name '*.go' \
        -not -path '*/attic/*' \
        -not -name '*.out.go' \
        -not -name '*.gen.go' \
        2>/dev/null | while read -r f; do
        base=$(basename "$f")
        appname=$(printf '%s\n' "$f" | awk -F/ '{print $2}')
        case "$base" in
            main.go|doc.go|app_register.go) continue ;;
            *_test.go) continue ;;
            "${appname}.go") continue ;;
        esac
        case "$base" in
            "${appname}_"*) ;;
            *) echo "app-prefix:$f" ;;
        esac
    done >> "$violations"
fi

sort -u -o "$violations" "$violations"

if [ -z "$baseline" ]; then
    cat "$violations"
    if [ -s "$violations" ]; then exit 1; fi
    exit 0
fi

if [ -f "$baseline" ]; then
    grep -vE '^[[:space:]]*(#|$)' "$baseline" | sort -u > "$baseline_clean"
else
    : > "$baseline_clean"
fi

new=$(comm -23 "$violations" "$baseline_clean")
stale=$(comm -13 "$violations" "$baseline_clean")

if [ -n "$new" ]; then
    echo "new naming violations (not in $baseline):"
    printf '%s\n' "$new" | sed 's/^/  /'
fi
if [ -n "$stale" ]; then
    echo "baseline entries no longer violating (remove these lines from $baseline):"
    printf '%s\n' "$stale" | sed 's/^/  /'
fi

if [ $strict -eq 1 ] && { [ -n "$new" ] || [ -n "$stale" ]; }; then
    exit 1
fi

exit 0
