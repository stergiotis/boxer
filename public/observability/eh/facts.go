package eh

import (
	"fmt"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/config/env"
)

// detectedGoRoot resolves the active GOROOT once so that stack-trace file paths
// rooted there can be classified (and shortened). It prefers the BOXER env
// override; failing that it asks the installed toolchain via
// goRootFromToolchain.
//
// runtime.GOROOT was deprecated in Go 1.24: the build-time value can be wrong
// when the binary is copied across machines, so the toolchain query (not
// runtime.GOROOT) is the fallback. goRootFromToolchain is build-tagged so the
// os/exec it needs is excluded under TinyGo (which has no process model); there
// the env override is the only source and an empty result is fine — paths just
// aren't shortened.
var detectedGoRoot = sync.OnceValue(func() string {
	// Prefer the env var so users can override without invoking go.
	r := env.GoRoot.Get()
	if r != "" {
		return r
	}
	return goRootFromToolchain()
})

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
}

func (inst *frameContainer) CleanupAndResolveType() {
	if inst.Type != FrameTypeNil {
		return
	}
	f := removeGoPath(inst.File)
	if f != inst.File {
		inst.Type = FrameTypeGoPath
		inst.File = f
		return
	}
	if r := detectedGoRoot(); r != "" && strings.HasPrefix(f, r) {
		inst.Type = FrameTypeGoRoot
		return
	}
	inst.Type = FrameTypeOther
}

// removeGoPath makes a path relative to one of the src directories in the $GOPATH
// environment variable. If $GOPATH is empty or the input path is not contained
// within any of the src directories in $GOPATH, the original path is returned.
func removeGoPath(path string) string {
	gopath := env.GoPath.Get()
	if gopath == "" {
		return path
	}
	srcdir := gopath + "/src/"
	if strings.HasPrefix(path, srcdir) {
		return path[len(srcdir):]
	}
	return path
}

type errorFact struct {
	Msg            string
	Frame          *frameContainer
	StructuredData []byte
	Id             uint64
	ParentId       uint64

	framePC uintptr
}

type gatherFactsAndStacks struct {
	perStackFacts        [] /*stackIndex*/ [] /*stackPos*/ []*errorFact
	stacks               [] /*stackIndex*/ StackTrace
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

func newGatherFactsAndStacks() *gatherFactsAndStacks {
	perStackFacts := make([][][]*errorFact, 0, 4)
	perStackFacts = append(perStackFacts, make([][]*errorFact, 0, 1))
	perStackFacts[0] = append(perStackFacts[0], make([]*errorFact, 0, 8))
	stacks := make([]StackTrace, 0, 4)
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

func (inst *gatherFactsAndStacks) materialize() {
	if inst.materialized {
		return
	}
	for stackIndex, st := range inst.stacks {
		facts := inst.perStackFacts[stackIndex]
		l := len(st)
		for inStackPos, frame := range st {
			fc := frameContainer{
				File:     frame.File,
				Line:     fmt.Sprintf("%d", frame.Line),
				Function: frame.ShortFunction(),
			}
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

	inst.materialized = true
}

func (inst *gatherFactsAndStacks) addError(err error, parentId uint64) error {
	if inst.materialized {
		return Errorf("unable to add error: object is in materialized state")
	}
	stackIndex := uint32(0)
	inStackPos := uint32(0)

	var framePC uintptr
	switch et := err.(type) {
	case stackTracer:
		st := et.StackTrace()
		if len(st) > 0 {
			stackIndex, inStackPos = inst.findStack(st)
			framePC = st[0].PC
		}
	}

	id := inst.nextId
	inst.nextId++
	facts := inst.perStackFacts[stackIndex][inStackPos]
	if facts == nil {
		facts = make([]*errorFact, 0, 2)
	}
	facts = append(facts, &errorFact{
		Msg:      err.Error(),
		Id:       id,
		ParentId: parentId,
		framePC:  framePC,
	})

	switch et := err.(type) {
	case ErrorWithStructuredDataI:
		data := et.GetCBORStructuredData()
		if len(data) > 0 {
			facts = append(facts, &errorFact{
				StructuredData: data,
				Id:             id,
				ParentId:       parentId,
				framePC:        framePC,
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
func (inst *gatherFactsAndStacks) findStack(st StackTrace) (stackIndex uint32, inStackPos uint32) {
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

// MarshalError builds the deduplicated fact/stack tree for err. The returned
// value is the zerolog egress object (see zerolog.go, !tinygo) but is itself
// zerolog-free — non-zerolog consumers reach the same tree via WalkStreams.
func MarshalError(err error) interface{} {
	if err == nil {
		return nil
	}
	g := newGatherFactsAndStacks()
	_ = g.addError(err, 0)
	return g
}
