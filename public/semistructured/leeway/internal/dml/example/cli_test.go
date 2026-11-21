package example

import (
	"bytes"
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

type locator struct {
	path   string
	params string
}

// shredLocators walks doc the way populateJsonEntity does and returns the
// attribute locator of every value token, in document order.
func shredLocators(t *testing.T, doc string) (out []locator) {
	t.Helper()
	dec := jsontext.NewDecoder(strings.NewReader(doc))
	lc, hc := &bytes.Buffer{}, &bytes.Buffer{}
	seen := make(map[string]struct{}, 16)
	for {
		token, err := dec.ReadToken()
		if err != nil {
			return
		}
		ptr := dec.StackPointer()
		if k, _ := dec.StackIndex(dec.StackDepth()); k == '{' {
			if _, ok := seen[string(ptr)]; !ok {
				seen[string(ptr)] = struct{}{}
				continue // the member's key, not its value
			}
		}
		switch token.Kind() {
		case '{', '[', '}', ']':
			continue
		}
		lowCard, highCard, err := splitPointer(dec, ptr, lc, hc)
		require.NoError(t, err)
		out = append(out, locator{path: string(lowCard), params: string(highCard)})
	}
}

func TestSplitPointerCarriesArrayIndices(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want []locator
	}{
		{
			name: "scalar members carry no parameters",
			doc:  `{"did":"did:plc:abc","time_us":17}`,
			want: []locator{{"/did", ""}, {"/time_us", ""}},
		},
		{
			// The defect this test exists for: every index used to encode as
			// base62(0) because splitPointer discarded the value it parsed, so
			// /langs/0 and /langs/1 were indistinguishable once shredded.
			name: "array elements are distinguishable",
			doc:  `{"commit":{"record":{"langs":["en","de","fr"]}}}`,
			want: []locator{
				{"/commit/record/langs/_", "0000"},
				{"/commit/record/langs/_", "0001"},
				{"/commit/record/langs/_", "0002"},
			},
		},
		{
			name: "nested arrays carry one parameter per level, outermost first",
			doc:  `{"a":[{"b":[10,20]},{"b":[30]}]}`,
			want: []locator{
				{"/a/_/b/_", "0000.0000"},
				{"/a/_/b/_", "0000.0001"},
				{"/a/_/b/_", "0001.0000"},
			},
		},
		{
			// An all-digit object key is a key, not an index. Deciding by
			// spelling put it in the parameter channel and erased it from the
			// path; the decoder's stack says which container it addresses.
			name: "all-digit object keys stay in the path",
			doc:  `{"0":{"12":true}}`,
			want: []locator{{"/0/12", ""}},
		},
		{
			name: "an array of objects keyed by digits",
			doc:  `{"a":[{"7":"x"}]}`,
			want: []locator{{"/a/_/7", "0000"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shredLocators(t, tc.doc))
		})
	}
}

// Array position must survive the round trip, or the shred cannot reconstruct
// the document it read.
func TestSplitPointerParametersDecode(t *testing.T) {
	got := shredLocators(t, `{"a":[{"b":[10,20]}]}`)
	require.Len(t, got, 2)

	idx0, err := membership.DecodeParams([]byte(got[0].params))
	require.NoError(t, err)
	idx1, err := membership.DecodeParams([]byte(got[1].params))
	require.NoError(t, err)
	require.Equal(t, []uint64{0, 0}, idx0)
	require.Equal(t, []uint64{0, 1}, idx1)
}
