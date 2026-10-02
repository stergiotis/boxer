//go:build integration

package play

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Play's own writes reach a live server through the seam (ADR-0270 §SD7):
// a Series verdict lands and reads back.
func TestLiveAppWritesVerdict(t *testing.T) {
	cli := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	ctx := context.Background()

	hash := "play-live-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() {
		_, _ = cli.rawTsvQuery(ctx, "DELETE FROM "+tsLabelsTable+" WHERE input_hash = '"+hash+"'")
	})
	w := newTsLabelsWriter(cli)
	require.NoError(t, w.doWrite(ctx, tsLabelRow{InputHash: hash, SpanFrom: "2026-01-01 00:00:00.000",
		SpanTo: "2026-01-01 00:05:00.000", Verdict: "confirmed", Detector: "live-test"}, appWriteLabel{}))
	raw, err := cli.rawTsvQuery(ctx, "SELECT count() FROM "+tsLabelsTable+" WHERE input_hash = '"+hash+"' FORMAT TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(raw)))
}
