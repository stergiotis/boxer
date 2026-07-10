CREATE TABLE IF NOT EXISTS provenance (
	"id:id:u64:g:0:0:" UInt64 CODEC(ZSTD(3)),
	"ts:ts:z64:2k:0:0:" DateTime64(9,'UTC') CODEC(Delta,ZSTD(3)),
	"tv:symbol:value:val:s:m:0:24:0::data" Array(LowCardinality(String)) CODEC(ZSTD(3)),
	"tv:symbol:lr:lr:u64:2q:0:0:0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3)),
	"tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data" Array(UInt64) CODEC(T64,ZSTD(3)),
	"tv:symbolArray:value:val:sh:g:0:24:0::data" Array(String) CODEC(ZSTD(3)),
	"tv:symbolArray:lr:lr:u64:2q:0:0:0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3)),
	"tv:symbolArray:len:len:u64:28o:0:0:0::data" Array(UInt64) CODEC(T64,ZSTD(3)),
	"tv:symbolArray:lrcard:lrcard:u64:4gw:0:0:0::data" Array(UInt64) CODEC(T64,ZSTD(3))
) ENGINE = MergeTree()
ORDER BY ("id:id:u64:g:0:0:", "ts:ts:z64:2k:0:0:")
SETTINGS allow_suspicious_low_cardinality_types=1