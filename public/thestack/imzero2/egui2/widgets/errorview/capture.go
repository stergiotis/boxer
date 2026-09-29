package errorview

import (
	"strconv"

	"github.com/fxamacker/cbor/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FromError is err's chain as eh records it — the tree eh.WalkStreams
// yields, which is the one eh.MarshalError logs — in the Context shape: a
// message per wrap, the frame triple, and the CBOR diagnostic of whatever
// eb attached. An error eh did not construct yields its message alone. A
// nil error is the zero Context.
//
// This is the adapter for a live error; the logviewer's toErrorviewContext
// is the one for a decoded log row. The walk resolves stack frames, so call
// it once per error rather than once per frame — Captured does that for a
// frame loop.
func FromError(err error) (out Context) {
	streams := eh.WalkStreams(err)
	if len(streams) == 0 {
		return
	}
	out.Streams = make([]Stream, 0, len(streams))
	for _, s := range streams {
		facts := make([]Fact, 0, len(s.Facts))
		for _, f := range s.Facts {
			fact := Fact{
				Msg:      f.Msg,
				Source:   f.Source,
				Function: f.Func,
				Data:     f.Data,
				Id:       f.Id,
				ParentId: f.ParentId,
			}
			if f.Line > 0 {
				fact.Line = strconv.FormatInt(int64(f.Line), 10)
			}
			if len(f.Data) > 0 {
				if diag, derr := cbor.Diagnose(f.Data); derr == nil {
					fact.DataDiag = diag
				}
			}
			facts = append(facts, fact)
		}
		out.Streams = append(out.Streams, Stream{Name: s.Name, Facts: facts})
	}
	return
}

// Captured is an error held for display: the error and its chain, walked
// once by Capture so a renderer draws it every frame without re-walking.
// It is a value; hand it from a worker goroutine to the frame goroutine
// under the owner's lock like any other field. The zero value holds no
// error.
type Captured struct {
	err   error
	chain Context
}

// Capture walks err's chain. A nil error yields the zero Captured.
func Capture(err error) (out Captured) {
	if err == nil {
		return
	}
	out = Captured{err: err, chain: FromError(err)}
	return
}

// Err is the captured error; nil for the zero value.
func (inst Captured) Err() (err error) {
	err = inst.err
	return
}

// IsEmpty reports whether no error is held.
func (inst Captured) IsEmpty() (ok bool) {
	ok = inst.err == nil
	return
}

// Chain is the error's chain as FromError built it.
func (inst Captured) Chain() (ctx Context) {
	ctx = inst.chain
	return
}
