package eh

import (
	"fmt"
	"runtime"

	"github.com/pkg/errors"
)

type singleWrappedWithStackError struct {
	wrappedErr unwrapableSingle
	stack      *stack
	cborData   []byte
}

func (inst *singleWrappedWithStackError) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *singleWrappedWithStackError) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *singleWrappedWithStackError) Error() string {
	return inst.wrappedErr.Error()
}

func (inst *singleWrappedWithStackError) StackTrace() errors.StackTrace {
	s := inst.stack
	if s != nil {
		f := make([]errors.Frame, len(*s))
		for i := 0; i < len(f); i++ {
			f[i] = errors.Frame((*s)[i])
		}
		return f
	}
	return nil
}

func (inst *singleWrappedWithStackError) Unwrap() error {
	return inst.wrappedErr.Unwrap()
}

var _ unwrapableSingle = (*singleWrappedWithStackError)(nil)
var _ stackTracer = (*singleWrappedWithStackError)(nil)
var _ ErrorWithStructuredData = (*singleWrappedWithStackError)(nil)

type multiWrappedWithStackError struct {
	wrappedErr unwrapableMulti
	stack      *stack
	cborData   []byte
}

func (inst *multiWrappedWithStackError) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *multiWrappedWithStackError) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *multiWrappedWithStackError) Error() string {
	return inst.wrappedErr.Error()
}

func (inst *multiWrappedWithStackError) StackTrace() errors.StackTrace {
	s := inst.stack
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}

func (inst *multiWrappedWithStackError) Unwrap() []error {
	return inst.wrappedErr.Unwrap()
}

var _ unwrapableMulti = (*multiWrappedWithStackError)(nil)
var _ stackTracer = (*multiWrappedWithStackError)(nil)
var _ ErrorWithStructuredData = (*multiWrappedWithStackError)(nil)

type withStackError struct {
	err      error
	stack    *stack
	cborData []byte
}

func (inst *withStackError) SetCBORStructuredData(p []byte) {
	inst.cborData = p
}

func (inst *withStackError) GetCBORStructuredData() []byte {
	return inst.cborData
}

func (inst *withStackError) Error() string {
	return inst.err.Error()
}

func (inst *withStackError) StackTrace() errors.StackTrace {
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

var _ error = (*withStackError)(nil)
var _ stackTracer = (*withStackError)(nil)
var _ ErrorWithStructuredData = (*withStackError)(nil)

func ErrorfWithData(cborData []byte, format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	s := callers(4)
	switch e := err.(type) {
	case unwrapableMulti:
		return &multiWrappedWithStackError{
			wrappedErr: e,
			stack:      s,
			cborData:   cborData,
		}
	case unwrapableSingle:
		return &singleWrappedWithStackError{
			wrappedErr: e,
			stack:      s,
			cborData:   cborData,
		}
	}
	return &withStackError{
		err:      err,
		stack:    s,
		cborData: cborData,
	}
}
func ErrorfWithDataWithoutStack(cborData []byte, format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	switch e := err.(type) {
	case unwrapableMulti:
		return &multiWrappedWithStackError{
			wrappedErr: e,
			stack:      nil,
			cborData:   cborData,
		}
	case unwrapableSingle:
		return &singleWrappedWithStackError{
			wrappedErr: e,
			stack:      nil,
			cborData:   cborData,
		}
	}
	return &withStackError{
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
