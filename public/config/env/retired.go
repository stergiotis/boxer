package env

import (
	"slices"
	"strings"
	"sync"
)

// Retired is a variable name the registry once declared and no longer
// reads. A process that finds one set warns, naming the replacement, so a
// renamed variable left in a shell profile is not silently ignored.
type Retired struct {
	Name string
	// ReplacedBy is the variable that took over its role; empty when none
	// did, or the history does not say which.
	ReplacedBy string
	// Since is the date the name left the registry, YYYY-MM-DD.
	Since string
}

// retired is append-only: a name stays for as long as an environment might
// still set it. Recovered from the history of doc/env-vars.md.
var retired = []Retired{
	{Name: "BOXER_JACKSTAY_PLAN_DIR", ReplacedBy: "BOXER_FS_APPDATA_DIR", Since: "2026-09-28"},
	{Name: "BOXER_LLM_KEEP_MESSAGES", ReplacedBy: "BOXER_LLM_RETAIN", Since: "2026-09-27"},
	{Name: "BOXER_MDEDIT_LLM_APIKEY", ReplacedBy: "BOXER_LLM_APIKEY", Since: "2026-09-23"},
	{Name: "BOXER_MDEDIT_LLM_ENDPOINT", ReplacedBy: "BOXER_LLM_ENDPOINT", Since: "2026-09-23"},
	{Name: "BOXER_MDEDIT_LLM_MAXTOKENS", ReplacedBy: "BOXER_LLM_MAXTOKENS", Since: "2026-09-23"},
	{Name: "BOXER_MDEDIT_LLM_MODEL", ReplacedBy: "BOXER_LLM_MODEL", Since: "2026-09-23"},
	{Name: "BOXER_MDEDIT_LLM_TIMEOUT", ReplacedBy: "BOXER_LLM_TIMEOUT", Since: "2026-09-23"},
	{Name: "BOXER_PLAY_EXPERIMENTS", Since: "2026-10-07"},
	{Name: "BOXER_PLAY_FOCUS_EXPERIMENTS", Since: "2026-10-07"},
	{Name: "BOXER_PLAY_FOCUS_MODEL", Since: "2026-10-08"},
	{Name: "BOXER_TAB_ORIGIN", ReplacedBy: "BOXER_TAB_BASE", Since: "2026-10-09"},
	{Name: "GODEPVIEW_ROOT", ReplacedBy: "BOXER_GODEP_ROOT", Since: "2026-08-01"},
	{Name: "GODEPVIEW_TAGS", ReplacedBy: "BOXER_GODEP_TAGS", Since: "2026-08-01"},
	{Name: "GOOGLE_API_KEY", ReplacedBy: "GEMINI_API_KEY", Since: "2026-06-23"},
	{Name: "HN_EXPLORER_CLICKHOUSE_URL", Since: "2026-06-21"},
	{Name: "IMZERO2_QUERYRUNS_CH_URL", ReplacedBy: "CLICKHOUSE_URL", Since: "2026-10-08"},
	{Name: "PEBBLE2_RUN_ID", ReplacedBy: "BOXER_RUN_ID", Since: "2026-11-09"},
	{Name: "SPINNAKER_PLAY_AUTORUN", ReplacedBy: "BOXER_PLAY_AUTORUN", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_EXIT_ON_SHOT", ReplacedBy: "BOXER_PLAY_EXIT_ON_SHOT", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_DIAGNOSTICS", ReplacedBy: "BOXER_PLAY_FOCUS_DIAGNOSTICS", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_GRAPH", ReplacedBy: "BOXER_PLAY_FOCUS_GRAPH", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_MAP", ReplacedBy: "BOXER_PLAY_FOCUS_MAP", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_SCHEMA", ReplacedBy: "BOXER_PLAY_FOCUS_SCHEMA", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_TIMELINE", ReplacedBy: "BOXER_PLAY_FOCUS_TIMELINE", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_FOCUS_WORLD", ReplacedBy: "BOXER_PLAY_FOCUS_WORLD", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_MAP_CENTER", ReplacedBy: "BOXER_PLAY_MAP_CENTER", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_MAP_SIZE", ReplacedBy: "BOXER_PLAY_MAP_SIZE", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_MAP_TABLE", ReplacedBy: "BOXER_PLAY_MAP_TABLE", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_MAP_ZOOM", ReplacedBy: "BOXER_PLAY_MAP_ZOOM", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_OBSERVE", ReplacedBy: "BOXER_PLAY_OBSERVE", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_PREVIEW_AS_SENT", ReplacedBy: "BOXER_PLAY_PREVIEW_AS_SENT", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_SCREENSHOT", ReplacedBy: "BOXER_PLAY_SCREENSHOT", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_SHOT_SETTLE", ReplacedBy: "BOXER_PLAY_SHOT_SETTLE", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_SQL", ReplacedBy: "BOXER_PLAY_SQL", Since: "2026-07-12"},
	{Name: "SPINNAKER_PLAY_TIMELINE_BANDS_SQL", ReplacedBy: "BOXER_PLAY_TIMELINE_BANDS_SQL", Since: "2026-07-12"},
}

// RetiredVars returns the retired names sorted by Name.
func RetiredVars() (out []Retired) {
	out = slices.Clone(retired)
	slices.SortFunc(out, func(a, b Retired) int { return strings.Compare(a.Name, b.Name) })
	return
}

// WatchedPrefixes are the namespaces whose unregistered names a process
// warns about: the ones boxer owns.
var WatchedPrefixes = []string{"BOXER_", "IMZERO2_"}

// ScriptOwned are names in a watched namespace that the repository's shell
// scripts read and set for themselves (build flags, benchmark and fuzz
// knobs); a Go process inherits them and does not read them, so they are not
// reported. A test in public/app holds every such name under scripts/ to be
// either registered or listed here.
var ScriptOwned = []string{
	"BOXER_BENCH_BASE", "BOXER_BENCH_COUNT", "BOXER_BENCH_MATCH", "BOXER_BENCH_OUT", "BOXER_BENCH_TIME",
	"BOXER_COVERLANE_COVERPKG", "BOXER_COVERLANE_DIR",
	"BOXER_FUZZ_LOG_DIR", "BOXER_FUZZ_MATCH", "BOXER_FUZZ_PARALLEL", "BOXER_FUZZ_TIME",
	"BOXER_GO_FLAGS", "BOXER_GO_TAGS", "BOXER_IDL_CHECK", "BOXER_ROOT",
	"IMZERO2_FFMPEG_SRC",
}

// EnvFindingKindE says why a set variable is reported.
type EnvFindingKindE uint8

const (
	// EnvFindingRetired is a set variable the registry retired.
	EnvFindingRetired EnvFindingKindE = iota + 1
	// EnvFindingUnregistered is a set variable in a watched namespace that
	// no package declares — this binary's or, as far as the generated name
	// list knows, any other of this repository's.
	EnvFindingUnregistered
)

// EnvFinding is one set variable a process should warn about.
type EnvFinding struct {
	Kind EnvFindingKindE
	Name string
	// Retired is the tombstone, for EnvFindingRetired.
	Retired Retired
}

// CheckEnviron reports the variables of environ ("NAME=value" entries, as
// os.Environ gives them) that are retired, or that sit in a watched
// namespace and are registered neither in this process nor in the
// repository-wide name list the doc generator writes (knownNames). An
// empty value counts as unset, as it does for resolution. Sorted by name.
func CheckEnviron(environ []string) (findings []EnvFinding) {
	byName := make(map[string]Retired, len(retired))
	for _, r := range retired {
		byName[r.Name] = r
	}
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || value == "" {
			continue
		}
		if r, isRetired := byName[name]; isRetired {
			findings = append(findings, EnvFinding{Kind: EnvFindingRetired, Name: name, Retired: r})
			continue
		}
		if !watched(name) {
			continue
		}
		if _, registered := LookupVar(name); registered {
			continue
		}
		if _, known := slices.BinarySearch(knownNames, name); known {
			continue
		}
		if slices.Contains(ScriptOwned, name) {
			continue
		}
		findings = append(findings, EnvFinding{Kind: EnvFindingUnregistered, Name: name})
	}
	slices.SortFunc(findings, func(a, b EnvFinding) int { return strings.Compare(a.Name, b.Name) })
	return
}

func watched(name string) bool {
	for _, p := range WatchedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

var checkOnce sync.Once

// CheckEnvironOnce is CheckEnviron the first time it is called in a process
// and nil after, for a start-up hook that may run more than once.
func CheckEnvironOnce(environ []string) (findings []EnvFinding) {
	checkOnce.Do(func() {
		findings = CheckEnviron(environ)
	})
	return
}
