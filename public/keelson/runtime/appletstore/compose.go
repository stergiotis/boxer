package appletstore

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// ComposeAppletDoc renders the ADR-0132 §SD1 document for a buffer: the
// documentation-standard frontmatter, a provenance line, and the buffer as
// the single sql fence (O4-D5). It is the authoring half of the store
// contract — the shape [SaveRequest.Doc] must carry, and the exact shape the
// store's gate ([sqlapplet] ParseDocSource) parses back — so it lives beside
// the wire types rather than in any one authoring app.
//
// endpoint is the frontmatter `endpoint` value ("" omits the key — the
// env-configured default; "introspection" pins the in-process endpoint the
// buffer was authored against). Title is required; a buffer containing a
// fence-opening line is refused rather than silently producing a document
// that re-parses differently.
func ComposeAppletDoc(title string, icon string, endpoint string, sql string) (doc []byte, err error) {
	if strings.TrimSpace(title) == "" {
		err = eh.Errorf("appletstore: applet title is required")
		return
	}
	sql = strings.TrimSpace(sql)
	if sql == "" {
		err = eh.Errorf("appletstore: the buffer is empty")
		return
	}
	for line := range strings.SplitSeq(sql, "\n") {
		if strings.HasPrefix(line, "```") {
			err = eh.Errorf("appletstore: the buffer contains a fence line (```); it cannot be embedded in an applet document")
			return
		}
	}
	var b strings.Builder
	b.WriteString("---\ntype: reference\naudience: end-user\nstatus: draft\ntitle: ")
	b.WriteString(strconv.Quote(title))
	b.WriteString("\n")
	if icon != "" {
		b.WriteString("icon: ")
		b.WriteString(strconv.Quote(icon))
		b.WriteString("\n")
	}
	if endpoint != "" {
		b.WriteString("endpoint: ")
		b.WriteString(strconv.Quote(endpoint))
		b.WriteString("\n")
	}
	b.WriteString("---\n\n# ")
	b.WriteString(title)
	b.WriteString("\n\nAuthored with the SQL applet creator (ADR-0132 O4).\n\n```sql\n")
	b.WriteString(sql)
	b.WriteString("\n```\n")
	doc = []byte(b.String())
	return
}
