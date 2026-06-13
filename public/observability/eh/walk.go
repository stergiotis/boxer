package eh

import (
	"fmt"
	"strconv"
)

// Stream is one PC-prefix-deduplicated facts stream produced by walking a
// boxer error chain. The Name mirrors the eh.MarshalError zerolog egress:
// "no-stack" for errors built via ErrorfWithDataWithoutStack, or "stack-N"
// for the N-th distinct stack trace (deduplication is by stack PC prefix,
// not by goroutine).
type Stream struct {
	Name  string
	Facts []Fact
}

// Fact is one entry in a Stream. The eh wire treats fact entries
// polymorphically — a fact carries one of {Msg, frame stub, Data} at a
// time. Message facts come from each wrap-chain step; frame stubs come
// from runtime.CallersFrames materialization; data facts carry the CBOR
// bytes produced by eb.Build / ErrorfWithData. Consumers disambiguate by
// checking which fields are set, mirroring the zerolog projection at
// zerolog.go:179-197.
//
// Id collisions between frame stubs (Id==0 by zero-value) and the first
// message fact (Id==0 from the nextId counter) are deliberate — the
// zerolog egress emits the same collision and consumers already handle
// it by field-presence discrimination.
type Fact struct {
	Msg      string
	Source   string
	Line     int32
	Func     string
	Data     []byte
	Id       uint64
	ParentId uint64
}

// WalkStreams produces the same logical stream/fact tree that
// eh.MarshalError emits via zerolog, but as a plain Go value suitable
// for non-zerolog consumers (CBOR codecs, in-memory inspection,
// downstream shredders such as keelson/runtime/rowmarshall).
//
// Returns nil if err is nil. The dedup logic is the existing
// gatherFactsAndStacks.addError + findStack + materialize pathway —
// same PC-prefix sub-stack detection, same fact ordering as the zerolog
// egress (see zerolog.go:266-282, 208-221).
func WalkStreams(err error) (streams []Stream) {
	if err == nil {
		return
	}
	g := newGatherFactsAndStacks()
	_ = g.addError(err, 0)
	g.materialize()

	if g.hasStacklessStream() {
		nls := g.stacklessFacts()
		facts := make([]Fact, len(nls))
		for i, f := range nls {
			facts[i] = toFactView(f)
		}
		streams = append(streams, Stream{Name: "no-stack", Facts: facts})
	}
	for i, perPositionFacts := range g.perStackFacts[1:] {
		total := 0
		for _, fs := range perPositionFacts {
			total += len(fs)
		}
		facts := make([]Fact, 0, total)
		for _, fs := range perPositionFacts {
			for _, f := range fs {
				facts = append(facts, toFactView(f))
			}
		}
		streams = append(streams, Stream{
			Name:  fmt.Sprintf("stack-%d", i),
			Facts: facts,
		})
	}
	return
}

// toFactView projects an internal errorFact into the public Fact view.
// frameContainer.Line is stored as a decimal string (per zerolog egress
// convention); we parse it back to int32 for typed consumers.
func toFactView(f *errorFact) (fv Fact) {
	fv.Msg = f.Msg
	fv.Data = f.StructuredData
	fv.Id = f.Id
	fv.ParentId = f.ParentId
	if f.Frame != nil {
		fv.Source = f.Frame.File
		if f.Frame.Line != "" {
			n, _ := strconv.Atoi(f.Frame.Line)
			fv.Line = int32(n)
		}
		fv.Func = f.Frame.Function
	}
	return
}
