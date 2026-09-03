package play

import "sync/atomic"

// ResultID identifies one delivered result of one lane (ADR-0219 SD8): the
// record a pane is handed, together with the metadata that landed with it.
// It is the key a pane's per-result cache should use, in place of the schema
// pointer, the record pointer or the executed timestamp — each of which is an
// accident of the delivery path rather than a contract.
//
// Contract:
//
//   - It changes exactly when the record a pane is handed is replaced — a
//     landed run, a failed run (the record becomes nil), a bound node's memo
//     swap — and never otherwise. Equal ids mean the same record object and
//     the same metadata; a repainted frame carries the id it carried before.
//   - It is unique across lanes and app instances within one process, so a
//     pane that alternates between the active result and a bound node's view
//     cannot alias two results under one id.
//   - Zero is "no result yet".
//   - It is not a content fingerprint: a re-run that returns identical bytes
//     gets a new id. laneView.fingerprint remains the early-cutoff hook for
//     observers that want one (ADR-0097 SD4).
type ResultID uint64

// resultSeq is the process-wide counter behind nextResultID. One counter for
// every lane is what makes the uniqueness clause of ResultID hold without a
// lane namespace in the id.
var resultSeq atomic.Uint64

// nextResultID mints the id for a result that is about to be installed. It is
// called under the installing lane's lock, at the one point the lane replaces
// what it serves — never for a result that is discarded (superseded, or
// landing on a closed lane), so an id that was minted is an id a pane can see.
func nextResultID() ResultID {
	return ResultID(resultSeq.Add(1))
}
