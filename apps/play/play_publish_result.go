package play

// publish_result (ADR-0288 (proposed) §SD4): the window's main result
// leaves play as an ad-hoc bundle — the result as one dataset, and an
// applet document whose SQL reads it in the shape a pane draws — so
// another window or app takes it in as the bytes play held, not as text a
// model copied. Publishing is consequential (ADR-0269 §SD5): the person
// confirms each one.
//
// Only a whole result is published. A result the row cap cut short is a
// prefix that looks complete, and a dataset made of it would be wrong with
// nothing to say so; the agent is told to aggregate or raise the cap. A
// node's lane says nothing about whether it was cut, so only the main
// result is offered: a node is published by running it as the main result.

import (
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opPublishResult = "publish_result"

// defaultResultLocalName is the name a published result is read by when
// the caller names none.
const defaultResultLocalName = "result"

// PublishResultArgs is publish_result's argument.
type PublishResultArgs struct {
	Bundle    string   `desc:"the bundle alias to publish under; publishing again under it from this window republishes it"`
	LocalName string   `json:",omitzero" desc:"the name the bundle's document reads the result by, keelson('<local name>'); result when left out"`
	Sql       string   `json:",omitzero" desc:"the bundle's own SQL over keelson('<local name>'), in the shape the panes it names draw; SELECT * when left out"`
	Tabs      []string `json:",omitzero" desc:"the panes the bundle opens on, as list_panes names them; table when left out"`
	Title     string   `json:",omitzero" desc:"the bundle's title; its alias when left out"`
}

// PublishResultOutcome is publish_result's result.
type PublishResultOutcome struct {
	Bundle    string `desc:"the bundle being published"`
	Dataset   string `desc:"the dataset's global alias, <bundle>__<local name>"`
	Rows      int64  `desc:"the rows published: the whole main result"`
	Columns   int32  `desc:"its columns"`
	Following string `desc:"how to learn the outcome: list_bundles names this window's last publish"`
}

// publishState is the window's last publish, shared between the render
// goroutine and the publishing goroutine.
type publishState struct {
	mu       sync.Mutex
	busy     bool
	last     LastPublish
	sequence uint64
}

// LastPublish is the outcome of the window's last publish_result.
type LastPublish struct {
	Bundle   string `desc:"the bundle"`
	Revision uint64 `json:",omitzero" desc:"the revision the publish made"`
	Rows     int64  `json:",omitzero" desc:"the rows published"`
	Error    string `json:",omitzero" desc:"why the publish failed"`
	Pending  bool   `json:",omitzero" desc:"true while the publish is in flight"`
	At       string `json:",omitzero" desc:"when it finished, RFC 3339"`
}

func addPublishResultOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(s, app.OperationSpec{Name: opPublishResult, Version: 1,
		Summary: "publish the main result as an ad-hoc bundle — the rows as one dataset and an applet document reading them — for another window or app to take in; the person confirms each publish unless the grant lists publish:<bundle prefix>",
		// It reads the result and changes nothing of the window's: what it
		// writes is outside, which is what makes it consequential.
		Effect: app.OperationEffectConsequential, Reads: []string{opsResResult}, Agents: true,
		Consent: app.OperationConsent{Class: "publish", Arg: "bundle"},
		Gesture: "",
		Follows: []string{"only a whole main result is published: one the row cap cut short is refused, as is one still loading or failed",
			"the publish runs off the frame; list_bundles reports this window's last publish and the bundle once it is live",
			"the bundle lives as long as this window, and publishing again under its alias replaces it",
			"another window opens it with open_bundle; another app reads the dataset whole with adhoc.read"}},
		func(inst *PlayLauncher, call app.OperationCall, in PublishResultArgs) (out PublishResultOutcome, err error) {
			return inst.publishResult(call, in)
		})
}

// publishResult checks the main result and the arguments on the render
// goroutine, then publishes on a goroutine of its own.
func (inst *PlayLauncher) publishResult(call app.OperationCall, in PublishResultArgs) (out PublishResultOutcome, err error) {
	p := inst.inner
	switch {
	case p == nil:
		return out, app.RefuseOperation("the window has not mounted")
	case inst.bus == nil:
		return out, app.RefuseOperation("the window has no bus to publish over")
	case !validDatasetIdentifier(in.Bundle) || strings.Contains(in.Bundle, adhocdata.BundleAliasSeparator):
		return out, app.RefuseOperation("a bundle alias is a bare identifier without a double underscore: letters, digits and _, at most 64 bytes")
	}
	local := in.LocalName
	if local == "" {
		local = defaultResultLocalName
	}
	if !validDatasetIdentifier(local) {
		return out, app.RefuseOperation("a local name is a bare identifier: letters, digits and _")
	}
	rec, _, _, loading, _, _, _, runErr, _ := p.graph.MainSnapshot()
	trunc := p.graph.MainTruncation()
	var refusal string
	switch {
	case loading:
		refusal = "the main result is still loading; publish it once the run finished"
	case runErr != nil:
		refusal = "the last run failed: " + runErr.Error()
	case rec == nil:
		refusal = "the window holds no result; run the buffer first"
	case trunc != "":
		refusal = "the main result is a prefix (" + trunc + "); a dataset made of it would miss rows — aggregate in SQL or raise the row limit with set_run_options, run, and publish then"
	}
	if refusal != "" {
		if rec != nil {
			rec.Release()
		}
		return out, app.RefuseOperation(refusal)
	}
	source := p.graph.MainSQL()
	spec := resultBundleSpec(in, local, source)
	doc, cerr := ComposeBundleDocE(spec)
	if cerr != nil {
		rec.Release()
		return out, app.RefuseOperation(cerr.Error())
	}
	inputs := p.client.inputHandlesOf(source)
	inst.publish.mu.Lock()
	if inst.publish.busy {
		inst.publish.mu.Unlock()
		rec.Release()
		return out, app.RefuseOperation("a publish from this window is in flight; list_bundles reports when it lands")
	}
	inst.publish.busy = true
	inst.publish.sequence++
	inst.publish.last = LastPublish{Bundle: in.Bundle, Pending: true}
	inst.publish.mu.Unlock()

	out = PublishResultOutcome{Bundle: in.Bundle, Dataset: adhocdata.DatasetAlias(in.Bundle, local), Rows: rec.NumRows(),
		Columns: int32(rec.NumCols()), Following: "list_bundles"}
	go inst.publishBundleOff(rec, in.Bundle, local, doc, adhocdata.BundleProvenance{SourceSql: source, InputHandles: inputs}, call.OnBehalfOf)
	return
}

// publishBundleOff encodes and publishes off the render goroutine; it owns
// rec and releases it.
func (inst *PlayLauncher) publishBundleOff(rec arrow.RecordBatch, bundle string, local string, doc []byte, prov adhocdata.BundleProvenance, obo *app.OnBehalfOf) {
	rows := rec.NumRows()
	stream, err := adhocdata.EncodeRecord(rec)
	rec.Release()
	last := LastPublish{Bundle: bundle, Rows: rows}
	if err == nil {
		var res adhocdata.BundleResult
		res, err = adhocdata.PublishBundleRequest(inst.bus, adhocdata.BundlePublishInput{Alias: bundle, Document: doc,
			Datasets: []adhocdata.BundleDatasetInput{{LocalName: local, ArrowIPCStream: stream}}, OnBehalfOf: obo, Provenance: prov})
		last.Revision = res.Revision
	}
	if err != nil {
		last.Error = err.Error()
	}
	last.At = time.Now().UTC().Format(time.RFC3339)
	inst.publish.mu.Lock()
	inst.publish.busy = false
	inst.publish.last = last
	inst.publish.mu.Unlock()
}

// lastPublish is the window's last publish, for list_bundles.
func (inst *PlayLauncher) lastPublish() (last LastPublish) {
	inst.publish.mu.Lock()
	last = inst.publish.last
	inst.publish.mu.Unlock()
	return
}

// resultBundleSpec is the bundle of a published result: the caller's SQL
// over the dataset on the panes it names, and, for the person who opens it,
// the query that produced the rows; the same query, and the datasets it
// read, travel as the bundle's provenance.
func resultBundleSpec(in PublishResultArgs, local string, source string) (spec BundleSpec) {
	spec = BundleSpec{Alias: in.Bundle, Title: in.Title, Summary: "a result published from play", Sql: in.Sql, Tabs: in.Tabs,
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: local}}}
	if src := strings.TrimSpace(source); src != "" && !strings.Contains(src, "```") {
		spec.Prose = "The rows of `" + local + "` are the result of this query, as play ran it:\n\n    " +
			strings.ReplaceAll(src, "\n", "\n    ")
	}
	return
}
