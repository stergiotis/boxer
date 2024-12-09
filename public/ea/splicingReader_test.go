package ea

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSplicingReader(t *testing.T) {
	{
		buf0 := strings.NewReader("world!")
		b, err := io.ReadAll(NewSplicingReader(buf0))
		require.NoError(t, err)
		require.Equal(t, "world!", string(b))
	}

	{
		buf0 := strings.NewReader("world!")
		buf1 := strings.NewReader("hello ")
		sr := NewSplicingReader(buf0)
		err := sr.SpliceReader(buf1)
		require.NoError(t, err)
		err = sr.SpliceReader(buf1)
		require.ErrorIs(t, ErrSplicingReaderAlreadySet, err)

		var b []byte
		b, err = io.ReadAll(sr)
		require.NoError(t, err)
		require.Equal(t, "hello world!", string(b))
	}
}
