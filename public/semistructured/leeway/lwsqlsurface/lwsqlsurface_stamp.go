package lwsqlsurface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
)

// DigestFunctionName is the zero-argument function that reports the content
// digest of the surface a server carries. It is the second half of the
// stamp: LW_SURFACE_VERSION says which generation, this says which bodies
// (ADR-0171, update of 2026-11-20).
//
// A separate function rather than a second value in the marker, because
// every reader of LW_SURFACE_VERSION parses a bare integer; a build that
// predates the digest keeps reading the generation unchanged.
const DigestFunctionName = "LW_SURFACE_DIGEST"

// StampSelect is the select list that reads a server's stamp in one round
// trip: the generation, then the digest. A server without either function
// fails the query; a caller reads that as "no current surface".
const StampSelect = VersionFunctionName + "(), " + DigestFunctionName + "()"

// digestPlaceholderStamp stands in for ViewStamp while the views are hashed:
// the stamp carries the digest, so hashing the stamped views would be
// circular.
const digestPlaceholderStamp = chviews.Stamp("digest")

// Digest is the hex sha256 over every statement the surface installs except
// its own two stamp functions: the three function families in installation
// order, then the views rendered for the default database.
//
// The decision "is this server current" compares it, so a body change is
// seen whether or not anyone bumped Version. TestSurfaceDigestPinned is what
// makes the two move together: it holds the digest of this generation as a
// literal and fails, asking for a bump, when the bodies move under it.
func Digest() (digest string) {
	return digestOnce()
}

var digestOnce = sync.OnceValue(func() (digest string) {
	h := sha256.New()
	for _, stmt := range familyStatements() {
		_, _ = h.Write([]byte(stmt))
		_, _ = h.Write([]byte{0})
	}
	for _, stmt := range chviews.Statements("", digestPlaceholderStamp) {
		_, _ = h.Write([]byte(stmt))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
})

// DigestStatement renders the CREATE for the digest function.
func DigestStatement() (sql string) {
	sql = chpack.Statement(chpack.Function{
		Name:   DigestFunctionName,
		Params: nil,
		Body:   "'" + Digest() + "'",
	})
	return
}

// ServerStamp is what a server reports about the surface it carries.
type ServerStamp struct {
	// Generation is LW_SURFACE_VERSION's value, -1 when the server has no
	// marker.
	Generation int
	// Digest is LW_SURFACE_DIGEST's value, empty when the server has none —
	// a surface installed by a build older than the digest.
	Digest string
	// Unreadable is true when the marker is installed but its value is not
	// an integer.
	Unreadable bool
}

// ParseStamp reads the two fields StampSelect returns, as text.
func ParseStamp(generation string, digest string) (stamp ServerStamp) {
	stamp.Generation = -1
	v, err := strconv.Atoi(strings.TrimSpace(generation))
	if err != nil {
		stamp.Unreadable = true
		return
	}
	stamp.Generation = v
	stamp.Digest = strings.TrimSpace(digest)
	return
}

// InstallActionE is what a build does about the surface a server carries.
type InstallActionE uint8

const (
	// ActionNone: the server carries exactly this build's surface.
	ActionNone InstallActionE = iota + 1
	// ActionInstall: the server carries no surface, an older generation, an
	// unreadable marker, or this generation with other bodies.
	ActionInstall
	// ActionKeepNewer: the server carries a newer generation. This build
	// neither installs nor drops — installs are monotonic, so an older build
	// starting against a shared server does not put older bodies back under a
	// newer host. It verifies the server still declares every name it needs
	// ([VerifyServes]) and reads through what is there.
	ActionKeepNewer
)

// String renders the action for logs.
func (inst InstallActionE) String() (s string) {
	switch inst {
	case ActionNone:
		s = "none"
	case ActionInstall:
		s = "install"
	case ActionKeepNewer:
		s = "keep-newer"
	default:
		s = "unknown"
	}
	return
}

// Decide maps a server's stamp to what this build does about it. A higher
// generation is newer; within this generation the digest decides.
//
// Two builds of one generation with different bodies would take turns
// re-installing — the pinned digest test is what keeps such a pair from
// being built.
func Decide(stamp ServerStamp) (action InstallActionE) {
	switch {
	case stamp.Unreadable || stamp.Generation < 0:
		action = ActionInstall
	case stamp.Generation > Version:
		action = ActionKeepNewer
	case stamp.Generation < Version:
		action = ActionInstall
	case stamp.Digest == Digest():
		action = ActionNone
	default:
		action = ActionInstall
	}
	return
}

// VerifyServes checks that a server carrying a newer generation still
// declares every function this build calls. A newer roster is expected to be
// a superset — names are retired one generation late for that reason — and
// a server that is not one is refused with the names it lacks rather than
// read from with errors later.
func VerifyServes(ctx context.Context, conn Conn) (err error) {
	installed, err := serverFunctions(ctx, conn)
	if err != nil {
		return
	}
	missing := missingFrom(installed)
	if len(missing) > 0 {
		err = eb.Build().Strs("missing", missing).Int("buildGeneration", Version).
			Errorf("the server carries a newer leeway SQL surface that lacks functions this build calls; install this build's surface explicitly or upgrade")
	}
	return
}

// missingFrom lists the declared names absent from installed, sorted.
func missingFrom(installed map[string]struct{}) (missing []string) {
	for name := range DeclaredNames() {
		if _, ok := installed[name]; !ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return
}

// retiredDue reports whether this build drops a retired name: only names
// retired before this generation, so the generation that last declared a
// name keeps finding it while a newer build shares its server.
func retiredDue(r chpack.RetiredName) (due bool) {
	return r.Generation < Version
}
