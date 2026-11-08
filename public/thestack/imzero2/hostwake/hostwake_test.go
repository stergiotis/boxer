package hostwake

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWakeCallsTheInstalledWaker(t *testing.T) {
	defer Set(nil)
	n := 0
	Wake()
	Set(func() { n++ })
	Wake()
	Wake()
	Set(nil)
	Wake()
	assert.Equal(t, 2, n)
}
