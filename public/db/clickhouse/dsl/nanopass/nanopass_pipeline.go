//go:build llm_generated_opus47

package nanopass

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultFixedPointMaxIter is the default convergence cap used by the runner
// when auto-wrapping a pass that declares Properties.NeedsFixedPoint.
const DefaultFixedPointMaxIter = 128

// ApplyFunc is the body of a Pass — it transforms (env, body) into a new body,
// optionally mutating env. The same env value is observed by subsequent passes
// in a Sequence.
type ApplyFunc func(e *env.Environment, body string) (newBody string, err error)

// Pass is the first-class unit of nanopass transformation. Apply does the work;
// Properties declares behavioural metadata that the runner and AssertProperties
// consume.
type Pass struct {
	Name       string
	Apply      ApplyFunc
	Properties PassProperties
}

// PassProperties declares behavioural metadata for a Pass.
//
// Idempotent and NeedsFixedPoint are mutually exclusive — declaring both is
// caught at AssertProperties time.
type PassProperties struct {
	// Idempotent: f(f(x)) == f(x) over the corpus.
	Idempotent bool

	// NeedsFixedPoint: the runner wraps Apply in a FixedPoint with
	// DefaultFixedPointMaxIter. Pass authors who need a different cap call
	// FixedPoint(p, n) explicitly.
	NeedsFixedPoint bool

	// Reads/Writes record which env regions the pass touches. Documentation
	// in v1; future schedulers may use these to parallelise independent passes.
	Reads, Writes EnvRegions

	// Requires/Produces are tag strings for ordering hints. v1 carries them
	// as documentation only; AssertProperties may corpus-check them later.
	Requires []FormTag
	Produces []FormTag
}

// EnvRegions is a bitset of environment regions a pass may read or write.
type EnvRegions uint8

const (
	RegionBody EnvRegions = 1 << iota
	RegionSessionSettings
	RegionStatementSettings
	RegionParams
	RegionFormat
)

// FormTag is a tag describing a body's normalisation state, used for
// Requires/Produces ordering hints.
type FormTag string

// Grammar identifies which ClickHouse grammar variant a Validating pass
// expects.
type Grammar uint8

const (
	GrammarG1 Grammar = iota
	GrammarG2
)

// Run applies the pass to a complete SQL string by extracting the env,
// applying, and integrating back. Most external callers use Run; passes
// invoked from another pass's Apply use Apply directly.
func (p Pass) Run(sql string) (result string, err error) {
	e, body, err := env.Extract(sql)
	if err != nil {
		err = eh.Errorf("Run %s: %w", p.Name, err)
		return
	}
	newBody, applyErr := p.applyWithProps(e, body)
	if applyErr != nil {
		err = applyErr
		return
	}
	result, err = e.Integrate(newBody)
	if err != nil {
		err = eh.Errorf("Run %s: %w", p.Name, err)
	}
	return
}

// applyWithProps invokes Apply, honouring NeedsFixedPoint by wrapping in a
// fixpoint loop with DefaultFixedPointMaxIter.
func (p Pass) applyWithProps(e *env.Environment, body string) (newBody string, err error) {
	if p.Apply == nil {
		err = eb.Build().Str("pass", p.Name).Errorf("pass has nil Apply")
		return
	}
	if !p.Properties.NeedsFixedPoint {
		newBody, err = p.Apply(e, body)
		if err != nil {
			err = eb.Build().Str("pass", p.Name).Errorf("apply failed: %w", err)
		}
		return
	}
	newBody, err = runFixedPoint(p.Name, p.Apply, e, body, DefaultFixedPointMaxIter)
	return
}

// runFixedPoint repeats fn until convergence or maxIter. Used both by
// applyWithProps for auto-wrapping and by the FixedPoint combinator.
func runFixedPoint(name string, fn ApplyFunc, e *env.Environment, body string, maxIter int) (result string, err error) {
	result = body
	for i := 0; i < maxIter; i++ {
		next, applyErr := fn(e, result)
		if applyErr != nil {
			err = eb.Build().Str("pass", name).Int("iter", i).Errorf("apply failed: %w", applyErr)
			return
		}
		if next == result {
			return
		}
		result = next
	}
	err = eb.Build().Str("pass", name).Int("maxIter", maxIter).Errorf("fixpoint did not converge: %w", ErrNoFixPointReached)
	return
}

// ErrNoFixPointReached is returned by FixedPoint when maxIter is exhausted.
var ErrNoFixPointReached = eh.Errorf("did not converge, no fix point reached")

// Sequence composes passes left-to-right under a single name. The returned
// Pass calls each child's applyWithProps in turn, sharing the same env.
func Sequence(name string, ps ...Pass) Pass {
	return Pass{
		Name: name,
		Apply: func(e *env.Environment, body string) (string, error) {
			cur := body
			for _, child := range ps {
				next, err := child.applyWithProps(e, cur)
				if err != nil {
					return "", err
				}
				cur = next
			}
			return cur, nil
		},
	}
}

// FixedPoint wraps p in a fixpoint loop bounded by maxIter. Use this when a
// pass needs a non-default convergence cap, or to wrap a Sequence.
func FixedPoint(p Pass, maxIter int) Pass {
	return Pass{
		Name: "FixedPoint(" + p.Name + ")",
		Apply: func(e *env.Environment, body string) (string, error) {
			fn := p.Apply
			if p.Properties.NeedsFixedPoint {
				// Avoid double-wrapping; call the raw Apply directly.
				fn = p.Apply
			}
			return runFixedPoint(p.Name, fn, e, body, maxIter)
		},
	}
}

// Validating wraps p so that p's output body is validated against grammar g
// after Apply. Pre-validation is achieved by inserting ValidateGrammar1 (or
// ValidateGrammar2) as a prior step in a Sequence.
func Validating(g Grammar, p Pass) Pass {
	return Pass{
		Name:       "Validating(" + p.Name + ")",
		Properties: p.Properties,
		Apply: func(e *env.Environment, body string) (string, error) {
			out, err := p.applyWithProps(e, body)
			if err != nil {
				return "", err
			}
			validator := validatorFor(g)
			if _, vErr := validator(e, out); vErr != nil {
				return "", eb.Build().Str("pass", p.Name).Errorf("output failed validation: %w", vErr)
			}
			return out, nil
		},
	}
}

// Conditional runs p only when pred(env) returns true; otherwise body passes
// through unchanged. Useful for pipelines that want optional steps based on
// environment shape (e.g. only run a param-binding pass when params exist).
func Conditional(name string, pred func(*env.Environment) bool, p Pass) Pass {
	return Pass{
		Name:       name,
		Properties: p.Properties,
		Apply: func(e *env.Environment, body string) (string, error) {
			if !pred(e) {
				return body, nil
			}
			return p.applyWithProps(e, body)
		},
	}
}

// LiftBodyPass wraps a body-only function (no env interaction) as a Pass.
// Use it for pure CST rewriters that have no business with settings, params,
// or format.
func LiftBodyPass(name string, fn func(sql string) (string, error), props PassProperties) Pass {
	return Pass{
		Name: name,
		Apply: func(_ *env.Environment, body string) (string, error) {
			return fn(body)
		},
		Properties: props,
	}
}

// ValidateGrammar1 is a Pass that parses body with Grammar1 (the full
// ClickHouse SELECT surface) and returns the body unchanged on success. Use
// this as a pipeline step to verify that a preceding pass produced valid
// Grammar1 SQL.
var ValidateGrammar1 = Pass{
	Name: "ValidateGrammar1",
	Apply: func(_ *env.Environment, body string) (string, error) {
		_, err := Parse(body)
		if err != nil {
			return "", eh.Errorf("ValidateGrammar1: %w", err)
		}
		return body, nil
	},
	Properties: PassProperties{
		Idempotent: true,
		Reads:      RegionBody,
	},
}

// ValidateGrammar2 is a Pass that parses body with Grammar2 (canonical-only
// surface) and returns the body unchanged on success. Use this as the final
// step of a normalisation pipeline to verify that the output conforms to
// Grammar2's canonical form.
var ValidateGrammar2 = Pass{
	Name: "ValidateGrammar2",
	Apply: func(_ *env.Environment, body string) (string, error) {
		_, err := ParseCanonical(body)
		if err != nil {
			return "", eh.Errorf("ValidateGrammar2: %w", err)
		}
		return body, nil
	},
	Properties: PassProperties{
		Idempotent: true,
		Reads:      RegionBody,
	},
}

// validatorFor maps a Grammar selector to its ValidateGrammar* Apply func.
func validatorFor(g Grammar) ApplyFunc {
	switch g {
	case GrammarG2:
		return ValidateGrammar2.Apply
	default:
		return ValidateGrammar1.Apply
	}
}
