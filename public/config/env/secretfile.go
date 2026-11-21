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
// not Sensitive itself.
func newSecretFile(spec Spec) (inst *secretFile) {
	name := spec.Name + SecretFileSuffix
	fs := Spec{
		Name:        name,
		Description: "path of a file whose contents are " + spec.Name + "'s value (one trailing newline is dropped); read when " + spec.Name + " is unset, so the secret stays out of the environment children inherit",
		Category:    spec.Category,
		Origin:      spec.Origin,
		Type:        TypePath,
	}
	p := register(&PathVar{spec: fs}).(*PathVar)
	inst = &secretFile{path: p}
	return
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
