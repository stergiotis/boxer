//go:build llm_generated_opus47

package inprocbus

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestSubjectAllowed_PubExact(t *testing.T) {
	caps := []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	}
	assert.True(t, SubjectAllowed(caps, "ch.query.boxer", app.CapDirectionPub))
	assert.False(t, SubjectAllowed(caps, "ch.query.boxer", app.CapDirectionSub))
	assert.False(t, SubjectAllowed(caps, "ch.query.spinnaker", app.CapDirectionPub))
}

func TestSubjectAllowed_BothCoversBoth(t *testing.T) {
	caps := []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionBoth},
	}
	assert.True(t, SubjectAllowed(caps, "fs.dialog.read", app.CapDirectionPub))
	assert.True(t, SubjectAllowed(caps, "fs.handle.abc.read", app.CapDirectionSub))
}

func TestSubjectAllowed_SubOnly(t *testing.T) {
	caps := []app.SubjectFilter{
		{Pattern: "app.*.event.>", Direction: app.CapDirectionSub},
	}
	assert.True(t, SubjectAllowed(caps, "app.play.event.row_selected", app.CapDirectionSub))
	assert.False(t, SubjectAllowed(caps, "app.play.event.row_selected", app.CapDirectionPub))
}

func TestSubjectAllowed_FirstMatchingCapWins(t *testing.T) {
	caps := []app.SubjectFilter{
		{Pattern: "fs.dialog.write", Direction: app.CapDirectionPub},
		{Pattern: "fs.>", Direction: app.CapDirectionPub},
	}
	assert.True(t, SubjectAllowed(caps, "fs.dialog.read", app.CapDirectionPub))
}

func TestSubjectAllowed_EmptyCapsAlwaysDeny(t *testing.T) {
	assert.False(t, SubjectAllowed(nil, "x", app.CapDirectionPub))
	assert.False(t, SubjectAllowed([]app.SubjectFilter{}, "x", app.CapDirectionPub))
}
