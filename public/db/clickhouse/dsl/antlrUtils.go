package dsl

import (
	"github.com/antlr4-go/antlr/v4"
	"iter"
)

func IterateAllByType[T antlr.Tree](tree antlr.Tree) iter.Seq[T] {
	return func(yield func(T) bool) {
		var inner func(tree antlr.Tree)
		var stop bool
		inner = func(tree antlr.Tree) {
			if stop {
				return
			}
			treet, ok := tree.(T)
			if ok {
				if !yield(treet) {
					stop = true
					return
				}
			}
			for i := 0; i < tree.GetChildCount(); i++ {
				inner(tree.GetChild(i))
			}
		}
		inner(tree)
	}
}
func IterateAll(tree antlr.Tree) iter.Seq[antlr.Tree] {
	return func(yield func(node antlr.Tree) bool) {
		var inner func(tree antlr.Tree)
		var stop bool
		inner = func(tree antlr.Tree) {
			if stop {
				return
			}
			if !yield(tree) {
				stop = true
				return
			}
			for i := 0; i < tree.GetChildCount(); i++ {
				inner(tree.GetChild(i))
			}
		}
		inner(tree)
	}
}
