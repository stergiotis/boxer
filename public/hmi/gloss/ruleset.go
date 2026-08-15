package gloss

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleSet is an ordered, named list of rules declared in code — the
// standing rules of a deployment, checked in beside the glosses they bind
// and registered on a Repository (ADR-0186, its 2026-08-15 Update on
// rules as code). It is built with a chain that reads as the rules do.
//
//	var sensors = gloss.Rules("acme-sensors").
//		Rule("kelvin readings").
//			When(gloss.Section("sensor"), gloss.NameMatches(`^temp`)).
//			Show(gloss.MediaTypeTemperature, gloss.Unit("K")).
//		Rule("secrets").
//			When(gloss.Sem(valueaspects.AspectSecret)).
//			Show(gloss.MediaTypeMasked)
//
// Nothing is validated until Repository.Register, which has the catalog:
// an unknown media type, a bad parameter, a pattern that does not compile,
// a rule without a condition or a duplicate name are all reported there,
// with the set and rule named — a set that does not register applies to
// nothing, loudly, rather than partially. Rules apply in declaration
// order; the first that matches a column wins.
type RuleSet struct {
	name  string
	rules []ruleDecl
	open  *RuleBuilder // a Rule(…) whose Show has not run yet
}

// ruleDecl is one declared rule before it is compiled against a catalog.
type ruleDecl struct {
	name      string
	when      []Predicate
	mediaType string
	params    map[string]string
}

// Rules opens a set. The name is its provenance in the Glosses tab and in
// the hover of every column it binds — a deployment or product name reads
// well; it must be non-empty and unique per repository.
func Rules(name string) *RuleSet {
	return &RuleSet{name: name}
}

// Name is the set's name.
func (inst *RuleSet) Name() string { return inst.name }

// Len is the number of rules declared so far.
func (inst *RuleSet) Len() int { return len(inst.rules) }

// Rule opens one rule; When names its condition and Show, which closes it,
// the gloss it binds. Rule names must be unique within the set.
func (inst *RuleSet) Rule(name string) *RuleBuilder {
	b := &RuleBuilder{set: inst, decl: ruleDecl{name: name}}
	inst.open = b
	return b
}

// RuleBuilder is one rule between Rule and Show.
type RuleBuilder struct {
	set  *RuleSet
	decl ruleDecl
}

// When sets the rule's condition: every predicate must hold. Calling it
// again replaces the condition.
func (inst *RuleBuilder) When(preds ...Predicate) *RuleBuilder {
	inst.decl.when = preds
	return inst
}

// Show closes the rule with the gloss to bind: a media type of the catalog
// and its parameters, and returns the set so the chain continues with the
// next Rule.
func (inst *RuleBuilder) Show(mediaType string, params ...Param) *RuleSet {
	inst.decl.mediaType = mediaType
	if len(params) > 0 {
		inst.decl.params = make(map[string]string, len(params))
		for _, p := range params {
			inst.decl.params[p.Name] = p.Value
		}
	}
	inst.set.rules = append(inst.set.rules, inst.decl)
	if inst.set.open == inst {
		inst.set.open = nil
	}
	return inst.set
}

// Param is one media-type parameter of a Show — `unit=K`.
type Param struct {
	Name  string
	Value string
}

// P is a parameter by name.
func P(name string, value string) Param { return Param{Name: name, Value: value} }

// Unit is the `unit` parameter the quantity glosses declare.
func Unit(unit string) Param { return P(ParamUnit, unit) }

// compile validates every declared rule against the catalog and binds it.
// source names the set in each rule's provenance.
func (inst *RuleSet) compile(cat *Catalog) (rules []Rule, err error) {
	if inst.name == "" {
		return nil, eb.Build().Errorf("a rule set needs a name")
	}
	if inst.open != nil {
		return nil, eb.Build().Str("set", inst.name).Str("rule", inst.open.decl.name).Errorf("rule was opened but never closed with Show")
	}
	seen := make(map[string]struct{}, len(inst.rules))
	rules = make([]Rule, 0, len(inst.rules))
	for _, d := range inst.rules {
		if d.name == "" {
			return nil, eb.Build().Str("set", inst.name).Errorf("a rule needs a name")
		}
		if _, dup := seen[d.name]; dup {
			return nil, eb.Build().Str("set", inst.name).Str("rule", d.name).Errorf("rule name declared twice")
		}
		seen[d.name] = struct{}{}
		if len(d.when) == 0 {
			return nil, eb.Build().Str("set", inst.name).Str("rule", d.name).Errorf("rule has no condition (When)")
		}
		pred := All(d.when...)
		if pred.err != nil {
			return nil, eb.Build().Str("set", inst.name).Str("rule", d.name).Errorf("%w", pred.err)
		}
		mt, params, instance, berr := cat.BindToken(CompactMediaType(d.mediaType, d.params))
		if berr != nil {
			return nil, eb.Build().Str("set", inst.name).Str("rule", d.name).Errorf("%w", berr)
		}
		rules = append(rules, Rule{
			Pattern:   pred.String(),
			MediaType: mt,
			Params:    params,
			Source:    SourceSet + " " + inst.name + ": " + d.name,
			Set:       inst.name,
			Name:      d.name,
			Instance:  instance,
			pred:      pred,
		})
	}
	return rules, nil
}
