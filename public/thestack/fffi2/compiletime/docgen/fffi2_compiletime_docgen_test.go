package docgen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func n(s string) naming.StylableName {
	return naming.MustBeValidStylableName(s)
}

func newTracker() *compiletime.StateAndErrTracker[GeneratorStateE] {
	return compiletime.NewStateAndErrTracker[GeneratorStateE](GenerateStateInitial, "")
}

func TestGenerateDocProcedure(t *testing.T) {
	proc := idl.NewProceduralNode(n("setColor")).
		WithIdentityId(true).
		WithSettingBlockIterator(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("re"), ctabb.U8).
			PlainArg(n("gr"), ctabb.U8).
			PlainArg(n("bl"), ctabb.U8).
			PlainArg(n("al"), ctabb.U8).
			Build()).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{proc}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	checks := []string{
		"---\ntype: reference\n",
		"status: draft\n",
		"> **Status: draft — pre-human-review.**",
		"# fffi2 API Reference",
		"## Summary",
		"| SetColor | Procedural | Yes | 4 | 0 | - | BlockIterator |",
		"## Procedural Nodes",
		"### SetColor",
		"**Type:** Procedural",
		"**Identity:** Yes",
		"**Features:** BlockIterator",
		"#### Constructor Arguments",
		"| re | plain | u8 |",
		"| gr | plain | u8 |",
		"| bl | plain | u8 |",
		"| al | plain | u8 |",
	}
	for _, want := range checks {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in:\n%s", want, doc)
		}
	}

	// Should not have factory or fetcher sections
	if strings.Contains(doc, "## BuilderFactory Nodes") {
		t.Error("unexpected BuilderFactory section")
	}
	if strings.Contains(doc, "## Fetcher Nodes") {
		t.Error("unexpected Fetcher section")
	}
}

func TestGenerateDocFactory(t *testing.T) {
	mb := idl.NewMethodBuilder()
	mb.BeginMethod(n("setWidth")).Arg(n("width"), ctabb.F32).EndMethod()
	mb.BeginMethod(n("setHeight")).Arg(n("height"), ctabb.F32).EndMethod()
	mb.BeginMethod(n("setLabel")).Arg(n("text"), ctabb.S).EndMethod()

	returnType := ir.NewConcreteType(n("myWidgetResponse"))

	factory := idl.NewBuilderFactoryNode(n("myWidget")).
		WithIdentityId(true).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithReturnType(returnType).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("label"), ctabb.S).
			PlainArg(n("width"), ctabb.F32).
			Build()).
		AddMethods(mb.Build()...).
		WithDeferredBlockMap("cells", ctabb.U64, ctabb.U32).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{factory}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	checks := []string{
		"| MyWidget | BuilderFactory | Yes | 2 | 0 | 3 | Immediate, Retained |",
		"## BuilderFactory Nodes",
		"### MyWidget",
		"**Type:** BuilderFactory",
		"**Identity:** Yes",
		"**Features:** Immediate, Retained",
		"| label | plain | s |",
		"| width | plain | f32 |",
		"#### Builder Methods",
		"**SetWidth**(width: f32)",
		"**SetHeight**(height: f32)",
		"**SetLabel**(text: s)",
		"#### Deferred Block Maps",
		"**Cells** — keys: (u64, u32)",
		"#### Return Type",
		"MyWidgetResponse",
	}
	for _, want := range checks {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in:\n%s", want, doc)
		}
	}
}

func TestGenerateDocFetcher(t *testing.T) {
	fetcher := idl.NewFetcherNode(n("fetchCounters")).
		AddReturnValue(n("count"), ctabb.U64).
		AddReturnValue(n("avg"), ctabb.F64).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{fetcher}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	checks := []string{
		"| FetchCounters | Fetcher | No | 0 | 0 | - | - |",
		"## Fetcher Nodes",
		"### FetchCounters",
		"**Type:** Fetcher",
		"#### Return Values",
		"| count | u64 |",
		"| avg | f64 |",
	}
	for _, want := range checks {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in:\n%s", want, doc)
		}
	}
}

func TestGenerateDocSummaryTable(t *testing.T) {
	proc := idl.NewProceduralNode(n("setColor")).
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("re"), ctabb.U8).
			Build()).
		Build()

	mb := idl.NewMethodBuilder()
	mb.BeginMethod(n("resize")).Arg(n("wi"), ctabb.F32).Arg(n("he"), ctabb.F32).EndMethod()
	factory := idl.NewBuilderFactoryNode(n("myPanel")).
		WithSettingImmediate(true).
		AddMethods(mb.Build()...).
		Build()

	fetcher := idl.NewFetcherNode(n("fetchState")).
		AddReturnValue(n("val"), ctabb.U32).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{proc, factory, fetcher}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	// Verify all three appear in the summary table
	if !strings.Contains(doc, "| SetColor | Procedural |") {
		t.Error("summary missing SetColor")
	}
	if !strings.Contains(doc, "| MyPanel | BuilderFactory |") {
		t.Error("summary missing MyPanel")
	}
	if !strings.Contains(doc, "| FetchState | Fetcher |") {
		t.Error("summary missing FetchState")
	}

	// Verify summary comes before detail sections
	summaryIdx := strings.Index(doc, "## Summary")
	factoryIdx := strings.Index(doc, "## BuilderFactory Nodes")
	procIdx := strings.Index(doc, "## Procedural Nodes")
	fetcherIdx := strings.Index(doc, "## Fetcher Nodes")

	if summaryIdx < 0 || factoryIdx < 0 || procIdx < 0 || fetcherIdx < 0 {
		t.Fatalf("missing sections")
	}
	if !(summaryIdx < factoryIdx && factoryIdx < procIdx && procIdx < fetcherIdx) {
		t.Errorf("section order wrong: Summary@%d, Factory@%d, Proc@%d, Fetcher@%d",
			summaryIdx, factoryIdx, procIdx, fetcherIdx)
	}
}

func TestGenerateDocMixedNodes(t *testing.T) {
	// Multiple nodes of each type
	proc1 := idl.NewProceduralNode(n("doA")).Build()
	proc2 := idl.NewProceduralNode(n("doB")).
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg(n("xc"), ctabb.I32).
			PlainArg(n("yc"), ctabb.I32).
			Build()).
		Build()

	factory1 := idl.NewBuilderFactoryNode(n("widgetX")).
		WithSettingBlockIterator(true).
		Build()

	fetcher1 := idl.NewFetcherNode(n("fetchA")).Build()
	fetcher2 := idl.NewFetcherNode(n("fetchB")).
		AddReturnValue(n("result"), ctabb.S).
		Build()

	nodes := []ir.NodeI{proc1, factory1, proc2, fetcher1, fetcher2}

	var buf bytes.Buffer
	err := GenerateDoc(&buf, nodes, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	// All 5 nodes should appear in summary
	for _, name := range []string{"DoA", "DoB", "WidgetX", "FetchA", "FetchB"} {
		if !strings.Contains(doc, "| "+name+" |") {
			t.Errorf("summary missing %s", name)
		}
	}

	// Grouped sections should exist
	if !strings.Contains(doc, "## BuilderFactory Nodes") {
		t.Error("missing BuilderFactory section")
	}
	if !strings.Contains(doc, "## Procedural Nodes") {
		t.Error("missing Procedural section")
	}
	if !strings.Contains(doc, "## Fetcher Nodes") {
		t.Error("missing Fetcher section")
	}

	// Both procedures should be in the Procedural section
	procSection := doc[strings.Index(doc, "## Procedural Nodes"):]
	if !strings.Contains(procSection, "### DoA") {
		t.Error("missing DoA in Procedural section")
	}
	if !strings.Contains(procSection, "### DoB") {
		t.Error("missing DoB in Procedural section")
	}

	// DoB should have argument table
	if !strings.Contains(doc, "| xc | plain | i32 |") {
		t.Error("missing arg xc for DoB")
	}

	// FetchB should have return value table
	if !strings.Contains(doc, "| result | s |") {
		t.Error("missing return value for FetchB")
	}
}

func TestGenerateDocEvaluatedArgs(t *testing.T) {
	absType := ir.NewAbstractType(n("atoms"))

	factory := idl.NewBuilderFactoryNode(n("labelAtoms")).
		AddArguments(idl.NewArgumentsBuilder().
			EvaluatedArg(n("content"), absType).
			PlainArg(n("truncate"), ctabb.B).
			Build()).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{factory}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	// Plain and evaluated args should both appear
	if !strings.Contains(doc, "| truncate | plain | b |") {
		t.Errorf("missing plain arg:\n%s", doc)
	}
	if !strings.Contains(doc, "| content | evaluated | Atoms (abstract) |") {
		t.Errorf("missing evaluated arg:\n%s", doc)
	}
	// Summary should show 1 plain, 1 eval
	if !strings.Contains(doc, "| LabelAtoms | BuilderFactory | No | 1 | 1 | 0 |") {
		t.Errorf("summary counts wrong:\n%s", doc)
	}
}

func TestGenerateDocEmptyFactory(t *testing.T) {
	// Factory with no methods, no args, no deferred blocks, no return type
	factory := idl.NewBuilderFactoryNode(n("empty")).
		WithSettingImmediate(true).
		Build()

	var buf bytes.Buffer
	err := GenerateDoc(&buf, []ir.NodeI{factory}, newTracker())
	if err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	doc := buf.String()

	if !strings.Contains(doc, "### Empty") {
		t.Error("missing heading")
	}
	// Should NOT have these sections
	if strings.Contains(doc, "#### Constructor Arguments") {
		t.Error("unexpected Constructor Arguments for empty factory")
	}
	if strings.Contains(doc, "#### Builder Methods") {
		t.Error("unexpected Builder Methods for empty factory")
	}
	if strings.Contains(doc, "#### Deferred Block Maps") {
		t.Error("unexpected Deferred Block Maps for empty factory")
	}
	if strings.Contains(doc, "#### Return Type") {
		t.Error("unexpected Return Type for empty factory")
	}
}
