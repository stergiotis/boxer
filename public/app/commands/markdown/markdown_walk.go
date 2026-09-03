package markdown

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// source is one markdown file to read: where it is, and the name it is
// stored under — the path relative to the root it was found from, with
// forward slashes, so `[[folder/note]]` in a document matches it.
type source struct {
	path string
	name string
}

// walkSources expands the arguments: a file is taken as is, named by its
// basename; a directory is walked for `.md` files, each named relative to it.
// Dot-directories (`.obsidian`, `.git`, `.trash`) are skipped, as Obsidian
// skips them.
func walkSources(args []string) (out []source, err error) {
	for _, arg := range args {
		var st os.FileInfo
		st, err = os.Stat(arg)
		if err != nil {
			err = eb.Build().Str("path", arg).Errorf("stat: %w", err)
			return
		}
		if !st.IsDir() {
			out = append(out, source{path: arg, name: filepath.Base(arg)})
			continue
		}
		root := arg
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, source{path: path, name: filepath.ToSlash(rel)})
			return nil
		})
		if err != nil {
			err = eb.Build().Str("root", root).Errorf("walk: %w", err)
			return
		}
	}
	return
}
