package play

// play_ops_docs.go: lookup_docs, the Docs pane's corpus for an agent
// (ADR-0270, update of 2026-10-05). An external read: the window's
// documentation source answers one name at once through DocsLookupNowI,
// never through the pane's lane, whose single slot and memo belong to the
// person's reading. With no name it answers for what the pane shows, and
// says so; the pane itself does not move.

import (
	"context"
	"errors"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opLookupDocs = "lookup_docs"

// Bounds on lookup_docs.
const (
	docsOpsMaxBlocks    = 8
	docsOpsMaxBlockText = 2 << 10
	docsOpsMaxCut       = 20
)

// docsOpsView is what lookup_docs reads of the window: its source, and what
// the Docs pane shows.
type docsOpsView struct {
	source    DocsSourceI
	shown     string
	shownKind string
	follow    bool
	miss      string
}

func snapshotDocs(p *PlayApp) (v docsOpsView) {
	if p.docs != nil {
		v.source = p.docs.source
	}
	if s := p.docsPane; s != nil {
		v.shown, v.shownKind, v.follow, v.miss = s.shown, s.shownKind, s.follow, s.lastMiss
	}
	return
}

// DocsArgs is lookup_docs' argument.
type DocsArgs struct {
	Name string `json:",omitzero" desc:"a function, data type, table engine, format, setting or SQL user-defined function name; when left out, what the Docs pane shows"`
	Kind string `json:",omitzero" desc:"which of the name's kinds to read (kinds lists them); the first, the exact-case match, when left out"`
}

// DocsReading is lookup_docs' result.
type DocsReading struct {
	Name        string   `desc:"the name looked up"`
	FromPane    bool     `json:",omitzero" desc:"true when no name was given and the name is what the Docs pane shows"`
	PaneFollows bool     `json:",omitzero" desc:"with from_pane, whether the pane follows the person's caret"`
	Found       bool     `desc:"false when the source documents nothing by that name"`
	Kinds       []string `json:",omitzero" desc:"every kind the name carries; kind picks one"`
	Kind        string   `json:",omitzero" desc:"the kind read"`
	Body        string   `json:",omitzero" desc:"the entry as markdown, cut at 6 KiB"`
	Source      string   `json:",omitzero" desc:"where the entry is defined, when the source says"`
	Sql         []string `json:",omitzero" desc:"the body's SQL blocks, each ready for set_sql; at most 8, each cut at 2 KiB"`
	Truncated   bool     `json:",omitzero" desc:"true when the body was cut"`
	CutSections []string `json:",omitzero" desc:"the headings of the sections the cut left out"`
	PaneMiss    string   `json:",omitzero" desc:"with from_pane, the last name the pane found nothing for"`
}

func addDocsOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: the user-defined half quotes create_query, which anyone with
	// DDL on the endpoint wrote.
	appops.ExternalRead(s, app.OperationSpec{Name: opLookupDocs, Version: 1,
		Summary: "read a name's documentation from the endpoint: built-in functions, types, engines, formats and settings, and the bodies of its SQL user-defined functions",
		Agents:  true, Untrusted: true,
		Follows: []string{"the Docs pane is unchanged; set_sql with a block of sql to use an example"}},
		func(sn opsSnap, call app.OperationCall, in DocsArgs) (DocsReading, error) {
			if !sn.mounted {
				return DocsReading{}, app.RefuseOperation("the window has not mounted")
			}
			return lookupDocs(sn.docs, call.OnBehalfOf, in)
		})
}

// lookupDocs answers one name through the source's DocsLookupNowI.
func lookupDocs(v docsOpsView, obo *app.OnBehalfOf, in DocsArgs) (out DocsReading, err error) {
	if v.source == nil {
		return out, app.RefuseOperation("the window has no documentation source")
	}
	now, ok := v.source.(DocsLookupNowI)
	if !ok {
		return out, app.RefuseOperation("this window's documentation source does not answer agents")
	}
	name := strings.TrimSpace(in.Name)
	kind := strings.TrimSpace(in.Kind)
	if name == "" {
		if v.shown == "" {
			return out, app.RefuseOperation("the Docs pane shows nothing; name what to look up")
		}
		name, out.FromPane, out.PaneFollows, out.PaneMiss = v.shown, true, v.follow, v.miss
		if kind == "" {
			kind = v.shownKind
		}
	}
	if len(name) > 256 {
		return out, app.RefuseOperation("a name is at most 256 bytes")
	}
	out.Name = name
	entries, err := now.LookupNow(context.Background(), name, obo)
	if err != nil {
		var refusal *app.OperationRefusal
		if errors.As(err, &refusal) {
			return out, err
		}
		if limit, isLimit := asAgentLimit(err); isLimit {
			if limit.Destination != "" {
				return out, app.RefuseForDestinations(limit.Error(), limit.Destination)
			}
			return out, app.RefuseOperation(limit.Error())
		}
		return out, app.RefuseOperation(v.source.ExplainError(err))
	}
	if len(entries) == 0 {
		return out, nil
	}
	out.Found = true
	pick := 0
	for i, e := range entries {
		out.Kinds = append(out.Kinds, e.Kind)
		if kind != "" && strings.EqualFold(e.Kind, kind) && (pick == 0 || !strings.EqualFold(entries[pick].Kind, kind)) {
			pick = i
		}
	}
	if kind != "" && !strings.EqualFold(entries[pick].Kind, kind) {
		return out, app.RefuseOperation(name + " has no kind " + kind + "; it has " + strings.Join(out.Kinds, ", "))
	}
	e := entries[pick]
	out.Kind, out.Source = e.Kind, e.Source
	body := v.source.AbsolutiseLinks(e.Body)
	for i, b := range sqlBlocks(body) {
		if i == docsOpsMaxBlocks {
			break
		}
		out.Sql = append(out.Sql, truncateBytes(b, docsOpsMaxBlockText))
	}
	out.Body = body
	if len(body) > refMaxText {
		out.Body, out.Truncated = truncateBytes(body, refMaxText), true
		out.CutSections = markdownHeadings(body[len(out.Body):], docsOpsMaxCut)
	}
	return out, nil
}

// markdownHeadings lists the ATX headings of md, at most limit of them,
// skipping fenced blocks.
func markdownHeadings(md string, limit int) (out []string) {
	fenced := false
	for line := range strings.SplitSeq(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.HasPrefix(t, "#") {
			continue
		}
		h := strings.TrimSpace(strings.TrimLeft(t, "#"))
		if h == "" {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, truncateRunes(h, 120))
	}
	return
}
