// Package keelsonqueryrequest is the leeway-coded wire form of a read over
// one introspection table, sent on `keelson.query.<table>` (ADR-0253 §SD2).
// The requesting app is not a payload field — the service attributes it
// from the bus envelope, and the bus audits the request because it is a
// request, not a publish.
//
// Vocabulary: the `kqReq…` cohort in vdd (keelson_dimdata_keelsonquery.go).
package keelsonqueryrequest

import "time"

// KeelsonQueryRequest is the flat wire form of one request.
type KeelsonQueryRequest struct {
	_ struct{} `kind:"keelsonQueryRequest"`

	// FactId is the per-row event id; zero from every producer.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the request instant.
	At time.Time `lw:",ts"`

	// Table names the one introspection table the statement may read. The
	// subject's last token carries the same name; the service refuses a
	// request whose two spellings disagree.
	Table string `lw:"kqReqTable,stringArray"`

	// Sql is the statement, in the introspection dialect: keelson('x') and
	// the bare x are interchangeable (ADR-0094 §SD4). No FORMAT clause —
	// Format names it.
	Sql string `lw:"kqReqSql,textArray"`

	// Format is the ClickHouse FORMAT the reply body is in ("JSONEachRow",
	// "ArrowStream", …). Empty takes the engine's default, and then the
	// reply's content type is the only thing that says what came back.
	Format string `lw:"kqReqFormat,symbol"`

	// ParamName and ParamValue bind `{name:Type}` placeholders by bare name
	// (ADR-0133 §SD2), zipped by index. Typed substitution stays the
	// engine's job.
	ParamName  []string `lw:"kqReqParamName,symbolArray"`
	ParamValue []string `lw:"kqReqParamValue,textArray"`
}
