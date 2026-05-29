//go:build llm_generated_opus47

package capinspector

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

func TestClassify_Subjects(t *testing.T) {
	cases := map[string]CapId{
		"fs.dialog.read":                   CapFs,
		"fs.handle.abc.read":               CapFs,
		"runtime.persist.play.editor.set":  CapPersist,
		"runtime.facts.read":               CapFacts,
		"runtime.heartbeat.tick":           CapRun,
		"runtime.run.start":                CapRun,
		"ch.local.exec.regex_explorer":     "",
		"":                                 "",
	}
	for subj, want := range cases {
		got := classify(subj)
		assert.Equal(t, want, got, "subj=%q", subj)
	}
}

func TestCounters_Record_PerCap(t *testing.T) {
	c := &Counters{}
	c.Record(audit.AuditRecord{Subject: "fs.dialog.read"})
	c.Record(audit.AuditRecord{Subject: "fs.handle.abc.read"})
	c.Record(audit.AuditRecord{Subject: "runtime.persist.play.x.set"})
	c.Record(audit.AuditRecord{Subject: "ch.local.exec.foo"}) // → other

	assert.EqualValues(t, 2, c.Count(CapFs))
	assert.EqualValues(t, 1, c.Count(CapPersist))
	assert.EqualValues(t, 0, c.Count(CapFacts))
	// CapBus is the universal substrate: every audit row counts.
	assert.EqualValues(t, 4, c.Count(CapBus))
	// "other" feeds the bus total but not the per-cap counters.
	assert.EqualValues(t, 1, c.other.Load())
}

func TestCounters_Reset(t *testing.T) {
	c := &Counters{}
	c.Record(audit.AuditRecord{Subject: "fs.dialog.read"})
	c.Reset()
	assert.EqualValues(t, 0, c.Count(CapFs))
	assert.EqualValues(t, 0, c.Count(CapBus))
}

func TestCounters_SatisfiesAuditSinkI(t *testing.T) {
	var _ audit.AuditSinkI = (*Counters)(nil)
	var _ audit.AuditSinkI = Tally
}
