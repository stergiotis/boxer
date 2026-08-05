#!/usr/bin/env bash
#
# Arm A load — the upstream ClickHouse JSON entry's DDL and ingest path, run
# locally as this trial's reference. Measurement is measure.sh, shared with
# arms B and C so every arm is timed by identical code.
#
# Reimplements the pinned upstream load discipline (upstream/PIN.md § Run
# discipline) against a system-installed `clickhouse-client` rather than a
# `./clickhouse` binary in the working directory, and reads the DDL from a
# fetched pin rather than a vendored copy (upstream/fetch-pin.sh).
#
# Usage:
#   JSONBENCH_WORK=<pin checkout> BLUESKY_DATA_DIR=<dir of file_*.json.gz> \
#   OUT=<run dir> TIER=1 ./arm-a.sh

set -euo pipefail

WORK="${JSONBENCH_WORK:?}"
DATA="${BLUESKY_DATA_DIR:?}"
OUT="${OUT:?}"
TIER="${TIER:-1}"
# ARM selects which reference variant to build:
#   a   — the pinned upstream DDL verbatim, clustered on the five backbone paths
#   a0  — the same DDL with the clustered index removed (ORDER BY tuple())
#   a00 — a0 with the JSON type's schema hints removed as well: plain `JSON`,
#         so the engine discovers paths itself at its own defaults
#
# Both controls answer the same objection: a store holding a mixture of
# document shapes cannot declare what arm A declares. It cannot sort on five
# paths most of its rows do not carry (a0), and it cannot name those five
# paths as typed subcolumns either, because for high-variability JSON there is
# no such five (a00). Arm A remains the benchmark's own entry and the upper
# bound on what workload-specific schema knowledge buys.
ARM="${ARM:-a}"

DB="jsonbench_${ARM}_${TIER}m"
TABLE=bluesky

mkdir -p "$OUT"

tmp=$(mktemp -d /var/tmp/jsonbench.XXXXXX)
trap 'rm -rf "$tmp"' EXIT

# Derive the DDL from the pin rather than carrying a second copy: only the
# ORDER BY clause differs, and deriving it keeps that visible and keeps the
# hashes in upstream/PIN.md authoritative.
ddl="$tmp/ddl.sql"
case "$ARM" in
a)
  cp "$WORK/clickhouse/ddl.sql" "$ddl"
  ;;
a0 | a00)
  python3 - "$WORK/clickhouse/ddl.sql" "$ddl" "$ARM" <<'PY'
import re, sys
src, dst, arm = open(sys.argv[1]).read(), sys.argv[2], sys.argv[3]
# Replace the whole ORDER BY (...) block, up to the trailing comment/SETTINGS.
out, n = re.subn(r'ORDER BY\s*\(.*?\)\s*(?=\n\s*(?:--|SETTINGS))',
                 'ORDER BY tuple()\n', src, flags=re.S)
if n != 1:
    sys.exit(f"expected exactly one ORDER BY block in the pinned DDL, replaced {n}")
if arm == 'a00':
    # Strip the JSON type's parameter list — the max_dynamic_paths bound and
    # the five typed paths — leaving a bare JSON column. Greedy up to the last
    # ')' before CODEC, since the parameter list itself contains parentheses.
    out, n = re.subn(r'JSON\(.*\)\s*CODEC', 'JSON CODEC', out, flags=re.S)
    if n != 1:
        sys.exit(f"expected exactly one JSON(...) type in the pinned DDL, replaced {n}")
open(dst, 'w').write(out)
PY
  ;;
*)
  echo "unknown ARM: $ARM" >&2; exit 1
  ;;
esac
grep -q 'ORDER BY' "$ddl" || { echo "derived DDL lost its ORDER BY" >&2; exit 1; }
cp "$ddl" "$OUT/ddl-as-applied.sql"

clickhouse-client -q "DROP DATABASE IF EXISTS $DB"
clickhouse-client -q "CREATE DATABASE $DB"
clickhouse-client --database="$DB" --enable_json_type=1 --multiquery < "$ddl"

for f in $(ls "$DATA"/*.json.gz | head -n "$TIER"); do
  u="$tmp/$(basename "${f%.gz}")"
  gunzip -c "$f" > "$u"
  # Note: the gunzip above is outside the timed section, matching upstream —
  # so this wall clock is not comparable with the facts arms', which
  # decompress inline. results.md says so where the numbers are reported.
  #
  # The retry is upstream's, not an embellishment (upstream/PIN.md § Run
  # discipline). Under `max_dynamic_paths = 0` the JSON type rejects
  # sufficiently baroque documents outright — the Bluesky corpus contains a
  # few, first hit in file 5 — and upstream's loader answers by re-running the
  # file with error tolerance wide open, which *skips* the offending rows.
  # So arm A can hold fewer rows than the tier nominally contains; the
  # difference is recorded rather than hidden (upstream's own results carry
  # `num_loaded_documents` beside `dataset_size` for the same reason).
  if ! /usr/bin/time -v clickhouse-client \
      --query="INSERT INTO $DB.$TABLE SETTINGS min_insert_block_size_rows = 1000000, min_insert_block_size_bytes = 0 FORMAT JSONAsObject" \
      < "$u" 2>>"$OUT/ingest.time"; then
    echo "first attempt failed for $(basename "$f"); retrying with errors allowed" >> "$OUT/ingest.time"
    /usr/bin/time -v clickhouse-client \
      --query="INSERT INTO $DB.$TABLE SETTINGS min_insert_block_size_rows = 1000000, min_insert_block_size_bytes = 0, input_format_allow_errors_num = 1000000000, input_format_allow_errors_ratio = 1 FORMAT JSONAsObject" \
      < "$u" 2>>"$OUT/ingest.time" \
      || echo "both attempts failed for $(basename "$f")" >> "$OUT/ingest.time"
  fi
  rm -f "$u"
done

clickhouse-client --database="$DB" -q "SELECT count() FROM $TABLE"
