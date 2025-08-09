package base62

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 1000; i++ {
		n := rnd.Uint64()
		n2, valid := Encode(n).Decode()
		require.True(t, valid)
		require.Equal(t, n, n2)
	}
}
func TestInvalid(t *testing.T) {
	require.False(t, IsValid(Base62Num("")))
	require.False(t, IsValid(Base62Num("-")))
	require.False(t, IsValid(Base62Num("-0")))
	require.False(t, IsValid(Base62Num("_")))
	require.False(t, IsValid(Base62Num("0-")))
	require.False(t, IsValid(Base62Num("0x2")))
}
func TestValid(t *testing.T) {
	require.True(t, IsValid(Base62Num("0")))
	require.True(t, IsValid(Base62Num("z")))
	require.True(t, IsValid(Base62Num("Z")))
	n, valid := Decode(Base62Num("0"))
	require.True(t, valid)
	require.Equal(t, uint64(0), n)
	n, valid = Decode(Base62Num("9"))
	require.True(t, valid)
	require.Equal(t, uint64(9), n)
	n, valid = Decode(Base62Num("f"))
	require.True(t, valid)
	require.Equal(t, uint64(0xf), n)
	n, valid = Decode(Base62Num("Z"))
	require.True(t, valid)
	require.Equal(t, uint64(0x3d), n)
}
