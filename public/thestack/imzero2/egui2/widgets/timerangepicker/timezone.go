package timerangepicker

import (
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

const (
	// TzIDSystem is the reserved catalogue index for the host's
	// current local zone (resolves to time.Local at lookup time).
	// Stable across processes.
	TzIDSystem uint16 = 0
	// TzIDUTC is the reserved catalogue index for UTC. Stable across
	// processes. Indices >= 2 are lazily allocated per-process by
	// LookupTz.
	TzIDUTC uint16 = 1
)

const (
	tzNameSystem = "System"
	tzNameUTC    = "UTC"
)

// tzCatalogue is the process-local IANA-tz interning table for the
// picker's TzID <-> name surface. Entries are looked up lazily via
// time.LoadLocation; once interned, the (name, id) mapping is stable
// for the lifetime of the process. Indices are dense uint16 starting
// at 2 (0 and 1 are reserved). Concurrent-safe.
type tzCatalogue struct {
	mu   sync.Mutex
	byID []string
	byNm map[string]uint16
}

var globalTzCatalogue = newTzCatalogue()

func newTzCatalogue() (inst *tzCatalogue) {
	inst = &tzCatalogue{
		byID: []string{tzNameSystem, tzNameUTC},
		byNm: map[string]uint16{tzNameSystem: TzIDSystem, tzNameUTC: TzIDUTC},
	}
	return
}

// LookupTz returns the stable per-process TzID for the given IANA tz
// name (e.g. "UTC", "Asia/Tokyo", "America/Los_Angeles"). The two
// reserved names "System" and "UTC" always resolve to TzIDSystem /
// TzIDUTC; any other name is validated via time.LoadLocation and
// interned on first sight. Returns a wrapped error when the name does
// not resolve to a known zone.
//
// The catalogue can hold up to 2^16 - 1 distinct names. Process-local
// stability is enough for the picker's wire format because the TzID
// always travels alongside Go-side state that re-resolves the name on
// startup.
func LookupTz(name string) (id uint16, err error) {
	id, err = globalTzCatalogue.lookup(name)
	return
}

// TzName returns the IANA name interned under the given id. The
// returned ok is false when the id was never registered in this
// process. Reserved ids (0 "System", 1 "UTC") always resolve.
func TzName(id uint16) (name string, ok bool) {
	name, ok = globalTzCatalogue.name(id)
	return
}

// LoadTzLocation returns the *time.Location for a TzID. System
// resolves to time.Local at call time so callers see the current host
// zone, even if the OS zone changed since process start.
func LoadTzLocation(id uint16) (loc *time.Location, err error) {
	loc, err = globalTzCatalogue.location(id)
	return
}

// IanaName returns the IANA zone name for a TzID. System resolves to
// the runtime's time.Local zone name (e.g. "Europe/Berlin" on a
// host configured for CET) — this is the value the picker injects
// into ClickHouse SQL as the anchor_now timezone literal.
func IanaName(id uint16) (name string, err error) {
	name, err = globalTzCatalogue.ianaName(id)
	return
}

func (inst *tzCatalogue) lookup(name string) (id uint16, err error) {
	if name == "" {
		err = eh.Errorf("timerangepicker: empty tz name")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if existing, ok := inst.byNm[name]; ok {
		id = existing
		return
	}
	if _, loadErr := time.LoadLocation(name); loadErr != nil {
		err = eh.Errorf("timerangepicker: unknown tz %q: %w", name, loadErr)
		return
	}
	next := len(inst.byID)
	if next >= 1<<16 {
		err = eh.Errorf("timerangepicker: tz catalogue full (%d entries)", next)
		return
	}
	id = uint16(next)
	inst.byID = append(inst.byID, name)
	inst.byNm[name] = id
	return
}

func (inst *tzCatalogue) name(id uint16) (name string, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if int(id) >= len(inst.byID) {
		return
	}
	name = inst.byID[id]
	ok = true
	return
}

func (inst *tzCatalogue) location(id uint16) (loc *time.Location, err error) {
	if id == TzIDSystem {
		loc = time.Local
		return
	}
	name, ok := inst.name(id)
	if !ok {
		err = eh.Errorf("timerangepicker: unknown TzID %d", id)
		return
	}
	loc, err = time.LoadLocation(name)
	if err != nil {
		err = eh.Errorf("timerangepicker: load %q: %w", name, err)
		return
	}
	return
}

func (inst *tzCatalogue) ianaName(id uint16) (name string, err error) {
	if id == TzIDSystem {
		name = time.Local.String()
		return
	}
	resolved, ok := inst.name(id)
	if !ok {
		err = eh.Errorf("timerangepicker: unknown TzID %d", id)
		return
	}
	name = resolved
	return
}
