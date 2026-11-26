// Package mdeditlink is the `mdedit://` link (ADR-0347): the one
// string that names a document in a lading snapshot, a place in it and the
// view mode to show it in, and the one way every app follows it — a launch
// request for a new mdedit window.
//
//	mdedit://<mount>/<snapshot>/<path>[?mode=<mode>][#<fragment>]
//
// The mount and the snapshot are spelled as the lading SFTP tree spells
// them (sftptree), so the link's authority and path are the file's path in
// that tree. The fragment names a location (ParseLocation): a heading slug,
// `^id` for an Obsidian block id, or `L<n>` for a line. The scheme is an
// alias of mdedit's launch kind (ADR-0409 §SD3): [LinkCodec]
// spells the launch config, and the host follows the link as it follows
// every `boxer:` link; a surface claims it with applink.Claimed.
//
// It is a leaf beside launchcfg so a caller does not import the app and its
// registering init(). Parse is strict, as docref's is: links are written
// into rows and messages, and a lenient parser would let a malformed
// producer go unnoticed until its links stop opening.
package mdeditlink

import (
	"io/fs"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp/sftptree"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Scheme is the URL scheme.
const Scheme = "mdedit"

// Prefix is what a link starts with.
const Prefix = Scheme + "://"

// ModeE is the view mode a link asks for.
type ModeE uint8

const (
	// ModeSplit is the editing layout, and what a link without a mode means.
	ModeSplit ModeE = iota
	ModeSource
	ModeRead
	ModeBlocks
)

// modeNames are the `mode=` values and the launch config's Mode strings.
var modeNames = [...]string{ModeSplit: "split", ModeSource: "source", ModeRead: "read", ModeBlocks: "blocks"}

// String is the mode's name in a link and in the launch config.
func (inst ModeE) String() string {
	if int(inst) < len(modeNames) {
		return modeNames[inst]
	}
	return "split"
}

// ParseMode reads a mode name; empty is Split. Unknown names are refused.
func ParseMode(s string) (m ModeE, ok bool) {
	if s == "" {
		return ModeSplit, true
	}
	for i, n := range modeNames {
		if n == s {
			return ModeE(i), true
		}
	}
	return ModeSplit, false
}

// Ref is a parsed link.
type Ref struct {
	Mount identifier.TaggedId
	Snap  time.Time
	// Path is the document's path in the snapshot, decoded.
	Path string
	Mode ModeE
	// Fragment is the location as written, "" for the top; see
	// [ParseLocation].
	Fragment string
}

// Format renders the link. The path is percent-encoded per segment; the
// fragment escapes only what a link cannot carry as it is — a space, '#',
// '%', a control byte and every byte of a non-ASCII letter — so `#^id` and
// a plain slug read as Obsidian writes them, and a slug of `## Überblick`
// or `## C# notes` still opens.
func Format(r Ref) (s string) {
	var b strings.Builder
	b.WriteString(Prefix)
	b.WriteString(sftptree.MountName(r.Mount))
	b.WriteByte('/')
	b.WriteString(sftptree.SnapshotName(r.Snap))
	for _, seg := range strings.Split(strings.TrimPrefix(r.Path, "/"), "/") {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	if r.Mode != ModeSplit {
		b.WriteString("?mode=")
		b.WriteString(r.Mode.String())
	}
	if r.Fragment != "" {
		b.WriteByte('#')
		b.WriteString(escapeFragment(r.Fragment))
	}
	return b.String()
}

// escapeFragment percent-encodes the bytes of a fragment a link cannot
// carry as they are; url.Parse decodes them back.
func escapeFragment(f string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c > 0x20 && c < 0x7f && c != '#' && c != '%' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0xf])
	}
	return b.String()
}

// String is [Format].
func (inst Ref) String() string { return Format(inst) }

// Claimed reports whether a link is one of ours — the predicate a markdown
// surface passes as its link claim.
func Claimed(link string) bool {
	return strings.HasPrefix(link, Prefix)
}

// Parse reads a link. It refuses anything but the shape above: another
// scheme, a mount or snapshot name the SFTP tree would not spell, a path
// that is not a valid fs.FS path (empty, a leading slash, an empty or `.`
// or `..` segment — what the snapshot read would refuse later, with a
// worse message), an unknown mode, or a query key it does not know.
func Parse(link string) (r Ref, err error) {
	if !Claimed(link) {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: not an mdedit:// link")
		return
	}
	u, perr := url.Parse(link)
	if perr != nil {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: %w", perr)
		return
	}
	mount, ok := sftptree.ParseMountName(u.Host)
	if !ok {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: the mount is not a 16-digit hex id")
		return
	}
	snapName, docPath, found := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !found || docPath == "" || docPath == "." || !fs.ValidPath(docPath) {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: needs '<mount>/<snapshot>/<path>', the path a valid snapshot path")
		return
	}
	snap, ok := sftptree.ParseSnapshotName(snapName)
	if !ok {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: the snapshot is not a snapshot name")
		return
	}
	q, qerr := url.ParseQuery(u.RawQuery)
	if qerr != nil {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: %w", qerr)
		return
	}
	mode := ModeSplit
	for k, vs := range q {
		if k != "mode" || len(vs) != 1 {
			err = eb.Build().Str("link", link).Str("key", k).Errorf("mdeditlink: unknown query")
			return
		}
		if mode, ok = ParseMode(vs[0]); !ok {
			err = eb.Build().Str("link", link).Str("mode", vs[0]).Errorf("mdeditlink: unknown mode")
			return
		}
	}
	if !utf8.ValidString(u.Fragment) || !utf8.ValidString(docPath) {
		err = eb.Build().Str("link", link).Errorf("mdeditlink: an escape decodes to bytes that are not UTF-8")
		return
	}
	r = Ref{Mount: mount, Snap: snap, Path: docPath, Mode: mode, Fragment: u.Fragment}
	return
}

// LocationKindE is what a fragment names.
type LocationKindE uint8

const (
	// LocationTop is the empty fragment: the document as it opens.
	LocationTop LocationKindE = iota
	// LocationHeading is a heading slug (markdown.SlugHeading's form, or an
	// explicit `{#anchor}`).
	LocationHeading
	// LocationBlock is `^id`, an Obsidian block id.
	LocationBlock
	// LocationLine is `L<n>`, a 1-based line.
	LocationLine
)

// Location is a parsed fragment. Only the field of the active Kind is set.
type Location struct {
	Kind    LocationKindE
	Slug    string
	BlockId string
	Line    int
}

// ParseLocation reads a fragment. The forms are told apart by their first
// character: `^` opens a block id, a capital `L` followed by digits is a
// line, anything else is a heading slug — slugs are lower-case, so `L12`
// cannot be one. A `^` with no id or an `L` with a bad number is an error.
func ParseLocation(fragment string) (loc Location, err error) {
	switch {
	case fragment == "":
		return Location{Kind: LocationTop}, nil
	case fragment[0] == '^':
		if len(fragment) == 1 {
			err = eb.Build().Str("fragment", fragment).Errorf("mdeditlink: a block id needs an id after '^'")
			return
		}
		return Location{Kind: LocationBlock, BlockId: fragment[1:]}, nil
	case fragment[0] == 'L' && len(fragment) > 1 && isDigits(fragment[1:]):
		n, aerr := strconv.Atoi(fragment[1:])
		if aerr != nil || n < 1 {
			err = eb.Build().Str("fragment", fragment).Errorf("mdeditlink: a line is L followed by a number from 1")
			return
		}
		return Location{Kind: LocationLine, Line: n}, nil
	}
	return Location{Kind: LocationHeading, Slug: fragment}, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// FormatLocation is the fragment for a location; the inverse of
// [ParseLocation].
func FormatLocation(loc Location) string {
	switch loc.Kind {
	case LocationHeading:
		return loc.Slug
	case LocationBlock:
		return "^" + loc.BlockId
	case LocationLine:
		return "L" + strconv.Itoa(loc.Line)
	}
	return ""
}

// Launch is the launch config that opens a link: the snapshot document by
// name, with no text — the window shows the snapshot's when its read lands —
// in the link's mode, at its location.
func Launch(r Ref) launchcfg.MdeditLaunch {
	return launchcfg.MdeditLaunch{
		Name:      path.Base(r.Path),
		Mount:     r.Mount.Value(),
		SnapNanos: r.Snap.UnixNano(),
		Path:      r.Path,
		Mode:      r.Mode.String(),
		Location:  r.Fragment,
	}
}
