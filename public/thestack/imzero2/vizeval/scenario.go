package vizeval

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"gopkg.in/yaml.v3"
)

// ScenarioSuffix names a scenario document.
const ScenarioSuffix = ".vizeval.md"

// Scenario is the fixed part of a problem (ADR-0257 §SD4): a dataset, what a
// reader wants from it, the viewport, the sinks that may answer, questions
// with computable answers, and the gates a candidate must pass.
type Scenario struct {
	// Name is the file's base name without ScenarioSuffix.
	Name string
	Path string
	Spec ScenarioSpec
	// Prose is the document body without fences: the scenario for a person,
	// and the context a judge is given.
	Prose string
	// SQL is the dataset's projection: the first role-less `sql` fence.
	SQL string
	// Base is the `sql base` fence, when there is one: the data with plain
	// names, which the projection and every answer read as the CTE `base`.
	// Writing the generator once is what keeps the answers on the same data
	// as the picture.
	Base string
}

// ScenarioSpec is the frontmatter's `vizeval:` key.
type ScenarioSpec struct {
	// Size is the capture's viewport, WxH logical points.
	Size string `yaml:"size"`
	// Intent says what a reader wants from the data, in one sentence.
	Intent string `yaml:"intent"`
	// Sinks lists the sinks the scenario admits. A scenario about ranking
	// hosts by load does not admit a picture that discards every value.
	Sinks []string `yaml:"sinks"`
	// SettleMs is how long the capture waits after the app mounts, for the
	// query to run and the pane to lay out. Zero means the default.
	SettleMs int `yaml:"settleMs"`
	// Questions are what a reader should be able to answer from the picture.
	Questions []Question `yaml:"questions"`
	// Gates bound metrics; a candidate outside one is not ranked.
	Gates map[string]Gate `yaml:"gates"`
}

// Question is asked of a judge in a later milestone; its answer is computed
// here, from the same data, so it cannot disagree with it.
type Question struct {
	ID     string `yaml:"id"`
	Prompt string `yaml:"prompt"`
	// Answer is SQL over the same engine whose result is the answer.
	Answer string `yaml:"answer"`
	// Compare is how a given answer is checked: eq (the default), set
	// (order-free rows), approx (a number within Tol).
	Compare string  `yaml:"compare"`
	Tol     float64 `yaml:"tol"`
}

// Gate bounds one metric. A metric absent from a scorecard fails a gate that
// names it, rather than passing by omission.
type Gate struct {
	Min *float64 `yaml:"min"`
	Max *float64 `yaml:"max"`
}

// DefaultSettleMs covers mount, query and first layout on the headless host.
const DefaultSettleMs = 2500

var compares = []string{"", "eq", "set", "approx"}

// ReadScenario reads and parses one scenario document.
func ReadScenario(path string) (sc *Scenario, err error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to read scenario: %w", err)
	}
	return ParseScenario(path, src)
}

// ParseScenario parses a scenario document. Unknown keys inside `vizeval:`
// are refused: a misspelt gate would otherwise never be enforced.
func ParseScenario(path string, src []byte) (sc *Scenario, err error) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ScenarioSuffix) {
		return nil, eb.Build().Str("path", path).Errorf("a scenario document is named *" + ScenarioSuffix)
	}
	sc = &Scenario{Name: strings.TrimSuffix(base, ScenarioSuffix), Path: path}
	front, fences, prose := scene.SplitDoc(src)
	sc.Prose = prose
	var fm struct {
		Vizeval yaml.Node `yaml:"vizeval"`
	}
	if err = yaml.Unmarshal(front, &fm); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to parse the frontmatter: %w", err)
	}
	if fm.Vizeval.Kind == 0 {
		return nil, eb.Build().Str("path", path).Errorf("frontmatter has no `vizeval:` key")
	}
	var raw bytes.Buffer
	enc := yaml.NewEncoder(&raw)
	if err = enc.Encode(&fm.Vizeval); err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, eh.Errorf("unable to re-encode the vizeval spec: %w", err)
	}
	dec := yaml.NewDecoder(&raw)
	dec.KnownFields(true)
	if err = dec.Decode(&sc.Spec); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to parse the `vizeval:` spec: %w", err)
	}
	for _, f := range fences {
		switch {
		case f.Lang == "sql" && f.Role == "" && sc.SQL == "":
			sc.SQL = strings.TrimSpace(f.Text)
		case f.Lang == "sql" && f.Role == "base":
			if sc.Base != "" {
				return nil, eb.Build().Str("path", path).Errorf("more than one `sql base` fence")
			}
			sc.Base = strings.TrimSpace(f.Text)
		}
	}
	if err = sc.check(); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("invalid scenario: %w", err)
	}
	return sc, nil
}

func (inst *Scenario) check() (err error) {
	if inst.SQL == "" {
		return eh.Errorf("no dataset: the scenario needs a `sql` fence")
	}
	if _, _, err = (scene.Spec{Size: inst.Spec.Size}).Dimensions(); err != nil {
		return err
	}
	if len(inst.Spec.Sinks) == 0 {
		return eh.Errorf("the scenario admits no sink")
	}
	for _, s := range inst.Spec.Sinks {
		if _, ok := SinkByID(s); !ok {
			return eb.Build().Str("sink", s).Errorf("unknown sink")
		}
	}
	ids := make([]string, 0, len(inst.Spec.Questions))
	for _, q := range inst.Spec.Questions {
		if q.ID == "" || q.Prompt == "" || q.Answer == "" {
			return eb.Build().Str("question", q.ID).Errorf("a question needs id, prompt and answer")
		}
		if slices.Contains(ids, q.ID) {
			return eb.Build().Str("question", q.ID).Errorf("duplicate question id")
		}
		ids = append(ids, q.ID)
		if !slices.Contains(compares, q.Compare) {
			return eb.Build().Str("question", q.ID).Str("compare", q.Compare).Errorf("unknown compare (want eq, set or approx)")
		}
	}
	for name, g := range inst.Spec.Gates {
		if g.Min == nil && g.Max == nil {
			return eb.Build().Str("gate", name).Errorf("a gate needs min or max")
		}
	}
	return nil
}

// DatasetSQL is the dataset as play runs it: the projection, under the base
// CTE when there is one.
func (inst *Scenario) DatasetSQL() string {
	return inst.withBase(inst.SQL)
}

// AnswerSQL is a question's answer query, under the base CTE when there is
// one.
func (inst *Scenario) AnswerSQL(q Question) string {
	return inst.withBase(q.Answer)
}

func (inst *Scenario) withBase(sql string) string {
	if inst.Base == "" {
		return sql
	}
	return "WITH base AS (\n" + strings.TrimRight(inst.Base, "; \n") + "\n)\n" + sql
}

// Admits reports whether the scenario admits a sink.
func (inst *Scenario) Admits(sink string) bool {
	return slices.Contains(inst.Spec.Sinks, sink)
}

// Pass reports whether a value satisfies the gate.
func (inst Gate) Pass(v float64, present bool) bool {
	if !present {
		return false
	}
	if inst.Min != nil && v < *inst.Min {
		return false
	}
	if inst.Max != nil && v > *inst.Max {
		return false
	}
	return true
}
