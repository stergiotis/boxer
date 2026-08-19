package marshallgen

import (
	"fmt"
	"strings"
)

// Text-emission primitives shared by every writer in this package.
//
// EmitPlan runs the assembled source through go/format, which recomputes all
// indentation, so the depth passed to line / linef only needs to be
// structurally faithful: it keeps the pre-format text readable and replaces
// the fragile hand-counted "\t\t\t" string prefixes. A wrong depth cannot
// change the final emitted output.

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+32) + s[1:]
	}
	return s
}

//
// EmitPlan runs the assembled source through go/format, which recomputes
// all indentation, so the depth passed to line / linef only needs to be
// structurally faithful: it keeps the pre-format text readable and
// replaces the fragile hand-counted "\t\t\t" string prefixes. A wrong
// depth cannot change the final emitted output.

// line writes s indented by depth tabs, followed by a newline.
func line(sb *strings.Builder, depth int, s string) {
	writeIndent(sb, depth)
	sb.WriteString(s)
	sb.WriteByte('\n')
}

// linef is line with a printf-style format string.
func linef(sb *strings.Builder, depth int, format string, a ...any) {
	writeIndent(sb, depth)
	fmt.Fprintf(sb, format, a...)
	sb.WriteByte('\n')
}

// blank writes a single empty line.
func blank(sb *strings.Builder) { sb.WriteByte('\n') }

func writeIndent(sb *strings.Builder, depth int) {
	for range depth {
		sb.WriteByte('\t')
	}
}
