package timerangepicker

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
// the host's zone (e.g. "Europe/Berlin" on a host configured for CET) —
// this is the value the picker injects into ClickHouse SQL as the
// anchor_now timezone literal. When the host zone has no IANA name (TZ
// unset and /etc/localtime not a link into a zoneinfo tree) System is an
// error rather than Go's "Local", which ClickHouse rejects.
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
		err = eb.Build().Str("name", name).Errorf("timerangepicker: unknown tz: %w", loadErr)
		return
	}
	next := len(inst.byID)
	if next >= 1<<16 {
		err = eb.Build().Int("entries", next).Errorf("timerangepicker: tz catalogue is full")
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
		err = eb.Build().Uint16("id", id).Errorf("timerangepicker: unknown TzID")
		return
	}
	loc, err = time.LoadLocation(name)
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("timerangepicker: load: %w", err)
		return
	}
	return
}

func (inst *tzCatalogue) ianaName(id uint16) (name string, err error) {
	if id == TzIDSystem {
		name, err = resolveSystemZone(time.Local.String(), os.Readlink)
		return
	}
	resolved, ok := inst.name(id)
	if !ok {
		err = eb.Build().Uint16("id", id).Errorf("timerangepicker: unknown TzID")
		return
	}
	name = resolved
	return
}

// localtimePath is the file Go reads the host zone from when TZ is unset.
const localtimePath = "/etc/localtime"

// resolveSystemZone maps Go's name for time.Local to an IANA name. With TZ
// set, Go names the zone after it (a zoneinfo path is cut back to the
// name); with TZ unset Go says "Local", and the name comes from the
// target of /etc/localtime instead.
func resolveSystemZone(localName string, readlink func(string) (string, error)) (name string, err error) {
	name = localName
	if name == "Local" {
		target, rlErr := readlink(localtimePath)
		if rlErr != nil {
			err = eb.Build().Str("path", localtimePath).Errorf("timerangepicker: System zone has no IANA name: %w", rlErr)
			name = ""
			return
		}
		name = target
	}
	if i := strings.LastIndex(name, "zoneinfo/"); i >= 0 {
		name = name[i+len("zoneinfo/"):]
	}
	if name == "" || name == "Local" || strings.HasPrefix(name, "/") {
		err = eb.Build().Str("name", name).Errorf("timerangepicker: System zone has no IANA name")
		name = ""
		return
	}
	if _, loadErr := time.LoadLocation(name); loadErr != nil {
		err = eb.Build().Str("name", name).Errorf("timerangepicker: System zone: %w", loadErr)
		name = ""
		return
	}
	return
}
