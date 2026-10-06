package play

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
)

// The Experiments pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): which source and sink it drives and with which options, the
// notice it shows (a row cap, a result that is not leeway-shaped), and the
// sink's output where the sink writes text — the box-drawn table, the
// sparks, the card-JSON. A sink that draws widgets is a picture: a capture
// shows it. set_experiments picks the source, the sink and options,
// resolved through the vizeval catalogue as a launch seed is; the
// artifact box of a seed is a capture knob and is not offered.

const (
	opGetExperiments      = "get_experiments"
	opSetExperiments      = "set_experiments"
	experimentsPaneId     = "experiments"
	opsResExperiments     = experimentsPaneId
	experimentsSourceFix  = "fixture"
	experimentsSourceRes  = "result"
	experimentsOutputMaxB = opsSampleMaxBytes
)

// ExperimentsOptionReading is one option's value.
type ExperimentsOptionReading struct {
	Name  string `desc:"the option's name"`
	Value string `desc:"its value as text: an enum choice, a number, true or false"`
}

// ExperimentsOptionSpec is one option a sink takes.
type ExperimentsOptionSpec struct {
	Name        string   `desc:"the option's name"`
	Description string   `json:",omitzero" desc:"what it changes"`
	Kind        string   `desc:"enum, int, float or bool"`
	Choices     []string `json:",omitzero" desc:"an enum's choices"`
	Min         *float64 `json:",omitzero" desc:"a number's least value"`
	Max         *float64 `json:",omitzero" desc:"a number's greatest value"`
	Default     string   `desc:"the default, as text"`
}

// ExperimentsSinkSpec is one sink of the catalogue.
type ExperimentsSinkSpec struct {
	Id      string                  `desc:"the sink's id, as set_experiments takes it"`
	Title   string                  `desc:"its title"`
	RowCap  int64                   `desc:"rows it draws at most"`
	Text    bool                    `json:",omitzero" desc:"it writes text get_experiments returns; otherwise it draws a picture"`
	Options []ExperimentsOptionSpec `json:",omitzero" desc:"its options"`
}

// ExperimentsReading is get_experiments' result.
type ExperimentsReading struct {
	Drawn     PaneDraw                   `desc:"which draw this is of"`
	Source    string                     `desc:"fixture (the built-in batch) or result (the current result, when leeway-shaped)"`
	Sink      string                     `desc:"the sink driven"`
	Candidate string                     `desc:"the candidate's identity in the vizeval catalogue"`
	Options   []ExperimentsOptionReading `desc:"the sink's options as set"`
	Guide     string                     `json:",omitzero" desc:"the pane's one-line reading guide for the sink"`
	Notice    string                     `json:",omitzero" desc:"what the pane says about its input: a row cap, or why the result cannot be driven"`
	Built     bool                       `desc:"the output is of the current source and options; false until the pane draws them"`
	Output    string                     `json:",omitzero" desc:"the sink's text output, at most 8 KiB"`
	OutputCut bool                       `json:",omitzero" desc:"the output was cut"`
	Picture   bool                       `json:",omitzero" desc:"the sink draws a picture rather than text; capture the pane to see it"`
	Catalog   []ExperimentsSinkSpec      `json:",omitzero" desc:"with catalog: every sink and its options"`
}

// GetExperimentsArgs is get_experiments' argument.
type GetExperimentsArgs struct {
	Catalog bool `json:",omitzero" desc:"also list every sink with its row cap and options"`
}

// SetExperimentsArgs is set_experiments' argument.
type SetExperimentsArgs struct {
	Source  *string           `json:",omitzero" desc:"fixture or result"`
	Sink    *string           `json:",omitzero" desc:"a sink id; get_experiments with catalog lists them"`
	Options map[string]string `json:",omitzero" desc:"options of the sink by name, each as text (an enum choice, a number, true or false); an option left out keeps its setting"`
}

// experimentsOpsView is what get_experiments reads.
type experimentsOpsView struct {
	source    experimentsSourceE
	sink      string
	cand      vizeval.Candidate
	candErr   string
	notice    string
	built     bool
	textOut   []string
	jsonText  string
	textSink  bool
	jsonReady bool
}

func (inst *PlayApp) experimentsView() (v experimentsOpsView) {
	d := inst.experiments
	v = experimentsOpsView{source: d.source, sink: d.sink, notice: d.notice, textOut: d.textOut, jsonText: d.jsonText,
		textSink: d.isTextSink(), jsonReady: d.jsonOK}
	cand, err := d.candidate()
	if err != nil {
		v.candErr = err.Error()
		return
	}
	v.cand = cand
	v.built = d.built && d.key.source == d.source && d.key.candidate == cand.ID()
	return
}

// experimentsValueText is an option's value as text.
func experimentsValueText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// experimentsCatalog is the vizeval catalogue as get_experiments lists it.
func experimentsCatalog() (out []ExperimentsSinkSpec) {
	for _, spec := range vizeval.Sinks() {
		s := ExperimentsSinkSpec{Id: spec.ID, Title: spec.Title, RowCap: spec.RowCap}
		switch spec.ID {
		case vizeval.SinkUnicode, vizeval.SinkTopoSpark, vizeval.SinkBrailleSpark, vizeval.SinkTreemapSpark, vizeval.SinkJSON:
			s.Text = true
		}
		for _, o := range spec.Space {
			os := ExperimentsOptionSpec{Name: o.Name, Description: o.Description, Kind: o.Kind.String(), Choices: o.Choices,
				Default: experimentsValueText(o.Default)}
			if o.Kind == vizeval.OptionKindInt || o.Kind == vizeval.OptionKindFloat {
				lo, hi := o.Min, o.Max
				os.Min, os.Max = &lo, &hi
			}
			s.Options = append(s.Options, os)
		}
		out = append(out, s)
	}
	return
}

// experimentsReading is get_experiments.
func experimentsReading(sn *opsSnap, in GetExperimentsArgs) (out ExperimentsReading, err error) {
	d, _, err := paneDrawOf(sn, experimentsPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.experiments
	out = ExperimentsReading{Drawn: d, Source: experimentsSourceFix, Sink: v.sink, Notice: v.notice, Built: v.built}
	if v.source == experimentsSourceResult {
		out.Source = experimentsSourceRes
	}
	out.Guide, _ = sinkGuide(v.sink)
	if v.candErr != "" {
		out.Notice = "the controls do not resolve to a candidate: " + v.candErr
	} else {
		out.Candidate = v.cand.ID()
		if spec, ok := vizeval.SinkByID(v.sink); ok {
			for _, o := range spec.Space {
				out.Options = append(out.Options, ExperimentsOptionReading{Name: o.Name, Value: experimentsValueText(v.cand.Options[o.Name])})
			}
		}
	}
	if in.Catalog {
		out.Catalog = experimentsCatalog()
	}
	var text string
	switch {
	case v.textSink:
		text = strings.Join(v.textOut, "\n")
	case v.sink == vizeval.SinkJSON:
		text = v.jsonText
	default:
		out.Picture = true
	}
	if !out.Built {
		return
	}
	if len(text) > experimentsOutputMaxB {
		text, out.OutputCut = truncateBytes(text, experimentsOutputMaxB), true
	}
	out.Output = text
	return
}

// experimentsParse turns an option's text into the raw value its kind
// resolves from.
func experimentsParse(o vizeval.Option, text string) (v any, err error) {
	text = strings.TrimSpace(text)
	switch o.Kind {
	case vizeval.OptionKindEnum:
		if !slices.Contains(o.Choices, text) {
			return nil, app.RefuseOperation("option " + o.Name + " is one of: " + strings.Join(o.Choices, ", "))
		}
		return text, nil
	case vizeval.OptionKindBool:
		b, perr := strconv.ParseBool(text)
		if perr != nil {
			return nil, app.RefuseOperation("option " + o.Name + " is true or false")
		}
		return b, nil
	}
	f, perr := strconv.ParseFloat(text, 64)
	if perr != nil || f < o.Min || f > o.Max || (o.Kind == vizeval.OptionKindInt && f != float64(int64(f))) {
		what := "a number"
		if o.Kind == vizeval.OptionKindInt {
			what = "a whole number"
		}
		return nil, app.RefuseOperation("option " + o.Name + " is " + what + " from " + strconv.FormatFloat(o.Min, 'g', -1, 64) +
			" to " + strconv.FormatFloat(o.Max, 'g', -1, 64))
	}
	return f, nil
}

// setExperiments is set_experiments on the driver: the options are laid
// over the sink's current controls and the whole is resolved through the
// catalogue, as a launch seed is, before anything is applied.
func (inst *experimentsDriver) setExperiments(in SetExperimentsArgs) (err error) {
	if in.Source == nil && in.Sink == nil && len(in.Options) == 0 {
		return noOptionsRefusal(experimentsPaneId, "source", "sink", "options")
	}
	source := inst.source
	if in.Source != nil {
		switch strings.TrimSpace(*in.Source) {
		case experimentsSourceFix:
			source = experimentsSourceFixture
		case experimentsSourceRes:
			source = experimentsSourceResult
		default:
			return app.RefuseOperation("source is fixture or result")
		}
	}
	spec := inst.spec()
	if in.Sink != nil {
		var ok bool
		if spec, ok = vizeval.SinkByID(strings.TrimSpace(*in.Sink)); !ok {
			ids := make([]string, 0, len(vizeval.Sinks()))
			for _, s := range vizeval.Sinks() {
				ids = append(ids, s.ID)
			}
			return app.RefuseOperation("sink is one of: " + strings.Join(ids, ", "))
		}
	}
	raw := inst.knobsRaw(spec)
	for name, text := range in.Options {
		i := slices.IndexFunc(spec.Space, func(o vizeval.Option) bool { return o.Name == name })
		if i < 0 {
			names := make([]string, 0, len(spec.Space))
			for _, o := range spec.Space {
				names = append(names, o.Name)
			}
			if len(names) == 0 {
				return app.RefuseOperation("the " + spec.ID + " sink takes no options")
			}
			return app.RefuseOperation("the " + spec.ID + " sink has no option " + strconv.Quote(name) + "; it takes " + strings.Join(names, ", "))
		}
		var v any
		if v, err = experimentsParse(spec.Space[i], text); err != nil {
			return
		}
		raw[name] = v
	}
	cand, err := vizeval.NewCandidate(spec.ID, raw)
	if err != nil {
		return app.RefuseOperation("the options do not resolve: " + err.Error())
	}
	inst.source = source
	inst.sink = cand.Sink
	inst.setKnobs(spec, cand.Options)
	inst.built = false
	return nil
}

// experimentsDigest is the experiments resource: the source and the
// candidate the controls resolve to.
func experimentsDigest(p *PlayApp) string {
	d := p.experiments
	if d == nil {
		return ""
	}
	id := ""
	if cand, err := d.candidate(); err == nil {
		id = cand.ID()
	}
	return strconv.Itoa(int(d.source)) + "|" + d.sink + "|" + id
}

func addExperimentsOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[ExperimentsReading, GetExperimentsArgs, SetExperimentsArgs]{
		pane:     experimentsPaneId,
		resource: "the Experiments pane's source, sink and options",
		digest:   experimentsDigest,
		get:      opGetExperiments,
		getSummary: "read the Experiments pane: the source and leeway sink it drives with their options, its notice, and the sink's " +
			"output where the sink writes text (box-drawn tables, sparks, card-JSON); on request the catalogue of sinks",
		set:        opSetExperiments,
		setSummary: "set the Experiments pane's source (fixture or result), sink and options, checked against the vizeval catalogue",
		follows: []string{"the pane drives the sink again when it next draws; get_experiments reports built once it has",
			"the person's source, sink and option controls change the same settings"},
		read:  experimentsReading,
		apply: func(p *PlayApp, in SetExperimentsArgs) error { return p.experiments.setExperiments(in) },
	})
}
