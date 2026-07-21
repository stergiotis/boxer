package godep

import "context"

// SourceI is the collection<->visualization seam defined by ADR-0064. The
// godepview app depends only on this interface and on the manifest DTOs —
// never on a concrete collector. The LiveCollector (package godepcollect)
// is the only adapter today; a FactsSource reading boxer.facts via the
// marshallgen-generated Unmarshal is the deferred second adapter (ADR-0064
// SD3/SD7). Load is expected to be a one-shot, potentially expensive call;
// callers run it off the render path.
type SourceI interface {
	Load(ctx context.Context) (m Manifest, err error)
}
