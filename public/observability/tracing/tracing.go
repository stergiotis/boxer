package tracing

import (
	"errors"
	"io"
	"iter"
	"strconv"

	"github.com/stergiotis/boxer/public/containers"
	"golang.org/x/exp/trace"
)

type TraceUtils struct {
	dedupCodeLocations *containers.HashSet[string]
}

func NewTraceUtils(estLinesOfCode int) *TraceUtils {
	return &TraceUtils{
		dedupCodeLocations: containers.NewHashSet[string](estLinesOfCode),
	}
}
func (inst *TraceUtils) IterateCodeLocations(tr io.Reader) iter.Seq2[string, uint64] {
	return func(yield func(string, uint64) bool) {
		dedup := inst.dedupCodeLocations
		dedup.Clear()
		defer dedup.Clear()
		for ev := range inst.IterateEvents(tr) {
			for frame := range ev.Stack().Frames() {
				file := frame.File
				if file != "" {
					line := frame.Line
					if !dedup.AddEx(file + ":" + strconv.FormatUint(line, 16)) {
						if !yield(file, line) {
							return
						}
					}
				}
			}
		}
	}
}
func (inst *TraceUtils) IterateEvents(tr io.Reader) iter.Seq[trace.Event] {
	return func(yield func(event trace.Event) bool) {
		reader, err := trace.NewReader(tr)
		if err == nil {
			for {
				var ev trace.Event
				ev, err = reader.ReadEvent()
				if errors.Is(err, io.EOF) {
					return
				}
				if !yield(ev) {
					return
				}
			}
		}
	}
}
