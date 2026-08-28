package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// PathVar is the typed env-var handle for filesystem path values. Get
// expands a leading "~" or "~/" to the user's home directory; absolute
// or relative paths are returned unchanged.
type PathVar struct {
	spec Spec
	res  resolved[string]
}

var _ VarI = (*PathVar)(nil)

func NewPath(spec Spec) (v *PathVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypePath
	v = &PathVar{spec: spec}
	register(v)
	return
}

func (inst *PathVar) Spec() (out Spec) {
	return inst.spec
}

func (inst *PathVar) Get() (out string) {
	out, _ = inst.res.get(inst.resolveEnv)
	return
}

func (inst *PathVar) resolveEnv() (out string, src ValueSourceE) {
	raw, ok := os.LookupEnv(inst.spec.Name)
	if !ok || raw == "" {
		return expandHome(inst.spec.Default), ValueSourceDefault
	}
	return expandHome(raw), ValueSourceEnv
}

func (inst *PathVar) Lookup() (raw string, set bool) {
	raw, set = os.LookupEnv(inst.spec.Name)
	if raw == "" {
		set = false
	}
	return
}

func (inst *PathVar) setCached(value string) {
	inst.res.setFlag(expandHome(value))
}

// WithPathAction attaches a caller-supplied Action func to the
// cli.PathFlag returned by AsCliFlag. The user action runs first; on
// success the parsed (and ~-expanded by the cache) value is written to
// the cache.
func WithPathAction(fn func(ctx *cli.Context, parsed string) error) (opt FlagOption) {
	return func(o *flagOptions) {
		o.actionFn = fn
	}
}

func (inst *PathVar) AsCliFlag(opts ...FlagOption) (out cli.Flag) {
	fo := resolveFlagOptions(inst.spec, opts)
	userAction, _ := fo.actionFn.(func(*cli.Context, string) error)
	return &cli.PathFlag{
		Name:     fo.cliFlagName,
		Usage:    inst.spec.Description,
		Category: string(inst.spec.Category),
		EnvVars:  []string{inst.spec.Name},
		Value:    inst.spec.Default,
		Action: func(ctx *cli.Context, parsed string) (err error) {
			// Empty means unset; see StringVar.AsCliFlag.
			if parsed == "" {
				inst.res.clearCache()
				return
			}
			if userAction != nil {
				err = userAction(ctx, parsed)
				if err != nil {
					return
				}
			}
			inst.setCached(parsed)
			return
		},
	}
}

// Override pins the resolved value for this process (ValueSourceOverride):
// it shadows the flag, the environment and the default until ClearOverride,
// and is never written to the process environment, so child processes and
// `env list`'s CURRENT column do not see it. It is the seam a wrapper
// command uses to seed another component's variables before that component
// reads them in-process (ADR-0009, update 2026-08-28).
func (inst *PathVar) Override(value string) {
	inst.res.setOverride(expandHome(value))
}

// ClearOverride removes the Override; resolution falls back to the flag,
// environment or default.
func (inst *PathVar) ClearOverride() {
	inst.res.clearOverride()
}

// ValueSource reports which tier Get's value comes from.
func (inst *PathVar) ValueSource() (src ValueSourceE) {
	_, src = inst.res.get(inst.resolveEnv)
	return
}

func (inst *PathVar) SetForTest(t testing.TB, value string) {
	t.Helper()
	inst.res.reset()
	t.Setenv(inst.spec.Name, value)
	t.Cleanup(func() {
		inst.res.reset()
	})
}

// expandHome rewrites a leading "~" or "~/" to the user's home directory.
// If os.UserHomeDir fails, the input is returned unchanged.
func expandHome(path string) (out string) {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
