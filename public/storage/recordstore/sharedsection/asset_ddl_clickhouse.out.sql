CREATE TABLE IF NOT EXISTS asset (
	"id:id:u64:::0:" UInt64,
	"ts:ts:z64:::0:" DateTime64(9,'UTC'),
	"tv:symbol:value:val:s::::0::data" Array(String),
	"tv:symbol:lr:lr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3)),
	"tv:symbol:lrcard:lrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("id:id:u64:::0:", "ts:ts:z64:::0:")
SETTINGS allow_suspicious_low_cardinality_types=1