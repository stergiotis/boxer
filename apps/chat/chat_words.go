package chat

import (
	"regexp"
	"strings"
)

// The transcript's line for a tool call says what was done in the reader's
// words (ADR-0404 §SD7). The coordinator writes its activity line with the
// tool's and the operation's names, and that line is what a resume reads
// back and what the transcript's export carries; the window rewrites it
// only where it draws it, and keeps it whole in the line's hover.

// toolWords are the chat's own tools as a line says what they did.
var toolWords = map[string]string{
	"artefact_write":        "wrote the artefact",
	"artefact_edit":         "edited the artefact",
	"artefact_read":         "read the artefact",
	"artefact_view_image":   "looked at an image",
	"artefact_crop_image":   "cut an image",
	"artefact_copy_image":   "copied an image",
	"artefact_remove_image": "removed an image",
	"artefact_capture":      "took a screenshot",
	"ask_user":              "asked you",
	"request_access":        "asked for access",
	"open_window":           "opened a window",
	"make_link":             "made a link",
	"read_link":             "read a link",
	"query_windows":         "read the windows",
	"call_operation":        "worked in a window",
	"point_out":             "pointed something out",
}

// phaseWords are a call's outcomes as a line says them.
var phaseWords = map[string]string{
	"rendered":  "done",
	"completed": "done",
	"accepted":  "queued",
	"denied":    "declined",
}

var (
	// opInWindow is an operation's name where a line says which window it
	// ran in.
	opInWindow = regexp.MustCompile(`\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+) in window\b`)
	// toolPrefix is a tool's name at the head of a segment, before what it
	// did or why it did not.
	toolPrefix = regexp.MustCompile(`^([a-z][a-z0-9]*(?:_[a-z0-9]+)+)(:.*)?$`)
	// taskIdRe is a task named by its id where the line says a task was
	// granted; the id is in the state chip's inspector.
	taskIdRe = regexp.MustCompile(`\bfor task task-[0-9a-f]+`)
	// keelsonTable is a host table read by name.
	keelsonTable = regexp.MustCompile(`row\(s\) of keelson\('([a-z_]+)'\)`)
)

// readerLine is an activity line in the reader's words.
func readerLine(text string) string {
	parts := strings.Split(text, " · ")
	for i, p := range parts {
		if w, ok := phaseWords[p]; ok && i > 0 {
			parts[i] = w
			continue
		}
		if m := toolPrefix.FindStringSubmatch(p); m != nil {
			w, ok := toolWords[m[1]]
			if !ok {
				w = strings.ReplaceAll(m[1], "_", " ")
			}
			parts[i] = w + m[2]
			continue
		}
		p = taskIdRe.ReplaceAllString(p, "for a task")
		p = opInWindow.ReplaceAllStringFunc(p, func(s string) string {
			return strings.ReplaceAll(s, "_", " ")
		})
		parts[i] = keelsonTable.ReplaceAllStringFunc(p, func(s string) string {
			table := keelsonTable.FindStringSubmatch(s)[1]
			return "rows of the host's " + strings.ReplaceAll(table, "_", " ") + " table"
		})
	}
	return strings.Join(parts, " · ")
}
