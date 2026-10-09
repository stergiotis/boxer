package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

func TestGrantTermsLine(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	assert.Equal(t, "", grantTermsLine(nil, now), "no terms reported, no line")
	assert.Equal(t, "the task may make 200 calls, 3 made; it ends in 1 h 30 min; destinations: clickhouse:localhost:8123, keelson:apps\n",
		grantTermsLine(&agent.GrantTerms{CallsBudget: 200, CallsUsed: 3, Deadline: now.Add(90 * time.Minute),
			Destinations: []string{"clickhouse:localhost:8123", "keelson:apps"}}, now))
	assert.Equal(t, "the task may make 50 calls, 0 made; destinations: none\n",
		grantTermsLine(&agent.GrantTerms{CallsBudget: 50}, now), "no deadline, no destinations")
	assert.Equal(t, "under a minute", roughDuration(20*time.Second))
	assert.Equal(t, "under a minute", roughDuration(-time.Minute), "a passed deadline")
	assert.Equal(t, "45 min", roughDuration(45*time.Minute))
	assert.Equal(t, "2 h", roughDuration(2*time.Hour))
}
