// Package watchbillrequest is the leeway-coded wire form of a watchbill
// client request, published on `watchbill.job.<op>` (ADR-0234 §SD1). One
// DTO serves the five verbs, River's client surface on the bus: enqueue
// reads the job fields, cancel / retry / get read Id, list reads the
// filter. The requesting app is not a payload field — the worker
// attributes it from the bus envelope (Msg.Sender), the way the window
// host attributes a launch.
//
// Vocabulary: the `wbReq…` cohort in vdd (keelson_dimdata_watchbill.go),
// kind-narrow because every field here is ExactlyOne while the reply's
// job columns are Arbitrary.
package watchbillrequest

import "time"

// The verbs. The subject's last token carries the same word, so a
// subscriber on the pattern can dispatch before decoding; Op is the
// payload's own statement of it and the two must agree.
const (
	OpEnqueue = "enqueue"
	OpCancel  = "cancel"
	OpRetry   = "retry"
	OpGet     = "get"
	OpList    = "list"
)

// WatchbillRequest is the flat wire form of one request.
type WatchbillRequest struct {
	_ struct{} `kind:"watchbillRequest"`

	// FactId is the per-row event id; zero from every producer today.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the request instant.
	At time.Time `lw:",ts"`

	// Op is the verb (one of the Op constants).
	Op string `lw:"wbReqOp,symbol"`

	// Id names the job for cancel, retry and get; for enqueue it is the
	// id to mint under, empty for one of the worker's choosing.
	Id string `lw:"wbReqId,stringArray"`

	// The job to enqueue (ADR-0223 §SD1, §SD6): the handler's kind, the
	// subject row in the consumer's own table, the queue, and the policy.
	Kind          string `lw:"wbReqKind,symbol"`
	Subject       string `lw:"wbReqSubject,textArray"`
	Queue         string `lw:"wbReqQueue,symbol"`
	Priority      uint32 `lw:"wbReqPriority,u32Array"`
	MaxAttempts   uint32 `lw:"wbReqMaxAttempts,u32Array"`
	Backoff       string `lw:"wbReqBackoff,symbol"`
	BackoffBaseMs uint64 `lw:"wbReqBackoffBaseMs,u64Array"`
	TimeoutMs     uint64 `lw:"wbReqTimeoutMs,u64Array"`
	// ArgsKind and Args are the facts-CBOR fallback for a consumer with
	// no subject row (ADR-0135 §SD2): Args claims ArgsKind.
	ArgsKind string `lw:"wbReqArgsKind,symbol"`
	Args     []byte `lw:"wbReqArgs,blobArray"`
	// RunAfterMs is when the job becomes due, Unix milliseconds; zero is
	// now.
	RunAfterMs int64 `lw:"wbReqRunAfterMs,i64Array"`

	// The list filter: any of the states and any of the kinds, at most
	// Limit rows. Empty matches everything the worker's snapshot bound
	// allows.
	States []string `lw:"wbReqStates,symbolArray"`
	Kinds  []string `lw:"wbReqKinds,symbolArray"`
	Limit  uint32   `lw:"wbReqLimit,u32Array"`

	// Note is recorded on the event a cancel or retry writes: why the
	// caller asked.
	Note string `lw:"wbReqNote,textArray"`
}
