// Package persist implements the ADR-0026 §SD3 runtime.persist.{alias}.{key}.{op}
// subject family. The Service subscribes to runtime.persist.>, parses each
// request, and dispatches to a pluggable StorageBackendI for the actual
// read/write/delete. Three backends ship:
//
//   - MemoryBackend, whose contents last exactly as long as the process.
//   - StoreBackend, the durable one the runtime wires (ADR-0105 D3a): a
//     generated record store over the `boxer.persiststate` table, whose
//     state view answers the latest-wins read directly.
//   - FactsBackend, its superseded predecessor, which lands every write as
//     a boxer.facts KindState row. Kept for tests; no production callers.
package persist

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// StorageRef identifies the persist namespace one request addresses.
//
// Alias is authoritative and always set: the subject family is
// alias-addressed by construction (§SD3), the per-app cap is written
// against the alias, and the alias is what the service parses out of the
// subject. Backends key on it.
//
// AppId is the durable identity of the requesting app, attributed by the
// service from the bus envelope (Msg.Sender) exactly as launch and audit
// rows are attributed — never from the request payload. Backends that
// record facts stamp it on the row so a state fact joins that app's grant,
// audit, and launch facts on one column, which is the property §SD6 argues
// the single-table design exists to preserve. It is empty when the sender
// could not be attributed to the addressed alias; a backend that needs an
// id then derives one from Alias via StateAppId.
type StorageRef struct {
	Alias string
	AppId app.AppIdT
}

// StateAppId is the app identity a fact-recording backend should stamp on
// a row: the attributed AppId when the service could vouch for it, else
// the alias promoted to an id so reads and writes still agree with each
// other. The fallback loses the join to that app's other facts; it does
// not lose the state.
func (inst StorageRef) StateAppId() (id app.AppIdT) {
	id = inst.AppId
	if id == "" {
		id = app.AppIdT(inst.Alias)
	}
	return
}

// StorageBackendI is the persistence contract. Keys are addressed by
// (ref.Alias, key) where ref.Alias is the AppIdT.SubjectAlias of the owning
// app. The alias rather than the raw AppId lets the service trust subject
// parsing without a reverse-lookup step; ref.AppId carries the durable
// identity alongside it for backends that record provenance.
//
// All methods are expected to be safe for concurrent use. Errors propagate
// to the requester as PersistReply.Error.
type StorageBackendI interface {
	Get(ref StorageRef, key string) (value []byte, found bool, err error)
	Set(ref StorageRef, key string, value []byte) (err error)
	Delete(ref StorageRef, key string) (err error)
}

// Convenience: a backend that knows the AppId of every call (when the caller
// has it) can chain into a StorageBackendI by deriving the alias once.
func AliasOf(id app.AppIdT) (alias string) {
	alias = id.SubjectAlias()
	return
}
