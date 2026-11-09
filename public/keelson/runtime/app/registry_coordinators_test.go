package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_Coordinator(t *testing.T) {
	reg := NewRegistry()
	id := AppIdT("github.com/example/apps/chat")
	assert.False(t, reg.Coordinator(id), "none until the host resolved them")
	reg.SetCoordinators([]string{string(id)})
	assert.True(t, reg.Coordinator(id))
	reg.SetCoordinators([]string{id.SubjectAlias()})
	assert.True(t, reg.Coordinator(id), "a subject alias names the app too")
	assert.False(t, reg.Coordinator("github.com/example/apps/other"))
}
