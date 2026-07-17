package topo_test

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

func fixtureInput() topo.Input {
	return topo.Input{
		Manifests: []app.Manifest{{
			Id: "imztop",
			Caps: []app.SubjectFilter{
				{Pattern: "sysmetrics.>", Direction: app.CapDirectionSub},
				{Pattern: "clipboard.write", Direction: app.CapDirectionBoth},
			},
		}},
		SelfComponent: topo.ImZero2Demo.Token,
		SelfHost:      "box-a",
		Observations: []topo.HostObservation{{
			Host: "box-a",
			Snap: &sysmsnap.BundleSnapshot{
				Procs: []sysmsnap.ProcInfo{
					{PID: 4711, PPID: 1, Component: topo.ImZero2Demo.Token, CgroupUnit: "imzero2-demo.service"},
					{PID: 4712, PPID: 4711, Component: topo.ImZero2Demo.Token}, // same mark: node dedups
					{PID: 9000, PPID: 1},
				},
				Sockets: &sysmsnap.SocketsSnapshot{Sockets: []sysmsnap.SocketInfo{
					{Proto: sysmsnap.SocketProtoTCP, Addr: "127.0.0.1", Port: 8089, PID: 4711},
					{Proto: sysmsnap.SocketProtoUnix, Addr: "/run/orphan.sock"}, // unattributed: node yes, edge no
				}},
			},
		}},
	}
}

func hasNode(nodes []topo.Node, n topo.Node) bool { return slices.Contains(nodes, n) }
func hasEdge(edges []topo.Edge, e topo.Edge) bool { return slices.Contains(edges, e) }

func TestAssembleNodes(t *testing.T) {
	nodes := topo.AssembleNodes(fixtureInput())

	// Every registry component appears declared (host-agnostic).
	for _, c := range topo.Registry() {
		if !hasNode(nodes, topo.Node{Kind: topo.NodeKindComponent, Key: topo.ComponentKey(c.Token), Origin: topo.OriginDeclared, Source: topo.SourceRegistry}) {
			t.Errorf("missing declared component node for %q", c.Token)
		}
	}
	// The marked component appears observed exactly once (deduped across
	// its two processes) — the drift GROUP BY sees both origins.
	observedComponents := 0
	for _, n := range nodes {
		if n.Kind == topo.NodeKindComponent && n.Origin == topo.OriginObserved {
			observedComponents++
			if n.Key != topo.ComponentKey(topo.ImZero2Demo.Token) || n.Host != "box-a" || n.Source != topo.SourceMark {
				t.Errorf("unexpected observed component node %+v", n)
			}
		}
	}
	if observedComponents != 1 {
		t.Errorf("observed component nodes = %d, want 1", observedComponents)
	}

	for _, want := range []topo.Node{
		{Kind: topo.NodeKindHost, Key: "host:box-a", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
		{Kind: topo.NodeKindProc, Key: "proc:4711", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
		{Kind: topo.NodeKindSock, Key: "sock:tcp/127.0.0.1:8089", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
		{Kind: topo.NodeKindSock, Key: "sock:unix//run/orphan.sock", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
		{Kind: topo.NodeKindApp, Key: "app:imztop", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
		{Kind: topo.NodeKindSubject, Key: "subject:sysmetrics.>", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
	} {
		if !hasNode(nodes, want) {
			t.Errorf("missing node %+v", want)
		}
	}

	// Deterministic: same input, same order.
	again := topo.AssembleNodes(fixtureInput())
	if !slices.Equal(nodes, again) {
		t.Error("AssembleNodes is not deterministic")
	}
}

func TestAssembleEdges(t *testing.T) {
	edges := topo.AssembleEdges(fixtureInput())

	for _, want := range []topo.Edge{
		// Declared registry dependency (caddy needs the carrier).
		{Kind: topo.EdgeKindComponentNeeds, SrcKey: topo.ComponentKey(topo.Caddy.Token), DstKey: topo.ComponentKey(topo.ImZero2Demo.Token), Origin: topo.OriginDeclared, Source: topo.SourceRegistry},
		// Sub-only cap yields exactly app-sub; Both yields both.
		{Kind: topo.EdgeKindAppSub, SrcKey: "app:imztop", DstKey: "subject:sysmetrics.>", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
		{Kind: topo.EdgeKindAppPub, SrcKey: "app:imztop", DstKey: "subject:clipboard.write", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
		{Kind: topo.EdgeKindAppSub, SrcKey: "app:imztop", DstKey: "subject:clipboard.write", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
		// The carrier stamps its apps (observed: depends on the runtime mark).
		{Kind: topo.EdgeKindAppInComponent, SrcKey: "app:imztop", DstKey: topo.ComponentKey(topo.ImZero2Demo.Token), Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceMark},
		// Observed containment, ancestry, listener.
		{Kind: topo.EdgeKindProcInComponent, SrcKey: "proc:4711", DstKey: topo.ComponentKey(topo.ImZero2Demo.Token), Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceMark},
		{Kind: topo.EdgeKindProcChildOf, SrcKey: "proc:4712", DstKey: "proc:4711", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
		{Kind: topo.EdgeKindProcListens, SrcKey: "proc:4711", DstKey: "sock:tcp/127.0.0.1:8089", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
	} {
		if !hasEdge(edges, want) {
			t.Errorf("missing edge %+v", want)
		}
	}

	// Sub-only must not fabricate a pub edge; an unattributed socket
	// must not fabricate a listens edge.
	for _, absent := range []topo.Edge{
		{Kind: topo.EdgeKindAppPub, SrcKey: "app:imztop", DstKey: "subject:sysmetrics.>", Origin: topo.OriginDeclared, Source: topo.SourceManifest},
		{Kind: topo.EdgeKindProcListens, SrcKey: "proc:0", DstKey: "sock:unix//run/orphan.sock", Host: "box-a", Origin: topo.OriginObserved, Source: topo.SourceProc},
	} {
		if hasEdge(edges, absent) {
			t.Errorf("unexpected edge %+v", absent)
		}
	}

	again := topo.AssembleEdges(fixtureInput())
	if !slices.Equal(edges, again) {
		t.Error("AssembleEdges is not deterministic")
	}
}

// TestAssemble_NoSelfComponent proves an unmarked host emits no
// app-in-component edges rather than inventing a component.
func TestAssemble_NoSelfComponent(t *testing.T) {
	in := fixtureInput()
	in.SelfComponent = ""
	for _, e := range topo.AssembleEdges(in) {
		if e.Kind == topo.EdgeKindAppInComponent {
			t.Fatalf("unexpected app-in-component edge %+v from unmarked host", e)
		}
	}
}
