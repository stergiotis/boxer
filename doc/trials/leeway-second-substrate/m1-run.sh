#!/usr/bin/env bash
#
# Run one M1 query file on one engine and emit a canonical per-statement result,
# so three engines' answers can be compared by bytes.
#
# Each engine is asked for CSV — the only format all three write for scalar
# columns — and the result is re-emitted through one normaliser, because CSV
# alone is not comparable across them: they disagree on when to quote, on how
# to spell NULL, on trailing zeros in a rounded float, and on the date/time
# separator (DataFusion writes ISO-8601 `T`, the other two a space). The
# normaliser fixes exactly those four spellings and nothing else, so a real
# difference still shows up as a difference.
#
# Statements are split on a `;` ending a line. No M1 query contains one
# anywhere else.
#
# A file may open with a **prelude** — everything above a line reading `-- @@` —
# which is prepended to every statement rather than run once. The ported files
# need it: leeway's physical column names are unusable inline, and neither
# target keeps a view across CLI invocations. So the prelude is where a port
# spells its aliases, which is each engine's answer to ADR-0116 column handles.
#
# Usage:
#   ENGINE=clickhouse DB=jsonbench_m1 FILE=m1-packed.clickhouse.sql OUT=<dir> ./m1-run.sh
#   ENGINE=duckdb     FILE=m1-packed.duckdb.sql     OUT=<dir> [CWD=<dir>] ./m1-run.sh
#   ENGINE=datafusion FILE=m1-exploded.datafusion.sql OUT=<dir> [CWD=<dir>] ./m1-run.sh
#
# CWD is where the engine resolves relative Parquet paths from; it defaults to
# the corpus directory.

set -uo pipefail

ENGINE="${ENGINE:?}"
FILE="${FILE:?}"
OUT="${OUT:?}"
CWD="${CWD:-$(cd "$(dirname "$FILE")" && pwd)/runs/2026-08-07-m1/corpus}"
DB="${DB:-jsonbench_m1}"
DUCKDB="${DUCKDB:-$HOME/.local/bin/duckdb}"
DATAFUSION="${DATAFUSION:-$HOME/.local/bin/datafusion-cli}"

mkdir -p "$OUT"
rm -f "$OUT"/q*.csv "$OUT"/status.tsv
FILE_ABS=$(cd "$(dirname "$FILE")" && pwd)/$(basename "$FILE")

# Canonicalise one engine's CSV: unquote, spell every null as the same token,
# and trim trailing zeros from decimals. Field and row order are left alone —
# the query files carry total orderings so that order is itself under test.
norm() {
  python3 -c '
import csv, sys, re
w = csv.writer(sys.stdout, delimiter="\t", lineterminator="\n")
for row in csv.reader(sys.stdin):
    out = []
    for f in row:
        s = f.strip()
        if s in ("", "\\N", "NULL", "null"):
            out.append("<NULL>"); continue
        if re.fullmatch(r"-?\d+\.\d+", s):
            s = s.rstrip("0").rstrip(".") or "0"
        # ISO-8601 T separator (DataFusion) vs a space (ClickHouse, DuckDB).
        s = re.sub(r"^(\d{4}-\d\d-\d\d)T(\d\d:\d\d:\d\d)", r"\1 \2", s)
        out.append(s)
    w.writerow(out)
'
}

run_one() { # sql -> stdout csv, stderr swallowed into the status file
  case "$ENGINE" in
  # session_timezone=UTC pins H4: `toHour` is server-timezone-dependent, this
  # box runs Europe/Zurich, and the targets have no server timezone at all.
  # Pinning the session leaves the query text alone.
  clickhouse) clickhouse-client --database="$DB" --use_query_condition_cache=0 \
                --min_execution_speed=0 --session_timezone=UTC \
                --format=CSV --query="$1" ;;
  duckdb)     (cd "$CWD" && "$DUCKDB" -csv -noheader -c "$1") ;;
  datafusion) (cd "$CWD" && "$DATAFUSION" --format csv -q -c "$1" | tail -n +2) ;;
  *) echo "unknown ENGINE $ENGINE" >&2; return 2 ;;
  esac
}

# Prelude: everything above a line reading `-- @@`, prepended to each statement.
# Comments are stripped from it: it is passed as one CLI argument, and a
# leading `--` reads as a flag to at least one of the three clients.
if grep -qx -- '-- @@' "$FILE_ABS"; then
  PRELUDE="$(sed -n '1,/^-- @@$/p' "$FILE_ABS" | sed '$d' | sed '/^[[:space:]]*--/d;/^[[:space:]]*$/d')
"
  BODY=$(mktemp); sed '1,/^-- @@$/d' "$FILE_ABS" > "$BODY"
else
  PRELUDE=""
  BODY="$FILE_ABS"
fi

n=0
printf 'stmt\tstatus\trows\tnote\n' > "$OUT/status.tsv"
while IFS= read -r -d '' stmt; do
  [ -z "$(printf '%s' "$stmt" | tr -d '[:space:]')" ] && continue
  n=$((n + 1))
  err=$(mktemp)
  if raw=$(run_one "$PRELUDE$stmt" 2>"$err"); then
    printf '%s\n' "$raw" | norm > "$OUT/q$n.tsv"
    printf '%d\tok\t%d\t\n' "$n" "$(wc -l < "$OUT/q$n.tsv")" >> "$OUT/status.tsv"
  else
    note=$(tr '\n\t' '  ' < "$err" | tr -s ' ' | cut -c1-180)
    printf '%d\terr\t0\t%s\n' "$n" "$note" >> "$OUT/status.tsv"
    : > "$OUT/q$n.tsv"
  fi
  rm -f "$err"
done < <(python3 - "$BODY" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
# Drop line comments, then split on a `;` that ends a line.
src = re.sub(r"(?m)^\s*--.*$", "", src)
for part in re.split(r";\s*(?:\n|$)", src):
    sys.stdout.write(part)
    sys.stdout.write("\0")
PY
)

column -t -s $'\t' "$OUT/status.tsv"
