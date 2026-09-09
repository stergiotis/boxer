package watchbillstore

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The value columns are read out of the generated DDL; every section the
// statements name must resolve, and the names must be the DDL's.
func TestValueColumnsResolveFromTheGeneratedDDL(t *testing.T) {
	for _, s := range []string{"jobKind", "jobState", "jobAttempt", "jobRunAfter", "jobWorkerRun", "jobFinishedAt", "jobLastError", "jobPriority"} {
		c := col(s)
		assert.True(t, strings.HasPrefix(c, `"tv:`+s+`:value:`), c)
		assert.Contains(t, watchbillDDLCreate, c)
	}
	assert.Panics(t, func() { col("jobNoSuchSection") })
}

var (
	testLayout = Layout{Database: "wb"}
	testNow    = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
)

// The statements are the decision (ADR-0223 §SD3); a silent change here is
// a change of the claim, so they are pinned verbatim.
func TestClaimSQL(t *testing.T) {
	sql := ClaimSQL(testLayout, "j1", "run-a", testNow)
	assert.Equal(t, `UPDATE wb.watchbill SET "tv:jobState:value:val:s:24:::0::data" = ['running'], "tv:jobWorkerRun:value:val:s:24:::0::data" = ['run-a'], "tv:jobAttempt:value:val:u32:4:::0::data" = ["tv:jobAttempt:value:val:u32:4:::0::data"[1] + 1] WHERE "id:id:s:4::0:" = 'j1' AND "tv:jobState:value:val:s:24:::0::data"[1] = 'queued' AND "tv:jobRunAfter:value:val:z64:4:::0::data"[1] <= fromUnixTimestamp64Nano(1788955200000000000)`, sql)
}

func TestQueueSQL(t *testing.T) {
	sql := QueueSQL(testLayout, []string{"a.b", "c"}, testNow, 5)
	assert.Equal(t, `SELECT "id:id:s:4::0:" FROM wb.watchbill WHERE "tv:jobState:value:val:s:24:::0::data"[1] = 'queued' AND "tv:jobRunAfter:value:val:z64:4:::0::data"[1] <= fromUnixTimestamp64Nano(1788955200000000000) AND "tv:jobKind:value:val:s:24:::0::data"[1] IN ('a.b', 'c') ORDER BY "tv:jobPriority:value:val:u32:4:::0::data"[1] ASC, "tv:jobRunAfter:value:val:z64:4:::0::data"[1] ASC, "ts:ts:z64:47::0:" ASC LIMIT 5`, sql)
	assert.NotContains(t, QueueSQL(testLayout, nil, testNow, 0), "IN (")
	assert.NotContains(t, QueueSQL(testLayout, nil, testNow, 0), "LIMIT")
}

func TestTransitionSQL(t *testing.T) {
	after := testNow.Add(time.Minute)
	msg := "it's broken"
	sql := TransitionSQL(testLayout, Transition{
		ID: "j1", From: []string{StateRunning}, WorkerRun: "run-a", To: StateFailed,
		SetWorkerRun: true, NewWorkerRun: "", RunAfter: &after, LastError: &msg,
	})
	assert.Equal(t, `UPDATE wb.watchbill SET "tv:jobState:value:val:s:24:::0::data" = ['failed'], "tv:jobWorkerRun:value:val:s:24:::0::data" = [''], "tv:jobRunAfter:value:val:z64:4:::0::data" = [fromUnixTimestamp64Nano(1788955260000000000)], "tv:jobLastError:value:val:s:4:::0::data" = ['it\'s broken'] WHERE "id:id:s:4::0:" = 'j1' AND "tv:jobState:value:val:s:24:::0::data"[1] IN ('running') AND "tv:jobWorkerRun:value:val:s:24:::0::data"[1] = 'run-a'`, sql)
	plain := TransitionSQL(testLayout, Transition{ID: "j2", To: StateCancel})
	assert.Equal(t, `UPDATE wb.watchbill SET "tv:jobState:value:val:s:24:::0::data" = ['cancel'] WHERE "id:id:s:4::0:" = 'j2'`, plain)
}

func TestSweepAndExpireSQL(t *testing.T) {
	assert.Equal(t, `"tv:jobState:value:val:s:24:::0::data"[1] = 'running' AND "tv:jobWorkerRun:value:val:s:24:::0::data"[1] IN ('r1', 'r2')`, RunningOfAnyPredicate([]string{"r1", "r2"}))
	assert.Equal(t, `DELETE FROM wb.watchbill WHERE "tv:jobState:value:val:s:24:::0::data"[1] IN ('succeeded', 'discarded', 'cancelled', 'abandoned') AND "tv:jobFinishedAt:value:val:z64:4:::0::data"[1] < fromUnixTimestamp64Nano(1788955200000000000)`, ExpireSQL(testLayout, testNow))
	assert.Equal(t, "ALTER TABLE wb.watchbill MODIFY SETTING enable_block_number_column=1, enable_block_offset_column=1", AlterJobTableSettingsSQL(testLayout))
}

func TestLayoutDefaults(t *testing.T) {
	var l Layout
	require.Equal(t, "boxer.watchbill", l.JobTable())
	require.Equal(t, "boxer.watchbillevent", l.EventTable())
	require.Equal(t, JobTableName, l.JobTable())
	require.Equal(t, EventTableName, l.EventTable())
}
