package hostboot

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/config/env/envtest"
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
	envtest.Set(t, imzero2env.HeadlessListen, "127.0.0.1:9000")
	envtest.Set(t, topo.Mark, "imzero2-demo")
	assert.False(t, sceneLaunched(), "a served appliance is headless but not a scene")
	envtest.Set(t, topo.Mark, "scene-chat-stream")
	assert.True(t, sceneLaunched())
	envtest.Set(t, imzero2env.HeadlessListen, "")
	assert.False(t, sceneLaunched(), "the desktop host is not a scene's")
}
