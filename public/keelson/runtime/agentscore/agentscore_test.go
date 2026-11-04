package agentscore_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentscore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
)

var (
	viewRef   = regexp.MustCompile(`\bboxer\.([a-z_]+)`)
	columnRef = regexp.MustCompile("`([^`]+)`")
)

// viewColumns maps each composed trail and journal view to its physical
// column names.
func viewColumns(t *testing.T) (byView map[string][]string) {
	t.Helper()
	byView = map[string][]string{}
	tv, err := trailviews.Compose("", "")
	require.NoError(t, err)
	for _, v := range tv {
		for _, c := range v.Columns {
			byView[v.Name] = append(byView[v.Name], c.Physical)
		}
	}
	jv, err := journalviews.Compose("")
	require.NoError(t, err)
	for _, v := range jv {
		for _, c := range v.Columns {
			byView[v.Name] = append(byView[v.Name], c.Physical)
		}
	}
	return
}

// Every view the scorecard reads is composed, and every column it names is a
// physical column of one of those views: a renamed or dropped column fails
// here.
func TestTheScorecardReadsOnlyComposedColumns(t *testing.T) {
	sql := agentscore.Scorecard()
	byView := viewColumns(t)

	var read []string
	for _, m := range viewRef.FindAllStringSubmatch(sql, -1) {
		_, ok := byView[m[1]]
		require.True(t, ok, "the scorecard reads boxer.%s, which neither trailviews nor journalviews composes", m[1])
		if !slices.Contains(read, m[1]) {
			read = append(read, m[1])
		}
	}
	require.NotEmpty(t, read)

	var known []string
	for _, v := range read {
		known = append(known, byView[v]...)
	}
	names := 0
	for _, m := range columnRef.FindAllStringSubmatch(sql, -1) {
		names++
		require.True(t, slices.Contains(known, m[1]), "column %s is not a column of the views the scorecard reads (%s)", m[1], strings.Join(read, ", "))
	}
	require.Greater(t, names, 20)
}

// The query takes the conversation as a typed parameter and leaves the
// output format to the caller.
func TestTheScorecardIsParameterisedAndUnformatted(t *testing.T) {
	sql := agentscore.Scorecard()
	require.Contains(t, sql, "{"+agentscore.ParamConversation+":String}")
	require.NotRegexp(t, `(?im)^\s*FORMAT\s`, sql)
}
