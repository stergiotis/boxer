CREATE TABLE IF NOT EXISTS minted (
	"id:id:u64:47::0:" UInt64 CODEC(Delta,ZSTD(3)),
	"ts:ts:z64:47::0:" DateTime64(9,'UTC') CODEC(Delta,ZSTD(3)),
	"tv:label:value:val:s:4:::0::data" Array(String) CODEC(ZSTD(3)),
	"tv:label:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:label:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("id:id:u64:47::0:", "ts:ts:z64:47::0:")
SETTINGS allow_suspicious_low_cardinality_types=1