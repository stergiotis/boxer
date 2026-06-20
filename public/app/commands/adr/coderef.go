package adr

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CodeRef is one row of the `coderef` table: a single citation of an ADR
// number found in a source file (the ADR corpus itself is excluded, so
// ADR-to-ADR cross-links are never counted as implementation evidence).
type CodeRef struct {
	Num       int
	Path      string
	Line      int
	Lang      string
	Pkg       string
	Qualifier string // §-qualified section the citation pins, e.g. "SD10", "4", "M2.7"; "" if none
	Snippet   string
}

var (
	// ADR-0080, ADR 0080, (ADR-0080), optionally pinned to a §section such as
	// §SD10, §4 or §M2.7 (a trailing sentence period is not captured).
	adrRefRe = regexp.MustCompile(`ADR[- ]?(\d{4})\b(?:[ ]*§[ ]*([A-Za-z]*\d+(?:\.\d+)*))?`)
	// path-style citation, e.g. doc/adr/0066-...
	adrPathRe = regexp.MustCompile(`adr/(\d{4})-`)
)

var langByExt = map[string]string{
	".go": "go", ".rs": "rust", ".sh": "shell", ".bash": "shell",
	".proto": "proto", ".nix": "nix", ".ts": "ts", ".tsx": "ts",
	".js": "js", ".mjs": "js", ".html": "html", ".css": "css",
	".toml": "toml", ".py": "python", ".sql": "sql", ".c": "c",
	".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp",
	".java": "java", ".kt": "kotlin", ".yaml": "yaml", ".yml": "yaml",
}

var skipDirs = map[string]struct{}{
	".git": {}, "node_modules": {}, "vendor": {}, "target": {},
	"build": {}, "dist": {}, ".idea": {}, ".vscode": {},
}

// ScanCodeRefs walks root collecting ADR citations from source files. The ADR
// corpus dir (excludeDir) and the artifact dir (outDir) are skipped. Markdown
// is excluded entirely — "the code", not the prose.
func ScanCodeRefs(root, excludeDir, outDir string) (refs []CodeRef, err error) {
	excludeAbs, _ := filepath.Abs(excludeDir)
	outAbs, _ := filepath.Abs(outDir)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			abs, _ := filepath.Abs(path)
			if abs == excludeAbs || (outAbs != "" && abs == outAbs) {
				return filepath.SkipDir
			}
			name := d.Name()
			if _, skip := skipDirs[name]; skip {
				return filepath.SkipDir
			}
			if name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		lang, ok := langByExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		fileRefs, rerr := scanFile(root, path, lang)
		if rerr != nil {
			return rerr
		}
		refs = append(refs, fileRefs...)
		return nil
	})
	if walkErr != nil {
		return nil, eh.Errorf("unable to scan code refs under %q: %w", root, walkErr)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Num != refs[j].Num {
			return refs[i].Num < refs[j].Num
		}
		if refs[i].Path != refs[j].Path {
			return refs[i].Path < refs[j].Path
		}
		return refs[i].Line < refs[j].Line
	})
	return refs, nil
}

func scanFile(root, path, lang string) (refs []CodeRef, err error) {
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, eh.Errorf("unable to read %q: %w", path, err)
	}
	if !bytes.Contains(data, []byte("ADR")) && !bytes.Contains(data, []byte("adr/")) {
		return nil, nil
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	pkg := filepath.ToSlash(filepath.Dir(rel))
	lineNo := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		lineNo++
		seen := make(map[string]struct{})
		add := func(numStr, qualifier string) {
			key := numStr + "|" + qualifier
			if _, dup := seen[key]; dup {
				return
			}
			seen[key] = struct{}{}
			num, _ := strconv.Atoi(numStr)
			refs = append(refs, CodeRef{
				Num: num, Path: rel, Line: lineNo, Lang: lang,
				Pkg: pkg, Qualifier: qualifier, Snippet: trimSnippet(line),
			})
		}
		for _, m := range adrRefRe.FindAllStringSubmatch(line, -1) {
			add(m[1], m[2])
		}
		for _, m := range adrPathRe.FindAllStringSubmatch(line, -1) {
			add(m[1], "")
		}
	}
	return refs, nil
}

func trimSnippet(line string) string {
	s := strings.TrimSpace(line)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Aggregate folds the code references into the Adr rows (matched by Num) and
// assigns the heuristic ImplEvidence bucket. References whose number has no
// ADR file are ignored here but remain in the coderef table.
func Aggregate(adrs []Adr, refs []CodeRef) []Adr {
	type agg struct {
		refs                      int
		files, pkgs, langs, quals map[string]struct{}
	}
	byNum := make(map[int]*agg)
	for _, r := range refs {
		a := byNum[r.Num]
		if a == nil {
			a = &agg{
				files: map[string]struct{}{}, pkgs: map[string]struct{}{},
				langs: map[string]struct{}{}, quals: map[string]struct{}{},
			}
			byNum[r.Num] = a
		}
		a.refs++
		a.files[r.Path] = struct{}{}
		a.pkgs[r.Pkg] = struct{}{}
		a.langs[r.Lang] = struct{}{}
		if r.Qualifier != "" {
			a.quals[r.Qualifier] = struct{}{}
		}
	}
	for i := range adrs {
		adrs[i].CodeLangs = []string{}
		adrs[i].CodeQualifiers = []string{}
		a := byNum[adrs[i].Num]
		if a == nil {
			adrs[i].ImplEvidence = "none"
			continue
		}
		adrs[i].CodeRefs = a.refs
		adrs[i].CodeFiles = len(a.files)
		adrs[i].CodePkgs = len(a.pkgs)
		adrs[i].CodeLangs = sortedKeys(a.langs)
		adrs[i].CodeQualifiers = sortedKeys(a.quals)
		adrs[i].ImplEvidence = evidenceBucket(adrs[i].CodeRefs, adrs[i].CodeFiles, adrs[i].CodePkgs)
	}
	return adrs
}

// evidenceBucket is a deliberately coarse, heuristic read of implementation
// degree from citation footprint. It is a starting point for queries, not an
// authoritative status — drill into the coderef table to judge any single ADR.
func evidenceBucket(refs, files, pkgs int) string {
	switch {
	case refs == 0:
		return "none"
	case files >= 8 || pkgs >= 4:
		return "broad"
	default:
		return "referenced"
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
