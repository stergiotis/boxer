package runinfo

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBenchTakeFromEnvIsZeroWhenUnset(t *testing.T) {
	envtest.Set(t, RunDriverJob, "")
	envtest.Set(t, RunDriverAttempt, "")
	envtest.Set(t, RunDriverJobKind, "")
	take, err := BenchTakeFromEnv()
	require.NoError(t, err)
	assert.True(t, take.IsZero())
}

func TestBenchTakeFromEnvReadsTheTake(t *testing.T) {
	envtest.Set(t, RunDriverJob, "seco-dq/report/take-4")
	envtest.Set(t, RunDriverAttempt, "2")
	envtest.Set(t, RunDriverJobKind, "suite v3 · ü")
	take, err := BenchTakeFromEnv()
	require.NoError(t, err)
	assert.Equal(t, BenchTake{Job: "seco-dq/report/take-4", Attempt: 2, Kind: "suite v3 · ü"}, take)

	envtest.Set(t, RunDriverAttempt, "")
	envtest.Set(t, RunDriverJobKind, "")
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
			envtest.Set(t, RunDriverJob, tc.job)
			envtest.Set(t, RunDriverAttempt, tc.attempt)
			envtest.Set(t, RunDriverJobKind, tc.kind)
			_, err := BenchTakeFromEnv()
			assert.Error(t, err)
		})
	}
}
