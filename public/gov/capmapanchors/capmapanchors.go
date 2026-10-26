// Package capmapanchors resolves a competence's anchors against the tree
// (ADR-0337 §SD1) and reads the evidence the tree carries for them (§SD3).
//
// [github.com/stergiotis/boxer/public/gov/capmapcorpus] defines what an
// anchor is and leaves resolving it to an interface, because resolving means
// reading the decision corpus, the app registry, the introspection roster and
// the doc tree, and that package is a pure library over markdown. This one is
// the implementation: [TreeResolver] answers for every anchor kind, and
// [Signals] turns the resolved anchors into the rows the `competenceevidence`
// table serves.
//
// # Reachability, not correctness
//
// Each kind's corpus is reached in one of two ways — a checkout on disk, or a
// registry in this process — and a kind whose corpus is out of reach answers
// [capmapcorpus.ResolutionUnchecked] rather than guessing. That is the
// providers' empty-rather-than-erroring convention applied to a verdict: a
// parse with no checkout around it says it could not look, not that nothing
// was there.
//
// The checkout is the directory holding `go.mod`, found by walking up from the
// working directory when none is given.
package capmapanchors

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// IndexWindow is how long one index of the checkout is reused. The corpora
// behind the anchors change at the pace of commits, and the vault's own reads
// are memoised for two seconds, so a stale answer lives at most this long.
const IndexWindow = 10 * time.Second

// TreeResolver resolves anchors against one checkout and this process's
// registries. Build it with [New]; the zero value resolves nothing.
type TreeResolver struct {
	root string

	mu        sync.Mutex
	indexedAt time.Time
	idx       *index
}

// index is one read of everything the file-backed kinds resolve against.
// Built whole rather than per kind because the walks share a root and a
// parse of a hundred competences asks for every kind anyway.
type index struct {
	root string
	// adrs maps a decision's number to its record, for the status and the
	// milestone tally.
	adrs map[int]adrcorpus.Adr
	// pkgs holds every directory under root with a non-test Go file, as a
	// slash-separated root-relative path, and every ancestor directory of
	// one — a package *tree*, which a subsystem anchors to — with the test
	// lanes of the packages beneath it folded in.
	pkgs map[string]pkgInfo
	// envs holds the variable names the generated env-vars page lists,
	// which is the registry as the tree records it.
	envs map[string]struct{}
	// pages holds `kind/slug` for every doc page an anchor can name.
	pages map[string]struct{}
	// apps holds the app directories that declare a manifest, by their last
	// path segment — the fallback for an app registry nothing has
	// populated.
	apps map[string]struct{}
	// applets holds applet slugs from every book; scenes holds scene stems;
	// skills holds skill names.
	applets map[string]struct{}
	scenes  map[string]struct{}
	skills  map[string]struct{}
}

// pkgInfo is what the evidence table wants to know about a package beyond
// its existence.
type pkgInfo struct {
	DefaultTests     bool
	IntegrationTests bool
}

// New builds a resolver over the checkout at root. An empty root is found by
// walking up from the working directory to the directory holding `go.mod`;
// when there is none, every file-backed kind answers unchecked.
func New(root string) (inst *TreeResolver) {
	if root == "" {
		root, _ = FindRoot()
	}
	return &TreeResolver{root: root}
}

// FindRoot walks up from the working directory to the nearest directory
// holding a `go.mod`, which is where every repository-relative key is read
// against.
func FindRoot() (root string, found bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for at := wd; ; {
		if _, sErr := os.Stat(filepath.Join(at, "go.mod")); sErr == nil {
			return at, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", false
		}
		at = parent
	}
}

// Root is the checkout the resolver reads, or empty when none was found.
func (inst *TreeResolver) Root() (root string) {
	return inst.root
}

// ResolveAnchor implements [capmapcorpus.AnchorResolverI].
func (inst *TreeResolver) ResolveAnchor(a capmapcorpus.Anchor) (res capmapcorpus.AnchorResolution) {
	switch a.Kind {
	case capmapcorpus.AnchorKindTable:
		return resolveTable(a.Key)
	case capmapcorpus.AnchorKindApp:
		if r, decided := resolveRegisteredApp(a.Key); decided {
			return r
		}
	}
	idx := inst.index()
	if idx == nil {
		if a.Kind == capmapcorpus.AnchorKindEnv {
			return resolveRegisteredEnv(a.Key)
		}
		return unchecked()
	}
	return idx.resolve(a)
}

// index returns the current index, reading the checkout when the last read
// is older than [IndexWindow]. nil when there is no checkout.
func (inst *TreeResolver) index() (idx *index) {
	if inst.root == "" {
		return nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.idx != nil && time.Since(inst.indexedAt) < IndexWindow {
		return inst.idx
	}
	inst.idx = buildIndex(inst.root)
	inst.indexedAt = time.Now()
	return inst.idx
}

func unchecked() (res capmapcorpus.AnchorResolution) {
	return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionUnchecked}
}

func found(status string) (res capmapcorpus.AnchorResolution) {
	return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionDirect, Status: status}
}

func missing() (res capmapcorpus.AnchorResolution) {
	return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionUnresolved}
}

// resolveTable answers for `boxer.<name>` from the facts schema's own
// coordinates and for `keelson/<name>` from the introspection roster. A
// process with no providers registered cannot vouch for a roster name, so
// that half is unchecked there.
func resolveTable(key string) (res capmapcorpus.AnchorResolution) {
	if name, ok := strings.CutPrefix(key, factsschema.DatabaseName+"."); ok {
		if name == factsschema.TableName {
			return found("")
		}
		return missing()
	}
	if name, ok := strings.CutPrefix(key, "keelson/"); ok {
		if len(introspect.Default.Names()) == 0 {
			return unchecked()
		}
		if _, has := introspect.Default.Lookup(name); has {
			return found("")
		}
		return missing()
	}
	return missing()
}

// resolveRegisteredEnv answers from the ADR-0009 registry in this process,
// which holds whatever the packages linked into it declared — the fallback
// when no checkout carries the generated page. An empty registry means no
// package has registered anything yet and is unchecked rather than a verdict
// on the variable.
func resolveRegisteredEnv(key string) (res capmapcorpus.AnchorResolution) {
	specs := env.All()
	if len(specs) == 0 {
		return unchecked()
	}
	for _, s := range specs {
		if s.Name == key {
			return found("")
		}
	}
	return missing()
}

// resolveRegisteredApp answers from the app registry when something has
// populated it. decided is false when the registry is empty, so the caller
// can fall back to the manifest sources on disk.
func resolveRegisteredApp(key string) (res capmapcorpus.AnchorResolution, decided bool) {
	manifests := app.AllManifests()
	if len(manifests) == 0 {
		return res, false
	}
	for _, m := range manifests {
		if appKey(string(m.Id)) == key {
			return found(""), true
		}
	}
	return missing(), true
}

// appKey is the anchor key of an app id: the segment after `/apps/`. Ids
// are module-path-shaped (`github.com/.../apps/watchbill`), the segment is
// the app's directory and what a person calls it, and an app that mints
// sub-ids beneath its own (`.../apps/sqlapplet/<slug>`) is still found by
// its directory.
func appKey(id string) (key string) {
	_, rest, found := strings.Cut(id, "/"+appsDir+"/")
	if !found {
		return id[strings.LastIndex(id, "/")+1:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// resolve answers the file-backed kinds from the index.
func (inst *index) resolve(a capmapcorpus.Anchor) (res capmapcorpus.AnchorResolution) {
	switch a.Kind {
	case capmapcorpus.AnchorKindAdr:
		n, err := strconv.Atoi(a.Key)
		if err != nil {
			return missing()
		}
		adr, ok := inst.adrs[n]
		if !ok {
			return missing()
		}
		return found(strings.ToLower(adr.Status))
	case capmapcorpus.AnchorKindPkg:
		if _, ok := inst.pkgs[strings.Trim(a.Key, "/")]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindApp:
		if _, ok := inst.apps[a.Key]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindHowto, capmapcorpus.AnchorKindExplanation,
		capmapcorpus.AnchorKindPositioning, capmapcorpus.AnchorKindTrial:
		if _, ok := inst.pages[string(a.Kind)+"/"+a.Key]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindApplet:
		if _, ok := inst.applets[a.Key]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindScene:
		if _, ok := inst.scenes[a.Key]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindSkill:
		if _, ok := inst.skills[a.Key]; ok {
			return found("")
		}
		return missing()
	case capmapcorpus.AnchorKindEnv:
		if len(inst.envs) == 0 {
			return resolveRegisteredEnv(a.Key)
		}
		if _, ok := inst.envs[a.Key]; ok {
			return found("")
		}
		return missing()
	}
	return unchecked()
}

// Where each file-backed kind lives, relative to the checkout.
const (
	adrDir         = "doc/adr"
	howtoDir       = "doc/howto"
	explanationDir = "doc/explanation"
	positioningDir = "doc/explanation/positioning"
	trialsDir      = "doc/trials"
	skillsDir      = "doc/skills"
	envVarsPage    = "doc/env-vars.md"
	appsDir        = "apps"
	scenesDirName  = "scenes"
	appletBookGlob = "apps/sqlapplet/book*/*.md"
)

// integrationTagRe matches the build constraint the integration lane
// selects on (ENGINEERING_PRACTICES §4).
var integrationTagRe = regexp.MustCompile(`(?m)^//go:build .*\bintegration\b`)

// envRowRe matches a variable's row in the generated env-vars page, whose
// first cell is the name in backticks.
var envRowRe = regexp.MustCompile("(?m)^\\| `([A-Z][A-Z0-9_]*)` \\|")

// buildIndex reads the checkout once. Every part is best-effort: a directory
// that is not there yields an empty map, and an anchor into it is then
// unresolved — which is right, since the checkout was consulted.
func buildIndex(root string) (idx *index) {
	idx = &index{
		root:    root,
		adrs:    map[int]adrcorpus.Adr{},
		pkgs:    map[string]pkgInfo{},
		pages:   map[string]struct{}{},
		apps:    map[string]struct{}{},
		applets: map[string]struct{}{},
		scenes:  map[string]struct{}{},
		skills:  map[string]struct{}{},
		envs:    map[string]struct{}{},
	}
	if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(envVarsPage))); err == nil {
		for _, m := range envRowRe.FindAllStringSubmatch(string(data), -1) {
			idx.envs[m[1]] = struct{}{}
		}
	}
	if adrs, err := adrcorpus.ParseDir(filepath.Join(root, adrDir)); err == nil {
		for _, a := range adrs {
			idx.adrs[a.Num] = a
		}
	}
	for kind, dir := range map[capmapcorpus.AnchorKindE]string{
		capmapcorpus.AnchorKindHowto:       howtoDir,
		capmapcorpus.AnchorKindExplanation: explanationDir,
		capmapcorpus.AnchorKindPositioning: positioningDir,
	} {
		for _, stem := range mdStems(filepath.Join(root, dir)) {
			idx.pages[string(kind)+"/"+stem] = struct{}{}
		}
	}
	for _, name := range subdirsWith(filepath.Join(root, trialsDir), "README.md") {
		idx.pages[string(capmapcorpus.AnchorKindTrial)+"/"+name] = struct{}{}
	}
	for _, name := range subdirsWith(filepath.Join(root, skillsDir), "SKILL.md") {
		idx.skills[name] = struct{}{}
	}
	if matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(appletBookGlob))); err == nil {
		for _, m := range matches {
			idx.applets[strings.TrimSuffix(filepath.Base(m), ".md")] = struct{}{}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(root, appsDir)); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			appDir := filepath.Join(root, appsDir, e.Name())
			if declaresManifest(appDir) {
				idx.apps[e.Name()] = struct{}{}
			}
			for _, m := range mdStems(filepath.Join(appDir, scenesDirName)) {
				if stem, ok := strings.CutSuffix(m, ".scene"); ok {
					idx.scenes[stem] = struct{}{}
				}
			}
		}
	}
	idx.pkgs = walkPackages(root)
	return idx
}

// mdStems lists the markdown files directly in dir by their stem, README
// excluded: a README is the directory's front page, not a page an anchor
// names.
func mdStems(dir string) (stems []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		stems = append(stems, strings.TrimSuffix(e.Name(), ".md"))
	}
	return stems
}

// subdirsWith lists the subdirectories of dir that hold marker.
func subdirsWith(dir, marker string) (names []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, sErr := os.Stat(filepath.Join(dir, e.Name(), marker)); sErr == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// declaresManifest reports whether a Go file directly in dir composes an
// app manifest — the shape every app's registration file has.
func declaresManifest(dir string) (ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, rErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rErr == nil && strings.Contains(string(data), "app.Manifest{") {
			return true
		}
	}
	return false
}

// walkPackages lists every directory under root holding a non-test Go file,
// with what test lanes it carries, and every ancestor of one as a package
// tree carrying the union of its packages' lanes. The walk skips what `go
// list` would: vendored and hidden trees, testdata, and build output that
// happens to sit inside the checkout. A directory holding only test files
// is not a package an anchor names.
func walkPackages(root string) (pkgs map[string]pkgInfo) {
	lanes := make(map[string]pkgInfo, 1024)
	isPkg := make(map[string]struct{}, 1024)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "vendor" || name == "testdata" || name == "node_modules" || name == "target") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, rErr := filepath.Rel(root, filepath.Dir(path))
		if rErr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if !strings.HasSuffix(d.Name(), "_test.go") {
			isPkg[key] = struct{}{}
			return nil
		}
		info := lanes[key]
		if data, fErr := os.ReadFile(path); fErr == nil && integrationTagRe.Match(data) {
			info.IntegrationTests = true
		} else {
			info.DefaultTests = true
		}
		lanes[key] = info
		return nil
	})
	pkgs = make(map[string]pkgInfo, len(isPkg)*2)
	for key := range isPkg {
		info := lanes[key]
		for at := key; at != "." && at != ""; at = parentDir(at) {
			merged := pkgs[at]
			merged.DefaultTests = merged.DefaultTests || info.DefaultTests
			merged.IntegrationTests = merged.IntegrationTests || info.IntegrationTests
			pkgs[at] = merged
		}
	}
	return pkgs
}

// parentDir is the slash-separated parent of a root-relative path, "." at
// the top.
func parentDir(key string) (parent string) {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[:i]
	}
	return "."
}
