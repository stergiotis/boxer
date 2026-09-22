package statestore_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore/statestoretest"
)

// TestMemoryConformance runs the shared contract against the in-memory
// store; persist runs the same cases against the durable backend.
func TestMemoryConformance(t *testing.T) {
	statestoretest.Run(t, func(t *testing.T) statestore.StoreI { return statestore.NewMemory() })
}
