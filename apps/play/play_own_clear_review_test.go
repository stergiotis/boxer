package play

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// A run on this process's plane reads a dataset by its handle for as long
// as the handle is registered, which outlasts the dataset's retraction by
// a grace. A statement that joined the task's own clear publication with a
// dataset a person published, and whose label is asked after the person's
// handle has left the plane, is labelled all the same: the label is judged
// by every handle the statement names, not by the ones still registered
// when the run ends.
func TestAHandleThatLeftThePlaneDuringTheRunStillLabelsIt(t *testing.T) {
	r := newOwnClearRig(t)
	handle := r.appPublishes("kernel_top")
	theirs := r.personPublishes("people")
	obo := r.attestedCall()
	ctx := t.Context()
	confined := queryengine.SensitivityConfined
	stmt := "SELECT 1 FROM keelson('" + handle + "') AS a JOIN keelson('" + theirs + "') AS b USING x"
	bare := "SELECT 1 FROM keelson('" + handle + "') AS a JOIN " + theirs + " AS b USING x"
	require.False(t, r.cl.ownClearRun(ctx, confined, stmt, obo, nil), "while both are registered the statement is labelled")

	// The run was placed and sent as confined, and is reading; the person
	// retracts the bundle and the grace passes before the run ends.
	require.NoError(t, adhocdata.RetractBundleRequest(ctx, r.person, "people", nil))
	r.adhoc.FlushRetracts()

	assert.False(t, r.cl.ownClearRun(ctx, confined, stmt, obo, nil), "the person's rows are in the result, whether or not their handle is still registered")
	assert.False(t, r.cl.ownClearRun(ctx, confined, bare, obo, nil), "named as a bare table too")
	dec := dispatchDecision{sensitivity: confined}
	assert.False(t, failedOwnClear(r.cl.markOwnClearFailure(ctx, errors.New("boom"), dec, stmt, obo, nil)), "and a failure of that run carries the label")
	assert.True(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM keelson('"+handle+"')", obo, nil), "the task's own publication alone still reads back unlabelled")
}
