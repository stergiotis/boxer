package task

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
)

func TestUnitE_StringRoundTrip(t *testing.T) {
	for _, u := range AllUnits {
		t.Run(u.String(), func(t *testing.T) {
			assert.Equal(t, u, ParseUnit(u.String()))
		})
	}
	assert.Equal(t, UnitUnspecified, ParseUnit("nonsense"))
}

func TestTaskCreated_MarshalRoundTrip(t *testing.T) {
	// At lands as u32 unix seconds on the wire — use a
	// second-aligned value (the codec/taskcreated buscodec suite
	// pins the truncation contract).
	in := taskcreated.TaskCreated{
		FactId:       42,
		TaskId:       "abc123",
		Kind:         "ch.export",
		Title:        "Export rows",
		OwnerAppId:   "test.app",
		CancellableB: true,
		EstimatedMs:  30_000,
		At:           time.Unix(1_700_000_000, 0).UTC(),
	}
	b, err := MarshalTaskCreated(in)
	require.NoError(t, err)
	out, err := UnmarshalTaskCreated(b)
	require.NoError(t, err)
	out.NaturalKey = in.NaturalKey // unused entity key; codec canonicalises nil→empty
	assert.Equal(t, in, out)
}

func TestTaskProgress_MarshalRoundTrip(t *testing.T) {
	// At lands as u32 unix seconds on the wire, so use a
	// second-aligned value to avoid sub-second truncation noise; the
	// codec/taskprogress buscodec suite pins the truncation contract.
	in := taskprogress.TaskProgress{
		FactId:           42,
		TaskId:           "abc123",
		Current:          47,
		Total:            100,
		Unit:             "items",
		ThroughputPerSec: 4.7,
		EtaMs:            11_000,
		Note:             "encoding",
		At:               time.Unix(1_700_000_001, 0).UTC(),
	}
	b, err := MarshalTaskProgress(in)
	require.NoError(t, err)
	out, err := UnmarshalTaskProgress(b)
	require.NoError(t, err)
	out.NaturalKey = in.NaturalKey // unused entity key; codec canonicalises nil→empty
	assert.Equal(t, in, out)
}

func TestTaskDone_MarshalRoundTrip(t *testing.T) {
	// At lands as u32 unix seconds — use a second-aligned value.
	// The codec/taskdone buscodec suite pins the scalar-blob wire
	// shape; this test just exercises the task/payload.go entry
	// points after the type move.
	in := taskdone.TaskDone{
		FactId: 5,
		TaskId: "abc123",
		At:     time.Unix(1_700_000_010, 0).UTC(),
		Result: []byte{0x01, 0x02, 0xff},
	}
	b, err := MarshalTaskDone(in)
	require.NoError(t, err)
	out, err := UnmarshalTaskDone(b)
	require.NoError(t, err)
	out.NaturalKey = in.NaturalKey // unused entity key; codec canonicalises nil→empty
	assert.Equal(t, in, out)
}

func TestTaskError_MarshalRoundTrip(t *testing.T) {
	// At lands as u32 unix seconds on the wire — use a
	// second-aligned value (the codec/taskerror buscodec suite pins
	// the truncation contract).
	in := taskerror.TaskError{
		FactId:    11,
		TaskId:    "abc123",
		At:        time.Unix(1_700_000_010, 0).UTC(),
		Reason:    "connect timeout",
		ErrorText: `{"streams":[{"main":[]}]}`,
	}
	b, err := MarshalTaskError(in)
	require.NoError(t, err)
	out, err := UnmarshalTaskError(b)
	require.NoError(t, err)
	out.NaturalKey = in.NaturalKey // unused entity key; codec canonicalises nil→empty
	assert.Equal(t, in, out)
}

func TestTaskCancel_MarshalRoundTrip(t *testing.T) {
	// At lands as u32 unix seconds on the wire — use a
	// second-aligned value to avoid truncation noise (the
	// codec/taskcancel buscodec suite pins the truncation contract).
	in := taskcancel.TaskCancel{
		FactId: 9,
		TaskId: "abc123",
		At:     time.Unix(1_700_000_005, 0).UTC(),
		Reason: "user clicked cancel",
	}
	b, err := MarshalTaskCancel(in)
	require.NoError(t, err)
	out, err := UnmarshalTaskCancel(b)
	require.NoError(t, err)
	out.NaturalKey = in.NaturalKey // unused entity key; codec canonicalises nil→empty
	assert.Equal(t, in, out)
}

func TestUnmarshalTaskCancel_EmptyPayloadIsZeroValue(t *testing.T) {
	c, err := UnmarshalTaskCancel(nil)
	require.NoError(t, err)
	assert.Equal(t, taskcancel.TaskCancel{}, c)
}
