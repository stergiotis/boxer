package ir

import (
	"bytes"
	"io"
	"runtime"
)

const DefaultStackDepth = 0

type StackCapture struct {
	Files []string
	Lines []int
	Funcs []string
}

func NewStackCapture(skip int, depth int) (inst *StackCapture) {
	inst = &StackCapture{
		Files: nil,
		Lines: nil,
		Funcs: nil,
	}
	if depth > 0 {
		inst.capture(skip+2, depth)
	}
	return
}
func (inst *StackCapture) capture(skip int, depth int) {
	pcs := make([]uintptr, 2*depth)
	l := runtime.Callers(skip, pcs)
	if l == 0 {
		return
	}
	frames := runtime.CallersFrames(pcs[:l])
	//t := false
	files := make([]string, 0, l)
	lines := make([]int, 0, l)
	funcs := make([]string, 0, l)
	t := 0
	for {
		frame, more := frames.Next()
		files = append(files, frame.File)
		lines = append(lines, frame.Line)
		funcs = append(funcs, frame.Function)
		switch frame.Function {
		case "fmt.Fprintf", "fmt.Fprint":
			t = len(files)
			break
		}
		if !more {
			break
		}
	}
	s := min(len(files), t+depth)
	inst.Files = files[t:s]
	inst.Lines = lines[t:s]
	inst.Funcs = funcs[t:s]
}

type DefaultCodeS struct {
}

var DefaultCode = &DefaultCodeS{}

func (inst *DefaultCodeS) UseDefaultCode() bool {
	return true
}

func (inst *DefaultCodeS) GetVerbatimCode() string {
	return ""
}

var _ VerbatimCodeI = (*DefaultCodeS)(nil)

type EmptyCodeS struct {
}

var EmptyCode = &EmptyCodeS{}

func (inst *EmptyCodeS) UseDefaultCode() bool {
	return false
}

func (inst *EmptyCodeS) GetVerbatimCode() string {
	return ""
}

var _ VerbatimCodeI = (*EmptyCodeS)(nil)

type CodeLocationBufferWriter struct {
	buf   *bytes.Buffer
	stack *StackCapture
}

var _ io.Writer = (*CodeLocationBufferWriter)(nil)
var _ io.StringWriter = (*CodeLocationBufferWriter)(nil)
var _ VerbatimCodeI = (*CodeLocationBufferWriter)(nil)

func NewCodeLocationBufferWriter(buf []byte) *CodeLocationBufferWriter {
	return &CodeLocationBufferWriter{
		buf:   bytes.NewBuffer(buf),
		stack: nil,
	}
}
func (inst *CodeLocationBufferWriter) OverrideCodeLocation(loc *StackCapture) {
	inst.stack = loc
}
func (inst *CodeLocationBufferWriter) Reset() {
	inst.buf.Reset()
}
func (inst *CodeLocationBufferWriter) String() string {
	return inst.buf.String()
}
func (inst *CodeLocationBufferWriter) captureCaller() {
	if inst.stack == nil {
		inst.stack = NewStackCapture(2, DefaultStackDepth)
	}
}
func (inst *CodeLocationBufferWriter) GetStack() (stack *StackCapture) {
	return inst.stack
}
func (inst *CodeLocationBufferWriter) WriteString(s string) (n int, err error) {
	inst.captureCaller()
	return inst.buf.WriteString(s)
}

func (inst *CodeLocationBufferWriter) Write(p []byte) (n int, err error) {
	inst.captureCaller()
	return inst.buf.Write(p)
}
func (inst *CodeLocationBufferWriter) UseDefaultCode() bool {
	return false
}

func (inst *CodeLocationBufferWriter) GetVerbatimCode() string {
	return inst.buf.String()
}
