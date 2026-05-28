package fibonacci

import (
	"math"
	"math/bits"
	"testing"

	"math/rand/v2"

	identifier2 "github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

func TestIdent(t *testing.T) {
	for i := 0; i < 100000; i++ {
		tv := identifier2.TagValue(rand.Uint64N(uint64(identifier2.MaxTagValue)))
		tag := tv.GetTag()
		maxPossibleId := tag.GetMaxPossibleIdIncl()
		nBitsId := bits.OnesCount64(uint64(maxPossibleId))
		uid := identifier2.UntaggedId(rand.Uint64N(uint64(maxPossibleId)))
		taggedId := uid.AddTag(tag)
		tagMask := taggedId.GetTagMask()
		nBitsTag := tag.GetTagWidth()
		/*fmt.Fprintf(os.Stderr, "nBitsId=%d nBitsTag=%d\n", nBitsId, nBitsTag)
		fmt.Fprintf(os.Stderr, "tag      = 0b%064b\n", tag)
		fmt.Fprintf(os.Stderr, "taggedId = 0b%064b\n", taggedId)
		fmt.Fprintf(os.Stderr, "maxI     = 0b%064b\n", maxPossibleId)
		fmt.Fprintf(os.Stderr, "tma      = 0b%064b (%d popcount)\n", tagMask, bits.OnesCount64(uint64(tagMask)))
		fmt.Fprintf(os.Stderr, "uid      = 0b%064b\n", uid)
		fmt.Fprintf(os.Stderr, "vma      = 0b%064b\n", ^tagMask)
		fmt.Fprintf(os.Stderr, "xxx      = 0b%064b\n", taggedId.RemoveTag())*/
		require.Equal(t, tv, tag.GetValue())
		require.Equal(t, 64, nBitsId+nBitsTag)
		require.Equal(t, nBitsTag, bits.LeadingZeros64(uint64(maxPossibleId)))
		require.Equal(t, uid, taggedId.RemoveTag())
		require.Equal(t, tagMask, identifier2.TaggedId(^maxPossibleId))
		tagSpl, uidSpl := taggedId.Split()
		require.Equal(t, uid, uidSpl)
		require.Equal(t, tag, tagSpl)

		require.Equal(t, tag, taggedId.GetTag())
		require.Equal(t, tv, tag.GetValue())
	}
}

func TestTagValue(t *testing.T) {
	t.Skip("needs work")
	require.Equal(t, Uint32TagValueTagWidth, identifier2.TagValue(math.MaxUint32).GetTag().GetTagWidth())
	require.True(t, (identifier2.TagValue(math.MaxUint32).GetTag()>>(64-Uint32TagValueTagWidth))&0b11 == 0b11)
}

func TestCompressionFriendlyBitLayout(t *testing.T) {
	tag := identifier2.TagValue(rand.Uint32()).GetTag()
	id1 := tag.ComposeId(1)
	id2 := tag.ComposeId(2)
	require.Equal(t, uint64(1), uint64(id2-id1))
}
