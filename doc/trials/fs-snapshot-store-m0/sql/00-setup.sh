#!/bin/bash
# Composes the facts-shaped scratch tables from the repository's own facts
# DDL and creates them. Run from the repository root.
set -euo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root="$here/../../../.."
cols=$(sed -n '/^\t"/p' "$root/public/keelson/runtime/sysmfacts/facts_ddl_clickhouse.out.sql")
ch=${CLICKHOUSE_CLIENT:-clickhouse-client}

facts_table() { # name, index_granularity, order-by tail
  cat <<EOF
CREATE TABLE boxerm0.$1 (
$cols
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD("lc:expiresAt:z64:4::0:")
ORDER BY ($3)
TTL "lc:expiresAt:z64:4::0:"
SETTINGS index_granularity = $2, ttl_only_drop_parts = 1, allow_suspicious_low_cardinality_types = 1;
EOF
}

key='"id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:"'
{
  cat "$here/00-setup.sql"
  facts_table fsmeta          1024 "$key"
  facts_table fssnap          1024 '"id:id:u64:47::0:", "ts:ts:z64:47::0:"'
  facts_table fsdata_facts_1m    1 "$key"
  facts_table fsdata_facts_256k  1 "$key"
  cat <<'EOF'
CREATE TABLE boxerm0.fsdata_bespoke_1m (
  mount UInt64 CODEC(Delta,ZSTD(3)),
  snap  DateTime64(9,'UTC') CODEC(Delta,ZSTD(3)),
  path  String CODEC(ZSTD(3)),
  seq   UInt32 CODEC(T64,ZSTD(3)),
  expires_at DateTime64(9,'UTC') CODEC(ZSTD(3)),
  data  String CODEC(ZSTD(3)),
  hash  String CODEC(ZSTD(3))
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(expires_at)
ORDER BY (mount, snap, path, seq)
TTL expires_at
SETTINGS index_granularity = 1, ttl_only_drop_parts = 1;
EOF
} | $ch --multiquery
echo "boxerm0 ready"
