package agent

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
)

// Help: the inline help apps ship (app.Manifest.Help), for a coordinator's
// model. It is the apps' own documentation, so it is not untrusted content,
// and it needs no grant, like describe.

// HelpMaxBytes bounds the text one help read returns; a longer section is
// cut, and the reply names its subsections to read instead.
const HelpMaxBytes = 12 << 10

// helpMaxHits bounds a help search.
const helpMaxHits = 10

type wireHelpRequest struct {
	V       uint8  `json:"v"`
	App     string `json:"app,omitempty"`
	Doc     string `json:"doc,omitempty"`
	Section string `json:"section,omitempty"`
	Search  string `json:"search,omitempty"`
}

type wireHelpSection struct {
	Slug    string `json:"slug"`
	Heading string `json:"heading"`
	Level   uint8  `json:"level"`
}

type wireHelpDoc struct {
	App      string            `json:"app"`
	Doc      string            `json:"doc"`
	Title    string            `json:"title"`
	Type     string            `json:"type,omitempty"`
	Sections []wireHelpSection `json:"sections,omitempty"`
}

type wireHelpHit struct {
	App     string `json:"app"`
	Doc     string `json:"doc"`
	Section string `json:"section,omitempty"`
	Title   string `json:"title"`
	Heading string `json:"heading,omitempty"`
	Context string `json:"context,omitempty"`
}

type wireHelpReply struct {
	V      uint8         `json:"v"`
	Ok     bool          `json:"ok"`
	Reason string        `json:"reason,omitempty"`
	Docs   []wireHelpDoc `json:"docs,omitempty"`
	Hits   []wireHelpHit `json:"hits,omitempty"`
	// Text is the section read, as markdown; Truncated says it was cut at
	// HelpMaxBytes, and Sections then lists its subsections.
	Text      string            `json:"text,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
	Sections  []wireHelpSection `json:"sections,omitempty"`
}

// HelpRequest asks for help. With Search, sections matching it across the
// apps; with App, that app's documents and their sections; with App and
// Doc, the document's text, or with Section that section's.
type HelpRequest struct {
	App     string
	Doc     string
	Section string
	Search  string
}

// HelpSection is one heading of a help document.
type HelpSection struct {
	Slug    string
	Heading string
	Level   uint8
}

// HelpDoc is one help document of an app.
type HelpDoc struct {
	App      string
	Doc      string
	Title    string
	Type     string
	Sections []HelpSection
}

// HelpHit is one section a help search found.
type HelpHit struct {
	App     string
	Doc     string
	Section string
	Title   string
	Heading string
	Context string
}

// HelpReply is what a help request returns: documents, hits, or a text.
type HelpReply struct {
	Docs      []HelpDoc
	Hits      []HelpHit
	Text      string
	Truncated bool
	Sections  []HelpSection
}

// Help reads the apps' inline help.
func (inst *Client) Help(ctx context.Context, r HelpRequest) (out HelpReply, err error) {
	rep, err := roundTrip[wireHelpRequest, wireHelpReply](ctx, inst, SubjectHelp,
		wireHelpRequest{V: wireVersion, App: r.App, Doc: r.Doc, Section: r.Section, Search: r.Search})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	for _, d := range rep.Docs {
		hd := HelpDoc{App: d.App, Doc: d.Doc, Title: d.Title, Type: d.Type}
		for _, s := range d.Sections {
			hd.Sections = append(hd.Sections, HelpSection(s))
		}
		out.Docs = append(out.Docs, hd)
	}
	for _, h := range rep.Hits {
		out.Hits = append(out.Hits, HelpHit(h))
	}
	out.Text, out.Truncated = rep.Text, rep.Truncated
	for _, s := range rep.Sections {
		out.Sections = append(out.Sections, HelpSection(s))
	}
	return
}

// helpBooks are the help books of the apps describe would list: those with
// a catalog that the launch limit admits, and help.
func (inst *Service) helpBooks(appName string) (books []help.BookI) {
	inst.helpMu.Lock()
	defer inst.helpMu.Unlock()
	for _, r := range inst.cfg.Registry.Registrations() {
		m := r.Manifest
		if m.Help == nil || m.Operations == nil || !inst.cfg.Registry.Launchable(m.Id) {
			continue
		}
		if appName != "" && !matchesApp(m, appName) {
			continue
		}
		b, ok := inst.helpCache[m.Id]
		if !ok {
			var err error
			if b, err = help.NewBook(m.Id, m.Help); err != nil {
				continue
			}
			inst.helpCache[m.Id] = b
		}
		books = append(books, b)
	}
	return
}

func (inst *Service) help(msg *app.Msg) (rep wireHelpReply) {
	rep.V = wireVersion
	req, err := decode[wireHelpRequest](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if (req.Doc != "" || req.Section != "") && req.App == "" {
		rep.Reason = "naming a document needs the app"
		return
	}
	books := inst.helpBooks(req.App)
	if len(books) == 0 {
		if req.App != "" {
			rep.Reason = "no help for that app; describe_app says what it does"
		} else {
			rep.Reason = "no app ships help"
		}
		return
	}
	switch {
	case strings.TrimSpace(req.Search) != "" && req.Doc == "":
		for _, h := range search.NewIndexBooks(books...).Search(search.ParseQuery(req.Search), helpMaxHits) {
			rep.Hits = append(rep.Hits, wireHelpHit{App: string(h.Ref.AppId), Doc: h.Ref.Doc, Section: h.Ref.Section,
				Title: h.DocTitle, Heading: h.Heading, Context: h.Context})
		}
		if len(rep.Hits) == 0 {
			rep.Reason = "no help section matches; list an app's documents with app alone"
			return
		}
	case req.Doc == "":
		for _, b := range books {
			for _, d := range b.Docs() {
				rep.Docs = append(rep.Docs, wireHelpDoc{App: string(b.AppId()), Doc: d.Path, Title: d.Title, Type: d.Type,
					Sections: helpSections(d.Sections, 0, len(d.Sections), 2)})
			}
		}
	default:
		if !inst.readHelp(books[0], req.Doc, req.Section, &rep) {
			return
		}
	}
	rep.Ok = true
	return
}

// readHelp puts a document's or a section's text in rep: a section runs to
// the next heading at its level or above, subsections included.
func (inst *Service) readHelp(b help.BookI, docPath string, section string, rep *wireHelpReply) (ok bool) {
	src, found := b.Source(docPath)
	_, info, parsed := b.Doc(docPath)
	if !found || !parsed {
		rep.Reason = "no help document " + docPath + "; list the app's documents with app alone"
		return
	}
	start, end, from, to := frontmatterEnd(src), len(src), 0, len(info.Sections)
	if section != "" {
		i := -1
		for j, s := range info.Sections {
			if s.Slug == section {
				i = j
				break
			}
		}
		if i < 0 {
			rep.Reason = "no section " + section + " in " + docPath + "; its sections are listed with app alone"
			return
		}
		start, from, to = info.Sections[i].ByteOffset, i+1, len(info.Sections)
		for j := i + 1; j < len(info.Sections); j++ {
			if info.Sections[j].Level <= info.Sections[i].Level {
				end, to = info.Sections[j].ByteOffset, j
				break
			}
		}
	}
	start, end = min(max(start, 0), len(src)), min(max(end, 0), len(src))
	if end < start {
		end = start
	}
	text := string(src[start:end])
	if len(text) > HelpMaxBytes {
		cut := strings.LastIndexByte(text[:HelpMaxBytes], '\n')
		if cut <= 0 {
			cut = HelpMaxBytes
		}
		text, rep.Truncated = text[:cut], true
		rep.Sections = helpSections(info.Sections, from, to, 0)
	}
	rep.Text = text
	return true
}

// helpSections are the headings in secs[from:to], those at or above
// maxLevel when it is not zero.
func helpSections(secs []help.SectionInfo, from int, to int, maxLevel uint8) (out []wireHelpSection) {
	for _, s := range secs[from:to] {
		if maxLevel != 0 && s.Level > maxLevel {
			continue
		}
		out = append(out, wireHelpSection{Slug: s.Slug, Heading: s.Text, Level: s.Level})
	}
	return
}

// frontmatterEnd is the offset after a leading `---` frontmatter block, or
// zero.
func frontmatterEnd(src []byte) (off int) {
	s := string(src)
	if !strings.HasPrefix(s, "---\n") {
		return
	}
	if i := strings.Index(s[4:], "\n---\n"); i >= 0 {
		off = 4 + i + len("\n---\n")
	}
	return
}
