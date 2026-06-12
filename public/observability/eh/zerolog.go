//go:build !tinygo

package eh

import (
	"fmt"
	"os"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// This file holds the zerolog egress for the eh fact model defined in facts.go.
// It is excluded from TinyGo builds (//go:build !tinygo) because
// github.com/rs/zerolog imports net (for its IP/MAC field helpers) and that
// import is unavailable on TinyGo/wasm. The zerolog-free fact tree in facts.go
// — and its WalkStreams projection — stay available everywhere; only the
// structured zerolog marshaling here and the human console formatter
// (eh_format_zerolog.go) drop out under TinyGo. See ADR-0078.

var (
	StackSourceFileName     = "source"
	StackSourceLineName     = "line"
	StackSourceFunctionName = "func"
)

func (inst *frameContainer) MarshalZerologObject(e *zerolog.Event) {
	if inst.File != "" {
		e.Str(StackSourceFileName, inst.File)
	}
	if inst.Line != "" {
		e.Str(StackSourceLineName, inst.Line)
	}
	if inst.Function != "" {
		e.Str(StackSourceFunctionName, inst.Function)
	}
}

var _ zerolog.LogObjectMarshaler = (*frameContainer)(nil)

type errorContainer struct {
	Facts []*errorFact
}

func (inst *errorContainer) CompactStackTrace() {
	for _, c := range inst.Facts {
		if c.Frame != nil {
			c.Frame.CleanupAndResolveType()
		}
	}

	pfx, pfxLen := inst.longestCommonPathPrefix()
	if pfxLen > 1 {
		for _, c := range inst.Facts {
			if c.Frame != nil && c.Frame.Type == FrameTypeOther {
				if strings.HasPrefix(c.Frame.File, pfx) {
					c.Frame.File = c.Frame.File[pfxLen:]
				} else {
					log.Warn().Str("pfx", pfx).Str("file", c.Frame.File).Msg("pfx is not prefix")
				}
			}
		}
	}
}
func (inst *errorContainer) longestCommonPathPrefix() (string, int) {
	var minPath string
	var maxPath string
	for _, c := range inst.Facts {
		if c.Frame != nil {
			f := c.Frame.File
			if c.Frame.Type != FrameTypeOther {
				continue
			}
			if minPath == "" || strings.Compare(f, minPath) < 0 {
				minPath = f
			}
			if maxPath == "" || strings.Compare(f, maxPath) > 0 {
				maxPath = f
			}
		}
	}
	sep := string(os.PathSeparator)
	minPathL := strings.Split(minPath, sep)
	maxPathL := strings.Split(maxPath, sep)
	for i := 0; i < len(minPathL)-1; i++ {
		if minPathL[i] != maxPathL[i] {
			pfx := strings.Join(minPathL[:i], sep) + sep
			return pfx, len(pfx)
		}
	}
	if len(minPathL) > 1 {
		pfx := strings.Join(minPathL[:len(minPathL)-2], sep) + sep
		return pfx, len(pfx)
	}
	return "", 0
}

func (inst *errorContainer) MarshalZerologObject(e *zerolog.Event) {
	e.Array("perStackFacts", inst)
}

func (inst *errorContainer) MarshalZerologArray(a *zerolog.Array) {
	for _, c := range inst.Facts {
		a.Object(c)
	}
}

var _ zerolog.LogArrayMarshaler = (*errorContainer)(nil)
var _ zerolog.LogObjectMarshaler = (*errorContainer)(nil)

func (inst *errorFact) MarshalZerologObject(e *zerolog.Event) {
	if inst.Msg != "" {
		e.Str("msg", inst.Msg)
	}
	if inst.Frame != nil {
		inst.Frame.MarshalZerologObject(e)
	}
	if inst.StructuredData != nil {
		e.RawCBOR("data", inst.StructuredData)
		diag, err := cbor.Diagnose(inst.StructuredData)
		if err == nil {
			e.Str("dataDiag", diag)
		}
	}

	e.Uint64("id", inst.Id)
	if inst.ParentId != inst.Id {
		e.Uint64("parentId", inst.ParentId)
	}
}

var _ zerolog.LogObjectMarshaler = (*errorFact)(nil)

type errorFactsLogger struct {
	name   string
	facts  []*errorFact
	facts2 [][]*errorFact
}

func (inst *errorFactsLogger) MarshalZerologArray(a *zerolog.Array) {
	if inst.facts != nil {
		for _, f := range inst.facts {
			a.Object(f)
		}
	}
	if inst.facts2 != nil {
		for _, fs := range inst.facts2 {
			for _, f := range fs {
				a.Object(f)
			}
		}
	}
}

func (inst *errorFactsLogger) MarshalZerologObject(e *zerolog.Event) {
	e.Array(inst.name, inst)
}

var _ zerolog.LogObjectMarshaler = (*errorFactsLogger)(nil)
var _ zerolog.LogArrayMarshaler = (*errorFactsLogger)(nil)

var _ zerolog.LogObjectMarshaler = (*gatherFactsAndStacks)(nil)
var _ zerolog.LogArrayMarshaler = (*gatherFactsAndStacks)(nil)

func (inst *gatherFactsAndStacks) MarshalZerologArray(a *zerolog.Array) {
	inst.materialize()
	if inst.hasStacklessStream() {
		a.Object(&errorFactsLogger{
			facts: inst.stacklessFacts(),
			name:  "no-stack",
		})
	}
	for i, perPositionFacts := range inst.perStackFacts[1:] {
		facts2 := perPositionFacts
		inst.validateFrames(facts2)
		a.Object(&errorFactsLogger{
			facts2: facts2,
			name:   fmt.Sprintf("stack-%d", i),
		})
	}
}
func (inst *gatherFactsAndStacks) validateFrames(facts2 [][]*errorFact) {
	for _, facts := range facts2 {
		var s uintptr
		for _, fact := range facts {
			f := fact.framePC
			if f != 0 {
				if s == 0 {
					s = f
				} else if s != f {
					log.Fatal().Interface("facts", facts2).Msg("found different frames within the same facts2 slice")
				}
			}
		}
	}
}

func (inst *gatherFactsAndStacks) MarshalZerologObject(e *zerolog.Event) {
	inst.materialize()
	e.Array("streams", inst)
}
