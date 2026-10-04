package queryrunfacts

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseCaptureStatus(t *testing.T) {
	st, err := ParseCaptureStatus(nil, errors.New("unknown table"))
	require.NoError(t, err)
	require.Equal(t, CaptureUnknown, st.State)

	st, err = ParseCaptureStatus([]byte(""), nil)
	require.NoError(t, err)
	require.Equal(t, CaptureAbsent, st.State)

	st, err = ParseCaptureStatus([]byte("Scheduled\t1700000000\t1700000300\tPoco::Exception. Code: 1000, Connection refused\n"), nil)
	require.NoError(t, err)
	require.Equal(t, CaptureFailing, st.State)
	require.Contains(t, st.Exception, "Connection refused")
	require.Equal(t, time.Unix(1700000000, 0).UTC(), st.LastSuccess)

	// A success after the failure: the stale exception is not current.
	st, err = ParseCaptureStatus([]byte("Scheduled\t1700000300\t1700000300\told error\n"), nil)
	require.NoError(t, err)
	require.Equal(t, CaptureRunning, st.State)

	st, err = ParseCaptureStatus([]byte("Scheduled\t1700000300\t1700000300\t\n"), nil)
	require.NoError(t, err)
	require.Equal(t, CaptureRunning, st.State)

	_, err = ParseCaptureStatus([]byte("a\tb\n"), nil)
	require.Error(t, err)
}

func TestCaptureStatusSql(t *testing.T) {
	sql := CaptureStatusSql("boxer")
	require.Contains(t, sql, "system.view_refreshes")
	require.Contains(t, sql, "database = 'boxer'")
	require.Contains(t, sql, "view = '"+MvBaseName+"'")
}
