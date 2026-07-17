package topo

import (
	"sort"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Node kinds (ADR-0126 §SD1). A node's key is "kind:name", stable
// across the declared/observed divide so drift is a GROUP BY over
// origins, not a cross-store join. "unit" is reserved for the deferred
// supervisor collector (§SD6) and never emitted here.
const (
	NodeKindHost      = "host"
	NodeKindComponent = "component"
	NodeKindProc      = "proc"
	NodeKindSock      = "sock"
	NodeKindApp       = "app"
	NodeKindSubject   = "subject"
)

// Edge kinds (ADR-0126 §SD1), each directed src→dst.
const (
	EdgeKindProcInComponent = "proc-in-component"
	EdgeKindProcChildOf     = "proc-child-of"
	EdgeKindProcListens     = "proc-listens"
	EdgeKindAppInComponent  = "app-in-component"
	EdgeKindAppPub          = "app-pub"
	EdgeKindAppSub          = "app-sub"
	EdgeKindComponentNeeds  = "component-needs"
)

// Origins: declared is intent (a registry entry, a manifest cap);
// observed is running state. The same key appearing under both origins
// is the reconciliation join working as designed.
const (
	OriginDeclared = "declared"
	OriginObserved = "observed"
)

// Sources name the mechanism that reported a row.
const (
	SourceRegistry = "registry" // the compiled-in component inventory
	SourceManifest = "manifest" // app manifests and their subject filters
	SourceMark     = "mark"     // the BOXER_COMPONENT environ mark
	SourceProc     = "proc"     // the /proc-derived plane snapshot
)

// Key formatters. Every producer of a key MUST go through these so the
// two halves land on identical strings.

func HostKey(token string) (key string)      { return NodeKindHost + ":" + token }
func ComponentKey(token string) (key string) { return NodeKindComponent + ":" + token }
func AppKey(id string) (key string)          { return NodeKindApp + ":" + id }
func SubjectKey(pattern string) (key string) { return NodeKindSubject + ":" + pattern }

func ProcKey(pid uint32) (key string) {
	return NodeKindProc + ":" + strconv.FormatUint(uint64(pid), 10)
}

// SockKey formats a listener key: "sock:{proto}/{addr}:{port}" for inet
// sockets, "sock:unix/{path}" for unix ones (no port dimension).
func SockKey(proto sysmsnap.SocketProto, addr string, port uint16) (key string) {
	if proto == sysmsnap.SocketProtoUnix {
		return NodeKindSock + ":" + string(proto) + "/" + addr
	}
	return NodeKindSock + ":" + string(proto) + "/" + addr + ":" + strconv.FormatUint(uint64(port), 10)
}

// Node is one graph-projection row (keelson.topology_nodes). Rows are
// deliberately narrow — detail lives in the typed tables, reachable by
// key (ADR-0126 §SD5). Host is empty on declared rows: intent is
// box-agnostic until the R1 desired-state store exists (§SD6).
type Node struct {
	Kind   string
	Key    string
	Host   string
	Origin string
	Source string
}

// Edge is one directed graph-projection row (keelson.topology_edges).
type Edge struct {
	Kind   string
	SrcKey string
	DstKey string
	Host   string
	Origin string
	Source string
}

// HostObservation pairs a host token with its latest plane bundle. The
// bus-side holder shape maps onto this without topo importing the bus.
type HostObservation struct {
	Host string
	Snap *sysmsnap.BundleSnapshot
}

// Input is everything the graph assembly reads. The component registry
// is compiled into this package and needs no field.
type Input struct {
	// Manifests is the declared app set (app.AllManifests()).
	Manifests []app.Manifest
	// SelfComponent is the running host's own mark (Self()). When
	// non-empty, every manifest app gets an observed app-in-component
	// edge onto it — the carrier stamping its apps (ADR-0126 §SD1).
	SelfComponent string
	// SelfHost is the local host token those self edges carry.
	SelfHost string
	// Observations are the per-host latest bundles.
	Observations []HostObservation
}

// AssembleNodes builds the deduplicated, deterministically-ordered node
// rows from in. Pure: no I/O, no clock.
func AssembleNodes(in Input) (nodes []Node) {
	seen := map[Node]struct{}{}
	add := func(n Node) {
		if _, dup := seen[n]; !dup {
			seen[n] = struct{}{}
			nodes = append(nodes, n)
		}
	}

	// Declared: the component inventory, apps, and their subject filters.
	for _, c := range Registry() {
		add(Node{Kind: NodeKindComponent, Key: ComponentKey(c.Token), Origin: OriginDeclared, Source: SourceRegistry})
	}
	for _, m := range in.Manifests {
		add(Node{Kind: NodeKindApp, Key: AppKey(string(m.Id)), Origin: OriginDeclared, Source: SourceManifest})
		for _, c := range m.Caps {
			add(Node{Kind: NodeKindSubject, Key: SubjectKey(c.Pattern), Origin: OriginDeclared, Source: SourceManifest})
		}
	}

	// Observed: hosts, processes, their marks, listeners.
	for _, obs := range in.Observations {
		add(Node{Kind: NodeKindHost, Key: HostKey(obs.Host), Host: obs.Host, Origin: OriginObserved, Source: SourceProc})
		for i := range obs.Snap.Procs {
			p := &obs.Snap.Procs[i]
			add(Node{Kind: NodeKindProc, Key: ProcKey(p.PID), Host: obs.Host, Origin: OriginObserved, Source: SourceProc})
			if p.Component != "" {
				add(Node{Kind: NodeKindComponent, Key: ComponentKey(p.Component), Host: obs.Host, Origin: OriginObserved, Source: SourceMark})
			}
		}
		if obs.Snap.Sockets != nil {
			for i := range obs.Snap.Sockets.Sockets {
				s := &obs.Snap.Sockets.Sockets[i]
				add(Node{Kind: NodeKindSock, Key: SockKey(s.Proto, s.Addr, s.Port), Host: obs.Host, Origin: OriginObserved, Source: SourceProc})
			}
		}
	}

	sort.Slice(nodes, func(i, j int) bool { return nodeLess(nodes[i], nodes[j]) })
	return
}

func nodeLess(a, b Node) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Key != b.Key {
		return a.Key < b.Key
	}
	if a.Host != b.Host {
		return a.Host < b.Host
	}
	return a.Origin < b.Origin
}

// AssembleEdges builds the deduplicated, deterministically-ordered edge
// rows from in. Pure: no I/O, no clock. Edges may dangle (a capped proc
// table can drop a parent's node row) — the graph is partial by nature
// and a dangling dst is still a true statement about the src.
func AssembleEdges(in Input) (edges []Edge) {
	seen := map[Edge]struct{}{}
	add := func(e Edge) {
		if _, dup := seen[e]; !dup {
			seen[e] = struct{}{}
			edges = append(edges, e)
		}
	}

	// Declared: component dependencies and the app↔subject bus graph.
	for _, c := range Registry() {
		for _, need := range c.Needs {
			add(Edge{Kind: EdgeKindComponentNeeds, SrcKey: ComponentKey(c.Token), DstKey: ComponentKey(need), Origin: OriginDeclared, Source: SourceRegistry})
		}
	}
	for _, m := range in.Manifests {
		appKey := AppKey(string(m.Id))
		for _, c := range m.Caps {
			subjKey := SubjectKey(c.Pattern)
			if c.Direction == app.CapDirectionPub || c.Direction == app.CapDirectionBoth {
				add(Edge{Kind: EdgeKindAppPub, SrcKey: appKey, DstKey: subjKey, Origin: OriginDeclared, Source: SourceManifest})
			}
			if c.Direction == app.CapDirectionSub || c.Direction == app.CapDirectionBoth {
				add(Edge{Kind: EdgeKindAppSub, SrcKey: appKey, DstKey: subjKey, Origin: OriginDeclared, Source: SourceManifest})
			}
		}
		// The carrier stamps its own mark onto its apps: observed,
		// because it depends on the runtime environment, not a manifest.
		if in.SelfComponent != "" {
			add(Edge{Kind: EdgeKindAppInComponent, SrcKey: appKey, DstKey: ComponentKey(in.SelfComponent), Host: in.SelfHost, Origin: OriginObserved, Source: SourceMark})
		}
	}

	// Observed: process containment, ancestry, and listeners.
	for _, obs := range in.Observations {
		for i := range obs.Snap.Procs {
			p := &obs.Snap.Procs[i]
			if p.Component != "" {
				add(Edge{Kind: EdgeKindProcInComponent, SrcKey: ProcKey(p.PID), DstKey: ComponentKey(p.Component), Host: obs.Host, Origin: OriginObserved, Source: SourceMark})
			}
			if p.PPID > 0 {
				add(Edge{Kind: EdgeKindProcChildOf, SrcKey: ProcKey(p.PID), DstKey: ProcKey(p.PPID), Host: obs.Host, Origin: OriginObserved, Source: SourceProc})
			}
		}
		if obs.Snap.Sockets != nil {
			for i := range obs.Snap.Sockets.Sockets {
				s := &obs.Snap.Sockets.Sockets[i]
				if s.PID > 0 {
					add(Edge{Kind: EdgeKindProcListens, SrcKey: ProcKey(s.PID), DstKey: SockKey(s.Proto, s.Addr, s.Port), Host: obs.Host, Origin: OriginObserved, Source: SourceProc})
				}
			}
		}
	}

	sort.Slice(edges, func(i, j int) bool { return edgeLess(edges[i], edges[j]) })
	return
}

func edgeLess(a, b Edge) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.SrcKey != b.SrcKey {
		return a.SrcKey < b.SrcKey
	}
	if a.DstKey != b.DstKey {
		return a.DstKey < b.DstKey
	}
	return a.Host < b.Host
}
