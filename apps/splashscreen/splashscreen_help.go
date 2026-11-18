package splashscreen

import (
	"fmt"
	"io/fs"
	"strings"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// The help book (ADR-0047, keelson-app A4): one page, the window's tabs as
// prose. The static part is help/overview.md; each credited party's
// identity, licence and NOTICE, and the Build fields, are appended when the
// page is opened, so the page says what the tabs say rather than a copy
// that goes stale. Only the overview is rendered this way; every other name
// is the embedded directory's.

// helpOverview is the page the tabs' content is appended to.
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
	return fstest.MapFS{helpOverview: &fstest.MapFile{Data: helpPage(src), ModTime: time.Now()}}.Open(helpOverview)
}

// helpPage appends the tabs' content to the static page: one section per
// party, in tab order, with the identity lines its About page draws and its
// NOTICE in a fenced block so file paths stay verbatim; then the Build
// fields.
func helpPage(static []byte) (page []byte) {
	var b strings.Builder
	b.Write(static)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	for _, p := range parties() {
		fmt.Fprintf(&b, "\n## %s\n\n", p.name)
		for _, line := range p.identityLines() {
			fmt.Fprintf(&b, "- %s\n", line)
		}
		if p.notice != "" {
			fmt.Fprintf(&b, "\n### NOTICE of %s\n\n```text\n", p.name)
			b.WriteString(strings.TrimRight(p.notice, "\n"))
			b.WriteString("\n```\n")
		}
	}
	b.WriteString("\n## Build\n\n")
	for _, kv := range buildInfo() {
		fmt.Fprintf(&b, "- **%s**: `%s`\n", kv[0], kv[1])
	}
	return []byte(b.String())
}

// newHelpFS roots the embedded help directory and wraps it.
func newHelpFS() (fsys fs.FS) {
	return aboutHelpFS{static: help.MustSub(helpFS, "help")}
}
