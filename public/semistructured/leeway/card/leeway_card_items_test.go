package card

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stretchr/testify/require"
)

// driveItems plays a hand-written entity stream into the extractor: each
// entity is a list of (section, column, values, low-card refs).
type itemEntity struct {
	sections []itemSection
}

type itemSection struct {
	name   string
	column string
	values []string
	refs   []uint64
	// numeric marks the column's canonical type as machine-numeric.
	numeric bool
}

func driveItems(t *testing.T, ents []itemEntity) *ItemExtractor {
	t.Helper()
	ie := NewItemExtractor()
	ie.BeginBatch()
	for _, e := range ents {
		ie.BeginEntity()
		ie.BeginTaggedSections()
		for _, s := range e.sections {
			ie.BeginSection(naming.StylableName(s.name), nil, nil, "", 1)
			ie.BeginTaggedValue()
			ie.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: 0, FullColumnName: "tv:" + s.name + ":" + s.column + ":val"}, naming.StylableName(s.column), canonical(s.numeric), "")
			ie.BeginHomogenousArrayValue(len(s.values))
			for i, v := range s.values {
				ie.BeginValueItem(i)
				_, _ = ie.WriteString(v)
				ie.EndValueItem()
			}
			ie.EndHomogenousArrayValue()
			ie.EndColumn()
			ie.BeginTags(len(s.refs))
			for _, ref := range s.refs {
				ie.AddMembershipRef(true, ref)
				ie.AddMembershipRef(false, ref+1000) // high-card: never an item
			}
			ie.EndTags()
			require.NoError(t, ie.EndTaggedValue())
			require.NoError(t, ie.EndSection())
		}
		require.NoError(t, ie.EndTaggedSections())
		require.NoError(t, ie.EndEntity())
	}
	require.NoError(t, ie.EndBatch())
	return ie
}

func canonical(numeric bool) canonicaltypes.PrimitiveAstNodeI {
	if numeric {
		return numericNode{}
	}
	return nil
}

// numericNode is the one method the extractor reads off a canonical type.
type numericNode struct {
	canonicaltypes.PrimitiveAstNodeI
}

func (numericNode) IsMachineNumericNode() bool { return true }

func TestItemExtractorVocabularyAndSupport(t *testing.T) {
	ie := driveItems(t, []itemEntity{
		{sections: []itemSection{{name: "geo", column: "lat", values: []string{"1", "2"}, refs: []uint64{7}, numeric: true}}},
		{sections: []itemSection{{name: "geo", column: "lat", values: []string{"1"}, refs: []uint64{7, 8}, numeric: true}, {name: "sym", column: "value", values: []string{"a"}}}},
		{sections: []itemSection{{name: "sym", column: "value", values: []string{"a", "a"}}}},
	})
	res := ie.Results()
	require.Len(t, res.Rows, 3)
	byName := map[string]int32{}
	for i, it := range res.Items {
		byName[it.Name] = int32(i)
	}
	require.Equal(t, int32(2), res.Support[byName["section:geo"]])
	require.Equal(t, int32(2), res.Support[byName["value:geo.lat=1"]])
	require.Equal(t, int32(1), res.Support[byName["value:geo.lat=2"]])
	require.Equal(t, int32(2), res.Support[byName["tag:geo#7"]])
	require.Equal(t, int32(1), res.Support[byName["tag:geo#8"]])
	require.Equal(t, int32(2), res.Support[byName["section:sym"]])
	require.Equal(t, int32(2), res.Support[byName["value:sym.value=a"]], "a repeated value counts once per entity")
	_, high := byName["tag:geo#1007"]
	require.False(t, high, "high-cardinality refs are not items")
	geo := res.Items[byName["section:geo"]]
	require.Equal(t, ItemKindSection, geo.Kind)
	require.Equal(t, "tv:geo:lat:val", geo.Column)
	require.Equal(t, "geo", geo.PhysicalSection)
	require.Equal(t, "geo", res.Items[byName["tag:geo#7"]].PhysicalSection)
	lat := res.Items[byName["value:geo.lat=1"]]
	require.Equal(t, ItemKindTaggedValue, lat.Kind)
	require.False(t, lat.Quoted)
	require.True(t, res.Items[byName["value:sym.value=a"]].Quoted)
	// Rows are ascending ids; entity 3 holds only the sym items.
	require.Equal(t, []int32{byName["section:sym"], byName["value:sym.value=a"]}, sortedCopy(res.Rows[2]))

	// Prune: support in [2, 2] keeps the four items two entities share,
	// ordered by name at equal support, and renumbers the rows.
	pruned := res.Prune(2, 2, 0)
	require.Len(t, pruned.Items, 5)
	names := make([]string, len(pruned.Items))
	for i, it := range pruned.Items {
		names[i] = it.Name
	}
	require.Equal(t, []string{"section:geo", "section:sym", "tag:geo#7", "value:geo.lat=1", "value:sym.value=a"}, names)
	require.Equal(t, []int32{1, 4}, pruned.Rows[2])
	capped := res.Prune(1, 3, 2)
	require.Len(t, capped.Items, 2)
	// Select picks entities and recounts.
	sel := res.Select([]int{2, 2, 0})
	require.Len(t, sel.Rows, 3)
	require.Equal(t, int32(1), sel.Support[byName["section:geo"]])
	require.Equal(t, int32(2), sel.Support[byName["section:sym"]])
}

func TestItemExtractorCaps(t *testing.T) {
	ie := NewItemExtractor()
	ie.MaxItems = 1
	ie.BeginEntity()
	ie.BeginSection(naming.StylableName("a"), nil, nil, "", 1)
	require.NoError(t, ie.EndSection())
	ie.BeginSection(naming.StylableName("b"), nil, nil, "", 1)
	require.NoError(t, ie.EndSection())
	require.NoError(t, ie.EndEntity())
	res := ie.Results()
	require.Len(t, res.Items, 1)
	require.Equal(t, []int32{0}, res.Rows[0])
	// A value past the length cap is content, a blob is not text, and a
	// plain-section value is identity: none is an item.
	ie2 := NewItemExtractor()
	ie2.BeginEntity()
	ie2.BeginPlainSection(0, nil, nil, 1)
	ie2.BeginColumn(streamreadaccess.PhysicalColumnAddr{FullColumnName: "id"}, naming.StylableName("id"), nil, "")
	_, _ = ie2.WriteString("key-1")
	ie2.EndColumn()
	require.NoError(t, ie2.EndPlainSection())
	ie2.BeginSection(naming.StylableName("a"), nil, nil, "", 1)
	ie2.BeginColumn(streamreadaccess.PhysicalColumnAddr{FullColumnName: "c"}, naming.StylableName("c"), nil, "")
	_, _ = ie2.WriteString(string(make([]byte, ItemMaxValueLen+1)))
	_, _ = ie2.WriteString("\x00\x01blob")
	_, _ = ie2.WriteString("\xff\xfe")
	_, _ = ie2.WriteString("short")
	ie2.EndColumn()
	require.NoError(t, ie2.EndSection())
	require.NoError(t, ie2.EndEntity())
	res2 := ie2.Results()
	require.Len(t, res2.Items, 2)
	require.Equal(t, "value:a.c=short", res2.Items[1].Name)
}

func sortedCopy(s []int32) []int32 {
	out := append([]int32(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
