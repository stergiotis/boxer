package errkind

import (
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FromBoxerError walks a boxer error chain (via eh.WalkStreams) and
// flattens the PC-prefix-deduplicated stream tree into the parallel-
// array Error shape. Drop-in replacement for the legacy
// rowmarshall.FromBoxerError, except the output is value-typed (not
// pointer) and pre-shredded.
//
// id and naturalKey are caller-supplied: typical sources are a request
// id, a trace id, or a monotonic counter. capturedTs is the capture
// time (typically time.Now(); callers control it so tests can pin a
// deterministic value).
//
// Returns zero-value Error if err is nil.
func FromBoxerError(id uint64, naturalKey []byte, capturedTs time.Time, err error) (out Error) {
	if err == nil {
		return
	}
	streams := eh.WalkStreams(err)
	var n int
	for _, s := range streams {
		n += len(s.Facts)
	}
	out = Error{
		Id:          id,
		NaturalKey:  naturalKey,
		CapturedTs:  capturedTs,
		Messages:    make([]string, 0, n),
		Sources:     make([]string, 0, n),
		Funcs:       make([]string, 0, n),
		StreamNames: make([]string, 0, n),
		Lines:       make([]uint32, 0, n),
		FactIds:     make([]uint64, 0, n),
		ParentIds:   make([]uint64, 0, n),
		Data:        make([][]byte, 0, n),
	}
	for _, s := range streams {
		for _, f := range s.Facts {
			out.Messages = append(out.Messages, f.Msg)
			out.Sources = append(out.Sources, f.Source)
			out.Funcs = append(out.Funcs, f.Func)
			out.StreamNames = append(out.StreamNames, s.Name)
			out.Lines = append(out.Lines, uint32(f.Line))
			out.FactIds = append(out.FactIds, f.Id)
			out.ParentIds = append(out.ParentIds, f.ParentId)
			out.Data = append(out.Data, f.Data)
		}
	}
	return
}
