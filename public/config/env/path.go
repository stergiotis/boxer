package env

import (
	"os"
	"path/filepath"
	"strings"
)

// PathVar is the typed env-var handle for filesystem path values. Get
// expands a leading "~" or "~/" to the user's home directory; absolute
// or relative paths are returned unchanged.
type PathVar struct {
	spec Spec
	res  resolved[string]
	// companionOf names the Sensitive variable this is the NAME_FILE
	// companion of; empty for a variable that is not one.
	companionOf string
}

var _ VarI = (*PathVar)(nil)

func NewPath(spec Spec) (v *PathVar) {
	mustValidate(spec)
	spec.Origin = callerOrigin(2)
	spec.Type = TypePath
	v = &PathVar{spec: spec}
	v = register(v).(*PathVar)
	return
}

func (inst *PathVar) Spec() (out Spec) {
	return inst.spec
}

// CompanionOf names the Sensitive variable whose NAME_FILE companion this
// is (SecretFileSuffix); empty when it is not one.
func (inst *PathVar) CompanionOf() (name string) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return inst.companionOf
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

// RecordFlag records the value a command-line flag parsed
// (ValueSourceFlag); it shadows the environment and the default but not an
// Override. public/config/env/envcli calls it from a derived flag's Action.
func (inst *PathVar) RecordFlag(value string) {
	inst.res.setFlag(expandHome(value))
}

// ClearCache forgets a recorded flag value and the cached environment
// read, so the next Get resolves from the environment again. The
// Override stays.
func (inst *PathVar) ClearCache() {
	inst.res.clearCache()
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
