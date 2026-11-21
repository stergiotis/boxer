package env

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// SecretFileSuffix names the companion variable every Sensitive variable
// gets: NAME_FILE holds the path of a file whose contents are NAME's value.
// It lets a secret reach the process without sitting in its environment,
// where every child would inherit it and /proc/<pid>/environ shows it.
//
// The companion is a *PathVar reached through StringVar.FileVar; its
// CompanionOf names the variable it belongs to. A package may also declare
// NAME_FILE itself with NewPath, no Default and not Sensitive — before or
// after NAME, in any init order: that declaration and the companion are one
// variable, the explicit declaration's description, category, flag name and
// origin stand, and both handles share one resolution state. Any other
// declaration of NAME_FILE conflicts (ErrConflictingDeclaration).
//
// Get prefers NAME over the file. A program with a stricter rule of its own
// — "setting both is an error" — reads StringVar.Lookup (NAME in the
// environment only) and the companion's Lookup, or Resolve's source, rather
// than comparing Get with the file.
const SecretFileSuffix = "_FILE"

// maxSecretFileBytes bounds what a secret file may hold.
const maxSecretFileBytes = 64 << 10

// errSecretFileTooLarge: the file holds more than maxSecretFileBytes. env
// sits below eh in the import graph, so it uses a plain error.
var errSecretFileTooLarge = errors.New("env: secret file larger than 64 KiB")

// secretFile is the NAME_FILE companion of one Sensitive variable.
type secretFile struct {
	path   *PathVar
	warned sync.Once
}

// newSecretFile registers NAME_FILE for the Sensitive spec. The companion
// shares the spec's category and origin; a path is not a secret, so it is
// not Sensitive itself. A compatible NAME_FILE declared earlier becomes the
// companion (register).
func newSecretFile(spec Spec) (inst *secretFile) {
	name := spec.Name + SecretFileSuffix
	fs := Spec{
		Name:        name,
		Description: "path of a file whose contents are " + spec.Name + "'s value (one trailing newline is dropped); read when " + spec.Name + " is unset, so the secret stays out of the environment children inherit",
		Category:    spec.Category,
		Origin:      spec.Origin,
		Type:        TypePath,
	}
	held := register(&PathVar{spec: fs, companionOf: spec.Name})
	p, ok := held.(*PathVar)
	if !ok {
		// register returns the variable it was handed on a conflict, so
		// the holder is always a *PathVar; keep the companion reading
		// NAME_FILE regardless.
		p = &PathVar{spec: fs, companionOf: spec.Name}
	}
	inst = &secretFile{path: p}
	return
}

// compatibleCompanionSpec reports whether an explicit declaration can be
// the NAME_FILE companion: a path with no default that is not itself
// Sensitive.
func compatibleCompanionSpec(s Spec) (ok bool) {
	return s.Type == TypePath && s.Default == "" && !s.Sensitive
}

// joinCompanion merges a companion and an explicit declaration of the same
// NAME_FILE, in either registration order. It reports false when neither is
// a companion or the explicit one is not compatible. registryMu is held.
func joinCompanion(existing VarI, incoming VarI) (held VarI, joined bool) {
	ep, eok := existing.(*PathVar)
	ip, iok := incoming.(*PathVar)
	if !eok || !iok {
		return
	}
	switch {
	case ep.companionOf != "" && ip.companionOf == "" && compatibleCompanionSpec(ip.spec):
		// The explicit declaration came second: its metadata stands.
		ep.spec = ip.spec
		return ep, true
	case ep.companionOf == "" && ip.companionOf != "" && compatibleCompanionSpec(ep.spec):
		// The explicit declaration came first and becomes the companion.
		ep.companionOf = ip.companionOf
		return ep, true
	}
	return
}

// companionNote explains, in a conflict message, that a name is a
// NAME_FILE companion; empty when neither declaration is one.
func companionNote(existing VarI, incoming VarI) (note string) {
	of := ""
	for _, v := range []VarI{existing, incoming} {
		if p, ok := v.(*PathVar); ok && p.companionOf != "" {
			of = p.companionOf
		}
	}
	if of == "" {
		return
	}
	return fmt.Sprintf("; it is the %s companion of Sensitive %s (SecretFileSuffix): declare it with NewPath, no Default and not Sensitive, or use %s's FileVar()", SecretFileSuffix, of, of)
}

// read returns the file's contents when NAME_FILE is set. A file that
// cannot be read is reported once on stderr and treated as unset: the
// variable then resolves to its default, as an unset variable does.
func (inst *secretFile) read() (value string, ok bool) {
	if inst == nil {
		return
	}
	path := inst.path.Get()
	if path == "" {
		return
	}
	b, err := readBounded(path)
	if err != nil {
		inst.warned.Do(func() {
			_, _ = fmt.Fprintf(os.Stderr, "env: %s names a file that cannot be read; the variable resolves as unset: %v\n", inst.path.spec.Name, err)
		})
		return
	}
	value = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	ok = value != ""
	return
}

func readBounded(path string) (b []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	b, err = io.ReadAll(io.LimitReader(f, maxSecretFileBytes+1))
	if err == nil && len(b) > maxSecretFileBytes {
		err = errSecretFileTooLarge
	}
	return
}
