package chserver

import (
	jsonv2 "encoding/json/v2"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// ParseSummary reads the counters ClickHouse reports in
// `X-ClickHouse-Summary` — a flat JSON object of string-encoded numbers.
//
// The in-band `X-ClickHouse-Progress` header carries the same shape (a
// prefix of these fields), so one parser serves both the finished run's
// summary and every live tick.
//
// A header that is absent or malformed yields the zero summary rather than
// an error. Every field of it means "not reported", and this plane is
// advisory throughout: a run is not wrong because its counters were
// unreadable.
func ParseSummary(header string) (out queryengine.Summary) {
	if header == "" {
		return
	}
	kv := map[string]string{}
	if err := jsonv2.Unmarshal([]byte(header), &kv); err != nil {
		return
	}
	u := func(key string) (n uint64) {
		n, _ = strconv.ParseUint(kv[key], 10, 64)
		return
	}
	out.ReadRows = u("read_rows")
	out.ReadBytes = u("read_bytes")
	out.WrittenRows = u("written_rows")
	out.WrittenBytes = u("written_bytes")
	out.TotalRowsToRead = u("total_rows_to_read")
	out.ResultRows = u("result_rows")
	out.ResultBytes = u("result_bytes")
	out.ElapsedNs = u("elapsed_ns")
	out.MemoryUsage = u("memory_usage")
	return
}
