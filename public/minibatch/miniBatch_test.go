package minibatch

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewMiniBatcher(t *testing.T) {
	validated := &bytes.Buffer{}
	direct := &bytes.Buffer{}
	batched := &bytes.Buffer{}

	var msgVal MessageValidationFunc = func(msg []byte) error {
		_, err := validated.Write(msg)
		require.NoError(t, err)
		return nil
	}
	sizeCriteria := 4096
	countCriteria := 11
	durationCriteria := time.Second
	batcher, err := NewMiniBatcher(sizeCriteria, countCriteria, durationCriteria, msgVal)
	require.NoError(t, err)
	require.NotNil(t, batcher.SetMessageValidationFunc(msgVal))
	require.False(t, batcher.NeedsEmit())
	ra := rand.New(rand.NewSource(time.Now().UnixNano()))

	totallyWrittenBytes := 0
	emittedBytes := 0
	n := 0
	emits := 0
	for i := 0; i < ra.Intn(10000)+200; i++ {
		l := ra.Intn(sizeCriteria / 100)
		b := make([]byte, l, l)
		_, err = io.ReadFull(ra, b)
		require.NoError(t, err)
		n, err = direct.Write(b)
		require.NoError(t, err)
		require.EqualValues(t, l, n)
		totallyWrittenBytes += n
		err = batcher.BeginMessage()
		require.NoError(t, err)
		_, err = batcher.Write(b)
		require.NoError(t, err)
		err = batcher.EndMessage()
		require.NoError(t, err)
		if batcher.NeedsEmit() {
			n, err = batcher.Emit(batched)
			require.NoError(t, err)
			emittedBytes += n
			emits++
		}
	}
	n, err = batcher.ForceEmit(batched)
	require.NoError(t, err)
	emittedBytes += n

	require.EqualValues(t, direct, batched)
	require.EqualValues(t, direct, validated)
	require.EqualValues(t, totallyWrittenBytes, emittedBytes)
	require.EqualValues(t, totallyWrittenBytes, batched.Len())
	s, d, c := batcher.EmitCriteriaStats()
	require.LessOrEqual(t, emits, s+d+c)
}
