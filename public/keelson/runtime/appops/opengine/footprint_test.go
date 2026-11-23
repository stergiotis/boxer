package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// The footprint reads the log: a command's undeclared change is the
// observed change after it, by the app's frame or between frames; the
// app's changes before any command are nobody's violation, and a refused
// command started nothing, so what follows it is nobody's either.
func TestUndeclaredWritesReadTheLog(t *testing.T) {
	entries := []LogEntry{
		{Seq: 1, Frame: 1, Writer: opwire.WriterApp, Resources: []string{"warmup"}},
		{Seq: 2, Frame: 2, Writer: "task:t", Op: "set_a", CallId: "c1", Phase: opwire.PhaseApplied, Resources: []string{"a"}},
		{Seq: 3, Frame: 2, Writer: opwire.WriterApp, Resources: []string{"a", "b"}},
		{Seq: 4, Frame: 3, Writer: opwire.WriterPerson, Resources: []string{"c"}},
		{Seq: 5, Frame: 4, Writer: opwire.WriterApp, Resources: []string{"d"}},
		{Seq: 6, Frame: 5, Writer: "task:t", Op: "set_c", CallId: "c2", Phase: opwire.PhaseRefused, Reason: "no", Resources: []string{"c"}},
		{Seq: 7, Frame: 5, Writer: opwire.WriterApp, Resources: []string{"e"}},
		{Seq: 8, Frame: 6, Writer: opwire.WriterPerson, Op: "set_b", Phase: opwire.PhaseApplied, Resources: []string{"b"}},
		{Seq: 9, Frame: 6, Writer: opwire.WriterApp, Resources: []string{"b"}},
	}
	got := undeclaredWrites(entries)
	assert.Equal(t, []UndeclaredWrite{
		{Op: "set_a", CallId: "c1", Writer: "task:t", Resource: "b", Frame: 2, Observed: opwire.WriterApp},
		{Op: "set_a", CallId: "c1", Writer: "task:t", Resource: "c", Frame: 3, Observed: opwire.WriterPerson},
		{Op: "set_a", CallId: "c1", Writer: "task:t", Resource: "d", Frame: 4, Observed: opwire.WriterApp},
	}, got)
	assert.Empty(t, undeclaredWrites(nil))
}

// A change an asynchronous command's settlement handler made is logged under
// the command's call id: it is still that command's work, so an undeclared
// resource in it is the command's, and the command's footprint is not
// replaced by what the handler changed.
func TestASettlementsChangeIsTheCommandsWork(t *testing.T) {
	entries := []LogEntry{
		{Seq: 1, Frame: 1, Writer: "task:t", Op: "start", CallId: "c1", Phase: opwire.PhaseApplied, Resources: []string{"a"}},
		{Seq: 2, Frame: 1, Writer: "task:t", Op: "start", CallId: "c1", Phase: opwire.PhaseApplied, Resources: []string{"a", "b"}},
		{Seq: 3, Frame: 2, Writer: opwire.WriterApp, Resources: []string{"b"}},
		{Seq: 4, Frame: 2, Writer: "task:t", Op: "start", CallId: "c1", Phase: opwire.PhaseCompleted},
	}
	assert.Equal(t, []UndeclaredWrite{
		{Op: "start", CallId: "c1", Writer: "task:t", Resource: "b", Frame: 1, Observed: "task:t"},
		{Op: "start", CallId: "c1", Writer: "task:t", Resource: "b", Frame: 2, Observed: opwire.WriterApp},
	}, undeclaredWrites(entries))
}
