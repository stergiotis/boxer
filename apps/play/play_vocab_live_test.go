//go:build integration

package play

// Live checks for the Vocabulary tab's probe against a real server. What these
// pin is the half a unit test cannot: that `system.functions` answers the
// listing the panel ships, and that its `origin` column arrives as the enum's
// NAME — which is what the whole user-defined/built-in split is made of
// (ADR-0174 §SD2).
//
// The split is worth a live test because getting it wrong is silent. `origin`
// is an Enum8 and ClickHouse ships an Enum8 over Arrow as the raw int8
// ordinal, so a bare `origin` decodes as "0"/"1", every function passes the
// `!= 'System'` test, and the panel reports a healthy server's whole built-in
// set as user-defined extras. Nothing errors; the tab just draws a bigger list —
// and sorts it on every frame, which is how the defect actually surfaced
// ("imzero2: slow frame", continuously, for as long as the tab was open).

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// The probe's own query must run, and its origin column must decode to the
// enum's spelling rather than its ordinal.
func TestLiveVocabProbeOriginDecodesToEnumName(t *testing.T) {
	client := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	rec, _, _, err := clientExecutor{client: client, opts: newExecOptions("vocabulary")}.
		execute(context.Background(), compiledNode{SQL: vocabProbeQuery}, memory.NewGoAllocator())
	require.NoError(t, err, "the vocabulary listing must run on this endpoint")
	require.NotNil(t, rec)
	defer rec.Release()
	require.Equal(t, int64(3), rec.NumCols(), "name, create_query, origin")
	require.Greater(t, rec.NumRows(), int64(0), "a server carries functions")

	origins := rec.Column(2)
	system := 0
	for row := range int(rec.NumRows()) {
		if origins.ValueStr(row) == "System" {
			system++
		}
	}
	require.Positive(t, system,
		"every ClickHouse carries built-ins, and they must read back as 'System' — an ordinal here means the enum never crossed the wire")
	require.Less(t, system, int(rec.NumRows()),
		"the fixtures install user-defined functions, so not every row is a built-in")
}

// The half the panel actually reads: demand() answers with the user-defined
// subset, which on any real server is a small fraction of the listing. It is
// the number renderVocabStatus prints and the population the extras families
// are drawn from.
func TestLiveVocabProbeUserDefinedIsASubset(t *testing.T) {
	probe := newVocabProbe(NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil))
	defer probe.close()

	// The lane answers off the render thread, so the first demands come back
	// not-ready; the panel simply asks again next frame.
	var userDefined map[string]string
	deadline := time.Now().Add(vocabProbeTimeout)
	for {
		var ready bool
		if userDefined, ready = probe.demand(); ready {
			break
		}
		require.False(t, time.Now().After(deadline), "the probe must answer within its own timeout")
		time.Sleep(50 * time.Millisecond)
	}

	all, ready := probe.demandAll()
	require.True(t, ready, "the same landed answer serves both halves")
	require.NotEmpty(t, userDefined, "the fixtures install user-defined functions")
	require.Less(t, len(userDefined), all.Len(),
		"the built-ins belong to the other half; counting them here is what buried the tab's extras families")
}
