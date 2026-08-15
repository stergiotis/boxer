package gloss

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Repository is the standing rules a host hands play (ADR-0186, its
// 2026-08-15 Update on rules as code): rule sets over one catalog, in
// registration order, then the catalog's own affinities. It is a value a
// host builds at wiring time and injects — PlayApp takes one in its
// constructor — not something play looks up.
type Repository struct {
	cat   *Catalog
	sets  []*RuleSet
	rules []Rule // the sets' rules, compiled, in precedence order
}

// NewRepository builds an empty repository over cat (nil = Default()).
func NewRepository(cat *Catalog) *Repository {
	if cat == nil {
		cat = Default()
	}
	return &Repository{cat: cat}
}

// Catalog is the catalog the repository's rules bind glosses from — and
// the one every declaration a host shows resolves against.
func (inst *Repository) Catalog() *Catalog { return inst.cat }

// Register validates a set against the catalog and appends it: later sets
// rank below earlier ones. A set that fails to validate is not registered
// at all — no partial set — and a set name already present is refused.
func (inst *Repository) Register(set *RuleSet) (err error) {
	if set == nil {
		return eb.Build().Errorf("nil rule set")
	}
	for _, s := range inst.sets {
		if s.name == set.name {
			return eb.Build().Str("set", set.name).Errorf("rule set already registered")
		}
	}
	rules, err := set.compile(inst.cat)
	if err != nil {
		return err
	}
	inst.sets = append(inst.sets, set)
	inst.rules = append(inst.rules, rules...)
	return nil
}

// MustRegister is Register for wiring code, where a set that does not
// validate is a programming error.
func (inst *Repository) MustRegister(set *RuleSet) {
	if err := inst.Register(set); err != nil {
		panic(err)
	}
}

// Sets lists the registered sets in precedence order.
func (inst *Repository) Sets() []*RuleSet { return inst.sets }

// Rules is every standing rule in precedence order: the sets' rules, then
// the catalog's affinities. A host lists a query's own directives before
// these.
func (inst *Repository) Rules() []Rule {
	aff := inst.cat.AffinityRules()
	out := make([]Rule, 0, len(inst.rules)+len(aff))
	out = append(out, inst.rules...)
	return append(out, aff...)
}
