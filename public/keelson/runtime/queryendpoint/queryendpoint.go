// Package queryendpoint lists the SQL query endpoints this OS process
// serves to its own apps: loopback HTTP interfaces in ClickHouse's dialect,
// each under the name a document or a menu names it by. A server publishes
// its entry once it listens and retracts it when it stops; a co-resident
// window looks an endpoint up by name, or lists them all (ADR-0377).
//
// The target the environment names is not an entry: NameDefault is reserved
// for it.
package queryendpoint

import (
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// NameDefault names the target the environment configures. It is
	// reserved: no endpoint publishes under it.
	NameDefault = "default"
	// NameIntrospection is the name the keelson introspection plane
	// publishes its `/query` endpoint under.
	NameIntrospection = "introspection"
)

var (
	// ErrName: the name is empty or holds something other than lowercase
	// letters, digits and hyphens.
	ErrName = eh.New("queryendpoint: a name is lowercase letters, digits and hyphens")
	// ErrReserved: the name is NameDefault.
	ErrReserved = eh.New("queryendpoint: the name is reserved for the environment's target")
	// ErrTaken: a live endpoint already has the name.
	ErrTaken = eh.New("queryendpoint: the name is already published")
	// ErrURL: the URL is not an http URL on loopback.
	ErrURL = eh.New("queryendpoint: an endpoint is an http URL on loopback")
)

// Endpoint is one query endpoint the process serves.
type Endpoint struct {
	// Name is what a document names the endpoint by.
	Name string
	// Display is what a menu shows; empty shows Name.
	Display string
	// Summary says in a clause what the endpoint answers.
	Summary string
	// URL is where statements are posted.
	URL string
	// User and Password are the credential a client in this process
	// presents, as ClickHouse's HTTP interface takes one; both empty when
	// the endpoint asks for none. A password is a secret minted for the
	// process: it is not logged or stored.
	User     string
	Password string
}

// Label is the endpoint's display name, or its name when it has none.
func (inst Endpoint) Label() (label string) {
	if inst.Display != "" {
		return inst.Display
	}
	return inst.Name
}

var (
	mu        sync.RWMutex
	endpoints = map[string]Endpoint{}
)

// Publish lists e until the returned retract is called. A name that is
// malformed, reserved or already listed, and a URL that is not http on
// loopback, are refused.
func Publish(e Endpoint) (retract func(), err error) {
	if !validName(e.Name) {
		return nil, eb.Build().Str("name", e.Name).Errorf("publish: %w", ErrName)
	}
	if e.Name == NameDefault {
		return nil, eb.Build().Str("name", e.Name).Errorf("publish: %w", ErrReserved)
	}
	u, perr := url.Parse(e.URL)
	if perr != nil || u.Scheme != "http" || !loopback.IsHost(u.Hostname()) {
		return nil, eb.Build().Str("name", e.Name).Errorf("publish: %w", ErrURL)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, taken := endpoints[e.Name]; taken {
		return nil, eb.Build().Str("name", e.Name).Errorf("publish: %w", ErrTaken)
	}
	endpoints[e.Name] = e
	var once sync.Once
	retract = func() {
		once.Do(func() {
			mu.Lock()
			delete(endpoints, e.Name)
			mu.Unlock()
		})
	}
	return retract, nil
}

// Lookup finds a listed endpoint by name.
func Lookup(name string) (e Endpoint, ok bool) {
	mu.RLock()
	e, ok = endpoints[name]
	mu.RUnlock()
	return
}

// ByURL finds the listed endpoint statements for target are posted to, so
// a client that holds only a URL can learn the credential that goes with
// it.
func ByURL(target string) (e Endpoint, ok bool) {
	if target == "" {
		return
	}
	mu.RLock()
	defer mu.RUnlock()
	for _, c := range endpoints {
		if c.URL == target {
			return c, true
		}
	}
	return
}

// All lists the endpoints, by name.
func All() (out []Endpoint) {
	mu.RLock()
	out = make([]Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		out = append(out, e)
	}
	mu.RUnlock()
	slices.SortFunc(out, func(a, b Endpoint) int { return strings.Compare(a.Name, b.Name) })
	return
}

// validName reports whether name is lowercase letters, digits and hyphens.
func validName(name string) (ok bool) {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
