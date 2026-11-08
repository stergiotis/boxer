package play

// The checked constructor of an ad-hoc bundle moved to the playground leaf
// (ADR-0361 §SD5), since a bundle is an input of the role and a producer can
// publish one without linking play. These names stay for play's own use and
// its importers.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
)

// BundleSpec is a bundle as its producer states it.
type BundleSpec = playground.BundleSpec

// ComposeBundleDoc writes spec's applet document and checks it with the
// installed parser; see playground.ComposeBundleDoc.
func ComposeBundleDoc(spec BundleSpec) (doc []byte, err error) {
	return playground.ComposeBundleDoc(spec)
}

// PublishBundle composes, checks and publishes spec; see
// playground.PublishBundle. It is a bus round trip: call it off the render
// goroutine.
func PublishBundle(bus app.BusI, spec BundleSpec) (res adhocdata.BundleResult, err error) {
	return playground.PublishBundle(bus, spec)
}
