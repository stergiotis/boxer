package hostboot

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// ADR-0269 §SD6 and M6: test grants skip the person, a scripted model's
// answers are not a model's, and the action file is for trials, so every
// host but one a scene launched — the desktop, and a served headless
// appliance — refuses all three.
func TestOnlyASceneHostTakesTheTestLaneKnobs(t *testing.T) {
	assert.False(t, testLaneOnly(true, false))
	assert.True(t, testLaneOnly(true, true))
	assert.False(t, testLaneOnly(false, true))
}

func TestSceneLaunchedNeedsTheMarkAndTheCarrier(t *testing.T) {
	imzero2env.HeadlessListen.SetForTest(t, "127.0.0.1:9000")
	topo.Mark.SetForTest(t, "imzero2-demo")
	assert.False(t, sceneLaunched(), "a served appliance is headless but not a scene")
	topo.Mark.SetForTest(t, "scene-chat-stream")
	assert.True(t, sceneLaunched())
	imzero2env.HeadlessListen.SetForTest(t, "")
	assert.False(t, sceneLaunched(), "the desktop host is not a scene's")
}
