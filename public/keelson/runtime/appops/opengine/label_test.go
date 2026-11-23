package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// oneLabel is a handler with one label for every reader; perReader one that
// says for whom its label does not apply.
type oneLabel struct {
	app.OperationsHandlerI
	confined bool
}

func (inst oneLabel) Confined() bool { return inst.confined }

type perReader struct {
	oneLabel
	clearFor string
}

func (inst perReader) LabelFor() (bool, string) { return inst.confined, inst.clearFor }

// An outcome's label is the window's for every reader but the one task the
// handler names: that task, by the context the dispatcher stamped or by its
// writer, and the person at the window. A handler that names nobody has one
// label for everyone, and an unconfined window labels nobody.
func TestTheWindowsLabelHasTheReaderInIt(t *testing.T) {
	task := func(id string) app.OperationCall {
		return app.OperationCall{Writer: opwire.WriterTask(id), OnBehalfOf: &app.OnBehalfOf{Task: id}}
	}
	person := app.OperationCall{Writer: opwire.WriterPerson}
	own := app.OperationCall{Writer: opwire.WriterApp}

	l := labelOf(perReader{oneLabel: oneLabel{confined: true}, clearFor: "task-1"})
	assert.False(t, l.forCall(task("task-1")), "the task whose own work the window holds")
	assert.False(t, l.forCall(app.OperationCall{Writer: opwire.WriterTask("task-1")}), "named by its writer alone")
	assert.True(t, l.forCall(task("task-2")), "another task")
	assert.True(t, l.forCall(app.OperationCall{Writer: opwire.WriterTask("task-2"), OnBehalfOf: &app.OnBehalfOf{}}), "a context without a task falls back to the writer")
	assert.True(t, l.forCall(app.OperationCall{Writer: opwire.WriterTask("task-1"), OnBehalfOf: &app.OnBehalfOf{Task: "task-2"}}), "the dispatcher's context wins over the writer")
	assert.True(t, l.forCall(app.OperationCall{OnBehalfOf: &app.OnBehalfOf{}}), "an agent's call that names no task")
	assert.False(t, l.forCall(person), "the person at the window, as before")
	assert.True(t, l.forCall(own), "the app's own frame logic is nobody's reading")
	assert.True(t, l.forCall(app.OperationCall{}), "a call that says nothing of its writer")
	assert.True(t, l.confined, "what a capture, a title and a listing take")

	l = labelOf(perReader{oneLabel: oneLabel{confined: true}})
	for _, c := range []app.OperationCall{task("task-1"), person, own} {
		assert.True(t, l.forCall(c), "nobody is named: one label for every reader")
	}
	l = labelOf(oneLabel{confined: true})
	for _, c := range []app.OperationCall{task("task-1"), person, own} {
		assert.True(t, l.forCall(c), "a handler without the per-reader form")
	}
	l = labelOf(perReader{clearFor: "task-1"})
	assert.Empty(t, l.clearFor, "an unconfined window names nobody")
	assert.False(t, l.forCall(task("task-2")))
	assert.False(t, labelOf(oneLabel{}).forCall(task("task-1")))
}
