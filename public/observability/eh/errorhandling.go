package eh

import (
	"fmt"
	"runtime"

	"github.com/pkg/errors"
)

type singleWrappedWithStack struct {
	wrappedErr unwrapableSingle
	stack      *stack
	cborData   []byte
}

func (inst *singleWrappedWithStack) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *singleWrappedWithStack) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *singleWrappedWithStack) Error() string {
	return inst.wrappedErr.Error()
}

func (inst *singleWrappedWithStack) StackTrace() errors.StackTrace {
	s := inst.stack
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}

func (inst *singleWrappedWithStack) Unwrap() error {
	return inst.wrappedErr.Unwrap()
}

var _ unwrapableSingle = (*singleWrappedWithStack)(nil)
var _ stackTracer = (*singleWrappedWithStack)(nil)
var _ ErrorWithStructuredData = (*singleWrappedWithStack)(nil)

type multiWrappedWithStack struct {
	wrappedErr unwrapableMulti
	stack      *stack
	cborData   []byte
}

func (inst *multiWrappedWithStack) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *multiWrappedWithStack) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *multiWrappedWithStack) Error() string {
	return inst.wrappedErr.Error()
}

func (inst *multiWrappedWithStack) StackTrace() errors.StackTrace {
	s := inst.stack
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}

func (inst *multiWrappedWithStack) Unwrap() []error {
	return inst.wrappedErr.Unwrap()
}

var _ unwrapableMulti = (*multiWrappedWithStack)(nil)
var _ stackTracer = (*multiWrappedWithStack)(nil)
var _ ErrorWithStructuredData = (*multiWrappedWithStack)(nil)

type withStack struct {
	err      error
	stack    *stack
	cborData []byte
}

func (inst *withStack) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *withStack) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *withStack) Error() string {
	return inst.err.Error()
}

func (inst *withStack) StackTrace() errors.StackTrace {
	s := inst.stack
	if s == nil {
		return nil
	}
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}

var _ error = (*withStack)(nil)
var _ stackTracer = (*withStack)(nil)
var _ ErrorWithStructuredData = (*withStack)(nil)

func ErrorfWithData(cborData []byte, format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	s := callers(4)
	switch e := err.(type) {
	case unwrapableMulti:
		return &multiWrappedWithStack{
			wrappedErr: e,
			stack:      s,
			cborData:   cborData,
		}
	case unwrapableSingle:
		return &singleWrappedWithStack{
			wrappedErr: e,
			stack:      s,
			cborData:   cborData,
		}
	}
	return &withStack{
		err:      err,
		stack:    s,
		cborData: cborData,
	}
}
func ErrorfWithDataWithoutStack(cborData []byte, format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	switch e := err.(type) {
	case unwrapableMulti:
		return &multiWrappedWithStack{
			wrappedErr: e,
			stack:      nil,
			cborData:   cborData,
		}
	case unwrapableSingle:
		return &singleWrappedWithStack{
			wrappedErr: e,
			stack:      nil,
			cborData:   cborData,
		}
	}
	return &withStack{
		err:      err,
		stack:    nil,
		cborData: cborData,
	}
}

func Errorf(format string, a ...any) error {
	return ErrorfWithData(nil, format, a...)
}
func New(msg string) error {
	return Errorf("%s", msg)
}

const maxStackDepth = 32

func callers(skip int) *stack {
	var pc [maxStackDepth]uintptr
	n := runtime.Callers(skip, pc[:])
	r := pc[0:n]
	return (*stack)(&r)
}
