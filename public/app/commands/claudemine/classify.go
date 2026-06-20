package claudemine

import (
	"path/filepath"
	"strings"
)

// repoClassifier maps an absolute filesystem path to the repository it belongs
// to. Classification is structural — it hardcodes no repository name. A path is
// attributed to (in order):
//
//   - an explicit root registered via --repo name=/abs/path (longest match wins),
//   - else the first path segment under the configured repo-parent directory
//     (repositories are assumed to be siblings there),
//   - else "claude-meta" when it lives under a ~/.claude tree,
//   - else "other".
//
// The path relative to the resolved repo root is returned alongside, so the
// emitted rows can be grouped by file within a repo. rel is "" for the
// "claude-meta"/"other" buckets and for a path that is itself a repo root.
type repoClassifier struct {
	repoParent string            // abs, slash-clean; "" disables segment inference
	explicit   map[string]string // abs root -> repo name
	claudeDirs []string          // abs ~/.claude-style roots -> "claude-meta"
}

func newClassifier(repoParent string, explicit map[string]string, claudeDirs []string) *repoClassifier {
	c := &repoClassifier{explicit: map[string]string{}}
	if repoParent != "" {
		c.repoParent = filepath.ToSlash(filepath.Clean(repoParent))
	}
	for root, name := range explicit {
		c.explicit[filepath.ToSlash(filepath.Clean(root))] = name
	}
	for _, d := range claudeDirs {
		if d != "" {
			c.claudeDirs = append(c.claudeDirs, filepath.ToSlash(filepath.Clean(d)))
		}
	}
	return c
}

func (c *repoClassifier) classify(path string) (repo, rel string) {
	if path == "" {
		return "other", ""
	}
	p := filepath.ToSlash(filepath.Clean(path))

	// Explicit roots, longest prefix wins.
	bestRoot, bestName := "", ""
	for root, name := range c.explicit {
		if under(p, root) && len(root) > len(bestRoot) {
			bestRoot, bestName = root, name
		}
	}
	if bestName != "" {
		return bestName, relUnder(p, bestRoot)
	}

	// First segment under the repo-parent directory.
	if c.repoParent != "" && under(p, c.repoParent) {
		rest := strings.TrimPrefix(p, c.repoParent+"/")
		seg, sub, _ := strings.Cut(rest, "/")
		if seg != "" {
			return seg, sub
		}
	}

	// Anything under a ~/.claude tree is boxer-external tooling state.
	for _, d := range c.claudeDirs {
		if under(p, d) {
			return "claude-meta", ""
		}
	}
	return "other", ""
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

func relUnder(p, root string) string {
	if p == root {
		return ""
	}
	return strings.TrimPrefix(p, root+"/")
}
