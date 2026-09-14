package watchbill

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// Args are a generated facts DTO or nothing: the kind comes from the
// codec, a mismatched claim is refused before any decode, and a type with
// no generated codec cannot be attached at all.
func TestArgsRoundTripThroughTheCodecKind(t *testing.T) {
	// A generated DTO from elsewhere in the runtime stands in for a
	// consumer's own args kind.
	in := launchrequest.LaunchRequest{TargetAppId: "apps/x", ConfigKind: "playLaunch"}
	req, err := WithArgs(Request{Kind: "k"}, in)
	require.NoError(t, err)
	assert.Equal(t, "launchRequest", req.ArgsKind)
	assert.NotEmpty(t, req.Args)

	job := watchbillstore.Job{ID: "j", ArgsKind: req.ArgsKind, Args: req.Args}
	out, err := ArgsOf[launchrequest.LaunchRequest](job)
	require.NoError(t, err)
	assert.Equal(t, in.TargetAppId, out.TargetAppId)
	assert.Equal(t, in.ConfigKind, out.ConfigKind)

	job.ArgsKind = "somethingElse"
	_, err = ArgsOf[launchrequest.LaunchRequest](job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another kind")

	type freeForm struct{ X int }
	_, err = WithArgs(Request{Kind: "k"}, freeForm{X: 1})
	require.Error(t, err, "a struct without a generated codec is not representable")
}
