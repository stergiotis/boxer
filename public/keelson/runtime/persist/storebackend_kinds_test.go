package persist

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore/statestoretest"
)

// TestStoreBackendStateConformance runs the shared state-store contract
// against the durable backend, over clickhouse-local. statestore runs the
// same cases against its in-memory twin, so the two answer alike — and the
// cases are the facts stores' from before both kinds left `boxer.facts`,
// so the move is checked against the behaviour it replaced.
func TestStoreBackendStateConformance(t *testing.T) {
	statestoretest.Run(t, func(t *testing.T) statestore.StoreI { return newStoreBackend(t) })
}
