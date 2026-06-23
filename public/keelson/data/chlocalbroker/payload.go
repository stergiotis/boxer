package chlocalbroker

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// wireRequest is the envelope sent on ch.local.exec.<pool>, wire-encoded
// via buscodec (CBOR canonical). []byte fields ride as CBOR major type 2
// (byte strings) — no base64 expansion.
type wireRequest struct {
	V         uint8             `json:"v"`
	SQL       string            `json:"sql"`
	Format    string            `json:"format,omitempty"`
	Streaming bool              `json:"streaming,omitempty"`
	Cacheable bool              `json:"cacheable,omitempty"`
	Settings  map[string]string `json:"settings,omitempty"`
	// InputTables rides as a CBOR map of byte strings (ADR-0094 §SD5):
	// table name → Arrow IPC `Arrow` file-format bytes, bound as
	// TEMPORARY tables by the broker. buscodec carries []byte as CBOR
	// major type 2, so no base64 expansion.
	InputTables map[string][]byte `json:"input_tables,omitempty"`
	// DeadlineUnixNanos encodes the caller's ctx.Deadline so the
	// broker can shorten its own execution context. 0 means "no
	// caller-supplied deadline"; the broker falls back to its
	// configured DefaultRequestTimeout. The MsgHandlerFunc signature
	// has no ctx parameter, so deadlines must travel on the wire.
	DeadlineUnixNanos int64 `json:"deadline_ns,omitempty"`
}

// wireReply is the envelope of a successful or failed run, wire-encoded
// via buscodec. On success: OK=true, Body holds the SQL output,
// ContentType is derived from Format. On failure: OK=false, Error
// carries a machine-readable code-ish message and Stderr the captured
// tail.
type wireReply struct {
	V           uint8  `json:"v"`
	OK          bool   `json:"ok"`
	Body        []byte `json:"body,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Error       string `json:"error,omitempty"`
	ExitCode    int32  `json:"exit_code,omitempty"`
	ElapsedNs   int64  `json:"elapsed_ns,omitempty"`
	CacheHit    bool   `json:"cache_hit,omitempty"`
}

const wireVersion uint8 = 1

func encodeRequest(req wireRequest) (b []byte, err error) {
	req.V = wireVersion
	b, err = buscodec.Encode(req)
	if err != nil {
		err = eh.Errorf("chlocalbroker: encode request: %w", err)
	}
	return
}

func decodeRequest(b []byte) (req wireRequest, err error) {
	req, err = buscodec.Decode[wireRequest](b)
	if err != nil {
		err = eh.Errorf("chlocalbroker: decode request: %w", err)
		return
	}
	if req.V == 0 {
		err = eh.Errorf("chlocalbroker: request missing version")
		return
	}
	if req.V > wireVersion {
		err = eh.Errorf("chlocalbroker: request version %d unsupported (max %d)", req.V, wireVersion)
		return
	}
	return
}

func encodeReply(rep wireReply) (b []byte, err error) {
	rep.V = wireVersion
	b, err = buscodec.Encode(rep)
	if err != nil {
		err = eh.Errorf("chlocalbroker: encode reply: %w", err)
	}
	return
}

func decodeReply(b []byte) (rep wireReply, err error) {
	rep, err = buscodec.Decode[wireReply](b)
	if err != nil {
		err = eh.Errorf("chlocalbroker: decode reply: %w", err)
		return
	}
	if rep.V == 0 {
		err = eh.Errorf("chlocalbroker: reply missing version")
		return
	}
	return
}

// contentTypeFor maps a ClickHouse FORMAT name to a best-effort
// content-type string so callers can dispatch parsers without
// re-checking the requested format. Unknown formats default to
// application/octet-stream.
func contentTypeFor(format string) (ct string) {
	switch format {
	case "":
		ct = "application/octet-stream"
	case "TabSeparated", "TSV", "TSVRaw", "TabSeparatedRaw":
		ct = "text/tab-separated-values"
	case "TabSeparatedWithNames", "TSVWithNames", "TabSeparatedWithNamesAndTypes", "TSVWithNamesAndTypes":
		ct = "text/tab-separated-values"
	case "CSV", "CSVWithNames", "CSVWithNamesAndTypes":
		ct = "text/csv"
	case "JSON", "JSONStrings", "JSONCompact", "JSONCompactStrings":
		ct = "application/json"
	case "JSONEachRow", "JSONEachRowWithProgress", "JSONStringsEachRow", "JSONCompactEachRow":
		ct = "application/x-ndjson"
	case "Pretty", "PrettyCompact", "PrettyCompactMonoBlock", "PrettyNoEscapes", "PrettySpace", "PrettyMonoBlock":
		ct = "text/plain; charset=utf-8"
	case "Vertical", "VerticalRaw":
		ct = "text/plain; charset=utf-8"
	case "Markdown":
		ct = "text/markdown"
	case "Arrow", "ArrowStream":
		ct = "application/vnd.apache.arrow.stream"
	case "Parquet":
		ct = "application/vnd.apache.parquet"
	case "Native":
		ct = "application/vnd.clickhouse.native"
	case "Avro":
		ct = "application/vnd.apache.avro"
	case "Protobuf", "ProtobufSingle":
		ct = "application/x-protobuf"
	case "MsgPack":
		ct = "application/msgpack"
	default:
		ct = "application/octet-stream"
	}
	return
}
