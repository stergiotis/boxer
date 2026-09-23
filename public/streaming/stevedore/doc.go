// Package stevedore is the routing, failure and state handling around an
// ingestion processor that a streaming framework drives as an external
// process (ADR-0252). It decodes nothing and shreds nothing: an application
// supplies a [HandlerI] that turns one request into items and a [SinkI] that
// lands items as rows, and links this package into a binary of its own.
//
// This package holds the conventions both hosts share — the request and its
// header, the item, the failure classes and their status text, the file
// reference derived from a request's origin, and the in-place retry. The
// processor host is [github.com/stergiotis/boxer/public/streaming/stevedore/host],
// the lander [github.com/stergiotis/boxer/public/streaming/stevedore/lander],
// the framing [github.com/stergiotis/boxer/public/streaming/stevedore/wire].
package stevedore
