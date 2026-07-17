package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview"
)

func passesTestRows() []passreg.CatalogRow {
	return []passreg.CatalogRow{
		{Stage: passreg.StagePreExecute, Name: "canonicalize", Order: 100, Description: "canonical form"},
		{Stage: passreg.StagePreExecute, Name: "macro-expand", Order: 200,
			Properties: nanopass.PassProperties{NeedsFixedPoint: true}},
		{Stage: passreg.StagePreExecute, Name: "resolve-handles", Order: 300, LateBound: true},
	}
}

func passesLayoutNode(t *testing.T, lay *pipelineview.Layout, id string) pipelineview.NodeLayout {
	t.Helper()
	for _, n := range lay.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not in layout", id)
	return pipelineview.NodeLayout{}
}

func TestPassesPipelineShape(t *testing.T) {
	p := passesPipeline(passesTestRows(), "http://localhost:8123/")
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	lay, err := pipelineview.Compute(p, pipelineview.LayoutOpts{FontSize: 13})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// Passes sit on one spine line in catalog order.
	canon := passesLayoutNode(t, lay, "pass/canonicalize")
	expand := passesLayoutNode(t, lay, "pass/macro-expand")
	resolve := passesLayoutNode(t, lay, "pass/resolve-handles")
	if !(canon.Center.X < expand.Center.X && expand.Center.X < resolve.Center.X) {
		t.Errorf("passes not in spine order: %v %v %v", canon.Center.X, expand.Center.X, resolve.Center.X)
	}
	if canon.Center.Y != expand.Center.Y || expand.Center.Y != resolve.Center.Y {
		t.Errorf("passes not on one line: %v %v %v", canon.Center.Y, expand.Center.Y, resolve.Center.Y)
	}

	// The editor source sits west of the first pass, the executor east of the
	// last, both on the spine line.
	editor := passesLayoutNode(t, lay, passesSrcEndpointID)
	sink := passesLayoutNode(t, lay, passesSinkEndpointID)
	if editor.Center.X+editor.W/2 > canon.Center.X-canon.W/2 {
		t.Errorf("editor endpoint not west of the first pass")
	}
	if sink.Center.X-sink.W/2 < resolve.Center.X+resolve.W/2 {
		t.Errorf("executor endpoint not east of the last pass")
	}
	if sink.Sublabel != "http://localhost:8123/" {
		t.Errorf("sink sublabel = %q, want the executor URL", sink.Sublabel)
	}
	if editor.H >= sink.H {
		t.Errorf("sublabelled store sink should be taller than the plain editor endpoint: %v vs %v", sink.H, editor.H)
	}

	// The fixed-point pass carries a dashed self-feedback loop.
	found := false
	for _, e := range lay.Edges {
		if e.From.Key() == "pass/macro-expand" && e.To.Key() == "pass/macro-expand" {
			found = true
			if e.Kind != pipelineview.EdgeFeedback || !e.Dashed {
				t.Errorf("fixpoint self-loop should be a dashed feedback edge, got kind=%d dashed=%v", e.Kind, e.Dashed)
			}
			if e.Label != "fixed point" {
				t.Errorf("fixpoint self-loop label = %q", e.Label)
			}
		}
	}
	if !found {
		t.Error("no self-loop for the NeedsFixedPoint pass")
	}
}

func TestPassesCatalogKey(t *testing.T) {
	rows := passesTestRows()
	const url = "http://localhost:8123/"
	base := passesCatalogKey(rows, url)
	if passesCatalogKey(passesTestRows(), url) != base {
		t.Error("key not stable over identical rows")
	}
	reordered := passesTestRows()
	reordered[0].Order = 250
	if passesCatalogKey(reordered, url) == base {
		t.Error("key blind to order change")
	}
	flagged := passesTestRows()
	flagged[0].Properties.NeedsFixedPoint = true
	if passesCatalogKey(flagged, url) == base {
		t.Error("key blind to fixed-point flag")
	}
	if passesCatalogKey(rows, "http://other:8123/") == base {
		t.Error("key blind to the executor URL")
	}
}

func TestPassPropsText(t *testing.T) {
	if got := passPropsText(nanopass.PassProperties{}); got != "no declared properties" {
		t.Errorf("empty properties = %q", got)
	}
	got := passPropsText(nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody | nanopass.RegionParams,
		Writes:     nanopass.RegionBody,
		Produces:   []nanopass.FormTag{"canonical"},
	})
	for _, want := range []string{"idempotent", "reads=body,params", "writes=body", "produces=canonical"} {
		if !strings.Contains(got, want) {
			t.Errorf("properties text %q missing %q", got, want)
		}
	}
}
