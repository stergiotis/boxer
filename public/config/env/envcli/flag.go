// Package envcli derives urfave/cli flags and the `env` command from the
// variables registered with public/config/env. It is the only part of the
// registry that links the CLI library: a package that reads variables
// imports env alone (ADR-0009, ADR-0394).
package envcli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v3"
)

// FlagOption customises the cli.Flag returned by Flag.
type FlagOption func(opts *flagOptions)

// flagOptions carries the caller's customisations. actionFn is typed per
// variable by the WithXxxAction helpers; Flag type-asserts it to the
// signature the variable's flag type calls.
type flagOptions struct {
	name     string
	actionFn any
}

// WithFlagName overrides Spec.CliFlagName for the produced flag. Intended
// for the Configer composition path: the caller's NameTransformFunc is
// applied to the spec's CliFlagName and the result is passed in here.
func WithFlagName(name string) (opt FlagOption) {
	return func(opts *flagOptions) {
		opts.name = name
	}
}

// WithBoolAction attaches an Action to the flag of a *env.BoolVar. It runs
// before the parsed value is recorded; an error stops the recording.
func WithBoolAction(fn func(ctx context.Context, cmd *cli.Command, parsed bool) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

// WithStringAction attaches an Action to the flag of a *env.StringVar or a
// *env.CategorialStringVar (where it runs after membership validation).
func WithStringAction(fn func(ctx context.Context, cmd *cli.Command, parsed string) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

// WithPathAction attaches an Action to the flag of a *env.PathVar.
func WithPathAction(fn func(ctx context.Context, cmd *cli.Command, parsed string) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

// WithInt64Action attaches an Action to the flag of a *env.IntVar.
func WithInt64Action(fn func(ctx context.Context, cmd *cli.Command, parsed int64) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

// WithFloat64Action attaches an Action to the flag of a *env.FloatVar.
func WithFloat64Action(fn func(ctx context.Context, cmd *cli.Command, parsed float64) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

// WithDurationAction attaches an Action to the flag of a *env.DurationVar.
func WithDurationAction(fn func(ctx context.Context, cmd *cli.Command, parsed time.Duration) error) (opt FlagOption) {
	return func(o *flagOptions) { o.actionFn = fn }
}

func resolve(spec env.Spec, opts []FlagOption) (out flagOptions) {
	out.name = spec.CliFlagName
	for _, opt := range opts {
		opt(&out)
	}
	return
}

// Flag derives a cli.Flag from a registered variable: the flag is bound to
// the variable's environment name, defaults to Spec.Default, and its Action
// records the parsed value on the variable (RecordFlag), so Get observes it.
// An empty parsed string is not a value (env.Spec: empty means unset): the
// Action then clears the variable's cache and runs no user action.
//
// v must be one of the typed variables env declares; any other VarI is a
// programmer error and panics.
func Flag(v env.VarI, opts ...FlagOption) (out cli.Flag) {
	spec := v.Spec()
	fo := resolve(spec, opts)
	switch tv := v.(type) {
	case *env.BoolVar:
		out = boolFlag(tv, spec, fo)
	case *env.StringVar:
		out = stringFlag(spec, fo, tv.RecordFlag, tv.ClearCache, false)
	case *env.PathVar:
		out = stringFlag(spec, fo, tv.RecordFlag, tv.ClearCache, true)
	case *env.CategorialStringVar:
		out = categorialFlag(tv, spec, fo)
	case *env.IntVar:
		out = intFlag(tv, spec, fo)
	case *env.FloatVar:
		out = floatFlag(tv, spec, fo)
	case *env.DurationVar:
		out = durationFlag(tv, spec, fo)
	default:
		panic(fmt.Sprintf("envcli: no flag derivation for %T (variable %q)", v, spec.Name))
	}
	return
}

func boolFlag(v *env.BoolVar, spec env.Spec, fo flagOptions) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, bool) error)
	return &cli.BoolFlag{
		Name:     fo.name,
		Usage:    spec.Description,
		Category: string(spec.Category),
		Sources:  cli.EnvVars(spec.Name),
		Value:    v.DefaultValue(),
		Action: func(ctx context.Context, cmd *cli.Command, parsed bool) (err error) {
			if userAction != nil {
				err = userAction(ctx, cmd, parsed)
				if err != nil {
					return
				}
			}
			v.RecordFlag(parsed)
			return
		},
	}
}

func intFlag(v *env.IntVar, spec env.Spec, fo flagOptions) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, int64) error)
	return &cli.Int64Flag{
		Name:     fo.name,
		Usage:    spec.Description,
		Category: string(spec.Category),
		Sources:  cli.EnvVars(spec.Name),
		Value:    v.DefaultValue(),
		Action: func(ctx context.Context, cmd *cli.Command, parsed int64) (err error) {
			if userAction != nil {
				err = userAction(ctx, cmd, parsed)
				if err != nil {
					return
				}
			}
			v.RecordFlag(parsed)
			return
		},
	}
}

func floatFlag(v *env.FloatVar, spec env.Spec, fo flagOptions) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, float64) error)
	return &cli.Float64Flag{
		Name:     fo.name,
		Usage:    spec.Description,
		Category: string(spec.Category),
		Sources:  cli.EnvVars(spec.Name),
		Value:    v.DefaultValue(),
		Action: func(ctx context.Context, cmd *cli.Command, parsed float64) (err error) {
			if userAction != nil {
				err = userAction(ctx, cmd, parsed)
				if err != nil {
					return
				}
			}
			v.RecordFlag(parsed)
			return
		},
	}
}

func durationFlag(v *env.DurationVar, spec env.Spec, fo flagOptions) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, time.Duration) error)
	return &cli.DurationFlag{
		Name:     fo.name,
		Usage:    spec.Description,
		Category: string(spec.Category),
		Sources:  cli.EnvVars(spec.Name),
		Value:    v.DefaultValue(),
		Action: func(ctx context.Context, cmd *cli.Command, parsed time.Duration) (err error) {
			if userAction != nil {
				err = userAction(ctx, cmd, parsed)
				if err != nil {
					return
				}
			}
			v.RecordFlag(parsed)
			return
		},
	}
}

// stringFlag serves StringVar and PathVar: the two differ only in
// TakesFile and in what RecordFlag does with the value (PathVar expands a
// leading "~").
func stringFlag(spec env.Spec, fo flagOptions, record func(string), clearCache func(), takesFile bool) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, string) error)
	return &cli.StringFlag{
		Name:      fo.name,
		Usage:     spec.Description,
		Category:  string(spec.Category),
		Sources:   cli.EnvVars(spec.Name),
		Value:     spec.Default,
		TakesFile: takesFile,
		Action: func(ctx context.Context, cmd *cli.Command, parsed string) (err error) {
			if parsed == "" {
				clearCache()
				return
			}
			if userAction != nil {
				err = userAction(ctx, cmd, parsed)
				if err != nil {
					return
				}
			}
			record(parsed)
			return
		},
	}
}

// categorialFlag appends "(one of: a|b|c)" to the Usage. An out-of-set
// value is treated by source, matching urfave/cli's env-vs-flag treatment
// of typed flags: from the bound environment variable it falls back to
// Spec.Default silently and the user action runs on the default; given
// explicitly as `--flag=…` it is a CLI error.
func categorialFlag(v *env.CategorialStringVar, spec env.Spec, fo flagOptions) (out cli.Flag) {
	userAction, _ := fo.actionFn.(func(context.Context, *cli.Command, string) error)
	allowed := v.Allowed()
	usage := spec.Description + fmt.Sprintf(" (one of: %s)", strings.Join(allowed, "|"))
	return &cli.StringFlag{
		Name:     fo.name,
		Usage:    usage,
		Category: string(spec.Category),
		Sources:  cli.EnvVars(spec.Name),
		Value:    spec.Default,
		Action: func(ctx context.Context, cmd *cli.Command, parsed string) (err error) {
			if parsed == "" {
				v.ClearCache()
				return
			}
			effective := parsed
			if !v.IsAllowed(effective) {
				envRaw, envSet := os.LookupEnv(spec.Name)
				if envSet && envRaw == parsed {
					effective = spec.Default
				} else {
					return eh.Errorf("%q is not in allowed values for --%s: %v", //boxer:lint disable=CS013 reason="a CLI flag error read as text: the value, the flag and the allowed set belong in the message"
						parsed, fo.name, allowed)
				}
			}
			if userAction != nil {
				err = userAction(ctx, cmd, effective)
				if err != nil {
					return
				}
			}
			v.RecordFlag(effective)
			return
		},
	}
}
