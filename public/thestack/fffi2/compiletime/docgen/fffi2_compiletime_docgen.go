package docgen

import (
	"fmt"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

type GeneratorStateE uint8

const (
	GenerateStateInitial GeneratorStateE = 1 << 0
	GenerateStateChecked GeneratorStateE = 1 << 1
)

func nodeTypeName(node ir.NodeI) string {
	switch node.(type) {
	case *ir.BuilderFactoryNode:
		return "BuilderFactory"
	case *ir.ProceduralNode:
		return "Procedural"
	case *ir.FetcherNode:
		return "Fetcher"
	}
	return "Unknown"
}

func formatFeatures(factory *ir.BuilderFactoryFeaturesSpec) string {
	var parts []string
	if factory.Immediate {
		parts = append(parts, "Immediate")
	}
	if factory.Retained {
		parts = append(parts, "Retained")
	}
	if factory.BlockIterator {
		parts = append(parts, "BlockIterator")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func formatProcFeatures(spec *ir.ProcedureFeaturesSpec) string {
	if spec.BlockIterator {
		return "BlockIterator"
	}
	return "-"
}

func formatIdentity(hasId bool) string {
	if hasId {
		return "Yes"
	}
	return "No"
}

func formatReturnType(t ir.TypeI) string {
	if t == nil {
		return "-"
	}
	return t.GetName().Convert(naming.UpperCamelCase).String()
}

func writeArgumentTable(w io.Writer, plain ir.PlainArgumentSpec, evaluated ir.EvaluatedArgumentSpec, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	hasPlain := !plain.IsEmpty()
	hasEval := !evaluated.IsEmpty()
	if !hasPlain && !hasEval {
		return
	}

	_, err := fmt.Fprint(w, "\n#### Constructor Arguments\n\n")
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "| Name | Kind | Type |\n|------|------|------|\n")
	tracker.MergeError(err)

	for name, typ := range plain.Iterate() {
		_, err = fmt.Fprintf(w, "| %s | plain | %s |\n",
			name.Convert(naming.LowerCamelCase),
			typ.String(),
		)
		tracker.MergeError(err)
	}

	for name, typ := range evaluated.Iterate() {
		qualifier := "concrete"
		if typ.IsAbstract() {
			qualifier = "abstract"
		}
		_, err = fmt.Fprintf(w, "| %s | evaluated | %s (%s) |\n",
			name.Convert(naming.LowerCamelCase),
			typ.GetName().Convert(naming.UpperCamelCase),
			qualifier,
		)
		tracker.MergeError(err)
	}
}

func generateBuilderFactoryDoc(w io.Writer, factory *ir.BuilderFactoryNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	var err error
	name := factory.Name.Convert(naming.UpperCamelCase)

	_, err = fmt.Fprintf(w, "\n### %s\n\n", name)
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "- **Type:** BuilderFactory\n")
	tracker.MergeError(err)
	_, err = fmt.Fprintf(w, "- **Identity:** %s\n", formatIdentity(factory.IdentityArguments.HasId))
	tracker.MergeError(err)
	_, err = fmt.Fprintf(w, "- **Features:** %s\n", formatFeatures(&factory.Settings))
	tracker.MergeError(err)

	writeArgumentTable(w, factory.Arguments.PlainArguments, factory.Arguments.EvaluatedArguments, tracker)

	if len(factory.BuilderMethods) > 0 {
		_, err = fmt.Fprint(w, "\n#### Builder Methods\n\n")
		tracker.MergeError(err)
		for _, mth := range factory.BuilderMethods {
			mName := mth.Spec.Name.Convert(naming.UpperCamelCase)
			var argParts []string
			for name, typ := range mth.Spec.PlainArguments.Iterate() {
				argParts = append(argParts, fmt.Sprintf("%s: %s",
					name.Convert(naming.LowerCamelCase), typ.String()))
			}
			_, err = fmt.Fprintf(w, "- **%s**(%s)\n", mName, strings.Join(argParts, ", "))
			tracker.MergeError(err)
		}
	}

	if len(factory.DeferredBlockMaps) > 0 {
		_, err = fmt.Fprint(w, "\n#### Deferred Block Maps\n\n")
		tracker.MergeError(err)
		for _, dbm := range factory.DeferredBlockMaps {
			dbmName := naming.MustBeValidStylableName(dbm.Name).Convert(naming.UpperCamelCase)
			var keyParts []string
			for _, kt := range dbm.KeyTypes {
				keyParts = append(keyParts, kt.String())
			}
			_, err = fmt.Fprintf(w, "- **%s** — keys: (%s)\n", dbmName, strings.Join(keyParts, ", "))
			tracker.MergeError(err)
		}
	}

	rt := formatReturnType(factory.ReturnType)
	if rt != "-" {
		_, err = fmt.Fprintf(w, "\n#### Return Type\n\n%s\n", rt)
		tracker.MergeError(err)
	}

	_, err = fmt.Fprint(w, "\n---\n")
	tracker.MergeError(err)
}

func generateProceduralDoc(w io.Writer, proc *ir.ProceduralNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	var err error
	name := proc.Name.Convert(naming.UpperCamelCase)

	_, err = fmt.Fprintf(w, "\n### %s\n\n", name)
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "- **Type:** Procedural\n")
	tracker.MergeError(err)
	_, err = fmt.Fprintf(w, "- **Identity:** %s\n", formatIdentity(proc.IdentityArguments.HasId))
	tracker.MergeError(err)
	features := formatProcFeatures(&proc.Settings)
	if features != "-" {
		_, err = fmt.Fprintf(w, "- **Features:** %s\n", features)
		tracker.MergeError(err)
	}

	writeArgumentTable(w, proc.Arguments.PlainArguments, proc.Arguments.EvaluatedArguments, tracker)

	rt := formatReturnType(proc.ReturnType)
	if rt != "-" {
		_, err = fmt.Fprintf(w, "\n#### Return Type\n\n%s\n", rt)
		tracker.MergeError(err)
	}

	_, err = fmt.Fprint(w, "\n---\n")
	tracker.MergeError(err)
}

func generateFetcherDoc(w io.Writer, fetcher *ir.FetcherNode, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	var err error
	name := fetcher.Name.Convert(naming.UpperCamelCase)

	_, err = fmt.Fprintf(w, "\n### %s\n\n", name)
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "- **Type:** Fetcher\n")
	tracker.MergeError(err)

	if !fetcher.ReturnTypes.IsEmpty() {
		_, err = fmt.Fprint(w, "\n#### Return Values\n\n")
		tracker.MergeError(err)
		_, err = fmt.Fprint(w, "| Name | Type |\n|------|------|\n")
		tracker.MergeError(err)
		for name, typ := range fetcher.ReturnTypes.Iterate() {
			_, err = fmt.Fprintf(w, "| %s | %s |\n",
				name.Convert(naming.LowerCamelCase),
				typ.String(),
			)
			tracker.MergeError(err)
		}
	}

	_, err = fmt.Fprint(w, "\n---\n")
	tracker.MergeError(err)
}

func generateSummaryTable(w io.Writer, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) {
	_, err := fmt.Fprint(w, "## Summary\n\n")
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "| Name | Type | Identity | Plain Args | Eval Args | Methods | Features |\n")
	tracker.MergeError(err)
	_, err = fmt.Fprint(w, "|------|------|----------|------------|-----------|---------|----------|\n")
	tracker.MergeError(err)

	for _, tl := range tls {
		name := tl.GetName().Convert(naming.UpperCamelCase)
		typeName := nodeTypeName(tl)

		var identity string
		var plainCount, evalCount int
		var methodCount string
		var features string

		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			identity = formatIdentity(tlt.IdentityArguments.HasId)
			plainCount = tlt.Arguments.PlainArguments.Len()
			evalCount = tlt.Arguments.EvaluatedArguments.Len()
			methodCount = fmt.Sprintf("%d", len(tlt.BuilderMethods))
			features = formatFeatures(&tlt.Settings)
		case *ir.ProceduralNode:
			identity = formatIdentity(tlt.IdentityArguments.HasId)
			plainCount = tlt.Arguments.PlainArguments.Len()
			evalCount = tlt.Arguments.EvaluatedArguments.Len()
			methodCount = "-"
			features = formatProcFeatures(&tlt.Settings)
		case *ir.FetcherNode:
			identity = "No"
			plainCount = 0
			evalCount = 0
			methodCount = "-"
			features = "-"
		}

		_, err = fmt.Fprintf(w, "| %s | %s | %s | %d | %d | %s | %s |\n",
			name, typeName, identity, plainCount, evalCount, methodCount, features)
		tracker.MergeError(err)
	}
	_, err = fmt.Fprint(w, "\n")
	tracker.MergeError(err)
}

func GenerateDoc(w io.Writer, tls []ir.NodeI, tracker *compiletime.StateAndErrTracker[GeneratorStateE]) error {
	// Doc front-matter + status banner mirror the sibling skill-asset
	// references (doc/skills/fffi2/assets/fffi2.md, doc/skills/imzero2/
	// assets/bindings.md). Required by doclint: DL001 enforces the YAML
	// stanza on every doc/ file and DL004 enforces the matching
	// "Status: draft — pre-human-review." banner. type=reference matches
	// the Diátaxis quadrant for an API catalogue; status stays at draft
	// because the catalogue is machine-generated and regenerated on
	// every IDL change.
	_, err := fmt.Fprint(w, "---\n"+
		"type: reference\n"+
		"audience: agent reading this skill asset\n"+
		"status: draft\n"+
		"# reviewed-by: \"@<handle>\"     # fill in and uncomment when flipping to stable\n"+
		"# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable\n"+
		"---\n\n")
	tracker.MergeError(err)

	_, err = fmt.Fprint(w, "> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.\n"+
		"> Machine-generated FFFI2 widget-catalogue overview; regenerated from the IDL on every `./generate.sh` run.\n\n")
	tracker.MergeError(err)

	_, err = fmt.Fprint(w, "# fffi2 API Reference\n\n")
	tracker.MergeError(err)

	generateSummaryTable(w, tls, tracker)

	err = tracker.Check(GenerateStateChecked, GenerateStateInitial)
	if err != nil {
		return err
	}

	// Group nodes by type
	var factories []*ir.BuilderFactoryNode
	var procedures []*ir.ProceduralNode
	var fetchers []*ir.FetcherNode

	for _, tl := range tls {
		switch tlt := tl.(type) {
		case *ir.BuilderFactoryNode:
			factories = append(factories, tlt)
		case *ir.ProceduralNode:
			procedures = append(procedures, tlt)
		case *ir.FetcherNode:
			fetchers = append(fetchers, tlt)
		}
	}

	if len(factories) > 0 {
		tracker.ResetStateAndError()
		_, err = fmt.Fprint(w, "\n## BuilderFactory Nodes\n")
		tracker.MergeError(err)
		for _, f := range factories {
			generateBuilderFactoryDoc(w, f, tracker)
		}
	}

	if len(procedures) > 0 {
		tracker.ResetStateAndError()
		_, err = fmt.Fprint(w, "\n## Procedural Nodes\n")
		tracker.MergeError(err)
		for _, p := range procedures {
			generateProceduralDoc(w, p, tracker)
		}
	}

	if len(fetchers) > 0 {
		tracker.ResetStateAndError()
		_, err = fmt.Fprint(w, "\n## Fetcher Nodes\n")
		tracker.MergeError(err)
		for _, f := range fetchers {
			generateFetcherDoc(w, f, tracker)
		}
	}

	return nil
}
