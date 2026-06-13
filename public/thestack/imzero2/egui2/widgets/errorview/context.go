// Package errorview renders a structured wrapped-error chain as a
// collapsing tree: per-stream sub-headers, per-fact rows showing
// message (red), stack frame triple (monospace muted), and CBOR
// diagnostic of any attached structured data (in a dark canvas
// Frame). Lifted out of the logviewer detail pane so any consumer
// of an `eh.MarshalError`-shaped chain — observability dashboards,
// crash reports, debug overlays — can render it with the same look
// without re-implementing the per-fact dispatch and wrap discipline.
//
// Usage:
//
//	r := errorview.New(ids, "card-err").DefaultOpen(false)
//	r.Render(ctx)
//
// Renderer is a value type; fluent setters return modified copies
// so a base config is safe to share. All widget IDs are derived
// from the caller-supplied WidgetIdStack under the per-Renderer
// idPrefix, so two renderers on the same stack can't collide as
// long as their prefixes differ.
//
// Wire-shape adapters live with each consumer (e.g.
// logviewer.toErrorviewContext bridges factsstore.LogErrorContext
// → errorview.Context); errorview itself stays decoupled from any
// particular decoder so it composes with other transports.
package errorview

// Fact is one node of an error chain. Mirrors the wire shape that
// boxer's eh.MarshalError emits per fact:
//   - Msg: the error's .Error() text (omitted on per-frame stub
//     facts that only carry a stack frame).
//   - Source / Line / Function: the stack frame triple — present
//     for stack-bearing facts, empty for message-only facts.
//   - Data / DataDiag: structured-data attached via eb.Build —
//     Data is the raw CBOR, DataDiag the cbor.Diagnose output.
//   - Id / ParentId: linkage forming the error tree; renderers
//     today display facts in chain order (linear) but the fields
//     are preserved for callers that want to walk the tree shape.
type Fact struct {
	Msg      string
	Source   string
	Line     string
	Function string
	Data     []byte
	DataDiag string
	Id       uint64
	ParentId uint64
}

// Stream is one bucket of facts grouped by stack identity. Name
// is "no-stack" (errors without stack info) or "stack-N" (the Nth
// deduplicated stack trace shared by one or more wrap levels).
// Facts are in chain order — the outermost wrap message first,
// then any per-frame stubs interleaved per the producer's
// materialize pass.
type Stream struct {
	Name  string
	Facts []Fact
}

// Context is the typed root of an error chain decode. Empty (zero
// Streams) is a valid no-op input — the Renderer short-circuits
// instead of producing an "error chain — 0 streams" header.
type Context struct {
	Streams []Stream
}

// IsEmpty reports whether the context carries no facts. Used by
// callers who want to gate UI affordances (e.g. an "Inspect error
// chain" button) on the presence of structured error info.
func (inst Context) IsEmpty() (ok bool) {
	if len(inst.Streams) == 0 {
		ok = true
		return
	}
	for _, s := range inst.Streams {
		if len(s.Facts) > 0 {
			return
		}
	}
	ok = true
	return
}

// FormatFrame composes the Fact's frame triple into a "func @
// source:line" string. Exported so callers that want to render
// frames their own way (without going through the Renderer) reuse
// the same composition rules. eh's CompactStackTrace already
// trimmed the longest common path prefix on the producer side, so
// Source paths arrive at a tractable length.
func FormatFrame(f Fact) (s string) {
	switch {
	case f.Function == "" && f.Line == "":
		s = f.Source
	case f.Function == "":
		s = f.Source + ":" + f.Line
	case f.Line == "":
		s = f.Function + " @ " + f.Source
	default:
		s = f.Function + " @ " + f.Source + ":" + f.Line
	}
	return
}
