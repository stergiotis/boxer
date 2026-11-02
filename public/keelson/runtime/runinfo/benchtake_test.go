package runinfo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBenchTakeFromEnvIsZeroWhenUnset(t *testing.T) {
	RunDriverJob.SetForTest(t, "")
	RunDriverAttempt.SetForTest(t, "")
	RunDriverJobKind.SetForTest(t, "")
	take, err := BenchTakeFromEnv()
	require.NoError(t, err)
	assert.True(t, take.IsZero())
}

func TestBenchTakeFromEnvReadsTheTake(t *testing.T) {
	RunDriverJob.SetForTest(t, "seco-dq/report/take-4")
	RunDriverAttempt.SetForTest(t, "2")
	RunDriverJobKind.SetForTest(t, "suite v3 · ü")
	take, err := BenchTakeFromEnv()
	require.NoError(t, err)
	assert.Equal(t, BenchTake{Job: "seco-dq/report/take-4", Attempt: 2, Kind: "suite v3 · ü"}, take)

	RunDriverAttempt.SetForTest(t, "")
	RunDriverJobKind.SetForTest(t, "")
	take, err = BenchTakeFromEnv()
	require.NoError(t, err)
	assert.Equal(t, BenchTake{Job: "seco-dq/report/take-4"}, take, "attempt and kind are optional")
}

func TestBenchTakeFromEnvRefusesWhatTheRowCannotCarry(t *testing.T) {
	for _, tc := range []struct {
		name, job, attempt, kind string
	}{
		{name: "attempt without job", attempt: "1"},
		{name: "kind without job", kind: "v1"},
		{name: "attempt not a number", job: "s/t/1", attempt: "two"},
		{name: "attempt negative", job: "s/t/1", attempt: "-1"},
		{name: "attempt beyond uint32", job: "s/t/1", attempt: "4294967296"},
		{name: "job too long", job: strings.Repeat("j", MaxBenchJobBytes+1)},
		{name: "kind too long", job: "s/t/1", kind: strings.Repeat("k", MaxBenchKindBytes+1)},
		{name: "job with a tab", job: "s/t\t1"},
		{name: "job with a newline", job: "s/t/1\n"},
		{name: "kind not UTF-8", job: "s/t/1", kind: "v\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RunDriverJob.SetForTest(t, tc.job)
			RunDriverAttempt.SetForTest(t, tc.attempt)
			RunDriverJobKind.SetForTest(t, tc.kind)
			_, err := BenchTakeFromEnv()
			assert.Error(t, err)
		})
	}
}
