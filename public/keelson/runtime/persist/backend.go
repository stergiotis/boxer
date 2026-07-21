// Package persist implements the ADR-0026 §SD3 runtime.persist.{alias}.{key}.{op}
// subject family. The Service subscribes to runtime.persist.>, parses each
// request, and dispatches to a pluggable StorageBackendI for the actual
// read/write/delete. M2.4 ships an in-memory backend; M2.5 introduces a
// boxer.facts-backed implementation that lands grants + audit + state
// writes through the same CH+leeway table.
package persist

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// StorageBackendI is the persistence contract. Keys are addressed by
// (appAlias, key) where appAlias is the AppIdT.SubjectAlias of the owning
// app. The alias rather than the raw AppId lets the service trust subject
// parsing without a reverse-lookup step.
//
// All methods are expected to be safe for concurrent use. Errors propagate
// to the requester as PersistReply.Error.
type StorageBackendI interface {
	Get(appAlias string, key string) (value []byte, found bool, err error)
	Set(appAlias string, key string, value []byte) (err error)
	Delete(appAlias string, key string) (err error)
}

// Convenience: a backend that knows the AppId of every call (when the caller
// has it) can chain into a StorageBackendI by deriving the alias once.
func AliasOf(id app.AppIdT) (alias string) {
	alias = id.SubjectAlias()
	return
}
