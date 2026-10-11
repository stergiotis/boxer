package projection

import (
	"encoding/json/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// The Go fold: the projection as the coordinator runs it in production
// (ADR-0308 §SD3). Its passes, in order, with the properties each
// declares (ADR-0309 §SD2):
//
//	sections   Reads section          Rewrites (latest per name wins)
//	compaction Reads compact          Rewrites (latest cut wins)
//	edits      Reads edit             Rewrites (latest per target wins)
//	messages   Reads user reply outcome note   AppendStable
//	tools      Reads tools            Rewrites on change
//
// The shape pass is Validate, which the caller runs over the batch. The
// SQL of sql.go is the same function on the engine; the two are held to
// one digest by law L4 in the test lanes.

// Order is the profile's declared order of prompt sections; a section
// not named follows the named ones, alphabetically. A name named twice
// is placed by its first mention (Distinct), in the fold and in the
// worker alike.
type Order []string

// Distinct is the order with every name once, at its first place.
func (inst Order) Distinct() (out Order) {
	seen := make(map[string]bool, len(inst))
	for _, n := range inst {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return
}

// Fold projects entries (any order; sorted by ordinal here) under the
// section order.
func Fold(entries []agentrun.Entry, order Order) (b Batch) {
	order = order.Distinct()
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })

	// sections: latest per name; a cleared section is one whose latest
	// body is empty.
	type section struct {
		entry agentrun.Entry
	}
	sections := make(map[string]section)
	for _, e := range es {
		if e.Kind == agentrun.KindSection {
			sections[e.Name] = section{entry: e}
		}
	}
	names := make([]string, 0, len(sections))
	for name, s := range sections {
		if s.entry.Body != "" {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(x, y string) int {
		px, py := position(order, x), position(order, y)
		if px != py {
			return px - py
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	})
	if len(names) > 0 {
		head := Message{Part: PartHead, Role: RoleSystem}
		parts := make([]string, 0, len(names))
		for _, name := range names {
			e := sections[name].entry
			parts = append(parts, e.Body)
			head.Sources = append(head.Sources, e.Ordinal)
			head.Label = head.Label.Join(e.Label)
		}
		head.Content = joinSections(parts)
		b.Messages = append(b.Messages, head)
		b.Sections = names
	}

	// compaction: the latest cut and its summary. The summary is model
	// output (or derived from it) re-entering, so it comes back in band at
	// the model's level (ADR-0316 §SD3, laws L16 and L17).
	var cut uint32
	var latest *agentrun.Entry
	for i := range es {
		if es[i].Kind == agentrun.KindCompact {
			latest = &es[i]
		}
	}
	if latest != nil {
		cut = latest.Target
		b.Messages = append(b.Messages, summaryMessage(*latest))
	}

	// edits: latest per target.
	edits := make(map[uint32]agentrun.Entry)
	for _, e := range es {
		if e.Kind == agentrun.KindEdit {
			edits[e.Target] = e
		}
	}

	// messages from the cut on, in ordinal order.
	for _, e := range es {
		if e.Ordinal < cut {
			continue
		}
		var role string
		switch e.Kind {
		case agentrun.KindUser:
			role = RoleUser
		case agentrun.KindReply:
			role = RoleAssistant
		case agentrun.KindOutcome:
			// A host act's outcome, the summariser's, has no message role
			// and is not a message.
			if e.Role != RoleTool {
				continue
			}
			role = RoleTool
		case agentrun.KindNote:
			role = RoleSystem
		default:
			continue
		}
		m := Message{Part: PartBody, Ordinal: e.Ordinal, Role: role, Content: e.Body, Calls: e.Calls, Key: e.Key, Attachments: e.Attachments,
			Sources: []uint32{e.Ordinal}, Label: e.Label}
		if e.Kind == agentrun.KindOutcome {
			// The provider's id for the call the result answers; the
			// dispatcher's key is the entry's Key (ADR-0277 §SD6).
			m.ToolCallId = e.Name
		}
		writerRun, writerOrdinal := e.WriterRun, e.WriterOrdinal
		if ed, edited := edits[e.Ordinal]; edited {
			if ed.Form == agentrun.FormDrop {
				continue
			}
			// A replacement replaces the body and the attachments; a
			// replacement a model wrote re-enters as the model's.
			m.Content, m.Attachments = ed.Body, ed.Attachments
			m.Sources = append(m.Sources, ed.Ordinal)
			m.Label = m.Label.Join(ed.Label)
			if ed.WriterRun != "" {
				writerRun, writerOrdinal = ed.WriterRun, ed.WriterOrdinal
			}
		}
		if e.Kind != agentrun.KindReply && writerRun != "" {
			// Re-entry (ADR-0311 §SD4): a body a model run wrote comes
			// back at the model's level, in band.
			source, level := ReentrySource(e.Run, writerRun)
			m.Content = Recalled(source, writerRun, writerOrdinal, m.Content)
			if m.Label.Level < level {
				m.Label.Level = level
			}
		}
		b.Messages = append(b.Messages, m)
	}

	// tools: the latest declaration.
	for _, e := range es {
		if e.Kind == agentrun.KindTools {
			b.Tools = toolNames(e.Body)
		}
	}
	return
}

// joinSections joins the live sections as the prompt book does: a blank
// line between them.
func joinSections(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n\n"
		}
		out += p
	}
	return out
}

func position(order Order, name string) int {
	for i, n := range order {
		if n == name {
			return i
		}
	}
	return len(order)
}

// toolNames reads a tools entry's body: a JSON list of names.
func toolNames(body string) (names []string) {
	if body == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(body), &names)
	return
}

// ToolsBody is a tools entry's body for the names.
func ToolsBody(names []string) string {
	b, _ := json.Marshal(names)
	return string(b)
}

// callIds reads the ids out of a reply's tool calls JSON.
func callIds(calls string) (ids []string) {
	if calls == "" {
		return
	}
	var list []struct {
		Id string `json:"Id"`
	}
	if json.Unmarshal([]byte(calls), &list) != nil {
		return
	}
	for _, c := range list {
		ids = append(ids, c.Id)
	}
	return
}

// ReentrySource says whose output a body is when it re-enters: the
// reader's own run or another's, and the level it enters at (law L16).
func ReentrySource(readerRun string, writerRun string) (source string, level agentrun.LevelE) {
	if writerRun == readerRun {
		return "self", agentrun.LevelOwnModel
	}
	return "other", agentrun.LevelOtherModel
}

// summaryMessage is a compact entry as the request carries it: the
// summary in band as the run's own earlier output, at the model's level
// at least.
func summaryMessage(e agentrun.Entry) (m Message) {
	m = Message{Part: PartSummary, Ordinal: e.Ordinal, Role: RoleSystem, Content: Recalled("self", e.Run, e.Ordinal, e.Body),
		Sources: []uint32{e.Ordinal}, Label: e.Label}
	if m.Label.Level < agentrun.LevelOwnModel {
		m.Label.Level = agentrun.LevelOwnModel
	}
	return
}

// Recalled is the band a re-entering body is wrapped in (law L17): the
// model is told this is a model's earlier output, not an instruction. The
// body cannot close the band early and the run cannot leave the
// attribute: both are escaped, the same way the SQL escapes them.
func Recalled(source string, run string, ordinal uint32, body string) string {
	return "<recalled source=\"" + source + "\" run=\"" + escapeAttribute(run) + "\" ord=\"" + strconv.FormatUint(uint64(ordinal), 10) + "\">\n" + escapeBand(body) + "\n</recalled>"
}

// escapeBand defuses a closing tag inside a band's body, whatever its
// case; nothing else in the body changes. The SQL applies the same
// pattern (replaceRegexpAll, case-insensitive).
func escapeBand(body string) string {
	return closingTag.ReplaceAllLiteralString(body, "&lt;/recalled")
}

var closingTag = regexp.MustCompile(`(?i)</recalled`)

// escapeAttribute is the minimal escape for an attribute value in the
// band, in the order the SQL applies it.
func escapeAttribute(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, "\"", "&quot;")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	return v
}
