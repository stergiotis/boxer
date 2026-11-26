// Package applink is the `boxer:` link (ADR-0409 (proposed)): a link that
// carries an operation call, or a launch config, with the target it names.
// The host owns the envelope — the scheme, the target, the app or kind, the
// operation and its version — and the target app owns the tail, through
// the codec its operation or kind registers ([app.LinkCodecI]).
//
//	boxer:<verb>/app/<alias>[/<operation>@<version>[<tail>]]
//	boxer:open/kind/<kind>[<tail>]
//	boxer:l/<handle>
//
// The verb is the target: open a new window, or raise an open window of
// the app. A handle names a record the host keeps in memory; only a handle
// carries a grant or names one window. `boxer:clip/` is the clipboard
// broker's and not a link.
//
// The package is a leaf beside the host, as windowopen is: a surface claims
// and follows links with it without linking the window host. Parsing is
// strict, as docref's and mdeditlink's are: links are written into rows and
// messages, and a lenient parser hides a broken producer.
package applink

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Scheme is the URL scheme of every link the host follows.
const Scheme = "boxer"

// Prefix is what a `boxer:` link starts with.
const Prefix = Scheme + ":"

// ClipPrefix is the clipboard broker's handle form (ADR-0317 §SD3), which
// is not a link.
const ClipPrefix = Prefix + "clip/"

// MaxLinkBytes bounds a link before any codec sees it; a call too large to
// spell in it is a handle.
const MaxLinkBytes = 8192

// VerbE is the target a link names (ADR-0409 §SD4).
type VerbE uint8

const (
	// VerbOpen opens a new window.
	VerbOpen VerbE = iota + 1
	// VerbRaise raises an open window of the app, or opens one.
	VerbRaise
)

func (inst VerbE) String() (s string) {
	switch inst {
	case VerbOpen:
		return "open"
	case VerbRaise:
		return "raise"
	}
	return "unknown"
}

// FormE is which of the three forms a link has.
type FormE uint8

const (
	// FormApp addresses an app by its alias, with an optional call.
	FormApp FormE = iota + 1
	// FormKind addresses a launch kind, with an optional config.
	FormKind
	// FormHandle names a record the host keeps.
	FormHandle
)

func (inst FormE) String() (s string) {
	switch inst {
	case FormApp:
		return "app"
	case FormKind:
		return "kind"
	case FormHandle:
		return "handle"
	}
	return "unknown"
}

// Envelope is a parsed link: the host's part and the app's tail, unread.
type Envelope struct {
	Form FormE
	Verb VerbE
	// App is the app's subject alias (app.Manifest.SubjectAlias), for
	// FormApp.
	App string
	// Kind is the launch kind, for FormKind.
	Kind string
	// Operation and Version name the call, for FormApp; empty Operation is
	// a link that reaches the window and calls nothing.
	Operation string
	Version   uint16
	// Tail is the codec's part, as written: empty, or starting with '/',
	// '?' or '#'.
	Tail string
	// Handle is set for FormHandle.
	Handle Handle
}

// HasCall says the link carries a call.
func (inst Envelope) HasCall() bool {
	return inst.Form == FormApp && inst.Operation != ""
}

// Claimed reports whether the host follows a link: a `boxer:` link other
// than a clipboard handle, or a link of a registered alias scheme. It reads
// only the scheme, so a surface claims without any app's code, and it is a
// pure function of the URL, as a markdown link claim must be.
func Claimed(link string) bool {
	if strings.HasPrefix(link, Prefix) {
		return !strings.HasPrefix(link, ClipPrefix)
	}
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return false
	}
	_, ok = aliasKind(scheme)
	return ok
}

// Parse reads a `boxer:` link's envelope, or resolves an alias link to the
// envelope it stands for. It refuses a link over MaxLinkBytes, another
// scheme, a clipboard handle, an unknown verb or form, a malformed alias,
// operation, version, kind or handle, a kind with the raise verb, and a
// tail with bytes a URL cannot carry. The tail itself is the codec's to
// read.
func Parse(link string) (e Envelope, err error) {
	if len(link) > MaxLinkBytes {
		err = eb.Build().Int("bytes", len(link)).Int("max", MaxLinkBytes).Errorf("applink: the link is longer than a link may be; a call this large is a handle")
		return
	}
	if !strings.HasPrefix(link, Prefix) {
		return resolveAlias(link)
	}
	if strings.HasPrefix(link, ClipPrefix) {
		err = eb.Build().Errorf("applink: a boxer:clip/ handle is the clipboard's, not a link")
		return
	}
	rest := link[len(Prefix):]
	if h, ok := strings.CutPrefix(rest, "l/"); ok {
		e.Form = FormHandle
		e.Handle, err = ParseHandle(h)
		return
	}
	verbText, rest, ok := strings.Cut(rest, "/")
	if !ok {
		err = eb.Build().Str("link", clip(link)).Errorf("applink: a link is boxer:<verb>/app/… or boxer:open/kind/… or boxer:l/<handle>")
		return
	}
	switch verbText {
	case "open":
		e.Verb = VerbOpen
	case "raise":
		e.Verb = VerbRaise
	default:
		err = eb.Build().Str("verb", clip(verbText)).Errorf("applink: the verb is open or raise")
		return
	}
	formText, rest, ok := strings.Cut(rest, "/")
	if !ok {
		err = eb.Build().Str("link", clip(link)).Errorf("applink: the verb is followed by app/<alias> or kind/<kind>")
		return
	}
	switch formText {
	case "app":
		e.Form = FormApp
		err = parseApp(&e, rest)
	case "kind":
		e.Form = FormKind
		if e.Verb != VerbOpen {
			err = eb.Build().Errorf("applink: a kind is only opened: a config is delivered when a window mounts")
			return
		}
		err = parseKind(&e, rest)
	default:
		err = eb.Build().Str("form", clip(formText)).Errorf("applink: the target is app/<alias> or kind/<kind>")
	}
	if err != nil {
		return
	}
	err = checkTail(e.Tail)
	return
}

func parseApp(e *Envelope, rest string) (err error) {
	i := strings.IndexAny(rest, "/?#")
	if i < 0 {
		i = len(rest)
	}
	e.App = rest[:i]
	if !validAlias(e.App) {
		return eb.Build().Str("alias", clip(e.App)).Errorf("applink: the app is named by its alias, [A-Za-z0-9_-]+")
	}
	rest = rest[i:]
	if rest == "" {
		return
	}
	if rest[0] != '/' {
		return eb.Build().Errorf("applink: a tail follows an operation, never the app")
	}
	rest = rest[1:]
	i = strings.IndexAny(rest, "/?#")
	if i < 0 {
		i = len(rest)
	}
	op, ver, ok := strings.Cut(rest[:i], "@")
	if !ok {
		return eb.Build().Str("operation", clip(rest[:i])).Errorf("applink: an operation carries its version, <operation>@<version>")
	}
	if !validOperation(op) {
		return eb.Build().Str("operation", clip(op)).Errorf("applink: an operation name is lower snake_case")
	}
	v, ok := parseVersion(ver)
	if !ok {
		return eb.Build().Str("version", clip(ver)).Errorf("applink: a version is a number from 1 to 65535 with no leading zero")
	}
	e.Operation, e.Version, e.Tail = op, v, rest[i:]
	return
}

func parseKind(e *Envelope, rest string) (err error) {
	i := strings.IndexAny(rest, "/?#")
	if i < 0 {
		i = len(rest)
	}
	e.Kind, e.Tail = rest[:i], rest[i:]
	if !validKind(e.Kind) {
		return eb.Build().Str("kind", clip(e.Kind)).Errorf("applink: a kind is a vocabulary name, [A-Za-z][A-Za-z0-9]*")
	}
	return
}

// Format spells an envelope. It is the inverse of Parse for every envelope
// Parse returns; an envelope Parse would refuse formats to a link Parse
// refuses.
func Format(e Envelope) (link string) {
	var b strings.Builder
	b.WriteString(Prefix)
	switch e.Form {
	case FormHandle:
		b.WriteString("l/")
		b.WriteString(e.Handle.String())
		return b.String()
	case FormKind:
		b.WriteString(e.Verb.String())
		b.WriteString("/kind/")
		b.WriteString(e.Kind)
		b.WriteString(e.Tail)
		return b.String()
	}
	b.WriteString(e.Verb.String())
	b.WriteString("/app/")
	b.WriteString(e.App)
	if e.Operation != "" {
		b.WriteByte('/')
		b.WriteString(e.Operation)
		b.WriteByte('@')
		b.WriteString(strconv.FormatUint(uint64(e.Version), 10))
	}
	// A tail with no operation is written too, so the link is one Parse
	// refuses rather than one that drops the arguments.
	b.WriteString(e.Tail)
	return b.String()
}

// String is Format.
func (inst Envelope) String() string { return Format(inst) }

func validAlias(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validOperation(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validKind(s string) bool {
	if s == "" || !(s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func parseVersion(s string) (v uint16, ok bool) {
	if s == "" || s[0] == '0' || len(s) > 5 {
		return
	}
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil || n == 0 {
		return
	}
	return uint16(n), true
}

// checkTail refuses a tail a link cannot carry: one that does not start
// with '/', '?' or '#', a space, a control byte, text that is not UTF-8, or
// a broken percent escape. A letter outside ASCII is carried as written,
// as a link typed into a document has it; a codec's own spelling escapes
// it. What the tail means is the codec's.
func checkTail(tail string) (err error) {
	if tail == "" {
		return
	}
	if c := tail[0]; c != '/' && c != '?' && c != '#' {
		return eb.Build().Errorf("applink: a tail starts with '/', '?' or '#'")
	}
	if !utf8.ValidString(tail) {
		return eb.Build().Errorf("applink: a link is UTF-8 text")
	}
	for i := 0; i < len(tail); i++ {
		c := tail[i]
		if c <= 0x20 || c == 0x7f {
			return eb.Build().Int("offset", i).Errorf("applink: a link carries no space or control character; percent-encode it")
		}
		if c == '%' {
			if i+2 >= len(tail) || !isHex(tail[i+1]) || !isHex(tail[i+2]) {
				return eb.Build().Int("offset", i).Errorf("applink: a broken percent escape")
			}
		}
	}
	return
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// clip shortens text quoted back in an error.
func clip(s string) string {
	const n = 64
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
