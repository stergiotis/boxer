package extbin

import (
	"errors"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrUndeclaredVariable: a Program names an environment variable
// (OverrideEnv, InheritSensitive) that no package declared through the env
// registry (ADR-0009), or InheritSensitive names one that is not Sensitive.
var ErrUndeclaredVariable = errors.New("extbin: program names an environment variable the env registry does not hold as required")

// DeclarationErrors checks every declared Program against the env
// registry and returns one error per offender (ErrUndeclaredVariable),
// joined; nil when every name resolves. OverrideEnv must name a declared
// variable; each InheritSensitive entry must name a declared Sensitive
// one.
//
// Run it after init, when every package's declarations are in: a host's
// start-up does (logging.Apply, beside env.RegistrationErrors), and a
// module's unit test does through CheckDeclarations, so a name a rarely
// taken path resolves is found before that path runs.
func DeclarationErrors() (err error) {
	var errs []error
	for _, p := range Registry() {
		errs = append(errs, p.declarationErrors()...)
	}
	return errors.Join(errs...)
}

func (p *Program) declarationErrors() (errs []error) {
	if p.OverrideEnv != "" {
		if _, declared := env.LookupVar(p.OverrideEnv); !declared {
			errs = append(errs, p.undeclared("OverrideEnv", p.OverrideEnv))
		}
	}
	for _, name := range p.InheritSensitive {
		v, declared := env.LookupVar(name)
		if !declared {
			errs = append(errs, p.undeclared("InheritSensitive", name))
			continue
		}
		if !v.Spec().Sensitive {
			errs = append(errs, eb.Build().Str("program", p.Name).Str("field", "InheritSensitive").Str("variable", name).Errorf("extbin: the variable is declared but not Sensitive, so it is never removed and needs no entry: %w", ErrUndeclaredVariable))
		}
	}
	return
}

func (p *Program) undeclared(field string, name string) (err error) {
	return eb.Build().Str("program", p.Name).Str("field", field).Str("variable", name).Errorf("extbin: no package declared the variable through the env registry: %w", ErrUndeclaredVariable)
}

// ReporterI is the part of testing.TB CheckDeclarations uses, so this
// package does not link the testing package.
type ReporterI interface {
	Helper()
	Errorf(format string, args ...any)
}

// CheckDeclarations fails t with every DeclarationErrors offender. A
// module that declares programs calls it from a unit test that links the
// packages declaring them and their variables.
func CheckDeclarations(t ReporterI) {
	t.Helper()
	if err := DeclarationErrors(); err != nil {
		t.Errorf("extbin declarations: %v", err)
	}
}
