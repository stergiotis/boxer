//go:build llm_generated_opus47

package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// Pre-built retained jobs for static Go snippets — zero per-frame cost on
// the Go side. Each PrepareGo call runs go/scanner + go/parser once at
// init and produces a retained CodeViewJob that the FFFI client emits as
// reference each frame.
var (
	goSimple = codeview.PrepareGo(`package main

import "fmt"

func main() {
	fmt.Println("hello, world")
}
`)

	goStruct = codeview.PrepareGo(`package store

import "io"

// Reader buffers reads from an underlying io.Reader.
type Reader struct {
	src io.Reader
	buf []byte
	pos int
}

const DefaultBufSize = 4096

// Read fills p from the buffer, refilling from src as needed.
func (inst *Reader) Read(p []byte) (n int, err error) {
	if inst.pos >= len(inst.buf) {
		n, err = inst.src.Read(inst.buf)
		if err != nil {
			return
		}
		inst.pos = 0
	}
	n = copy(p, inst.buf[inst.pos:])
	inst.pos += n
	return
}
`)

	goInterfaceGenerics = codeview.PrepareGo(`package iter

// Source is anything that can yield items of type T.
type Source[T any] interface {
	Next() (item T, ok bool)
}

// Map adapts a Source[A] into a Source[B] via fn.
func Map[A, B any](src Source[A], fn func(A) B) Source[B] {
	return &mapper[A, B]{src: src, fn: fn}
}

type mapper[A, B any] struct {
	src Source[A]
	fn  func(A) B
}

func (inst *mapper[A, B]) Next() (item B, ok bool) {
	var a A
	a, ok = inst.src.Next()
	if !ok {
		return
	}
	item = inst.fn(a)
	return
}
`)

	goBuildTag = codeview.PrepareGo(`//go:build linux && amd64

// Package syscallx exposes platform-specific syscalls.
package syscallx

import (
	"errors"
	"syscall"
)

// ErrUnsupported is returned when a syscall is not available on this build.
var ErrUnsupported = errors.New("syscallx: unsupported on this platform")

// Pidfd opens a pidfd for the given pid.
func Pidfd(pid int) (fd int, err error) {
	r1, _, e1 := syscall.Syscall(434, uintptr(pid), 0, 0)
	if e1 != 0 {
		err = e1
		return
	}
	fd = int(r1)
	return
}
`)

	// goWindowSource is a ~32-line file used by the line-range demo.
	// Two windows are pulled from it to show that the gutter reflects the
	// original line numbers and that AST refinement survives clipping.
	goWindowSource = `//go:build linux

// Package svc is a tiny periodic-runner.
package svc

import (
	"fmt"
	"io"
	"time"
)

type StatusE int

const (
	StatusIdle StatusE = iota
	StatusRunning
	StatusFailed
)

// Service runs a periodic loop driven by an io.Reader.
type Service struct {
	name   string
	status StatusE
	src    io.Reader
}

// Start runs the service for at most timeout.
func (inst *Service) Start(timeout time.Duration) (err error) {
	inst.status = StatusRunning
	fmt.Println("starting", inst.name, "for", timeout)
	buf := make([]byte, 4096)
	n, err := inst.src.Read(buf)
	if err != nil {
		inst.status = StatusFailed
		return
	}
	fmt.Println("read", n, "bytes")
	return
}
`

	// Lines 6-18 cover imports + const block.
	goWindowImports = codeview.PrepareGoLines(goWindowSource, 6, 18)
	// Lines 21-32 cover the struct + start of method.
	goWindowMethod = codeview.PrepareGoLines(goWindowSource, 21, 32)
)

func demoGoView(ids *c.WidgetIdStack) {
	for range c.CollapsingHeader(ids.PrepareStr("go-window-imports"), c.WidgetText().Text("line range 6-18 (imports + const block)").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-win-imports"), goWindowImports).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("go-window-method"), c.WidgetText().Text("line range 21-32 (struct + method body)").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-win-method"), goWindowMethod).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("go-simple"), c.WidgetText().Text("hello, world").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-simple"), goSimple).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("go-struct"), c.WidgetText().Text("struct + methods + doc comments").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-struct"), goStruct).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("go-generics"), c.WidgetText().Text("interface + generics").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-generics"), goInterfaceGenerics).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("go-buildtag"), c.WidgetText().Text("build constraint + imports").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-go-buildtag"), goBuildTag).Send()
	}
}
