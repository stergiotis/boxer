CREATE TABLE IF NOT EXISTS rev (
	"id:id:s:4::0:" String CODEC(ZSTD(3)),
	"id:ord:u64:47::0:" UInt64 CODEC(Delta,ZSTD(3)),
	"lc:rev:u32:4::0:" UInt32 CODEC(ZSTD(3)),
	"tv:note:value:val:s:4:::0::data" Array(String) CODEC(ZSTD(3)),
	"tv:note:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:note:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("id:id:s:4::0:", "id:ord:u64:47::0:", "lc:rev:u32:4::0:")
SETTINGS allow_suspicious_low_cardinality_types=1