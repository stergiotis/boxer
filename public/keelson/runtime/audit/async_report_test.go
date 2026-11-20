package audit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Drops reach the log: a Warn naming the count when the sink closes, and
// none when nothing was dropped.
func TestAsyncSink_ReportsDropsInTheLog(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })

	quiet := NewAsyncSink(&recordingSink{}, 0)
	quiet.Record(AuditRecord{Subject: "x"})
	quiet.Close()
	if strings.Contains(buf.String(), "dropped") {
		t.Fatalf("a sink that dropped nothing logged: %s", buf.String())
	}

	gate := make(chan struct{})
	s := NewAsyncSink(&blockingSink{gate: gate}, 1)
	for range 50 {
		s.Record(AuditRecord{Subject: "x"})
	}
	close(gate)
	s.Close()
	if s.Dropped() == 0 {
		t.Fatal("expected drops")
	}
	if !strings.Contains(buf.String(), `"droppedTotal"`) {
		t.Errorf("the drops were not logged: %s", buf.String())
	}
}
