package ea

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestTransferDataWithSplitReads(t *testing.T) {
	ra := rand.New(rand.NewSource(time.Now().UnixNano()))
	l := rand.Intn(1*1024*1024) + 100
	v := make([]byte, l, l)
	_, err := ra.Read(v)
	require.NoError(t, err)
	b := bytes.NewBuffer(make([]byte, 0, l))
	var n int
	var singleBytesReads int
	var blockReads int
	var bytesBlockReads int
	singleBytesReads, blockReads, bytesBlockReads, n, err = TransferDataWithSplitReadAndWrites(b, l, NewBytesBlockByteReadReader(bytes.NewBuffer(v)), 111, ra)
	require.NoError(t, err)
	require.EqualValues(t, l, n)
	require.EqualValues(t, l, b.Len())
	require.Equal(t, v, b.Bytes())
	log.Info().Int("singleBytesRead", singleBytesReads).Int("bytesBlockRead", bytesBlockReads).Int("blockReads", blockReads).Int("n", n).Msg("split read summary")
}
