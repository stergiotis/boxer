// Package logcomment is the one wire format of the identity stamp boxer
// clients put in ClickHouse's log_comment setting (ADR-0115 §SD7, ADR-0295
// §SD2). log_comment is the one free-form setting a client may fill that
// survives the round trip into system.query_log (query_id survives too, but
// names one execution and is owned by its caller), so a stamp there makes a
// statement attributable from the server's own log, with no boxer process
// running.
//
// Producers — play's client, the recordstore executors — marshal [Stamp]; the
// query-run capture parses it with [Parse]. The JSON keys are the contract:
// renaming one orphans every stamp already in a query log.
package logcomment

import (
	"encoding/json/v2"
	"strings"

	"github.com/stergiotis/boxer/public/identity/callident"
)

// Stamp is the client-side identity riding log_comment. Every field is
// optional — a stamp is whatever subset its producer set — and absent fields
// stay off the wire.
type Stamp struct {
	RunId string `json:"run_id,omitzero"`
	App   string `json:"app,omitzero"`
	// Instance is the window the statement was issued from (ADR-0191 §SD4),
	// so a captured run attributes to the same lane as the app-lifecycle row
	// that opened it.
	Instance   uint64 `json:"instance,omitzero"`
	Lane       string `json:"lane,omitzero"`
	AuthoredFp string `json:"authored_fp,omitzero"`
	SentFp     string `json:"sent_fp,omitzero"`
	ChainFp    string `json:"chain_fp,omitzero"`
	EnvFp      string `json:"env_fp,omitzero"`
	// Task, TaskEpoch and TaskCall name the agent task whose work the run
	// was and the dispatcher's call that caused it (ADR-0277 §SD7); empty for
	// the person's own run.
	Task      string `json:"task,omitzero"`
	TaskEpoch uint64 `json:"task_epoch,omitzero"`
	TaskCall  string `json:"task_call,omitzero"`
	// Principal, Purpose and Correlation are the caller's claims of a
	// callident.CallIdentity (ADR-0295 §SD1). Principal is a pseudonymous
	// reference, never a personal value.
	Principal   string `json:"principal,omitzero"`
	Purpose     string `json:"purpose,omitzero"`
	Correlation string `json:"correlation,omitzero"`
	// Batch is the recordstore batch id of the call (ADR-0295 §SD6), which
	// joins the query-log row to the store's and the observer's record.
	Batch string `json:"batch,omitzero"`
}

// FromCallIdentity is the stamp of a call made for ci, in batch.
func FromCallIdentity(ci callident.CallIdentity, batch string) (st Stamp) {
	st = Stamp{
		RunId:       ci.Origin.Run,
		App:         ci.Origin.App,
		Instance:    ci.Origin.Instance,
		Principal:   ci.Claims.Principal,
		Purpose:     ci.Claims.Purpose,
		Correlation: ci.Claims.Correlation,
		Batch:       batch,
	}
	return
}

// Marshal renders st for the log_comment setting. It returns "" for the zero
// stamp, so a caller can skip the setting. Invalid UTF-8 in a field is
// replaced with U+FFFD rather than refused: the JSON encoder rejects it, and a
// stamp lost to one bad byte would leave the statement anonymous with nothing
// to say so.
func Marshal(st Stamp) (s string) {
	if st == (Stamp{}) {
		return
	}
	for _, f := range [...]*string{&st.RunId, &st.App, &st.Lane, &st.AuthoredFp, &st.SentFp, &st.ChainFp, &st.EnvFp,
		&st.Task, &st.TaskCall, &st.Principal, &st.Purpose, &st.Correlation, &st.Batch} {
		*f = strings.ToValidUTF8(*f, "\uFFFD")
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	s = string(b)
	return
}

// Parse decodes a log_comment stamp. ok is false when the comment is empty,
// not a JSON object, or carries none of the stamp's keys.
func Parse(logComment string) (st Stamp, ok bool) {
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
