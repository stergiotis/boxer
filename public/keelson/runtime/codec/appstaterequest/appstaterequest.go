// Package appstaterequest is the leeway-coded wire form of an app-state
// manager request, sent on `runtime.appstate.<op>` (ADR-0185 §SD3). One DTO
// serves the two verbs: delete clears one entry, forget clears every entry
// one app keeps. The requesting app is not a payload field — the service
// attributes it from the bus envelope, and the bus audits the request
// because it is a request, not a publish.
//
// Vocabulary: the `asReq…` cohort in vdd (keelson_dimdata_appstate.go).
package appstaterequest

import "time"

// The verbs. The subject's last token carries the same word; Op is the
// payload's own statement of it and the two must agree.
const (
	OpDelete = "delete"
	OpForget = "forget"
)

// AppStateRequest is the flat wire form of one request.
type AppStateRequest struct {
	_ struct{} `kind:"appStateRequest"`

	// FactId is the per-row event id; zero from every producer.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the request instant.
	At time.Time `lw:",ts"`

	// Op is the verb (one of the Op constants).
	Op string `lw:"asReqOp,symbol"`

	// AppId names the app whose state the verb clears.
	AppId string `lw:"asReqAppId,stringArray"`

	// Kind and Key name the entry a delete clears, as keelson('app_state')
	// shows them: `persist` with the persist key, `workingset` with the
	// name, `column_width` with `tier/scope/column_key`.
	Kind string `lw:"asReqKind,symbol"`
	Key  string `lw:"asReqKey,textArray"`

	// EntityId is the store's own key, read for a delete of Kind `unknown`
	// — a row of a kind the host cannot address by (kind, key).
	EntityId string `lw:"asReqEntityId,stringArray"`
}
