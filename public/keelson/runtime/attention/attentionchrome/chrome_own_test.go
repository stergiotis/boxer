package attentionchrome

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// A window's own request opens at once while the person works there, once;
// another window's waits in the queue, and so does one of a kind not its own.
func TestARequestOpensFromTheWindowThePersonIsIn(t *testing.T) {
	entry := func(key string, kind string, window uint64) attention.Entry {
		return attention.Entry{Item: attention.Item{Key: key, Kind: kind, Instance: window}}
	}
	active, last := uint64(1), uint64(2)
	inst := &Chrome{svc: &attention.Service{}, activeW: func() uint64 { return active }, lastInW: func() uint64 { return last }}
	inst.entries = []attention.Entry{
		entry("active", attention.KindGrantRequest, 1),
		entry("last-input", attention.KindWidening, 2),
		entry("elsewhere", attention.KindConfirmation, 3),
		entry("question", attention.KindQuestion, 1),
		entry("no-window", attention.KindGrantRequest, 0),
	}
	inst.openOwnRequests()
	assert.Equal(t, []string{"active", "last-input"}, inst.open)

	inst.open = nil
	inst.openOwnRequests()
	assert.Empty(t, inst.open, "each once: a card the person closed stays closed")
}
