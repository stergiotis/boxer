package sysmreplay

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
)

// The reader's own SQL reads the relation its store reads, so with a read
// table set the aggregates and the scans see the same rows (ADR-0402 §SD3).
func TestReaderSQL_FollowsTheStoresReadTable(t *testing.T) {
	st := sysmfacts.NewSysmetricsStore(nil, nil, sysmfacts.SysmetricsStoreConfig{ReadTable: "carrier.facts_view"})
	defer st.Close()
	inst := &Reader{store: st, host: "h"}
	now := time.Now().UTC()
	w := Window{From: now.Add(-time.Hour), To: now}
	for name, sql := range map[string]string{
		"coverage": inst.coverageSQL(w, time.Minute),
		"as-of":    asOfPredicate(inst.table(), 1, now),
	} {
		assert.Contains(t, sql, "carrier.facts_view", name)
		assert.NotContains(t, sql, sysmfacts.SysmetricsTableName, name)
	}
}
