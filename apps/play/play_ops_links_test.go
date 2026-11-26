package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

func TestLinkCodecs_RoundTripTheirExamples(t *testing.T) {
	require.NoError(t, applink.CheckCodec(setSqlLinkCodec))
	require.NoError(t, applink.CheckCodec(queryLinkCodec))
}

func TestQueryLinkCodec_ReadsWhatALinkSpells(t *testing.T) {
	args, err := queryLinkCodec.ParseLink("?sql=SELECT+1&limit=10")
	require.NoError(t, err)
	q, err := buscodec.Decode[QueryArgs](args)
	require.NoError(t, err)
	assert.Equal(t, QueryArgs{Sql: "SELECT 1", Limit: 10}, q)
	for name, tail := range map[string]string{
		"no sql":           "?limit=10",
		"unknown key":      "?sql=x&keep_as=y",
		"limit zero":       "?sql=x&limit=0",
		"limit not a word": "?sql=x&limit=ten",
		"leading zero":     "?sql=x&statement=01",
		"a path":           "/x?sql=y",
		"a fragment":       "?sql=x#y",
	} {
		_, err = queryLinkCodec.ParseLink(tail)
		assert.Error(t, err, name)
	}
	b, err := buscodec.Encode(QueryArgs{Sql: "x", KeepAs: "k"})
	require.NoError(t, err)
	_, err = queryLinkCodec.FormatLink(b)
	assert.Error(t, err, "keep_as has no spelling")
}

func TestQueryIsStagedForALinkNobodysGrantRuns(t *testing.T) {
	spec, ok := playOps.Catalog().Lookup(opQuery)
	require.True(t, ok)
	assert.Equal(t, opStageQuery, spec.LinkStage)
	stage, ok := playOps.Catalog().Lookup(opStageQuery)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectDocument, stage.Effect)
	assert.False(t, stage.Agents, "the stage is the host's, for authorless links; an agent calls set_sql")
}

func FuzzQueryLinkCodec(f *testing.F) {
	for _, ex := range queryLinkCodec.LinkExamples() {
		tail, err := queryLinkCodec.FormatLink(ex)
		require.NoError(f, err)
		f.Add(tail)
	}
	f.Add("?sql=a&sql=b")
	f.Fuzz(func(t *testing.T, tail string) {
		if err := applink.CheckParsed(queryLinkCodec, tail); err != nil {
			t.Fatal(err)
		}
		if err := applink.CheckParsed(setSqlLinkCodec, tail); err != nil {
			t.Fatal(err)
		}
	})
}
