package kafka

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseTopicsRejectsMalformedRanges covers spec shapes the upstream
// table does not: a range expanding past MaxTopicPartitions, a reversed
// range, and a partition spec with no topic name.
func TestParseTopicsRejectsMalformedRanges(t *testing.T) {
	overLimit := "t:0-" + strconv.Itoa(MaxTopicPartitions)

	tests := []struct {
		name        string
		input       []string
		expectedErr string
	}{
		{name: "range one past the limit", input: []string{overLimit}, expectedErr: "partition limit"},
		{name: "ranges summing past the limit", input: []string{"t:0-40000", "u:0-40000"}, expectedErr: "partition limit"},
		{name: "reversed range", input: []string{"t:7-5"}, expectedErr: "start of range is after its end"},
		{name: "empty topic name", input: []string{":3"}, expectedErr: "empty topic name"},
		{name: "blank topic name", input: []string{" \t:0-2"}, expectedErr: "empty topic name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ParseTopics(test.input, -1, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.expectedErr)
		})
	}

	// The full int32 range expands to 2^31 partitions (~8 GiB of int32s
	// before the map). It runs only once the just-over-limit case above is
	// rejected, so an implementation without the limit fails there instead
	// of exhausting memory here.
	if _, _, err := ParseTopics([]string{overLimit}, -1, false); err == nil {
		t.Fatal("limit not enforced; not running the full-range case")
	}
	_, _, err := ParseTopics([]string{"t:0-2147483647"}, -1, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partition limit")
}

// TestParseTopicsAtLimit pins that the limit is inclusive: a spec naming
// exactly MaxTopicPartitions partitions parses.
func TestParseTopicsAtLimit(t *testing.T) {
	spec := "t:0-" + strconv.Itoa(MaxTopicPartitions-1)
	_, tps, err := ParseTopics([]string{spec}, -1, false)
	require.NoError(t, err)
	assert.Len(t, tps["t"], MaxTopicPartitions)
}
