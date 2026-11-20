package typed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A nil argument is a contract violation the caller can recover from, not an
// exit of the process.
func TestSettersPanicOnNil(t *testing.T) {
	assert.Panics(t, func() { SetCurrentFffiVar(nil) })
	assert.Panics(t, func() { SetCurrentFffiErrorHandler(nil) })
}
