#!/bin/bash
# M0 checks 5, 8, 9 and 10: everything that needs generated Go.
#
# The generated store is ~9 300 lines and throwaway, so it is not committed
# and it is not generated into the repository: this script builds a scratch
# module beside the tree — its own go.mod with a `replace` back to the
# repository — copies the committed templates in, generates, compiles and
# runs. `go build ./...` in the repository never sees any of it.
#
# Needs: a ClickHouse at $CLICKHOUSE_ENDPOINT (default localhost:8123).
# Leaves the scratch module behind when M0_WORKDIR is set, so the generated
# code can be read.
set -euo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(readlink -f "$here/../../..")
work=${M0_WORKDIR:-$(mktemp -d)}
tags=$(cat "$root/tags" | tr -d '\n')

echo "scratch module: $work"
mkdir -p "$work"/{generate,fsmeta,fsdata,exercise,bench,corpus,blake3}
{
  echo "module m0gen"; echo
  sed -n '/^go /p;/^toolchain /p' "$root/go.mod"; echo
  sed -n '/^require (/,/^)/p' "$root/go.mod"; echo
  echo "require github.com/stergiotis/boxer v0.0.0"; echo
  echo "replace github.com/stergiotis/boxer => $root"
} > "$work/go.mod"
cp "$root/go.sum" "$work/go.sum"

cp "$here/gen/generate.go.tmpl"    "$work/generate/main.go"
cp "$here/gen/fsentry_dto.go.tmpl" "$work/fsmeta/fsentry_dto.go"
cp "$here/gen/fsblock_dto.go.tmpl" "$work/fsdata/fsblock_dto.go"
cp "$here/gen/exercise.go.tmpl"    "$work/exercise/main.go"
cp "$here/gen/bench.go.tmpl"       "$work/bench/main.go"
cp "$here/gen/corpus.go.tmpl"      "$work/corpus/main.go"
cp "$here/gen/blake3.go.tmpl"      "$work/blake3/main.go"

export GOWORK=off
cd "$work"

echo "== check 5a: does recordstore/gen accept the combination? =="
go run -tags="$tags" ./generate

echo "== check 5b: does the emitted store compile? =="
go build -tags="$tags" ./...
echo "  compiles; $(cat fsmeta/*.out.go fsmeta/internal/lowlevel/*.out.go fsdata/*.out.go fsdata/internal/lowlevel/*.out.go | wc -l) lines emitted"

echo "== check 5c: EnsureTable / ALTERs / VerifySchema / many rows per (id,ts) / Scan =="
go run -tags="$tags" ./exercise

echo "== check 8: insert throughput, facts-shaped against bespoke =="
M0_BLOCK_KIB=1024 go run -tags="$tags" ./bench
M0_BLOCK_KIB=256  go run -tags="$tags" ./bench

echo "== check 9: compression on one representative mount =="
for kib in 1024 256; do
  for lvl in 3 1; do
    M0_CORPUS_ROOT="$root/public" M0_BLOCK_KIB=$kib M0_ZSTD=$lvl go run -tags="$tags" ./corpus
    "${CLICKHOUSE_CLIENT:-clickhouse-client}" -q "OPTIMIZE TABLE boxerm0.fsdata_corpus FINAL" < /dev/null
    "${CLICKHOUSE_CLIENT:-clickhouse-client}" -q "
      SELECT 'data column' AS seg,
             formatReadableSize(sum(column_data_uncompressed_bytes)) AS uncompressed,
             formatReadableSize(sum(column_data_compressed_bytes)) AS compressed,
             round(sum(column_data_uncompressed_bytes) / sum(column_data_compressed_bytes), 2) AS ratio
      FROM system.parts_columns
      WHERE database = 'boxerm0' AND table = 'fsdata_corpus' AND active
        AND column = 'tv:blobArray:value:val:yh:4:::0::data'
      UNION ALL
      SELECT 'the other 184', formatReadableSize(sum(column_data_uncompressed_bytes)),
             formatReadableSize(sum(column_data_compressed_bytes)),
             round(sum(column_data_uncompressed_bytes) / sum(column_data_compressed_bytes), 2)
      FROM system.parts_columns
      WHERE database = 'boxerm0' AND table = 'fsdata_corpus' AND active
        AND column != 'tv:blobArray:value:val:yh:4:::0::data'" < /dev/null
    "${CLICKHOUSE_CLIENT:-clickhouse-client}" -q "
      SELECT formatReadableSize(sum(bytes_on_disk)) AS on_disk,
             formatReadableSize(sum(marks_bytes)) AS marks,
             round(sum(marks_bytes) / sum(rows), 1) AS marks_bytes_per_row
      FROM system.parts WHERE database = 'boxerm0' AND table = 'fsdata_corpus' AND active" < /dev/null
  done
done

echo "== check 10: BLAKE3 subtree chaining values and Bao chunk groups =="
go run -tags="$tags" ./blake3
"${CLICKHOUSE_CLIENT:-clickhouse-client}" -q "SELECT hex(BLAKE3('hello world'))" < /dev/null

if [ -z "${M0_WORKDIR:-}" ]; then rm -rf "$work"; fi
