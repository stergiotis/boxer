package agent

import (
	"bufio"
	"bytes"
	"context"
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

func (inst *lockedBuffer) actionLines() (out []actionLine) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	sc := bufio.NewScanner(bytes.NewReader(inst.buf.Bytes()))
	for sc.Scan() {
		var r actionLine
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return
}

// Under a test grant the actions file keeps what the model sent, and a
// refused argument shape comes back with the schema it has to fit.
func TestATestGrantsActionsKeepTheArgumentsAndASchemaRefusalNamesItsSchema(t *testing.T) {
	log := &lockedBuffer{}
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.ActionsLog = true, log })
	g := r.grant(ModeAct)
	r.call(g, "k0", "get_text", "{}")
	out := r.call(g, "k1", "set_text", "{}")
	require.Equal(t, "refused", out.Phase)
	require.NotNil(t, out.Remedy)
	assert.Contains(t, out.Remedy.ArgsSchema, `"properties"`)

	var sent string
	for _, l := range log.actionLines() {
		if l.Key == "k1" {
			sent = l.Args
		}
	}
	assert.Equal(t, "{}", sent)
}

// A refused test grant is a row of the actions file, with the request as sent.
func TestARefusedTestGrantLandsInTheActionsFile(t *testing.T) {
	log := &lockedBuffer{}
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.ActionsLog = true, log })
	_, err := r.cli.Request(context.Background(), GrantRequest{Plan: "look around"})
	require.Error(t, err)
	rows := log.actionLines()
	require.Len(t, rows, 1)
	assert.Equal(t, "grant", rows[0].Decision)
	assert.Equal(t, "refused", rows[0].Phase)
	assert.Contains(t, rows[0].Args, "look around")

	_, err = r.cli.Request(context.Background(), GrantRequest{Plan: "edit window 42", Entries: []GrantEntry{{Instance: 42, Mode: ModeAct}}})
	require.Error(t, err)
	rows = log.actionLines()
	require.Len(t, rows, 2, "every refusal is a row, not only an empty request")
	assert.Contains(t, rows[1].Reason, "no open window")
}
