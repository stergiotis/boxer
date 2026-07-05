//go:build fixtag

package main

func TaggedFunc() {}

func callTagged() {
	TaggedFunc() // @tagged-call
}
