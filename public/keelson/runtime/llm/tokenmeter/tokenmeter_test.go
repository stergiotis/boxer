package tokenmeter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// One history, one number: the estimate counts bytes over four and images
// flat, and a reported usage wins over it.
func TestOneNumberForOneHistory(t *testing.T) {
	ms := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "12345678"}, {Role: openaichat.ChatRoleUser, Content: "abcd", Images: []openaichat.Image{{Data: []byte{1}}}}}
	assert.Equal(t, int64(3+ImageTokens), Estimate(ms))
	assert.Equal(t, Estimate(ms), Used(0, 0, ms), "unreported: the estimate")
	assert.Equal(t, int64(150), Used(100, 50, ms), "reported: the provider's count")
	assert.Equal(t, int64(100), Used(100, 0, ms))
	assert.Equal(t, Used(100, 50, ms), Used(100, 50, nil), "the report does not depend on the messages")
}

func TestThresholdsAreExact(t *testing.T) {
	assert.True(t, Over(80, 100, 80))
	assert.False(t, Over(79, 100, 80))
	assert.False(t, Over(1000, 0, 80), "unknown context is never full")
	assert.Equal(t, float32(0.5), Fraction(50, 100))
	assert.Equal(t, float32(0), Fraction(50, 0))
}
