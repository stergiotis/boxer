CREATE TABLE IF NOT EXISTS seq (
	"id:id:u64:47::0:" UInt64 CODEC(Delta,ZSTD(3)),
	"id:eid:u64:47::0:" UInt64 CODEC(Delta,ZSTD(3)),
	"lc:lifecycle:u8:4::0:" UInt8 CODEC(ZSTD(3)),
	"tv:measure:value:val:u64:4:::0::data" Array(UInt64) CODEC(ZSTD(3)),
	"tv:measure:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:measure:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("id:id:u64:47::0:", "id:eid:u64:47::0:")
SETTINGS allow_suspicious_low_cardinality_types=1