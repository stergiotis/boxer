// Package queryrunfacts is the pure library of the queryrunsd capture
// pipeline (ADR-0115 S1): it turns terminal system.query_log events into
// runtime.facts entities of kind QueryRun. It owns the extract SQL
// (extract.go), the refreshable-MV pipeline DDL (mv.go), and the
// row→entity encoding below — all side-effect free; the HTTP service and
// boot reconciliation live in runtime/queryrunsvc.
//
// The encoding runs the same generated DML builders every other facts
// writer uses (factsschema/dml.InEntityFacts) — single-sourcing the
// leeway encoder is the reason the transform lives in a Go service at
// all (ADR-0115 C1/O6).
package queryrunfacts

import (
	"encoding/binary"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// KindLabel is the human-readable kind tag on the symbol section — the
// same convention as chstore's "grant" / "log" / "runtime-run" labels.
const KindLabel = "query-run"

// QueryTextCap bounds the inline query text (bytes, before the rune-safe
// trim). Interning full texts is deferred to the DimensionStore substrate
// (ADR-0112); until then a run carries a capped inline copy plus the
// fingerprints from the log_comment stamp. The extract SQL pre-caps
// server-side with substring() so oversized texts never cross the wire.
const QueryTextCap = 16384

// ExceptionTextCap bounds the inline exception text (bytes, before the
// rune-safe trim) — ClickHouse exception strings can embed stack traces.
const ExceptionTextCap = 4096

// IdBand is the reserved deterministic-id band (ADR-0115 SD2): capture
// ids carry the top bit so they cannot collide with the in-process
// counter ids other facts writers mint. ADR-0111 leased ranges are the
// eventual unification.
const IdBand uint64 = 1 << 63

// Row is one terminal system.query_log event as the extract SQL selects
// it (FORMAT JSONEachRow, output_format_json_quote_64bit_integers=0 —
// encoding/json decodes integer literals into uint64 fields exactly, so
// full-range values like normalized_query_hash survive).
type Row struct {
	Type           string            `json:"type"`
	EventUs        int64             `json:"event_us"`
	QueryId        string            `json:"query_id"`
	Query          string            `json:"query"`
	NormalizedHash uint64            `json:"normalized_query_hash"`
	QueryKind      string            `json:"query_kind"`
	DurationMs     uint64            `json:"query_duration_ms"`
	ReadRows       uint64            `json:"read_rows"`
	ReadBytes      uint64            `json:"read_bytes"`
	WrittenRows    uint64            `json:"written_rows"`
	WrittenBytes   uint64            `json:"written_bytes"`
	ResultRows     uint64            `json:"result_rows"`
	ResultBytes    uint64            `json:"result_bytes"`
	MemoryUsage    uint64            `json:"memory_usage"`
	ExceptionCode  int32             `json:"exception_code"`
	Exception      string            `json:"exception"`
	ProfileEvents  map[string]uint64 `json:"ProfileEvents"`
	LogComment     string            `json:"log_comment"`
}

// Ts is the fact timestamp: the event's own microsecond time, so the
// destination watermark in the extract SQL compares like-for-like.
func (inst Row) Ts() time.Time {
	return time.UnixMicro(inst.EventUs).UTC()
}

// Stamp is the client-side identity riding log_comment (ADR-0115 SD7).
// All fields optional — a stamp is whatever subset the client set; rows
// without a parseable stamp still capture, just without lifted identity.
type Stamp struct {
	RunId      string `json:"run_id"`
	App        string `json:"app"`
	Lane       string `json:"lane"`
	AuthoredFp string `json:"authored_fp"`
	SentFp     string `json:"sent_fp"`
	ChainFp    string `json:"chain_fp"`
	EnvFp      string `json:"env_fp"`
}

// ParseStamp decodes a log_comment stamp. ok is false when the comment
// is empty, not JSON, or carries none of the stamp fields.
func ParseStamp(logComment string) (st Stamp, ok bool) {
	if logComment == "" || !strings.HasPrefix(strings.TrimSpace(logComment), "{") {
		return
	}
	if json.Unmarshal([]byte(logComment), &st) != nil {
		st = Stamp{}
		return
	}
	ok = st != Stamp{}
	return
}

// DeterministicId derives the fact id from the event's own identity, so
// every re-read of the same query_log row (url() read amplification,
// overlap re-extraction, catch-up after downtime) yields the same id and
// the MV anti-join can discard it (ADR-0115 SD2). The event time and
// type are part of the hash because a query_id is NOT unique per
// execution: play deliberately reuses a stable per-lane query_id
// (ADR-0097 SD5), so successive runs of one lane share it — they are
// distinct events at distinct microseconds.
func DeterministicId(queryId string, eventUs int64, eventType string) (id uint64) {
	h := blake3.New(8, nil)
	_, _ = h.Write([]byte(KindLabel))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(queryId))
	_, _ = h.Write([]byte{0})
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(eventUs))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(eventType))
	id = binary.BigEndian.Uint64(h.Sum(nil)) | IdBand
	return
}

// EncodeEntity encodes one row into ent (BeginEntity through the last
// section, no CommitEntity — the caller commits, mirroring chstore's
// encodeLogEntity contract). The natural key is the raw query_id — the
// correlation anchor a future client-side emitter joins on; uniqueness
// across runs is carried by the deterministic id, not the key.
func EncodeEntity(ent *dml.InEntityFacts, row Row) {
	id := DeterministicId(row.QueryId, row.EventUs, row.Type)
	ent.BeginEntity().SetId(id, []byte(row.QueryId)).SetTimestamp(row.Ts())

	st, hasStamp := ParseStamp(row.LogComment)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute(KindLabel).AddMembershipLowCardRef(vocab.MembKindQueryRun.GetId().Value()).EndAttribute()
	if row.Type != "" {
		sym.BeginAttribute(row.Type).AddMembershipLowCardRef(vocab.MembQueryRunEventType.GetId().Value()).EndAttribute()
	}
	if row.QueryKind != "" {
		sym.BeginAttribute(row.QueryKind).AddMembershipLowCardRef(vocab.MembQueryRunQueryKind.GetId().Value()).EndAttribute()
	}
	if hasStamp {
		if st.App != "" {
			sym.BeginAttribute(st.App).AddMembershipMixedLowCardRef(
				vocab.MembRuntimeApp.GetId().Value(), []byte(st.App)).EndAttribute()
		}
		if st.RunId != "" {
			sym.BeginAttribute(st.RunId).AddMembershipMixedLowCardRef(
				vocab.MembRuntimeRun.GetId().Value(), []byte(st.RunId)).EndAttribute()
		}
		if st.Lane != "" {
			sym.BeginAttribute(st.Lane).AddMembershipLowCardRef(vocab.MembQueryRunLane.GetId().Value()).EndAttribute()
		}
	}
	sym.EndSection()

	str := ent.GetSectionStringArray()
	if row.Query != "" {
		str.BeginAttributeSingle(truncateRuneSafe(row.Query, QueryTextCap)).
			AddMembershipLowCardRef(vocab.MembQueryRunQueryText.GetId().Value()).EndAttribute()
	}
	if row.Exception != "" {
		str.BeginAttributeSingle(truncateRuneSafe(row.Exception, ExceptionTextCap)).
			AddMembershipLowCardRef(vocab.MembQueryRunExceptionText.GetId().Value()).EndAttribute()
	}
	if hasStamp {
		for _, fp := range []struct {
			value string
			memb  uint64
		}{
			{st.AuthoredFp, vocab.MembQueryRunAuthoredFp.GetId().Value()},
			{st.SentFp, vocab.MembQueryRunSentFp.GetId().Value()},
			{st.ChainFp, vocab.MembQueryRunChainFp.GetId().Value()},
			{st.EnvFp, vocab.MembQueryRunEnvFp.GetId().Value()},
		} {
			if fp.value != "" {
				str.BeginAttributeSingle(fp.value).AddMembershipLowCardRef(fp.memb).EndAttribute()
			}
		}
	}
	str.EndSection()

	u64 := ent.GetSectionU64Array()
	// normalized_query_hash always writes — it is the QueryDef join key;
	// counters follow the audit-row precedent of absent-when-zero.
	u64.BeginAttributeSingle(row.NormalizedHash).AddMembershipLowCardRef(vocab.MembQueryRunNormalizedHash.GetId().Value()).EndAttribute()
	for _, c := range []struct {
		value uint64
		memb  uint64
	}{
		{row.DurationMs, vocab.MembQueryRunDurationMs.GetId().Value()},
		{row.ReadRows, vocab.MembQueryRunReadRows.GetId().Value()},
		{row.ReadBytes, vocab.MembQueryRunReadBytes.GetId().Value()},
		{row.WrittenRows, vocab.MembQueryRunWrittenRows.GetId().Value()},
		{row.WrittenBytes, vocab.MembQueryRunWrittenBytes.GetId().Value()},
		{row.ResultRows, vocab.MembQueryRunResultRows.GetId().Value()},
		{row.ResultBytes, vocab.MembQueryRunResultBytes.GetId().Value()},
		{row.MemoryUsage, vocab.MembQueryRunMemoryPeakBytes.GetId().Value()},
	} {
		if c.value > 0 {
			u64.BeginAttributeSingle(c.value).AddMembershipLowCardRef(c.memb).EndAttribute()
		}
	}
	if len(row.ProfileEvents) > 0 {
		// Sorted for deterministic encoding; the event NAME is the
		// high-card parameter (the MembLogField pattern).
		peMembId := vocab.MembQueryRunProfileEvent.GetId().Value()
		names := make([]string, 0, len(row.ProfileEvents))
		for name := range row.ProfileEvents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			u64.BeginAttributeSingle(row.ProfileEvents[name]).
				AddMembershipMixedLowCardRef(peMembId, []byte(name)).EndAttribute()
		}
	}
	u64.EndSection()

	if row.ExceptionCode != 0 {
		i64 := ent.GetSectionI64Array()
		i64.BeginAttributeSingle(int64(row.ExceptionCode)).AddMembershipLowCardRef(vocab.MembQueryRunExceptionCode.GetId().Value()).EndAttribute()
		i64.EndSection()
	}
}

// BuildEntities encodes and commits every row into a fresh builder. The
// caller transfers the records (and releases them); an empty rows slice
// yields a builder whose TransferRecords returns no records — the
// service serves a schema-only stream in that case.
func BuildEntities(ent *dml.InEntityFacts, rows []Row) (err error) {
	for i := range rows {
		EncodeEntity(ent, rows[i])
		err = ent.CommitEntity()
		if err != nil {
			err = eh.Errorf("queryrunfacts: commit entity %d (query_id %q): %w", i, rows[i].QueryId, err)
			return
		}
	}
	return
}

// truncateRuneSafe caps s at limit bytes without splitting a UTF-8
// sequence — the Arrow string columns expect valid UTF-8.
func truncateRuneSafe(s string, limit int) (out string) {
	if len(s) <= limit {
		out = s
		return
	}
	out = s[:limit]
	for len(out) > 0 && !utf8.ValidString(out) {
		out = out[:len(out)-1]
	}
	return
}
