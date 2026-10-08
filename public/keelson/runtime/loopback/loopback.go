// Package loopback answers whether a host names this machine's loopback
// interface — the one predicate behind the loopback-contained surfaces:
// the introspecthttp and queryrunsd bind gates (ADR-0082 §SD1), the llm
// service's local endpoint, and httpegress's local destinations
// (ADR-0262 §SD4).
//
// The answer is literal, never resolved: "localhost" in any case, or an IP
// literal that is loopback. A name that resolves to loopback through a
// hosts file is not a fact this process can vouch for. An empty host is
// not loopback: a listen address of ":port" binds every interface.
package loopback

import (
	"net"
	"strings"
)

// IsHost says host is a loopback host. It takes a bare host, as
// net.SplitHostPort or url.URL.Hostname return it; brackets around an IPv6
// literal are tolerated.
func IsHost(host string) (yes bool) {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
