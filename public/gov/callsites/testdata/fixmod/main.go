// Package main is the classification corpus for the callsites analyzer
// tests. Call sites are addressed by their trailing @marker comments; each
// marked line carries exactly one call expression.
package main

import (
	"fmt"

	"example.com/extdep"
	"example.com/fixmod/lib"
)

type I interface{ MDyn() }

type S struct{}

func (S) MDyn() {}

type W struct{ I }

type IG[T any] interface{ MIG() T }

type SIG struct{}

func (SIG) MIG() int { return 0 }

type G[T any] struct{ v T }

func (g G[T]) Val() T { return g.v }

func (g *G[T]) Set(next T) { g.v = next }

func Gen[T any](t T) {}

func ParenGen[T any](t T) {}

type IntSlice = []int

func LocalMono() {}

func UseGeneric[T I](t T) {
	t.MDyn() // @typeparam-recv
	Gen(t)   // @typeparam-passthrough
}

func main() {
	fmt.Println("hello") // @stdlib-mono
	LocalMono()          // @local-mono
	bs := []byte("hi")   // @conv-slice
	_ = bs
	ps := (*S)(nil) // @conv-paren-pointer
	_ = ps
	n := int64(7) // @conv-ident
	_ = n
	func() {}()        // @funclit
	(ParenGen[int])(1) // @paren-generic
	g := G[int]{}
	_ = g.Val() // @generic-value-recv
	gp := &G[int]{}
	gp.Set(2) // @generic-pointer-recv
	var i I = S{}
	i.MDyn()  // @iface-devirt
	I.MDyn(i) // @method-expr
	w := W{I: S{}}
	w.MDyn() // @embedded-iface
	var ig IG[int] = SIG{}
	_ = ig.MIG()               // @generic-iface-recv
	RunDynamic(i)              // @mono-arg-pass
	UseGeneric(S{})            // @generic-struct-arg
	Gen[IntSlice](IntSlice{1}) // @alias-arg
	Gen(&g)                    // @pointer-arg
	var rd I
	Gen[I](rd)           // @interface-arg
	lib.SameModuleFunc() // @same-module
	extdep.ExtFunc()     // @third-party
	fns := []func(){func() {}}
	fns[0]()               // @func-value
	m := make(map[int]int) // @builtin
	_ = m
	var e error
	if e != nil {
		_ = e.Error() // @universe-method
	}
}
