package agent

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedBuffer is a bytes.Buffer the dispatcher and the test share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (inst *lockedBuffer) Write(p []byte) (int, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.Write(p)
}

func (inst *lockedBuffer) lines() (out []ActionRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	sc := bufio.NewScanner(bytes.NewReader(inst.buf.Bytes()))
	for sc.Scan() {
		var r ActionRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return
}

// ADR-0269 M6: every action record also lands in the actions log as one
// JSON line, for a trial's scorer.
func TestTheActionsLogGetsEveryRecord(t *testing.T) {
	log := &lockedBuffer{}
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.ActionsLog = true, log })
	g := r.grant(ModeAct)
	q := r.call(g, "k1", "get_text", "{}")
	require.Equal(t, "completed", q.Phase, q.Reason)

	rows := log.lines()
	require.NotEmpty(t, rows)
	assert.Equal(t, len(r.svc.Actions()), len(rows), "the log holds what the in-process record holds")
	last := rows[len(rows)-1]
	assert.Equal(t, "get_text", last.Operation)
	assert.Equal(t, "k1", last.Key)
	assert.Equal(t, g.Task, last.Task)
	assert.True(t, last.Test)
}
