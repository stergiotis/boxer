package splashscreen

import (
	"fmt"
	"io/fs"
	"strings"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/observability/vcs"
)

// The help book (ADR-0047, keelson-app A4): one page, the About tab as
// prose. The static part is help/overview.md; the values the About tab
// reads from the running process — version, provenance, run identity —
// and the NOTICE text are appended when the page is opened, so the page
// says what the tab says rather than a copy that goes stale. Only the
// overview is rendered this way; every other name is the embedded
// directory's.

// helpOverview is the page the About content is appended to.
const helpOverview = "overview.md"

// aboutHelpFS serves the embedded help directory, with the overview
// rendered on each open.
type aboutHelpFS struct {
	static fs.FS
}

var _ fs.FS = aboutHelpFS{}

// Open serves the overview from a one-file in-memory FS built on each
// call, and everything else, the directory itself included, from the
// embedded tree.
func (inst aboutHelpFS) Open(name string) (f fs.File, err error) {
	if name != helpOverview {
		return inst.static.Open(name)
	}
	src, err := fs.ReadFile(inst.static, helpOverview)
	if err != nil {
		return
	}
	return fstest.MapFS{helpOverview: &fstest.MapFile{Data: renderAbout(src), ModTime: time.Now()}}.Open(helpOverview)
}

// renderAbout appends the About tab's content to the static page: the
// identity and licence lines the tab draws, the Build section's fields,
// and the NOTICE in a fenced block so its file paths stay verbatim.
func renderAbout(static []byte) (page []byte) {
	loadAssets()
	var b strings.Builder
	b.Write(static)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n## Identity\n\n")
	fmt.Fprintf(&b, "- %s · version %s\n", manifest.Display, manifest.Version)
	fmt.Fprintf(&b, "- %s\n", vcs.CopyrightInfo())
	b.WriteString("- Licensed under the MIT License (see the NOTICE below).\n")
	b.WriteString("\n## Build\n\n")
	for _, kv := range buildInfo() {
		fmt.Fprintf(&b, "- **%s**: `%s`\n", kv[0], kv[1])
	}
	b.WriteString("\n## NOTICE\n\n```text\n")
	b.WriteString(strings.TrimRight(noticeText, "\n"))
	b.WriteString("\n```\n")
	return []byte(b.String())
}

// buildInfo is the Build section as key-value pairs, the same fields in
// the same order renderBuildInfo draws them.
func buildInfo() (kvs [][2]string) {
	rev, modified, revErr := vcs.GetVcsRevision()
	switch {
	case revErr != nil || rev == "":
		kvs = append(kvs, [2]string{"Revision", vcs.NoBuildInfo})
	case modified:
		kvs = append(kvs, [2]string{"Revision", rev + " (modified)"})
	default:
		kvs = append(kvs, [2]string{"Revision", rev + " (clean)"})
	}
	kvs = append(kvs, [2]string{"Module", vcs.ModuleInfo()})
	if ri, err := runinfo.Get(); err == nil {
		kvs = append(kvs,
			[2]string{"Go", ri.GoVersion},
			[2]string{"Host", ri.Hostname},
			[2]string{"PID", fmt.Sprintf("%d", ri.Pid)},
			[2]string{"Run ID", ri.RunId},
			[2]string{"Started", ri.StartedAt.Format(time.RFC3339)})
	} else {
		kvs = append(kvs, [2]string{"Run", "metadata unavailable (runinfo not initialised)"})
	}
	return
}

// newHelpFS roots the embedded help directory and wraps it.
func newHelpFS() (fsys fs.FS) {
	return aboutHelpFS{static: help.MustSub(helpFS, "help")}
}
