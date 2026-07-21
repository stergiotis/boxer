// Package kindcheck answers "do these facts-CBOR bytes claim kind K?"
// for hosts that must validate a payload at a boundary without importing
// the payload's Go type (ADR-0135 §SD1: the window host refuses a
// malformed or mistargeted launch config before the target app sees it).
//
// The boxer.facts sparse-CBOR wire carries no kind marker — a row's
// kind is implied by which vocabulary membership ids populate its tagged
// sections, and only the kind's generated codec knows that set. So the
// check cannot be a header peek; instead each codec module registers a
// probe minted from its own generated decoder, and Check runs the
// claimed kind's probe against the bytes. A probe failure (garbage,
// truncation, or a payload whose memberships belong to another kind)
// refuses the claim.
//
// Registration is one hand-written init line per module (see
// launchrequest/register.go); the generated .out.go files are untouched.
// Only kinds that cross a validated boundary need to register — the
// registry is deliberately not a census of the codec corpus.
package kindcheck

import (
	"sort"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ProbeFunc reports whether b decodes as the registering kind's DTO.
// Implementations are typically one line over the module's generated
// buscodec decoder. A nil return accepts the bytes.
type ProbeFunc func(b []byte) (err error)

var (
	mu     sync.RWMutex
	probes = map[string]ProbeFunc{}
)

// Register installs the probe for a kind name (the DTO's `kind:` tag).
// Intended for package-init use from the codec module that owns the
// kind. Registering a nil probe or re-registering an existing kind
// panics — both indicate a wiring bug worth failing loudly at startup.
func Register(kind string, probe ProbeFunc) {
	if kind == "" || probe == nil {
		panic("kindcheck: Register requires a kind name and a non-nil probe")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := probes[kind]; dup {
		panic("kindcheck: duplicate Register for kind " + kind)
	}
	probes[kind] = probe
}

// Check verifies that b decodes as the claimed kind. An unregistered
// kind is refused — the caller cannot distinguish "unknown kind" from
// "kind with no codec", and both must fail closed.
func Check(kind string, b []byte) (err error) {
	mu.RLock()
	probe := probes[kind]
	mu.RUnlock()
	if probe == nil {
		err = eb.Build().Str("kind", kind).Errorf("kindcheck: kind is not registered (known: %s)", knownList())
		return
	}
	err = probe(b)
	if err != nil {
		err = eb.Build().Str("kind", kind).Errorf("kindcheck: bytes do not decode as claimed kind: %w", err)
		return
	}
	return
}

// PeekKind identifies which registered kind b decodes as by probing all
// registrations. Exactly one probe must accept: zero acceptances refuse
// the bytes (garbage, truncation, or an unregistered kind), and more
// than one is reported as ambiguous rather than resolved by iteration
// order. Diagnostic / test helper; boundary code that already holds a
// claimed kind should call Check instead.
func PeekKind(b []byte) (kind string, err error) {
	mu.RLock()
	names := make([]string, 0, len(probes))
	for k := range probes {
		names = append(names, k)
	}
	sort.Strings(names)
	var matches []string
	for _, k := range names {
		if probes[k](b) == nil {
			matches = append(matches, k)
		}
	}
	mu.RUnlock()
	switch len(matches) {
	case 1:
		kind = matches[0]
	case 0:
		err = eb.Build().Int("len", len(b)).Errorf("kindcheck: bytes decode as no registered kind (known: %s)", knownList())
	default:
		err = eb.Build().Str("matches", strings.Join(matches, ",")).Errorf("kindcheck: bytes decode ambiguously as %d registered kinds", len(matches))
	}
	return
}

// knownList renders the registered kind names for error messages.
// Callers hold no lock; the read is taken here.
func knownList() (s string) {
	mu.RLock()
	names := make([]string, 0, len(probes))
	for k := range probes {
		names = append(names, k)
	}
	mu.RUnlock()
	sort.Strings(names)
	if len(names) == 0 {
		s = "none"
		return
	}
	s = strings.Join(names, ",")
	return
}
