package identifier

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundtrip(t *testing.T) {
	rnd := rand.NewPCG(rand.Uint64(), rand.Uint64())
	n := 100000
	if testing.Short() {
		n = 1000
	}
	for i := 0; i < n; i++ {
		tv := TagValue(rnd.Uint64() % uint64(MaxTagValue))
		require.True(t, tv.IsValid())
		tg := tv.GetTag()
		require.EqualValues(t, tv, tg.GetValue())
		require.EqualValues(t, uint64(tg), tg.Value())
		require.EqualValues(t, tv.Value(), tg.GetValue().Value())
		u := UntaggedId(rnd.Uint64() % uint64(tg.GetMaxPossibleIdIncl()+1))
		id := tg.ComposeId(u)
		require.EqualValues(t, u.AddTag(tg), id)
		require.EqualValues(t, tg, id.GetTag())
		require.EqualValues(t, tg.GetTagWidth(), id.GetTagWidth())
		require.EqualValues(t, u, id.RemoveTag())
		tg2, u2 := id.Split()
		require.EqualValues(t, tg, tg2)
		require.EqualValues(t, u, u2)
	}
}
