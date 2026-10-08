package httpegress

import (
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Destination is what a destination name resolves to (ADR-0262 §SD2): the
// URLs it covers and the transport policy that reaches them.
type Destination struct {
	// Prefixes are the URLs a request may start with: scheme, host and a
	// path prefix, no userinfo, query or fragment. Scheme and host compare
	// exactly; the path is a plain string prefix, so end it with "/" to
	// mean a directory. [TilePrefixes] derives them from a tile template.
	Prefixes []string
	// CAFile is a PEM bundle to trust instead of the system roots.
	// Verification stays on.
	CAFile string
	// InsecureTLS disables certificate verification, and with it the
	// protocol floor and the modern-only cipher list — see tlsConfig.
	InsecureTLS bool
	// UserAgent is sent on every request; empty sends Go's default.
	UserAgent string
	// Timeout bounds one fetch; zero is DefaultTimeout.
	Timeout time.Duration
	// MaxBodyBytes caps a reply body; zero is DefaultMaxBodyBytes.
	MaxBodyBytes int64
}

// DestinationSpec registers a destination: a name and how to resolve it.
// Resolve runs once, when the host's service starts, so a destination can
// read the environment registry (ADR-0009).
type DestinationSpec struct {
	// Name is the subject token, [a-z0-9_-]+.
	Name string
	// Description says what the destination is for, for the
	// http_destinations table.
	Description string
	Resolve     func() (d Destination, err error)
}

var destinationNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ValidDestinationName says name can be a subject token.
func ValidDestinationName(name string) (ok bool) { return destinationNameRe.MatchString(name) }

var registry struct {
	mu    sync.Mutex
	specs []DestinationSpec
}

// Register adds a destination to the host-wide registry. Called from a
// package's init; a bad or duplicate name is a programming error and
// panics, as the environment registry does.
func Register(spec DestinationSpec) {
	if !ValidDestinationName(spec.Name) {
		panic("httpegress: invalid destination name " + spec.Name)
	}
	if spec.Resolve == nil {
		panic("httpegress: destination " + spec.Name + " has no resolve function")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, s := range registry.specs {
		if s.Name == spec.Name {
			panic("httpegress: destination registered twice: " + spec.Name)
		}
	}
	registry.specs = append(registry.specs, spec)
}

// Registered returns the registry, sorted by name.
func Registered() (specs []DestinationSpec) {
	registry.mu.Lock()
	specs = slices.Clone(registry.specs)
	registry.mu.Unlock()
	slices.SortFunc(specs, func(a, b DestinationSpec) int { return strings.Compare(a.Name, b.Name) })
	return
}

// prefix is one parsed entry of Destination.Prefixes.
type prefix struct {
	scheme, host, path string
}

func parsePrefix(raw string) (p prefix, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return p, eb.Build().Str("prefix", raw).Errorf("httpegress: unparsable prefix: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return p, eb.Build().Str("prefix", raw).Errorf("httpegress: prefix scheme must be http or https")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return p, eb.Build().Str("prefix", raw).Errorf("httpegress: a prefix is scheme, host and path, nothing else")
	}
	if hasDotSegment(u.Path) {
		return p, eb.Build().Str("prefix", raw).Errorf("httpegress: prefix path holds a dot segment")
	}
	p = prefix{scheme: u.Scheme, host: strings.ToLower(u.Host), path: u.Path}
	if p.path == "" {
		p.path = "/"
	}
	return
}

// hasDotSegment says a decoded path holds "." or ".." as a segment.
func hasDotSegment(path string) (yes bool) {
	for seg := range strings.SplitSeq(path, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// matchURL says whether raw lies under one of prefixes. The URL is parsed,
// not compared as text: userinfo and dot segments are refused outright
// (both are ways to make a URL read as one place and go to another), and
// scheme and host compare exactly.
func matchURL(prefixes []prefix, raw string) (u *url.URL, err error) {
	u, err = url.Parse(raw)
	if err != nil {
		return nil, eh.Errorf("unparsable url: %w", err)
	}
	if u.User != nil {
		return nil, eh.Errorf("a url with userinfo is refused")
	}
	if u.Opaque != "" || u.Host == "" {
		return nil, eh.Errorf("not an absolute http url")
	}
	if hasDotSegment(u.Path) {
		return nil, eh.Errorf("a url with a dot segment is refused")
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	host := strings.ToLower(u.Host)
	for _, p := range prefixes {
		if u.Scheme == p.scheme && host == p.host && strings.HasPrefix(path, p.path) {
			return u, nil
		}
	}
	return nil, eh.Errorf("the url lies outside the destination")
}

// localPrefixes says every prefix's host is loopback, by loopback.IsHost —
// the sensitivity wall's "local" (ADR-0262 §SD4).
func localPrefixes(prefixes []prefix) (yes bool) {
	if len(prefixes) == 0 {
		return false
	}
	for _, p := range prefixes {
		h := p.host
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		if !loopback.IsHost(h) {
			return false
		}
	}
	return true
}

// TilePrefixes derives a destination's prefixes from an XYZ tile template:
// each {s} subdomain expanded, the rest cut at the first remaining
// placeholder or at the query, whichever comes first. "https://{s}.tile.example/{z}/{x}/{y}.png" with a, b, c is
// three prefixes, "https://a.tile.example/" and its siblings.
func TilePrefixes(template string, subdomains []string) (prefixes []string, err error) {
	template = strings.TrimSpace(template)
	subs := []string{""}
	if strings.Contains(template, "{s}") {
		if len(subdomains) == 0 {
			return nil, eb.Build().Str("template", template).Errorf("httpegress: template uses {s} but no subdomains are given")
		}
		subs = subdomains
	}
	for _, s := range subs {
		t := strings.ReplaceAll(template, "{s}", s)
		if i := strings.IndexAny(t, "{?"); i >= 0 {
			t = t[:i]
		}
		if !slices.Contains(prefixes, t) {
			prefixes = append(prefixes, t)
		}
	}
	for _, p := range prefixes {
		if _, err = parsePrefix(p); err != nil {
			return nil, err
		}
	}
	return
}
