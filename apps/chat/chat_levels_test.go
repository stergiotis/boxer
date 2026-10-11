package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

func TestStepsClassifyByTheDefaults(t *testing.T) {
	inst := newApp()
	cases := []struct {
		s    turnStep
		want attention.LevelE
	}{
		{turnStep{kind: stepModel, done: true}, attention.LevelData},
		{turnStep{kind: stepModel, done: true, failed: true}, attention.LevelMessage},
		{turnStep{kind: stepTool, done: true, effect: agentrun.EffectNone}, attention.LevelData},
		{turnStep{kind: stepTool, done: true, effect: agentrun.EffectView}, attention.LevelData},
		{turnStep{kind: stepTool, done: true, effect: agentrun.EffectDocument}, attention.LevelMessage},
		{turnStep{kind: stepTool, done: true, effect: agentrun.EffectRun, refused: true}, attention.LevelMessage},
		{turnStep{kind: stepTool, waitSince: time.Now(), effect: agentrun.EffectRun}, attention.LevelYourTurn},
		{turnStep{kind: stepTool, waitSince: time.Now(), effect: agentrun.EffectConsequential}, attention.LevelCritical},
	}
	for i, c := range cases {
		assert.Equal(t, c.want, inst.stepLevel(&c.s), "case %d", i)
	}
}

func TestTheConversationsOverridesApply(t *testing.T) {
	inst := newApp()
	inst.levels.snap = attention.Snapshot{Conversation: map[attention.Key]attention.LevelE{
		{Kind: "intent", Effect: app.OperationEffectView}: attention.LevelHighlight}}
	s := turnStep{kind: stepTool, done: true, effect: agentrun.EffectView}
	assert.Equal(t, attention.LevelHighlight, inst.stepLevel(&s))
}

func TestRoutineRunsFoldButNotTheLastStep(t *testing.T) {
	inst := newApp()
	steps := []turnStep{
		{kind: stepModel, done: true},
		{kind: stepTool, done: true, effect: agentrun.EffectNone},
		{kind: stepModel, done: true},
		{kind: stepTool, done: true, effect: agentrun.EffectRun},
		{kind: stepModel, done: true},
		{kind: stepModel, done: true},
	}
	assert.Equal(t, 3, inst.routineRun(steps, 0), "model, read, model")
	assert.Zero(t, inst.routineRun(steps, 3), "a run stands out")
	assert.Equal(t, 1, inst.routineRun(steps, 4), "the last step is what the turn is doing")
	steps[1].done = false
	assert.Equal(t, 1, inst.routineRun(steps, 0), "a running step ends the run")
}
