package eh

import (
	"fmt"
	"github.com/fxamacker/cbor/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/internal/3rdParty/errorhandling"
	"os"
	"runtime"
	"strings"
)

type FrameTypeE uint8

const (
	FrameTypeNil    FrameTypeE = 0
	FrameTypeGoPath FrameTypeE = 1
	FrameTypeGoRoot FrameTypeE = 2
	FrameTypeOther  FrameTypeE = 3
)

type frameContainer struct {
	File     string
	Line     string
	Function string
	Type     FrameTypeE
	Frame    errors.Frame
}

type state struct {
	b     []byte
	flags string
}

var _ fmt.State = (*state)(nil)

func (inst *state) Write(b []byte) (n int, err error) {
	inst.b = b
	return len(b), nil
}

func (inst *state) Width() (wid int, ok bool) {
	return 0, false
}

func (inst *state) Precision() (prec int, ok bool) {
	return 0, false
}

func (inst *state) Flag(c int) bool {
	return strings.ContainsRune(inst.flags, rune(c))
}

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

func (inst *frameContainer) CleanupAndResolveType() {
	if inst.Type != FrameTypeNil {
		return
	}
	f := errorhandling.RemoveGoPath(inst.File)
	if f != inst.File {
		inst.Type = FrameTypeGoPath
		inst.File = f
		return
	}
	if strings.HasPrefix(f, runtime.GOROOT()) {
		inst.Type = FrameTypeGoRoot
		return
	}
	inst.Type = FrameTypeOther
}

type errorFact struct {
	Msg            string
	Frame          *frameContainer
	StructuredData []byte
	Id             uint64
	ParentId       uint64

	frameForAssertion errors.Frame
}
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
	facts  []*errorFact
	facts2 [][]*errorFact
	name   string
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

type gatherFactsAndStacks struct {
	perStackFacts        [] /*stackIndex*/ [] /*stackPos*/ []*errorFact
	stacks               [] /*stackIndex*/ errors.StackTrace
	stackRepresentations [] /*stackIndex*/ []byte
	nextId               uint64
	materialized         bool
}

func (inst *gatherFactsAndStacks) hasStacks() bool {
	return len(inst.stacks) > 1
}
func (inst *gatherFactsAndStacks) hasStacklessStream() bool {
	return len(inst.stacklessFacts()) > 0
}
func (inst *gatherFactsAndStacks) stacklessFacts() []*errorFact {
	return inst.perStackFacts[0][0]
}

var _ zerolog.LogObjectMarshaler = (*gatherFactsAndStacks)(nil)
var _ zerolog.LogArrayMarshaler = (*gatherFactsAndStacks)(nil)

func newGatherFactsAndStacks() *gatherFactsAndStacks {
	perStackFacts := make([][][]*errorFact, 0, 4)
	perStackFacts = append(perStackFacts, make([][]*errorFact, 0, 1))
	perStackFacts[0] = append(perStackFacts[0], make([]*errorFact, 0, 8))
	stacks := make([]errors.StackTrace, 0, 4)
	stacks = append(stacks, nil)
	stackRepresentations := make([][]byte, 0, 4)
	stackRepresentations = append(stackRepresentations, nil)
	return &gatherFactsAndStacks{
		perStackFacts:        perStackFacts,
		stacks:               stacks,
		stackRepresentations: stackRepresentations,
		nextId:               0,
	}
}
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
		var s errors.Frame
		for _, fact := range facts {
			f := fact.frameForAssertion
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

func (inst *gatherFactsAndStacks) materialize() {
	if inst.materialized {
		return
	}
	s := &state{
		b:     nil,
		flags: "+",
	}
	for stackIndex, st := range inst.stacks {
		facts := inst.perStackFacts[stackIndex]
		l := len(st)
		for inStackPos, frame := range st {
			fc := frameContainer{Frame: frame}
			frame.Format(s, 'd')
			fc.Line = string(s.b)
			frame.Format(s, 's')
			fc.File = string(s.b)
			frame.Format(s, 'n')
			fc.Function = string(s.b)
			fact := &errorFact{
				Frame: &fc,
			}
			p := l - 1 - inStackPos
			if facts[p] == nil {
				facts[p] = []*errorFact{
					fact,
				}
			} else {
				facts[p] = append(facts[p], fact)
			}
		}
	}

	// TODO: release memory in inst.stackRepresentations?

	inst.materialized = true
}

func (inst *gatherFactsAndStacks) addError(err error, parentId uint64) error {
	if inst.materialized {
		return Errorf("unable to add error: object is in materialized state")
	}
	stackIndex := uint32(0)
	inStackPos := uint32(0)

	var frame errors.Frame
	switch et := err.(type) {
	case stackTracer:
		st := et.StackTrace()
		if st != nil && len(st) > 0 {
			stackIndex, inStackPos = inst.findStack(st)
			frame = st[0]
		}
	}

	id := inst.nextId
	inst.nextId++
	facts := inst.perStackFacts[stackIndex][inStackPos]
	if facts == nil {
		facts = make([]*errorFact, 0, 2)
	}
	facts = append(facts, &errorFact{
		Msg:               err.Error(),
		Id:                id,
		ParentId:          parentId,
		frameForAssertion: frame,
	})

	switch et := err.(type) {
	case ErrorWithStructuredData:
		data := et.GetCBORStructuredData()
		if data != nil && len(data) > 0 {
			facts = append(facts, &errorFact{
				StructuredData:    data,
				Id:                id,
				ParentId:          parentId,
				frameForAssertion: frame,
			})
		}
	}

	inst.perStackFacts[stackIndex][inStackPos] = facts

	var ses []error
	switch et := err.(type) {
	case unwrapableMulti:
		ses = et.Unwrap()
	}
	switch et := err.(type) {
	case unwrapableSingle:
		ses = []error{et.Unwrap()}
	}

	if ses != nil {
		parentId = id
		for _, se := range ses {
			if se != err && se != nil {
				err = inst.addError(se, parentId)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (inst *gatherFactsAndStacks) findStack(st errors.StackTrace) (stackIndex uint32, inStackPos uint32) {
	rep := toBinaryRepresentation(st)
	inStackPos = uint32(len(st))
	for i, rep2 := range inst.stackRepresentations {
		if i == 0 {
			// skip non-stack index 0
			continue
		}
		if isSubStack(rep, rep2) {
			if inst.perStackFacts[i][inStackPos] == nil {
				inst.perStackFacts[i][inStackPos] = make([]*errorFact, 0, 2)
			}
			// found in known stacks
			stackIndex = uint32(i)
			return
		}
		if isSubStack(rep2, rep) {
			inst.stackRepresentations[i] = rep
			inst.stacks[i] = st
			stackIndex = uint32(i)
			gap := int(inStackPos) - len(inst.perStackFacts[i]) + 1
			if gap > 0 {
				inst.perStackFacts[i] = append(inst.perStackFacts[i], make([][]*errorFact, gap)...)
			}
			return
		}
	}

	stackIndex = uint32(len(inst.stackRepresentations))
	inst.stacks = append(inst.stacks, st)
	inst.stackRepresentations = append(inst.stackRepresentations, rep)
	inst.perStackFacts = append(inst.perStackFacts, make([][]*errorFact, inStackPos+1))
	return
}

func MarshalError(err error) interface{} {
	if err == nil {
		return nil
	}
	g := newGatherFactsAndStacks()
	_ = g.addError(err, 0)
	return g
}
